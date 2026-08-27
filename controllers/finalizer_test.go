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
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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

func createTestNamespace(ctx context.Context, name string, finalizers ...string) *v1beta1.TemporalNamespace {
	namespace := &v1beta1.TemporalNamespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  "default",
			Finalizers: finalizers,
		},
		Spec: v1beta1.TemporalNamespaceSpec{
			ClusterRef:      v1beta1.ObjectReference{Name: "test-cluster"},
			RetentionPeriod: &metav1.Duration{Duration: time.Hour},
			AllowDeletion:   true,
		},
	}
	Expect(k8sClient.Create(ctx, namespace)).To(Succeed())

	return namespace
}

// createTestCluster reports the cluster ready so namespace reconciliations get past the readiness gate.
func createTestCluster(ctx context.Context, name string) *v1beta1.TemporalCluster {
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

	v1beta1.SetTemporalClusterReady(cluster, metav1.ConditionTrue, v1beta1.ServicesReadyReason, "")
	Expect(k8sClient.Status().Update(ctx, cluster)).To(Succeed())

	return cluster
}

// deleteAfterGetClient stands in for an object that goes away between a reconciliation's Get and
// its finalizer patch.
type deleteAfterGetClient struct {
	client.Client
	target client.ObjectKey
}

func (c *deleteAfterGetClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if err := c.Client.Get(ctx, key, obj, opts...); err != nil {
		return err
	}

	if key != c.target {
		return nil
	}

	return c.Client.Delete(ctx, obj)
}

func getTestNamespace(ctx context.Context, key client.ObjectKey) *v1beta1.TemporalNamespace {
	namespace := &v1beta1.TemporalNamespace{}
	Expect(k8sClient.Get(ctx, key, namespace)).To(Succeed())

	return namespace
}

// deleteInForeground makes the API server append foregroundDeletion to the object's finalizers.
func deleteInForeground(ctx context.Context, obj client.Object) {
	Expect(k8sClient.Delete(ctx, obj, client.PropagationPolicy(metav1.DeletePropagationForeground))).To(Succeed())
}

// runGarbageCollector strips foregroundDeletion by hand: envtest runs no controller-manager to do it.
func runGarbageCollector(ctx context.Context, key client.ObjectKey) {
	namespace := getTestNamespace(ctx, key)

	patched := namespace.DeepCopy()
	Expect(controllerutil.RemoveFinalizer(patched, metav1.FinalizerDeleteDependents)).To(BeTrue())
	Expect(k8sClient.Patch(ctx, patched, client.MergeFrom(namespace))).To(Succeed())
}

