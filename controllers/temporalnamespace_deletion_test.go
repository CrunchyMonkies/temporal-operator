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

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.temporal.io/api/operatorservice/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/api/workflowservice/v1"
	temporalclient "go.temporal.io/sdk/client"
	"google.golang.org/grpc"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/alexandrevilain/temporal-operator/api/v1beta1"
	"github.com/alexandrevilain/temporal-operator/internal/targetcluster"
)

// fakeOperatorService answers DeleteNamespace with whatever the test asked for, recording the
// namespaces it was called for.
type fakeOperatorService struct {
	operatorservice.OperatorServiceClient
	deleted []string
	err     error
}

func (s *fakeOperatorService) DeleteNamespace(_ context.Context, req *operatorservice.DeleteNamespaceRequest, _ ...grpc.CallOption) (*operatorservice.DeleteNamespaceResponse, error) {
	s.deleted = append(s.deleted, req.GetNamespace())
	if s.err != nil {
		return nil, s.err
	}

	return &operatorservice.DeleteNamespaceResponse{}, nil
}

// fakeTemporalClient stands in for a frontend connection. Only the calls the deletion path makes
// are implemented; anything else panics on the nil embedded interface, which is the point.
type fakeTemporalClient struct {
	temporalclient.Client
	operator *fakeOperatorService
	closed   bool
}

func (c *fakeTemporalClient) OperatorService() operatorservice.OperatorServiceClient {
	return c.operator
}

func (c *fakeTemporalClient) Close() { c.closed = true }

// fakeNamespaceClient stands in for the namespace client the registration path uses.
type fakeNamespaceClient struct {
	temporalclient.NamespaceClient
	registered []string
}

func (c *fakeNamespaceClient) Register(_ context.Context, req *workflowservice.RegisterNamespaceRequest) error {
	c.registered = append(c.registered, req.GetNamespace())

	return nil
}

func (c *fakeNamespaceClient) Close() {}

// createTestClusterWithReadiness creates a cluster reported ready or not, so tests can put a
// namespace behind the reconciliation's readiness gate.
func createTestClusterWithReadiness(ctx context.Context, name string, ready bool) *v1beta1.TemporalCluster {
	cluster := &v1beta1.TemporalCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: v1beta1.TemporalClusterSpec{
			NumHistoryShards: 1,
			Persistence: v1beta1.TemporalPersistenceSpec{
				DefaultStore:    &v1beta1.DatastoreSpec{},
				VisibilityStore: &v1beta1.DatastoreSpec{},
			},
		},
	}
	Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

	status := metav1.ConditionFalse
	reason := v1beta1.ServicesNotReadyReason
	if ready {
		status = metav1.ConditionTrue
		reason = v1beta1.ServicesReadyReason
	}
	v1beta1.SetTemporalClusterReady(cluster, status, reason, "")
	Expect(k8sClient.Status().Update(ctx, cluster)).To(Succeed())

	return cluster
}

// createTestNamespaceFor creates a deletable namespace pointing at the given cluster.
func createTestNamespaceFor(ctx context.Context, name, clusterName string, annotations map[string]string, finalizers ...string) *v1beta1.TemporalNamespace {
	namespace := &v1beta1.TemporalNamespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   "default",
			Annotations: annotations,
			Finalizers:  finalizers,
		},
		Spec: v1beta1.TemporalNamespaceSpec{
			ClusterRef:      v1beta1.ObjectReference{Name: clusterName},
			RetentionPeriod: &metav1.Duration{Duration: time.Hour},
			AllowDeletion:   true,
		},
	}
	Expect(k8sClient.Create(ctx, namespace)).To(Succeed())

	return namespace
}

// namespaceReconcilerFor builds a reconciler whose temporal clients are the given fakes.
func namespaceReconcilerFor(temporalClient *fakeTemporalClient, namespaceClient *fakeNamespaceClient) *TemporalNamespaceReconciler {
	reconciler := &TemporalNamespaceReconciler{
		Client:    k8sClient,
		APIReader: k8sClient,
		Resolver:  targetcluster.NewResolver(&targetcluster.Target{Client: k8sClient}, k8sClient.Scheme(), logr.Discard()),
	}

	if temporalClient != nil {
		reconciler.clusterClient = func(context.Context, client.Client, *v1beta1.TemporalCluster) (temporalclient.Client, error) {
			return temporalClient, nil
		}
	}
	if namespaceClient != nil {
		reconciler.namespaceClient = func(context.Context, client.Client, *v1beta1.TemporalCluster) (temporalclient.NamespaceClient, error) {
			return namespaceClient, nil
		}
	}

	return reconciler
}

