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

package controllers

import (
	"context"
	"time"

	"github.com/alexandrevilain/controller-tools/pkg/patch"
	"github.com/alexandrevilain/temporal-operator/api/v1beta1"
	"github.com/alexandrevilain/temporal-operator/internal/targetcluster"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	k8sdiscovery "k8s.io/client-go/discovery"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// targetClusterProbeInterval is how often a reachable target is re-probed. Credentials expire and
// clusters go away without telling anyone, so the Ready condition is worth refreshing even when
// nothing about the resource changed.
const targetClusterProbeInterval = 5 * time.Minute

// kubeconfigSecretField indexes target clusters by the name of the Secret holding their
// kubeconfig, so rotating one re-probes the targets using it.
const kubeconfigSecretField = "spec.kubeconfigSecretRef.name" //nolint:gosec // G101: a field index path, not a credential.

// TemporalTargetClusterReconciler reconciles a TemporalTargetCluster object.
//
// It doesn't create anything. Its job is to tell the user whether the operator can reach the
// cluster the resource describes, which is otherwise only discoverable from the status of whatever
// TemporalCluster happens to reference it.
type TemporalTargetClusterReconciler struct {
	Base

	Resolver *targetcluster.Resolver
}

//+kubebuilder:rbac:groups=temporal.io,resources=temporaltargetclusters,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=temporal.io,resources=temporaltargetclusters/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=temporal.io,resources=temporaltargetclusters/finalizers,verbs=update
//+kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch

// Reconcile probes the described cluster and reports what it found in the resource's status.
func (r *TemporalTargetClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	logger := log.FromContext(ctx)

	targetCluster := &v1beta1.TemporalTargetCluster{}
	err := r.Get(ctx, req.NamespacedName, targetCluster)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Drop the connection so a target cluster recreated under the same name is dialled
			// afresh rather than through credentials that may no longer apply.
			r.Resolver.Forget(req.NamespacedName)

			return reconcile.Result{}, nil
		}

		return reconcile.Result{}, err
	}

	if !targetCluster.ObjectMeta.DeletionTimestamp.IsZero() {
		r.Resolver.Forget(req.NamespacedName)

		return reconcile.Result{}, nil
	}

	patchHelper, err := patch.NewHelper(targetCluster, r.Client)
	if err != nil {
		return reconcile.Result{}, err
	}

	defer func() {
		err := patchHelper.Patch(ctx, targetCluster)
		if err != nil {
			reterr = kerrors.NewAggregate([]error{reterr, err})
		}
	}()

	ref := &v1beta1.ObjectReference{Name: targetCluster.GetName(), Namespace: targetCluster.GetNamespace()}

	target, err := r.Resolver.For(ctx, r.Client, ref, targetCluster.GetNamespace())
	if err != nil {
		logger.Info("Target cluster is not reachable", "reason", err.Error())

		v1beta1.SetTemporalTargetClusterReady(targetCluster, metav1.ConditionFalse,
			v1beta1.TargetClusterUnreachableReason, err.Error())

		// Everything that makes a target unusable — a missing Secret, an unparseable kubeconfig, an
		// api server that isn't answering — is something the user or the network may fix without
		// touching this resource, so keep retrying rather than reporting a terminal error.
		return reconcile.Result{RequeueAfter: 30 * time.Second}, nil
	}

	version, err := serverVersion(target)
	if err != nil {
		v1beta1.SetTemporalTargetClusterReady(targetCluster, metav1.ConditionFalse,
			v1beta1.TargetClusterUnreachableReason, err.Error())

		return reconcile.Result{RequeueAfter: 30 * time.Second}, nil
	}

	targetCluster.Status.ServerVersion = version
	targetCluster.Status.AvailableAPIs = &v1beta1.TargetClusterAvailableAPIs{
		CertManager:        target.AvailableAPIs.CertManager,
		Istio:              target.AvailableAPIs.Istio,
		PrometheusOperator: target.AvailableAPIs.PrometheusOperator,
	}

	v1beta1.SetTemporalTargetClusterReady(targetCluster, metav1.ConditionTrue,
		v1beta1.TargetClusterReachableReason, "Target cluster is reachable")

	return reconcile.Result{RequeueAfter: targetClusterProbeInterval}, nil
}

// serverVersion asks the target which kubernetes version it runs. It doubles as the reachability
// probe: it is the cheapest call that proves the credentials work end to end.
func serverVersion(target *targetcluster.Target) (string, error) {
	discoveryClient, err := k8sdiscovery.NewDiscoveryClientForConfig(target.Config)
	if err != nil {
		return "", err
	}

	info, err := discoveryClient.ServerVersion()
	if err != nil {
		return "", err
	}

	return info.GitVersion, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *TemporalTargetClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	err := mgr.GetFieldIndexer().IndexField(
		context.Background(),
		&v1beta1.TemporalTargetCluster{},
		kubeconfigSecretField,
		func(rawObj client.Object) []string {
			targetCluster := rawObj.(*v1beta1.TemporalTargetCluster)
			return []string{targetCluster.Spec.KubeconfigSecretRef.Name}
		})
	if err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&v1beta1.TemporalTargetCluster{}).
		Watches(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(r.targetClustersUsingSecret),
		).
		Complete(r)
}

// targetClustersUsingSecret finds the target clusters whose credentials live in the given Secret,
// so a rotated kubeconfig is noticed rather than waiting for the next probe.
func (r *TemporalTargetClusterReconciler) targetClustersUsingSecret(ctx context.Context, secret client.Object) []reconcile.Request {
	targetClusters := &v1beta1.TemporalTargetClusterList{}

	err := r.List(ctx, targetClusters,
		client.InNamespace(secret.GetNamespace()),
		client.MatchingFields{kubeconfigSecretField: secret.GetName()},
	)
	if err != nil {
		log.FromContext(ctx).Error(err, "Unable to list target clusters referencing secret",
			"secret", client.ObjectKeyFromObject(secret))

		return nil
	}

	requests := make([]reconcile.Request, 0, len(targetClusters.Items))
	for _, targetCluster := range targetClusters.Items {
		requests = append(requests, reconcile.Request{
			NamespacedName: client.ObjectKeyFromObject(&targetCluster),
		})
	}

	return requests
}
