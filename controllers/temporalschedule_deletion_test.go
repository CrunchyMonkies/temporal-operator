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

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.temporal.io/api/serviceerror"
	temporalclient "go.temporal.io/sdk/client"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/alexandrevilain/temporal-operator/api/v1beta1"
	"github.com/alexandrevilain/temporal-operator/internal/targetcluster"
	"github.com/alexandrevilain/temporal-operator/pkg/temporal"
)

// createTestScheduleFor creates a deletable schedule pointing at the given namespace.
func createTestScheduleFor(ctx context.Context, name, namespaceName string, annotations map[string]string, finalizers ...string) *v1beta1.TemporalSchedule {
	schedule := &v1beta1.TemporalSchedule{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   "default",
			Annotations: annotations,
			Finalizers:  finalizers,
		},
		Spec: v1beta1.TemporalScheduleSpec{
			NamespaceRef:  v1beta1.ObjectReference{Name: namespaceName},
			AllowDeletion: true,
		},
	}
	Expect(k8sClient.Create(ctx, schedule)).To(Succeed())

	return schedule
}

// scheduleReconcilerFor builds a reconciler whose temporal client is the given fake.
func scheduleReconcilerFor(temporalClient *fakeTemporalClient) *TemporalScheduleReconciler {
	reconciler := &TemporalScheduleReconciler{
		Client:    k8sClient,
		APIReader: k8sClient,
		Resolver:  targetcluster.NewResolver(&targetcluster.Target{Client: k8sClient}, k8sClient.Scheme(), logr.Discard()),
	}

	if temporalClient != nil {
		reconciler.clusterClient = func(context.Context, client.Client, *v1beta1.TemporalCluster, ...temporal.ClientOption) (temporalclient.Client, error) {
			return temporalClient, nil
		}
	}

	return reconciler
}

