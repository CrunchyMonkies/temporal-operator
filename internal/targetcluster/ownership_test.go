// Licensed to Alexandre VILAIN under one or more contributor
// license agreements. See the NOTICE file distributed with
// this work for additional information regarding copyright
// ownership. Alexandre VILAIN licenses this file to you under
// the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package targetcluster

import (
	"context"
	"fmt"
	"testing"

	"github.com/alexandrevilain/controller-tools/pkg/resource"
	internaldiscovery "github.com/alexandrevilain/temporal-operator/internal/discovery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// ownerCluster stands in for a custom resource whose resources are created in another cluster. Its
// own kind doesn't matter here — only its name, namespace and UID end up on those resources.
var ownerCluster = &corev1.ConfigMap{
	ObjectMeta: metav1.ObjectMeta{Name: "prod", Namespace: "demo", UID: types.UID("cluster-uid")},
}

// configMapBuilder is a minimal resource.Builder that behaves like the real ones in the respect
// that matters here: it sets an owner reference on everything it produces.
type configMapBuilder struct{}

func (b configMapBuilder) Build() client.Object {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "prod-config",
			Namespace: "demo",
			Labels:    map[string]string{"app.kubernetes.io/name": "temporal"},
		},
	}
}

func (b configMapBuilder) Enabled() bool { return true }

func (b configMapBuilder) Update(obj client.Object) error {
	obj.SetOwnerReferences([]metav1.OwnerReference{{
		APIVersion: "temporal.io/v1beta1",
		Kind:       "TemporalCluster",
		Name:       ownerCluster.Name,
		UID:        ownerCluster.UID,
	}})

	return nil
}

// comparingConfigMapBuilder also provides a comparison function, like builders that need one for
// fields the api server defaults.
type comparingConfigMapBuilder struct {
	configMapBuilder
}

func (b comparingConfigMapBuilder) Equal() {}

func TestDecorateBuildersLeavesLocalTargetAlone(t *testing.T) {
	builders := []resource.Builder{configMapBuilder{}}

	decorated := DecorateBuilders(builders, localTarget(), ownerCluster, "TemporalCluster")

	// Same-cluster reconciliation has to keep using real owner references, so the builders must
	// come back untouched rather than merely equivalent.
	require.Len(t, decorated, 1)
	assert.Equal(t, builders[0], decorated[0])

	obj := decorated[0].Build()
	require.NoError(t, decorated[0].Update(obj))
	assert.Len(t, obj.GetOwnerReferences(), 1)
	assert.NotContains(t, obj.GetLabels(), OwnerNameLabel)
}

func TestDecorateBuildersClaimsByLabelInRemoteTarget(t *testing.T) {
	decorated := DecorateBuilders([]resource.Builder{configMapBuilder{}}, remoteTarget(), ownerCluster, "TemporalCluster")
	require.Len(t, decorated, 1)

	obj := decorated[0].Build()
	require.NoError(t, decorated[0].Update(obj))

	// The owner reference the builder set names a resource this cluster has never heard of. Left in
	// place, the target's garbage collector deletes the resource as orphaned.
	assert.Empty(t, obj.GetOwnerReferences())

	assert.Equal(t, map[string]string{
		"app.kubernetes.io/name": "temporal",
		OwnerNameLabel:           "prod",
		OwnerNamespaceLabel:      "demo",
		OwnerKindLabel:           "TemporalCluster",
		OwnerUIDLabel:            "cluster-uid",
	}, obj.GetLabels())

	assert.True(t, decorated[0].Enabled())
}

// TestDecorateBuildersClaimsExistingObject covers the update path, where Update is handed the
// object as it exists in the target cluster rather than a freshly built one.
func TestDecorateBuildersClaimsExistingObject(t *testing.T) {
	decorated := DecorateBuilders([]resource.Builder{configMapBuilder{}}, remoteTarget(), ownerCluster, "TemporalCluster")

	existing := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "prod-config", Namespace: "demo"},
	}
	require.NoError(t, decorated[0].Update(existing))

	assert.Empty(t, existing.GetOwnerReferences())
	assert.Equal(t, "prod", existing.GetLabels()[OwnerNameLabel])
}

// TestDecorateBuildersPreservesComparer guards the optional interface: the reconciler only uses a
// builder's comparison function if the builder still satisfies resource.Comparer after decoration.
func TestDecorateBuildersPreservesComparer(t *testing.T) {
	target := remoteTarget()

	plain := DecorateBuilders([]resource.Builder{configMapBuilder{}}, target, ownerCluster, "TemporalCluster")[0]
	_, isComparer := plain.(resource.Comparer)
	assert.False(t, isComparer, "a builder without a comparison function must not gain one")

	comparing := DecorateBuilders([]resource.Builder{comparingConfigMapBuilder{}}, target, ownerCluster, "TemporalCluster")[0]
	_, isComparer = comparing.(resource.Comparer)
	assert.True(t, isComparer, "a builder with a comparison function must keep it")
}