var _ = Describe("Namespace deletion", func() {
	ctx := context.Background()

	// The readiness gate used to sit in front of the deletion path, so a namespace whose cluster
	// was merely unhealthy held its finalizer forever: the ordinary teardown order deletes the
	// namespace and its cluster together.
	It("deletes a namespace whose cluster is not ready", func() {
		createTestClusterWithReadiness(ctx, "delete-unready-cluster", false)

		namespace := createTestNamespaceFor(ctx, "delete-with-unready-cluster", "delete-unready-cluster", nil, deletionFinalizer)
		key := client.ObjectKeyFromObject(namespace)

		deleteInForeground(ctx, namespace)

		temporalClient := &fakeTemporalClient{operator: &fakeOperatorService{}}
		result, err := namespaceReconcilerFor(temporalClient, nil).Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal(reconcile.Result{}))

		Expect(temporalClient.operator.deleted).To(ConsistOf(namespace.GetName()))
		Expect(temporalClient.closed).To(BeTrue())
		Expect(getTestNamespace(ctx, key).Finalizers).To(ConsistOf(metav1.FinalizerDeleteDependents))
	})

	// The namespace is already gone on the server, which is what the deletion asked for: the
	// CRD-side cleanup has to finish rather than error out and retry forever.
	It("treats a namespace already absent on the server as deleted", func() {
		createTestClusterWithReadiness(ctx, "delete-not-found-cluster", true)

		namespace := createTestNamespaceFor(ctx, "delete-already-absent", "delete-not-found-cluster", nil, deletionFinalizer)
		key := client.ObjectKeyFromObject(namespace)

		deleteInForeground(ctx, namespace)

		temporalClient := &fakeTemporalClient{operator: &fakeOperatorService{
			err: serviceerror.NewNamespaceNotFound(namespace.GetName()),
		}}
		result, err := namespaceReconcilerFor(temporalClient, nil).Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal(reconcile.Result{}))

		Expect(getTestNamespace(ctx, key).Finalizers).To(ConsistOf(metav1.FinalizerDeleteDependents))
	})

	It("reports a refused deletion as blocked instead of retrying it", func() {
		createTestClusterWithReadiness(ctx, "delete-refused-cluster", true)

		namespace := createTestNamespaceFor(ctx, "delete-refused", "delete-refused-cluster", nil, deletionFinalizer)
		key := client.ObjectKeyFromObject(namespace)

		deleteInForeground(ctx, namespace)

		temporalClient := &fakeTemporalClient{operator: &fakeOperatorService{
			err: serviceerror.NewPermissionDenied("no delete for you", ""),
		}}
		_, err := namespaceReconcilerFor(temporalClient, nil).Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).To(HaveOccurred())
		Expect(errors.Is(err, reconcile.TerminalError(nil))).To(BeTrue())

		// The finalizer stays: the namespace still exists on the server, and nothing here is
		// allowed to silently drop it.
		deleting := getTestNamespace(ctx, key)
		Expect(deleting.Finalizers).To(ContainElement(deletionFinalizer))

		condition := meta.FindStatusCondition(deleting.Status.Conditions, v1beta1.ReconcileErrorCondition)
		Expect(condition).NotTo(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionTrue))
		Expect(condition.Reason).To(Equal(v1beta1.DeletionBlockedReason))
		Expect(condition.Message).To(ContainSubstring(forceDeleteAnnotation))
	})

	// A frontend that merely failed to answer is a different matter: the deletion is worth
	// retrying, so the error goes back to controller-runtime rather than latching a blocked state.
	It("retries a deletion the server failed to carry out", func() {
		createTestClusterWithReadiness(ctx, "delete-unavailable-cluster", true)

		namespace := createTestNamespaceFor(ctx, "delete-unavailable", "delete-unavailable-cluster", nil, deletionFinalizer)
		key := client.ObjectKeyFromObject(namespace)

		deleteInForeground(ctx, namespace)

		temporalClient := &fakeTemporalClient{operator: &fakeOperatorService{
			err: serviceerror.NewUnavailable("frontend is restarting"),
		}}
		_, err := namespaceReconcilerFor(temporalClient, nil).Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).To(HaveOccurred())
		Expect(errors.Is(err, reconcile.TerminalError(nil))).To(BeFalse())

		deleting := getTestNamespace(ctx, key)
		Expect(deleting.Finalizers).To(ContainElement(deletionFinalizer))
		Expect(meta.FindStatusCondition(deleting.Status.Conditions, v1beta1.ReconcileErrorCondition).Reason).To(Equal(v1beta1.ReconcileErrorReason))
	})

	// The escape hatch for a cluster that is gone for good: no client is built at all.
	It("drops the finalizer without dialling when force deletion is requested", func() {
		createTestClusterWithReadiness(ctx, "force-delete-cluster", false)

		annotations := map[string]string{forceDeleteAnnotation: "true"}
		namespace := createTestNamespaceFor(ctx, "force-delete", "force-delete-cluster", annotations, deletionFinalizer)
		key := client.ObjectKeyFromObject(namespace)

		deleteInForeground(ctx, namespace)

		reconciler := namespaceReconcilerFor(nil, nil)
		reconciler.clusterClient = func(context.Context, client.Client, *v1beta1.TemporalCluster) (temporalclient.Client, error) {
			Fail("the force-delete path must not dial the frontend")

			return nil, nil
		}

		result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal(reconcile.Result{}))

		Expect(getTestNamespace(ctx, key).Finalizers).To(ConsistOf(metav1.FinalizerDeleteDependents))
	})

	// The whole lifecycle, against the same name twice: a deletion that leaves anything behind on
	// the object makes the second creation unreconcilable.
	It("supports create, delete then re-create of the same namespace", func() {
		createTestClusterWithReadiness(ctx, "lifecycle-cluster", true)

		temporalClient := &fakeTemporalClient{operator: &fakeOperatorService{}}
		namespaceClient := &fakeNamespaceClient{}
		reconciler := namespaceReconcilerFor(temporalClient, namespaceClient)

		namespace := createTestNamespaceFor(ctx, "lifecycle", "lifecycle-cluster", nil)
		key := client.ObjectKeyFromObject(namespace)

		By("creating it")
		_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(namespaceClient.registered).To(ConsistOf("lifecycle"))

		created := getTestNamespace(ctx, key)
		Expect(controllerutil.ContainsFinalizer(created, deletionFinalizer)).To(BeTrue())
		Expect(meta.IsStatusConditionTrue(created.Status.Conditions, v1beta1.ReadyCondition)).To(BeTrue())

		By("deleting it")
		deleteInForeground(ctx, created)

		_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(temporalClient.operator.deleted).To(ConsistOf("lifecycle"))

		// envtest runs no garbage collector, so foregroundDeletion is dropped by hand here; the
		// deletion finalizer, the one this controller owns, is already gone.
		Expect(getTestNamespace(ctx, key).Finalizers).To(ConsistOf(metav1.FinalizerDeleteDependents))
		runGarbageCollector(ctx, key)
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, key, &v1beta1.TemporalNamespace{}))).To(BeTrue())

		By("re-creating it under the same name")
		recreated := createTestNamespaceFor(ctx, "lifecycle", "lifecycle-cluster", nil)

		_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(recreated)})
		Expect(err).NotTo(HaveOccurred())
		Expect(namespaceClient.registered).To(ConsistOf("lifecycle", "lifecycle"))

		reconciled := getTestNamespace(ctx, key)
		Expect(reconciled.DeletionTimestamp.IsZero()).To(BeTrue())
		Expect(controllerutil.ContainsFinalizer(reconciled, deletionFinalizer)).To(BeTrue())
		Expect(meta.IsStatusConditionTrue(reconciled.Status.Conditions, v1beta1.ReadyCondition)).To(BeTrue())
	})
})

