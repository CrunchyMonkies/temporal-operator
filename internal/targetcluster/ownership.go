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
	"errors"
	"fmt"

	"github.com/alexandrevilain/controller-tools/pkg/resource"
	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	istionetworkingv1beta1 "istio.io/client-go/pkg/apis/networking/v1beta1"
	istiosecurityv1beta1 "istio.io/client-go/pkg/apis/security/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// OwnerNameLabel records the name of the custom resource a target cluster resource was created
	// for.
	OwnerNameLabel = "operator.temporal.io/owner-name"
	// OwnerNamespaceLabel records the namespace of that custom resource.
	OwnerNamespaceLabel = "operator.temporal.io/owner-namespace"
	// OwnerKindLabel records its kind, so two kinds of custom resource sharing a name don't claim
	// each other's resources.
	OwnerKindLabel = "operator.temporal.io/owner-kind"
	// OwnerUIDLabel records its UID, which distinguishes a deleted and recreated custom resource
	// from the original.
	OwnerUIDLabel = "operator.temporal.io/owner-uid"

	// TargetClusterCleanupFinalizer keeps a custom resource around until the resources it owns in a
	// target cluster have been deleted.
	//
	// Nothing in the target cluster can cascade-delete them: an owner reference to a custom
	// resource in another cluster is unresolvable, and the target's garbage collector would treat
	// the resource as orphaned and delete it immediately. So the operator deletes them itself, and
	// this finalizer is what gives it the chance to.
	TargetClusterCleanupFinalizer = "operator.temporal.io/target-cluster-cleanup"
)

// managedKinds are the kinds the operator creates in a target cluster on a custom resource's
// behalf, and therefore the kinds it has to delete by hand when that resource goes away.
//
// certManagerKinds, istioKinds and prometheusKinds are listed separately because their APIs may
// not be served at all; asking for a kind the target doesn't know about is an error, not an empty
// list.
var (
	managedKinds = []client.Object{
		&appsv1.Deployment{},
		&corev1.ConfigMap{},
		&corev1.Service{},
		&corev1.ServiceAccount{},
		&networkingv1.Ingress{},
		&batchv1.Job{},
	}

	certManagerKinds = []client.Object{
		&certmanagerv1.Issuer{},
		&certmanagerv1.Certificate{},
	}

	istioKinds = []client.Object{
		&istiosecurityv1beta1.PeerAuthentication{},
		&istionetworkingv1beta1.DestinationRule{},
	}

	prometheusKinds = []client.Object{
		&monitoringv1.ServiceMonitor{},
	}
)

// ManagedKinds returns the kinds the operator creates in the given target on behalf of a custom
// resource, limited to those the target actually serves.
func ManagedKinds(target *Target) []client.Object {
	kinds := make([]client.Object, 0, len(managedKinds)+len(certManagerKinds)+len(istioKinds)+len(prometheusKinds))
	kinds = append(kinds, managedKinds...)

	if target.AvailableAPIs.CertManager {
		kinds = append(kinds, certManagerKinds...)
	}
	if target.AvailableAPIs.Istio {
		kinds = append(kinds, istioKinds...)
	}
	if target.AvailableAPIs.PrometheusOperator {
		kinds = append(kinds, prometheusKinds...)
	}

	return kinds
}

// OwnerLabels returns the labels identifying owner as the custom resource a target cluster resource
// belongs to. They stand in for the owner reference that can't cross a cluster boundary: they let
// the operator find everything it created for a custom resource, and let a change in the target
// cluster be mapped back to the resource that should react to it.
func OwnerLabels(owner client.Object, kind string) map[string]string {
	return map[string]string{
		OwnerNameLabel:      owner.GetName(),
		OwnerNamespaceLabel: owner.GetNamespace(),
		OwnerKindLabel:      kind,
		OwnerUIDLabel:       string(owner.GetUID()),
	}
}