func TestOwnerLabelsRoundTrip(t *testing.T) {
	name, ok := OwnerFromLabels(OwnerLabels(ownerCluster, "TemporalCluster"), "TemporalCluster")
	assert.True(t, ok)
	assert.Equal(t, types.NamespacedName{Namespace: "demo", Name: "prod"}, name)
}

func TestOwnerFromLabels(t *testing.T) {
	tests := map[string]map[string]string{
		"no labels at all":        nil,
		"another kind's resource": OwnerLabels(ownerCluster, "TemporalWorkerProcess"),
		"missing name": {
			OwnerKindLabel:      "TemporalCluster",
			OwnerNamespaceLabel: "demo",
		},
		"missing namespace": {
			OwnerKindLabel: "TemporalCluster",
			OwnerNameLabel: "prod",
		},
	}

	for name, labels := range tests {
		t.Run(name, func(t *testing.T) {
			// Anything the operator didn't create must map to no custom resource, so unrelated
			// changes in the target cluster don't enqueue phantom reconciles.
			_, ok := OwnerFromLabels(labels, "TemporalCluster")
			assert.False(t, ok)
		})
	}
}

func TestCleanupDeletesLabelledResourcesOnly(t *testing.T) {
	owned := configMap("prod-config", "demo", OwnerLabels(ownerCluster, "TemporalCluster"))
	otherOwner := configMap("staging-config", "demo", OwnerLabels(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "staging", Namespace: "demo", UID: "other-uid"},
	}, "TemporalCluster"))
	unmanaged := configMap("someone-elses", "demo", nil)
	otherNamespace := configMap("prod-config", "elsewhere", OwnerLabels(ownerCluster, "TemporalCluster"))

	fakeClient := fake.NewClientBuilder().WithScheme(testScheme(t)).
		WithObjects(owned, otherOwner, unmanaged, otherNamespace).Build()

	target := remoteTarget()
	target.Client = fakeClient

	require.NoError(t, Cleanup(context.Background(), target, ownerCluster, "TemporalCluster"))

	// Only the resources belonging to this custom resource, in its namespace, may go.
	assert.ElementsMatch(t, []string{
		"demo/staging-config",
		"demo/someone-elses",
		"elsewhere/prod-config",
	}, remainingConfigMaps(t, fakeClient))
}

// TestCleanupSkipsLocalTarget records that the local path is left to the garbage collector: owner
// references are real there, and deleting by label as well would only race with it.
func TestCleanupSkipsLocalTarget(t *testing.T) {
	owned := configMap("prod-config", "demo", OwnerLabels(ownerCluster, "TemporalCluster"))
	fakeClient := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(owned).Build()

	target := localTarget()
	target.Client = fakeClient

	require.NoError(t, Cleanup(context.Background(), target, ownerCluster, "TemporalCluster"))

	assert.Equal(t, []string{"demo/prod-config"}, remainingConfigMaps(t, fakeClient))
}

func TestManagedKindsFollowAvailableAPIs(t *testing.T) {
	target := remoteTarget()

	kinds := typeNames(ManagedKinds(target))
	assert.Contains(t, kinds, "*v1.Deployment")
	// Asking a target for a kind it doesn't serve is an error rather than an empty result, so
	// optional APIs must only be included when discovery found them.
	assert.NotContains(t, kinds, "*v1.Certificate")

	target.AvailableAPIs = &internaldiscovery.AvailableAPIs{
		CertManager: true, Istio: true, PrometheusOperator: true,
	}

	kinds = typeNames(ManagedKinds(target))
	assert.Contains(t, kinds, "*v1.Certificate")
	assert.Contains(t, kinds, "*v1beta1.PeerAuthentication")
	assert.Contains(t, kinds, "*v1.ServiceMonitor")
}

func typeNames(objs []client.Object) []string {
	names := make([]string, 0, len(objs))
	for _, obj := range objs {
		names = append(names, fmt.Sprintf("%T", obj))
	}

	return names
}

func remainingConfigMaps(t *testing.T, c client.Client) []string {
	t.Helper()

	list := &corev1.ConfigMapList{}
	require.NoError(t, c.List(context.Background(), list))

	names := make([]string, 0, len(list.Items))
	for _, item := range list.Items {
		names = append(names, item.Namespace+"/"+item.Name)
	}

	return names
}

func configMap(name, namespace string, labels map[string]string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: labels},
	}
}