var _ = Describe("Finalizers", func() {
	ctx := context.Background()

	Context("When an object is deleted with foreground propagation", func() {
		It("gets foregroundDeletion appended to its finalizers by the API server", func() {
			namespace := createTestNamespace(ctx, "foreground-delete", deletionFinalizer)
			key := client.ObjectKeyFromObject(namespace)

			deleteInForeground(ctx, namespace)

			deleting := getTestNamespace(ctx, key)
			Expect(deleting.DeletionTimestamp.IsZero()).To(BeFalse())
			Expect(deleting.Finalizers).To(ConsistOf(deletionFinalizer, metav1.FinalizerDeleteDependents))
		})

		It("has the deferred whole-object patch rejected once the garbage collector has run", func() {
			namespace := createTestNamespace(ctx, "deferred-patch-rejected", deletionFinalizer)
			key := client.ObjectKeyFromObject(namespace)

			deleteInForeground(ctx, namespace)

			snapshot := getTestNamespace(ctx, key)
			Expect(snapshot.Finalizers).To(ContainElement(metav1.FinalizerDeleteDependents))

			patchHelper, err := patch.NewHelper(snapshot, k8sClient)
			Expect(err).NotTo(HaveOccurred())

			runGarbageCollector(ctx, key)

			Expect(controllerutil.RemoveFinalizer(snapshot, deletionFinalizer)).To(BeTrue())

			err = patchHelper.Patch(ctx, snapshot)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no new finalizers can be added if the object is being deleted"))
			Expect(err.Error()).To(ContainSubstring(metav1.FinalizerDeleteDependents))
		})
	})

	Context("When adding a finalizer", func() {
		It("does not write when the finalizer is already set", func() {
			namespace := createTestNamespace(ctx, "add-no-write-when-set", deletionFinalizer)
			key := client.ObjectKeyFromObject(namespace)

			snapshot := getTestNamespace(ctx, key)
			Expect(addFinalizer(ctx, k8sClient, snapshot, deletionFinalizer)).To(Succeed())
			Expect(getTestNamespace(ctx, key).ResourceVersion).To(Equal(snapshot.ResourceVersion))
		})

		It("conflicts when the object is being deleted", func() {
			namespace := createTestNamespace(ctx, "add-conflicts-on-deleting-object")
			key := client.ObjectKeyFromObject(namespace)

			snapshot := getTestNamespace(ctx, key)
			deleteInForeground(ctx, namespace)

			err := addFinalizer(ctx, k8sClient, snapshot, deletionFinalizer)
			Expect(apierrors.IsConflict(err)).To(BeTrue())
			Expect(getTestNamespace(ctx, key).Finalizers).To(ConsistOf(metav1.FinalizerDeleteDependents))
		})

		It("fails when the object is already deleted", func() {
			namespace := createTestNamespace(ctx, "add-on-deleted-object")
			key := client.ObjectKeyFromObject(namespace)

			snapshot := getTestNamespace(ctx, key)
			Expect(k8sClient.Delete(ctx, namespace)).To(Succeed())

			err := addFinalizer(ctx, k8sClient, snapshot, deletionFinalizer)
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})

		It("fails the controller's finalizer step when the object is already deleted", func() {
			namespace := createTestNamespace(ctx, "add-failure-reaches-reconciliation")
			key := client.ObjectKeyFromObject(namespace)

			snapshot := getTestNamespace(ctx, key)
			Expect(k8sClient.Delete(ctx, namespace)).To(Succeed())

			reconciler := &TemporalNamespaceReconciler{Client: k8sClient}
			Expect(apierrors.IsNotFound(reconciler.ensureFinalizer(ctx, snapshot))).To(BeTrue())
		})

		It("ends the reconciliation cleanly when the object goes away mid-cycle", func() {
			createTestCluster(ctx, "test-cluster")

			namespace := createTestNamespace(ctx, "add-on-object-deleted-mid-reconciliation")
			key := client.ObjectKeyFromObject(namespace)

			reconciler := &TemporalNamespaceReconciler{
				Client:   &deleteAfterGetClient{Client: k8sClient, target: key},
				Resolver: targetcluster.NewResolver(&targetcluster.Target{Client: k8sClient}, k8sClient.Scheme(), logr.Discard()),
			}

			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: key})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			Expect(apierrors.IsNotFound(k8sClient.Get(ctx, key, &v1beta1.TemporalNamespace{}))).To(BeTrue())
		})
	})

	Context("When removing a finalizer", func() {
		It("drops the deletion finalizer and the object with it", func() {
			namespace := createTestNamespace(ctx, "remove-releases-object", deletionFinalizer)
			key := client.ObjectKeyFromObject(namespace)

			deleteInForeground(ctx, namespace)
			runGarbageCollector(ctx, key)

			Expect(removeFinalizer(ctx, k8sClient, getTestNamespace(ctx, key), deletionFinalizer)).To(Succeed())

			err := k8sClient.Get(ctx, key, &v1beta1.TemporalNamespace{})
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})

		It("conflicts when foregroundDeletion was removed underneath the snapshot", func() {
			namespace := createTestNamespace(ctx, "remove-conflicts-on-stale-snapshot", deletionFinalizer)
			key := client.ObjectKeyFromObject(namespace)

			deleteInForeground(ctx, namespace)

			snapshot := getTestNamespace(ctx, key)
			runGarbageCollector(ctx, key)

			err := removeFinalizer(ctx, k8sClient, snapshot, deletionFinalizer)
			Expect(apierrors.IsConflict(err)).To(BeTrue())
			Expect(getTestNamespace(ctx, key).Finalizers).To(ConsistOf(deletionFinalizer))
		})

		It("conflicts when foregroundDeletion was added underneath the snapshot", func() {
			namespace := createTestNamespace(ctx, "remove-keeps-foreground-deletion", deletionFinalizer)
			key := client.ObjectKeyFromObject(namespace)

			snapshot := getTestNamespace(ctx, key)
			deleteInForeground(ctx, namespace)

			err := removeFinalizer(ctx, k8sClient, snapshot, deletionFinalizer)
			Expect(apierrors.IsConflict(err)).To(BeTrue())
			Expect(getTestNamespace(ctx, key).Finalizers).To(ConsistOf(deletionFinalizer, metav1.FinalizerDeleteDependents))
		})

		It("does not write when the finalizer is already gone", func() {
			namespace := createTestNamespace(ctx, "remove-no-write-when-absent", deletionFinalizer)
			key := client.ObjectKeyFromObject(namespace)

			snapshot := getTestNamespace(ctx, key)
			Expect(removeFinalizer(ctx, k8sClient, snapshot, "unknown.finalizers.temporal.io")).To(Succeed())
			Expect(getTestNamespace(ctx, key).ResourceVersion).To(Equal(snapshot.ResourceVersion))
		})

		It("succeeds when the object is already deleted", func() {
			namespace := createTestNamespace(ctx, "gone-before-removal", deletionFinalizer)
			key := client.ObjectKeyFromObject(namespace)

			snapshot := getTestNamespace(ctx, key)

			Expect(k8sClient.Delete(ctx, namespace)).To(Succeed())
			Expect(removeFinalizer(ctx, k8sClient, getTestNamespace(ctx, key), deletionFinalizer)).To(Succeed())

			// The stale snapshot still carries the finalizer, so this retry reaches a gone object.
			Expect(removeFinalizer(ctx, k8sClient, snapshot, deletionFinalizer)).To(Succeed())
		})
	})
})