var _ = Describe("Refused deletion classification", func() {
	It("counts the answers no retry can change", func() {
		Expect(deletionRefused(serviceerror.NewPermissionDenied("denied", ""))).To(BeTrue())
		Expect(deletionRefused(serviceerror.NewInvalidArgument("bad request"))).To(BeTrue())
		Expect(deletionRefused(serviceerror.NewUnimplemented("no such method"))).To(BeTrue())
	})

	It("leaves everything else to the ordinary retry", func() {
		Expect(deletionRefused(serviceerror.NewUnavailable("frontend is restarting"))).To(BeFalse())
		Expect(deletionRefused(serviceerror.NewNamespaceNotFound("gone"))).To(BeFalse())
		Expect(deletionRefused(errors.New("boom"))).To(BeFalse())
	})
})

// A finalizer added back onto an object the API server has already marked for deletion is rejected
// outright, which is the failure the upstream report shows.
var _ = Describe("Finalizers on a deleting namespace", func() {
	ctx := context.Background()

	It("are not re-added by the reconciliation's finalizer step", func() {
		namespace := createTestNamespaceFor(ctx, "no-finalizer-readd", "readd-cluster", nil)
		key := client.ObjectKeyFromObject(namespace)

		deleteInForeground(ctx, namespace)

		snapshot := getTestNamespace(ctx, key)
		Expect(controllerutil.ContainsFinalizer(snapshot, deletionFinalizer)).To(BeFalse())

		reconciler := &TemporalNamespaceReconciler{Client: k8sClient}
		Expect(reconciler.ensureFinalizer(ctx, snapshot)).To(Succeed())

		Expect(getTestNamespace(ctx, key).Finalizers).To(ConsistOf(metav1.FinalizerDeleteDependents))
	})
})