// OwnerFromLabels reads back the custom resource identified by OwnerLabels. It reports false when
// the labels don't identify one, which is the case for every resource the operator didn't create.
func OwnerFromLabels(labels map[string]string, kind string) (types.NamespacedName, bool) {
	if labels[OwnerKindLabel] != kind {
		return types.NamespacedName{}, false
	}

	name := types.NamespacedName{
		Namespace: labels[OwnerNamespaceLabel],
		Name:      labels[OwnerNameLabel],
	}
	if name.Name == "" || name.Namespace == "" {
		return types.NamespacedName{}, false
	}

	return name, true
}

// DecorateBuilders adapts builders for a target cluster that isn't the one owner lives in.
//
// For a local target it returns builders untouched, so single-cluster reconciliation keeps using
// real owner references and real garbage collection. Otherwise each builder is wrapped to replace
// the owner reference it sets with identity labels, since a reference to an object in another
// cluster is not just useless but actively harmful — see TargetClusterCleanupFinalizer.
func DecorateBuilders(builders []resource.Builder, target *Target, owner client.Object, kind string) []resource.Builder {
	if target.IsLocal() {
		return builders
	}

	labels := OwnerLabels(owner, kind)

	decorated := make([]resource.Builder, 0, len(builders))
	for _, b := range builders {
		decorated = append(decorated, decorate(b, labels))
	}

	return decorated
}

// decorate wraps a single builder, preserving whether it also acts as a resource.Comparer so the
// reconciler's optional-interface check still sees it.
func decorate(builder resource.Builder, labels map[string]string) resource.Builder {
	base := crossClusterBuilder{Builder: builder, labels: labels}

	if comparer, ok := builder.(resource.Comparer); ok {
		return crossClusterComparingBuilder{crossClusterBuilder: base, Comparer: comparer}
	}

	return base
}

// crossClusterBuilder builds the resource its wrapped builder would, then makes it owner-reference
// free and label-owned instead.
type crossClusterBuilder struct {
	resource.Builder

	labels map[string]string
}

func (b crossClusterBuilder) Build() client.Object {
	obj := b.Builder.Build()
	b.claim(obj)

	return obj
}

func (b crossClusterBuilder) Update(obj client.Object) error {
	if err := b.Builder.Update(obj); err != nil {
		return err
	}

	b.claim(obj)

	return nil
}

// claim marks obj as belonging to the custom resource, and removes the owner references the
// wrapped builder set, which point at an object this cluster has never heard of.
func (b crossClusterBuilder) claim(obj client.Object) {
	obj.SetOwnerReferences(nil)

	labels := obj.GetLabels()
	if labels == nil {
		labels = make(map[string]string, len(b.labels))
	}
	for key, value := range b.labels {
		labels[key] = value
	}
	obj.SetLabels(labels)
}

// crossClusterComparingBuilder is crossClusterBuilder for builders that provide their own
// comparison function.
type crossClusterComparingBuilder struct {
	crossClusterBuilder
	resource.Comparer
}

// Cleanup deletes everything the operator created in target on owner's behalf.
//
// It is the replacement for the cascade delete the target cluster's garbage collector can't do,
// and it deliberately deletes by label rather than by builder: a resource whose builder has since
// been disabled, or whose kind the operator no longer creates, still has to go.
func Cleanup(ctx context.Context, target *Target, owner client.Object, kind string) error {
	if target.IsLocal() {
		// Owner references already cover this; deleting by label as well would race with the
		// garbage collector for no benefit.
		return nil
	}

	selector := client.MatchingLabels(OwnerLabels(owner, kind))
	namespace := client.InNamespace(owner.GetNamespace())

	var errs []error
	for _, obj := range ManagedKinds(target) {
		err := target.Client.DeleteAllOf(ctx, obj, namespace, selector)
		if err != nil && !apierrors.IsNotFound(err) && !apimeta.IsNoMatchError(err) {
			errs = append(errs, fmt.Errorf("can't delete %T in %s: %w", obj, target, err))
		}
	}

	return errors.Join(errs...)
}