func createTestSchedule(ctx context.Context, name string, allowDeletion bool, finalizers ...string) *v1beta1.TemporalSchedule {
	schedule := &v1beta1.TemporalSchedule{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  "default",
			Finalizers: finalizers,
		},
		Spec: v1beta1.TemporalScheduleSpec{
			NamespaceRef:  v1beta1.ObjectReference{Name: "test-namespace"},
			AllowDeletion: allowDeletion,
		},
	}
	Expect(k8sClient.Create(ctx, schedule)).To(Succeed())

	return schedule
}

func getTestSchedule(ctx context.Context, key client.ObjectKey) *v1beta1.TemporalSchedule {
	schedule := &v1beta1.TemporalSchedule{}
	Expect(k8sClient.Get(ctx, key, schedule)).To(Succeed())

	return schedule
}

var _ = Describe("Strict finalizer removal", func() {
	ctx := context.Background()

	It("reports a gone object where the tolerant removal reports success", func() {
		schedule := createTestSchedule(ctx, "strict-removal-on-deleted-object", false, deletionFinalizer)
		key := client.ObjectKeyFromObject(schedule)

		snapshot := getTestSchedule(ctx, key)

		// Deleting leaves it Terminating; dropping the finalizer from a fresh read completes it.
		Expect(k8sClient.Delete(ctx, schedule)).To(Succeed())
		Expect(removeFinalizer(ctx, k8sClient, getTestSchedule(ctx, key), deletionFinalizer)).To(Succeed())
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, key, &v1beta1.TemporalSchedule{}))).To(BeTrue())

		// The stale snapshot still carries the finalizer, so both calls reach a gone object: the
		// tolerant wrapper swallows that, the strict one reports it.
		Expect(removeFinalizer(ctx, k8sClient, snapshot.DeepCopy(), deletionFinalizer)).To(Succeed())
		Expect(apierrors.IsNotFound(removeFinalizerStrict(ctx, k8sClient, snapshot, deletionFinalizer))).To(BeTrue())
	})

	// The schedule reconciliation goes on to CreateSchedule after ensureFinalizer, so a swallowed
	// NotFound here means creating a schedule in Temporal for a CR that is already gone. The
	// snapshot predates the deletion, which is what a reconciliation racing the API server holds.
	It("surfaces a mid-cycle deletion through ensureFinalizer when the finalizer is being dropped", func() {
		schedule := createTestSchedule(ctx, "ensure-finalizer-strict-when-dropping", false, deletionFinalizer)
		key := client.ObjectKeyFromObject(schedule)

		snapshot := getTestSchedule(ctx, key)

		Expect(k8sClient.Delete(ctx, schedule)).To(Succeed())
		Expect(removeFinalizer(ctx, k8sClient, getTestSchedule(ctx, key), deletionFinalizer)).To(Succeed())

		reconciler := &TemporalScheduleReconciler{Client: k8sClient}
		Expect(apierrors.IsNotFound(reconciler.ensureFinalizer(ctx, snapshot))).To(BeTrue())
	})

	// Nothing is written when there is no finalizer diff, so this branch has no API call to learn
	// the deletion from. confirmLive covers it before the reconciliation reaches Temporal.
	It("cannot see a mid-cycle deletion when there is no finalizer to drop", func() {
		schedule := createTestSchedule(ctx, "ensure-finalizer-no-write-no-signal", false)
		key := client.ObjectKeyFromObject(schedule)

		snapshot := &v1beta1.TemporalSchedule{}
		Expect(k8sClient.Get(ctx, key, snapshot)).To(Succeed())
		Expect(k8sClient.Delete(ctx, schedule)).To(Succeed())

		reconciler := &TemporalScheduleReconciler{Client: k8sClient}
		Expect(reconciler.ensureFinalizer(ctx, snapshot)).To(Succeed())
	})

	It("surfaces a mid-cycle deletion through ensureFinalizer when deletion is allowed", func() {
		schedule := createTestSchedule(ctx, "ensure-finalizer-strict-when-deletion-allowed", true)
		key := client.ObjectKeyFromObject(schedule)

		snapshot := &v1beta1.TemporalSchedule{}
		Expect(k8sClient.Get(ctx, key, snapshot)).To(Succeed())
		Expect(k8sClient.Delete(ctx, schedule)).To(Succeed())

		reconciler := &TemporalScheduleReconciler{Client: k8sClient}
		Expect(apierrors.IsNotFound(reconciler.ensureFinalizer(ctx, snapshot))).To(BeTrue())
	})
})

