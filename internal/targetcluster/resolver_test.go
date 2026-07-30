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

// The tests live inside the package rather than beside it so they can build a Target directly.
// Whether a target is local is decided once, by NewLocalTarget, and is not settable from outside
// on purpose: it is what governs whether resources may carry owner references.
package targetcluster

import (
	"context"
	"testing"

	"github.com/alexandrevilain/temporal-operator/api/v1beta1"
	internaldiscovery "github.com/alexandrevilain/temporal-operator/internal/discovery"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const kubeconfig = `apiVersion: v1
kind: Config
current-context: default
clusters:
- name: default
  cluster:
    server: https://default.example.com:6443
- name: other
  cluster:
    server: https://other.example.com:6443
contexts:
- name: default
  context:
    cluster: default
    user: default
- name: other
  context:
    cluster: other
    user: default
users:
- name: default
  user:
    token: token
`

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, v1beta1.AddToScheme(scheme))

	return scheme
}

// localTarget is the target custom resources without a reference resolve to.
func localTarget() *Target {
	return &Target{local: true, AvailableAPIs: &internaldiscovery.AvailableAPIs{}}
}

// remoteTarget is a target in another cluster, where owner references can't be used.
func remoteTarget() *Target {
	return &Target{
		Name:          types.NamespacedName{Namespace: "demo", Name: "remote"},
		AvailableAPIs: &internaldiscovery.AvailableAPIs{},
	}
}

func newResolver(t *testing.T, objects ...client.Object) (*Resolver, client.Client) {
	t.Helper()

	scheme := testScheme(t)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()

	return NewResolver(localTarget(), scheme, logr.Discard()), fakeClient
}

// TestForWithoutReference is the backwards compatibility guarantee: a custom resource that names no
// target cluster — which is every one written before this feature existed — must resolve to the
// cluster the operator watches.
func TestForWithoutReference(t *testing.T) {
	resolver, fakeClient := newResolver(t)

	for name, ref := range map[string]*v1beta1.ObjectReference{
		"nil reference":            nil,
		"reference without a name": {Namespace: "demo"},
	} {
		t.Run(name, func(t *testing.T) {
			target, err := resolver.For(context.Background(), fakeClient, ref, "demo")
			require.NoError(t, err)
			assert.Same(t, resolver.Local(), target)
			assert.True(t, target.IsLocal())
		})
	}
}

func TestForUnusableReference(t *testing.T) {
	tests := map[string]struct {
		objects []client.Object
		ref     *v1beta1.ObjectReference
		message string
	}{
		"target cluster does not exist": {
			ref:     &v1beta1.ObjectReference{Name: "remote"},
			message: "demo/remote does not exist",
		},
		"kubeconfig secret does not exist": {
			objects: []client.Object{targetClusterResource("remote", "demo", "creds")},
			ref:     &v1beta1.ObjectReference{Name: "remote"},
			message: "kubeconfig secret demo/creds does not exist",
		},
		"kubeconfig secret has no such key": {
			objects: []client.Object{
				targetClusterResource("remote", "demo", "creds"),
				secret("creds", "demo", map[string][]byte{"something-else": []byte("x")}),
			},
			ref:     &v1beta1.ObjectReference{Name: "remote"},
			message: `kubeconfig secret demo/creds has no key "kubeconfig"`,
		},
		"kubeconfig is not parseable": {
			objects: []client.Object{
				targetClusterResource("remote", "demo", "creds"),
				secret("creds", "demo", map[string][]byte{"kubeconfig": []byte("not a kubeconfig")}),
			},
			ref:     &v1beta1.ObjectReference{Name: "remote"},
			message: "has an unusable kubeconfig",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			resolver, fakeClient := newResolver(t, test.objects...)

			target, err := resolver.For(context.Background(), fakeClient, test.ref, "demo")
			assert.Nil(t, target)
			require.Error(t, err)
			// Every one of these is a condition the user can fix without editing the referencing
			// resource, so it has to come back as retryable rather than terminal.
			assert.ErrorIs(t, err, ErrTargetClusterNotReady)
			assert.Contains(t, err.Error(), test.message)
		})
	}
}

// TestForUsesReferenceNamespace covers a target cluster referenced from another namespace: the
// reference decides where the TemporalTargetCluster is looked up, and the kubeconfig Secret is
// read from that same namespace rather than the referencing resource's.
func TestForUsesReferenceNamespace(t *testing.T) {
	resolver, fakeClient := newResolver(t,
		targetClusterResource("remote", "operators", "creds"),
	)

	_, err := resolver.For(context.Background(), fakeClient,
		&v1beta1.ObjectReference{Name: "remote", Namespace: "operators"}, "demo")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "kubeconfig secret operators/creds does not exist")
}

func TestRestConfigFromKubeconfig(t *testing.T) {
	t.Run("uses the current context by default", func(t *testing.T) {
		restConfig, err := RestConfigFromKubeconfig([]byte(kubeconfig), "")
		require.NoError(t, err)
		assert.Equal(t, "https://default.example.com:6443", restConfig.Host)
	})

	t.Run("uses the named context when given one", func(t *testing.T) {
		restConfig, err := RestConfigFromKubeconfig([]byte(kubeconfig), "other")
		require.NoError(t, err)
		assert.Equal(t, "https://other.example.com:6443", restConfig.Host)
	})

	// Without this check the context name is silently ignored and the operator manages whichever
	// cluster the kubeconfig happens to point at — the worst failure mode available here.
	t.Run("rejects a context the kubeconfig doesn't have", func(t *testing.T) {
		_, err := RestConfigFromKubeconfig([]byte(kubeconfig), "missing")
		require.Error(t, err)
		assert.Contains(t, err.Error(), `no context named "missing"`)
	})

	t.Run("rejects an unparseable kubeconfig", func(t *testing.T) {
		_, err := RestConfigFromKubeconfig([]byte("}"), "")
		require.Error(t, err)
	})
}

func TestForgetUnknownTarget(t *testing.T) {
	resolver, _ := newResolver(t)

	// Forget runs on every target cluster deletion, including ones never connected to.
	resolver.Forget(types.NamespacedName{Namespace: "demo", Name: "remote"})
}

// TestForgetDropsWatchRegistrations covers what happens when credentials are rotated or a target
// cluster is deleted: the informers backing drift detection go away with the cache, so the
// registrations have to be forgotten too or the replacement cache is never watched.
func TestForgetDropsWatchRegistrations(t *testing.T) {
	resolver, _ := newResolver(t)

	name := types.NamespacedName{Namespace: "demo", Name: "remote"}
	target := remoteTarget()

	resolver.mu.Lock()
	resolver.targets[name] = target
	resolver.watched[name.String()+"/TemporalCluster"] = struct{}{}
	resolver.watched[types.NamespacedName{Namespace: "demo", Name: "other"}.String()+"/TemporalCluster"] = struct{}{}
	resolver.mu.Unlock()

	resolver.Forget(name)

	resolver.mu.Lock()
	defer resolver.mu.Unlock()
	assert.NotContains(t, resolver.targets, name)
	assert.NotContains(t, resolver.watched, "demo/remote/TemporalCluster")
	assert.Contains(t, resolver.watched, "demo/other/TemporalCluster")
}

func targetClusterResource(name, namespace, secretName string) *v1beta1.TemporalTargetCluster {
	return &v1beta1.TemporalTargetCluster{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: v1beta1.TemporalTargetClusterSpec{
			KubeconfigSecretRef: corev1.LocalObjectReference{Name: secretName},
		},
	}
}

func secret(name, namespace string, data map[string][]byte) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, ResourceVersion: "1"},
		Data:       data,
	}
}
