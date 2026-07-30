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
	"errors"
	"time"

	"github.com/alexandrevilain/controller-tools/pkg/patch"
	"github.com/alexandrevilain/temporal-operator/internal/discovery"
	certmanagerapiutil "github.com/cert-manager/cert-manager/pkg/api/util"
	certmanagermeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/alexandrevilain/temporal-operator/api/v1beta1"
	"github.com/alexandrevilain/temporal-operator/internal/resource/mtls/certmanager"
	"github.com/alexandrevilain/temporal-operator/internal/targetcluster"
	"github.com/alexandrevilain/temporal-operator/pkg/kubernetes"
	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
)

// TemporalClusterClientReconciler reconciles a ClusterClient object.
type TemporalClusterClientReconciler struct {
	Base

	// AvailableAPIs describes the cluster the operator watches, which is where the certificates of
	// clusters using the local target are issued.
	AvailableAPIs *discovery.AvailableAPIs
	Resolver      *targetcluster.Resolver
}

var (
	clusterRefNameField      = "spec.clusterRef.name"
	clusterRefNamespaceField = "spec.clusterRef.namespace"
)

//+kubebuilder:rbac:groups=temporal.io,resources=temporalclusterclients,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=temporal.io,resources=temporalclusterclients/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=temporal.io,resources=temporalclusterclients/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *TemporalClusterClientReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	logger := log.FromContext(ctx)

	logger.Info("Starting reconciliation")

	clusterClient := &v1beta1.TemporalClusterClient{}
	err := r.Get(ctx, req.NamespacedName, clusterClient)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	// Check if the resource has been marked for deletion
	if !clusterClient.ObjectMeta.DeletionTimestamp.IsZero() {
		return reconcile.Result{}, nil
	}

	patchHelper, err := patch.NewHelper(clusterClient, r.Client)
	if err != nil {
		return reconcile.Result{}, err
	}

	defer func() {
		// Always attempt to Patch the ClusterClient object and status after each reconciliation.
		err := patchHelper.Patch(ctx, clusterClient)
		if err != nil {
			reterr = kerrors.NewAggregate([]error{reterr, err})
		}
	}()

	// Get referenced cluster.
	cluster := &v1beta1.TemporalCluster{}
	err = r.Get(ctx, clusterClient.Spec.ClusterRef.NamespacedName(clusterClient), cluster)
	if err != nil {
		return reconcile.Result{}, err
	}

	if !cluster.IsReady() {
		logger.Info("Skipping cluster client reconciliation until referenced cluster is ready")

		return reconcile.Result{RequeueAfter: 10 * time.Second}, nil
	}

	if !(cluster.MTLSWithCertManagerEnabled() && cluster.Spec.MTLS.FrontendEnabled()) {
		return reconcile.Result{Requeue: false}, errors.New("mTLS for frontend not enabled using cert-manager for the cluster, can't create a client")
	}

	clusterClient.Status.ServerName = cluster.Spec.MTLS.Frontend.ServerName(cluster)
	if clusterClient.Status.SecretRef == nil {
		clusterClient.Status.SecretRef = &corev1.LocalObjectReference{
			Name: "",
		}
	}

	// The certificate has to be issued where the cluster's CA issuer is, which is the cluster the
	// temporal deployment runs in rather than necessarily this one.
	target, err := r.Resolver.For(ctx, r.Client, cluster.Spec.TargetClusterRef, cluster.GetNamespace())
	if err != nil {
		return reconcile.Result{RequeueAfter: 30 * time.Second}, err
	}

	builder := certmanager.NewGenericFrontendClientCertificateBuilder(cluster, r.Scheme, clusterClient.GetName())
	certificateObject := builder.Build()

	_, err = controllerutil.CreateOrUpdate(ctx, target.Client, certificateObject, func() error {
		if err := builder.Update(certificateObject); err != nil {
			return err
		}

		// The builder owner-references the TemporalCluster, which the target may not have a copy
		// of; label ownership takes over there so the cluster's cleanup still reaches this.
		targetcluster.Claim(target, certificateObject, cluster, temporalClusterKind)

		return nil
	})
	if err != nil {
		return reconcile.Result{}, err
	}

	certificate := certificateObject.(*certmanagerv1.Certificate)

	condition := certmanagerapiutil.GetCertificateCondition(certificate, certmanagerv1.CertificateConditionReady)
	if condition == nil || condition.Status != certmanagermeta.ConditionTrue {
		logger.Info("Waiting for certificate to become ready, requeuing")
		return reconcile.Result{RequeueAfter: 10 * time.Second}, nil
	}

	// A remote target always needs the copy: the secret cert-manager wrote is in the target cluster,
	// and the client asking for it reads from this one.
	if clusterClient.GetNamespace() != cluster.GetNamespace() || !target.IsLocal() {
		originalSecret := client.ObjectKey{Namespace: certificate.GetNamespace(), Name: certificate.Spec.SecretName}
		err = kubernetes.NewSecretCopierFrom(target.Client, r.Client, r.Scheme).Copy(ctx, clusterClient, originalSecret, clusterClient.GetNamespace())
		if err != nil {
			return reconcile.Result{}, err
		}
	}

	clusterClient.Status.SecretRef = &corev1.LocalObjectReference{
		Name: certificate.Spec.SecretName,
	}

	return reconcile.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *TemporalClusterClientReconciler) SetupWithManager(mgr ctrl.Manager) error {
	controller := ctrl.NewControllerManagedBy(mgr).
		For(&v1beta1.TemporalClusterClient{})

	err := mgr.GetFieldIndexer().IndexField(
		context.Background(),
		&v1beta1.TemporalClusterClient{},
		clusterRefNameField,
		func(rawObj client.Object) []string {
			clusterClient := rawObj.(*v1beta1.TemporalClusterClient)
			return []string{clusterClient.Spec.ClusterRef.Name}
		})
	if err != nil {
		return err
	}

	err = mgr.GetFieldIndexer().IndexField(
		context.Background(),
		&v1beta1.TemporalClusterClient{},
		clusterRefNamespaceField,
		func(rawObj client.Object) []string {
			clusterClient := rawObj.(*v1beta1.TemporalClusterClient)
			return []string{clusterClient.Spec.ClusterRef.Namespace}
		})
	if err != nil {
		return err
	}

	// These watches only cover the cluster the operator watches. A certificate issued in a target
	// cluster is picked up by the requeue below instead, which is why the not-ready branch above
	// requeues rather than waiting for an event.
	if r.AvailableAPIs.CertManager {
		controller = controller.
			Watches(
				&certmanagerv1.Certificate{},
				handler.EnqueueRequestsFromMapFunc(
					EnqueueRequestForClusterClientReferencingOwnerCluster(r.Client),
				)).
			Watches(
				&certmanagerv1.Issuer{},
				handler.EnqueueRequestsFromMapFunc(
					EnqueueRequestForClusterClientReferencingOwnerCluster(r.Client),
				))
	}

	controller.Owns(&corev1.Secret{})

	return controller.Complete(r)
}

// EnqueueRequestForClusterClientReferencingOwnerCluster returns a reconcile request for any TemporalClusterClient
// referencing the given the TemporalCluster owner for the provided object.
func EnqueueRequestForClusterClientReferencingOwnerCluster(c client.Client) handler.MapFunc {
	return func(ctx context.Context, object client.Object) []reconcile.Request {
		log := log.FromContext(ctx)
		result := []reconcile.Request{}

		// from the object get the owner cluster.
		// then get the cluster client from the cluster.
		for _, owner := range object.GetOwnerReferences() {
			if owner.Kind != v1beta1.TemporalClusterTypeMeta.Kind {
				continue
			}
			list := &v1beta1.TemporalClusterClientList{}
			err := c.List(ctx,
				list,
				client.MatchingFields{
					clusterRefNameField: owner.Name,
				},
				client.MatchingFields{
					clusterRefNamespaceField: object.GetNamespace(),
				},
			)
			if err != nil {
				log.Error(err, "Failed to get TemporalClusterClient referencing owner TemporalCluster, skipping mapping.")
				return nil
			}

			for _, client := range list.Items {
				result = append(result, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name:      client.GetName(),
						Namespace: client.GetNamespace(),
					},
				})
			}
		}

		return result
	}
}