var _ = Describe("Reconcile error handling", func() {
	ctx := context.Background()

	// controller-runtime rate-limits the retry off the returned error; swallowing it leaves the
	// predicates as the only way back, and they only fire on a spec change.
	It("returns the error to the caller for a namespace", func() {
		namespace := createTestNamespace(ctx, "namespace-error-is-returned")

		reconciler := &TemporalNamespaceReconciler{Client: k8sClient}
		_, err := reconciler.handleErrorWithRequeue(namespace, v1beta1.ReconcileErrorReason, errors.New("boom"), 0)

		Expect(err).To(MatchError("boom"))
		Expect(meta.IsStatusConditionTrue(namespace.Status.Conditions, v1beta1.ReconcileErrorCondition)).To(BeTrue())
	})

	It("returns the error to the caller for a schedule", func() {
		schedule := createTestSchedule(ctx, "schedule-error-is-returned", false)

		reconciler := &TemporalScheduleReconciler{Client: k8sClient}
		_, err := reconciler.handleErrorWithRequeue(ctx, schedule, v1beta1.ReconcileErrorReason, "Testing", errors.New("boom"), 0)

		Expect(err).To(MatchError("boom"))
		Expect(meta.IsStatusConditionTrue(schedule.Status.Conditions, v1beta1.ReconcileErrorCondition)).To(BeTrue())
	})
})

// failingReader stands in for an API server that cannot be reached, rather than one reporting a
// deletion.
type failingReader struct {
	client.Reader
	err error
}

func (r *failingReader) Get(context.Context, client.ObjectKey, client.Object, ...client.GetOption) error {
	return r.err
}