var _ = Describe("Schedule deletion", func() {
	ctx := context.Background()

	// Neither the namespace nor the cluster is ready here: a schedule on its way out must not wait
	// for either, since all three are commonly deleted in the same breath.
	It("deletes a schedule whose namespace and cluster are not ready", func() {
		createTestClusterWithReadiness(ctx, "schedule-delete-unready-cluster", false)
		createTestNamespaceFor(ctx, "schedule-delete-unready-namespace", "schedule-delete-unready-cluster", nil)

		schedule := createTestScheduleFor(ctx, "delete-with-unready-namespace", "schedule-delete-unready-namespace", nil, deletionFinalizer)
		key := client.ObjectKeyFromObject(schedule)

		deleteInForeground(ctx, schedule)

		temporalClient := &fakeTemporalClient{workflow: &fakeWorkflowService{}}
		result, err := scheduleReconcilerFor(temporalClient).Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal(reconcile.Result{}))

		Expect(temporalClient.workflow.deleted).To(ConsistOf(schedule.GetName()))
		Expect(temporalClient.closed).To(BeTrue())
		Expect(getTestSchedule(ctx, key).Finalizers).To(ConsistOf(metav1.FinalizerDeleteDependents))
	})

	It("treats a schedule already absent on the server as deleted", func() {
		createTestClusterWithReadiness(ctx, "schedule-not-found-cluster", true)
		createTestNamespaceFor(ctx, "schedule-not-found-namespace", "schedule-not-found-cluster", nil)

		schedule := createTestScheduleFor(ctx, "schedule-already-absent", "schedule-not-found-namespace", nil, deletionFinalizer)
		key := client.ObjectKeyFromObject(schedule)

		deleteInForeground(ctx, schedule)

		temporalClient := &fakeTemporalClient{workflow: &fakeWorkflowService{
			err: serviceerror.NewNotFound("schedule not found"),
		}}
		result, err := scheduleReconcilerFor(temporalClient).Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal(reconcile.Result{}))

		Expect(getTestSchedule(ctx, key).Finalizers).To(ConsistOf(metav1.FinalizerDeleteDependents))
	})

	It("reports a refused deletion as blocked instead of retrying it", func() {
		createTestClusterWithReadiness(ctx, "schedule-refused-cluster", true)
		createTestNamespaceFor(ctx, "schedule-refused-namespace", "schedule-refused-cluster", nil)

		schedule := createTestScheduleFor(ctx, "schedule-refused", "schedule-refused-namespace", nil, deletionFinalizer)
		key := client.ObjectKeyFromObject(schedule)

		deleteInForeground(ctx, schedule)

		temporalClient := &fakeTemporalClient{workflow: &fakeWorkflowService{
			err: serviceerror.NewPermissionDenied("no delete for you", ""),
		}}
		_, err := scheduleReconcilerFor(temporalClient).Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).To(HaveOccurred())
		Expect(errors.Is(err, reconcile.TerminalError(nil))).To(BeTrue())

		// The finalizer stays: the schedule still exists on the server, and nothing here is
		// allowed to silently drop it.
		deleting := getTestSchedule(ctx, key)
		Expect(deleting.Finalizers).To(ContainElement(deletionFinalizer))

		condition := meta.FindStatusCondition(deleting.Status.Conditions, v1beta1.ReconcileErrorCondition)
		Expect(condition).NotTo(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionTrue))
		Expect(condition.Reason).To(Equal(v1beta1.DeletionBlockedReason))
		Expect(condition.Message).To(ContainSubstring(forceDeleteAnnotation))
	})

	It("retries a deletion the server failed to carry out", func() {
		createTestClusterWithReadiness(ctx, "schedule-unavailable-cluster", true)
		createTestNamespaceFor(ctx, "schedule-unavailable-namespace", "schedule-unavailable-cluster", nil)

		schedule := createTestScheduleFor(ctx, "schedule-unavailable", "schedule-unavailable-namespace", nil, deletionFinalizer)
		key := client.ObjectKeyFromObject(schedule)

		deleteInForeground(ctx, schedule)

		temporalClient := &fakeTemporalClient{workflow: &fakeWorkflowService{
			err: serviceerror.NewUnavailable("frontend is restarting"),
		}}
		_, err := scheduleReconcilerFor(temporalClient).Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).To(HaveOccurred())
		Expect(errors.Is(err, reconcile.TerminalError(nil))).To(BeFalse())

		deleting := getTestSchedule(ctx, key)
		Expect(deleting.Finalizers).To(ContainElement(deletionFinalizer))
		Expect(meta.FindStatusCondition(deleting.Status.Conditions, v1beta1.ReconcileErrorCondition).Reason).To(Equal(v1beta1.ReconcileErrorReason))
	})

	// The escape hatch runs before anything on the way to the frontend, the cluster's target
	// resolution included: here the cluster points at a target that does not exist.
	It("drops the finalizer without dialling when force deletion is requested", func() {
		cluster := &v1beta1.TemporalCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "schedule-force-delete-cluster",
				Namespace: "default",
			},
			Spec: v1beta1.TemporalClusterSpec{
				NumHistoryShards: 1,
				Persistence: v1beta1.TemporalPersistenceSpec{
					DefaultStore:    &v1beta1.DatastoreSpec{},
					VisibilityStore: &v1beta1.DatastoreSpec{},
				},
				TargetClusterRef: &v1beta1.ObjectReference{Name: "does-not-exist"},
			},
		}
		Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
		createTestNamespaceFor(ctx, "schedule-force-delete-namespace", cluster.GetName(), nil)

		reconciler := scheduleReconcilerFor(nil)
		reconciler.clusterClient = func(context.Context, client.Client, *v1beta1.TemporalCluster, ...temporal.ClientOption) (temporalclient.Client, error) {
			Fail("the force-delete path must not dial the frontend")

			return nil, nil
		}

		By("checking a plain deletion is held up by the target resolution")
		plain := createTestScheduleFor(ctx, "schedule-plain-delete-unresolvable", "schedule-force-delete-namespace", nil, deletionFinalizer)
		plainKey := client.ObjectKeyFromObject(plain)
		deleteInForeground(ctx, plain)

		_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: plainKey})
		Expect(err).To(HaveOccurred())
		held := getTestSchedule(ctx, plainKey)
		Expect(held.Finalizers).To(ContainElement(deletionFinalizer))
		Expect(meta.FindStatusCondition(held.Status.Conditions, v1beta1.ReconcileErrorCondition).Reason).To(Equal(v1beta1.TargetClusterResolutionFailedReason))

		By("checking the annotation releases a schedule on the same cluster")
		annotations := map[string]string{forceDeleteAnnotation: "true"}
		forced := createTestScheduleFor(ctx, "schedule-force-delete", "schedule-force-delete-namespace", annotations, deletionFinalizer)
		forcedKey := client.ObjectKeyFromObject(forced)
		deleteInForeground(ctx, forced)

		result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: forcedKey})
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal(reconcile.Result{}))
		Expect(getTestSchedule(ctx, forcedKey).Finalizers).To(ConsistOf(metav1.FinalizerDeleteDependents))
	})
})