var _ = Describe("Live object confirmation", func() {
	ctx := context.Background()

	It("confirms an object that is still there", func() {
		schedule := createTestSchedule(ctx, "confirm-live-present", false)

		Expect(confirmLive(ctx, k8sClient, schedule)).To(BeTrue())
	})

	// This is the shape of the race: the reconciliation holds the object it read at the top of the
	// cycle, and the deletion lands while it is still working.
	It("reports an object deleted after the reconciliation read it", func() {
		schedule := createTestSchedule(ctx, "confirm-live-deleted-mid-cycle", false)
		snapshot := getTestSchedule(ctx, client.ObjectKeyFromObject(schedule))

		Expect(k8sClient.Delete(ctx, schedule)).To(Succeed())

		Expect(confirmLive(ctx, k8sClient, snapshot)).To(BeFalse())
	})

	// A finalizer keeps the object present, so an existence check alone would still call it live.
	It("reports an object that is present but terminating", func() {
		schedule := createTestSchedule(ctx, "confirm-live-terminating", false, deletionFinalizer)
		key := client.ObjectKeyFromObject(schedule)
		snapshot := getTestSchedule(ctx, key)

		Expect(k8sClient.Delete(ctx, schedule)).To(Succeed())
		Expect(getTestSchedule(ctx, key).DeletionTimestamp.IsZero()).To(BeFalse())

		Expect(confirmLive(ctx, k8sClient, snapshot)).To(BeFalse())
	})

	It("does not read an unreachable API server as a deletion", func() {
		schedule := createTestSchedule(ctx, "confirm-live-read-failure", false)

		reader := &failingReader{err: apierrors.NewInternalError(errors.New("boom"))}
		live, err := confirmLive(ctx, reader, schedule)
		Expect(err).To(HaveOccurred())
		Expect(live).To(BeFalse())
	})

	// The pair that closes the gap: ensureFinalizer has nothing to write and so reports success,
	// while confirmLive asks the API server directly and sees the object is gone.
	It("sees the deletion that ensureFinalizer cannot", func() {
		schedule := createTestSchedule(ctx, "confirm-live-covers-finalizer-blind-spot", false)
		snapshot := getTestSchedule(ctx, client.ObjectKeyFromObject(schedule))

		Expect(k8sClient.Delete(ctx, schedule)).To(Succeed())

		reconciler := &TemporalScheduleReconciler{Client: k8sClient, APIReader: k8sClient}
		Expect(reconciler.ensureFinalizer(ctx, snapshot)).To(Succeed())
		Expect(confirmLive(ctx, reconciler.APIReader, snapshot)).To(BeFalse())
	})
})

var _ = Describe("Stale deletion finalizers", func() {
	ctx := context.Background()

	// Turning allowDeletion back off has to drop the finalizer this controller added, or the
	// namespace can never be deleted again.
	It("are dropped from a namespace once deletion is no longer allowed", func() {
		namespace := createTestNamespace(ctx, "namespace-stale-finalizer-dropped", deletionFinalizer)
		key := client.ObjectKeyFromObject(namespace)

		current := getTestNamespace(ctx, key)
		current.Spec.AllowDeletion = false

		reconciler := &TemporalNamespaceReconciler{Client: k8sClient}
		Expect(reconciler.ensureFinalizer(ctx, current)).To(Succeed())

		Expect(getTestNamespace(ctx, key).Finalizers).To(BeEmpty())
	})
})

var _ = Describe("Terminal reconcile errors", func() {
	ctx := context.Background()

	// A malformed spec fails identically on every retry. controller-runtime records a terminal
	// error without requeueing; the generation change from a corrected spec brings the object back.
	It("are not requeued for a schedule", func() {
		schedule := createTestSchedule(ctx, "schedule-terminal-error", false)

		reconciler := &TemporalScheduleReconciler{Client: k8sClient}
		result, err := reconciler.handleTerminalError(ctx, schedule, "Testing", errors.New("boom"))

		Expect(errors.Is(err, reconcile.TerminalError(nil))).To(BeTrue())
		Expect(err).To(MatchError(ContainSubstring("boom")))
		Expect(result).To(Equal(reconcile.Result{}))
		Expect(meta.FindStatusCondition(schedule.Status.Conditions, v1beta1.ReconcileErrorCondition).Reason).
			To(Equal(v1beta1.SpecValidationFailedReason))
	})

	It("are not requeued for a namespace", func() {
		namespace := createTestNamespace(ctx, "namespace-terminal-error")

		reconciler := &TemporalNamespaceReconciler{Client: k8sClient}
		result, err := reconciler.handleTerminalError(namespace, errors.New("boom"))

		Expect(errors.Is(err, reconcile.TerminalError(nil))).To(BeTrue())
		Expect(err).To(MatchError(ContainSubstring("boom")))
		Expect(result).To(Equal(reconcile.Result{}))
		Expect(meta.FindStatusCondition(namespace.Status.Conditions, v1beta1.ReconcileErrorCondition).Reason).
			To(Equal(v1beta1.SpecValidationFailedReason))
	})
})
