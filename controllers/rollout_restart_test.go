package controllers

import (
	"context"

	"github.com/alexandrevilain/controller-tools/pkg/discovery"
	"github.com/alexandrevilain/controller-tools/pkg/reconciler"
	ctresource "github.com/alexandrevilain/controller-tools/pkg/resource"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/alexandrevilain/temporal-operator/api/v1beta1"
	"github.com/alexandrevilain/temporal-operator/internal/resource/base"
	"github.com/alexandrevilain/temporal-operator/pkg/version"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Rollout restart reconciliation", func() {
	// Regression test for upstream alexandrevilain/temporal-operator#858:
	// a user-initiated `kubectl rollout restart` must not be reverted by the
	// operator. The rollout imprints `kubectl.kubernetes.io/restartedAt` on the
	// pod template; the reconciler must preserve that foreign annotation instead
	// of regenerating it away (which would scale down the new ReplicaSet).
	It("preserves the restart annotation and does not churn the deployment", func() {
		ctx := context.Background()
		dm, err := discovery.NewManager(cfg, scheme.Scheme)
		Expect(err).NotTo(HaveOccurred())
		rec := &reconciler.Reconciler{Client: k8sClient, Scheme: scheme.Scheme, Recorder: record.NewFakeRecorder(100), Discovery: dm}

		cluster := &v1beta1.TemporalCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "restart-repro", Namespace: "default"},
			Spec: v1beta1.TemporalClusterSpec{
				Version:          version.MustNewVersionFromString("1.24.1"),
				Image:            "temporalio/server",
				NumHistoryShards: 1,
				Services: &v1beta1.ServicesSpec{
					Frontend: &v1beta1.ServiceSpec{},
				},
				Persistence: v1beta1.TemporalPersistenceSpec{
					DefaultStore:    &v1beta1.DatastoreSpec{},
					VisibilityStore: &v1beta1.DatastoreSpec{},
				},
			},
		}
		cluster.Default()
		Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, cluster) })

		spec, err := cluster.Spec.Services.GetServiceSpec("frontend")
		Expect(err).NotTo(HaveOccurred())
		newBuilders := func() []ctresource.Builder {
			return []ctresource.Builder{base.NewDeploymentBuilder("frontend", cluster, scheme.Scheme, spec, "hash-1")}
		}

		_, err = rec.ReconcileBuilders(ctx, cluster, newBuilders())
		Expect(err).NotTo(HaveOccurred())

		key := types.NamespacedName{Name: cluster.ChildResourceName("frontend"), Namespace: "default"}
		get := func() *appsv1.Deployment {
			d := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, key, d)).To(Succeed())
			return d
		}

		// Reconcile again with no changes: the deployment must not be touched.
		before := get()
		_, err = rec.ReconcileBuilders(ctx, cluster, newBuilders())
		Expect(err).NotTo(HaveOccurred())
		after := get()
		Expect(after.ResourceVersion).To(Equal(before.ResourceVersion), "operator should not churn an unchanged deployment")

		// Simulate `kubectl rollout restart`.
		restartPatch := []byte(`{"spec":{"template":{"metadata":{"annotations":{"kubectl.kubernetes.io/restartedAt":"2026-09-10T00:00:00Z"}}}}}`)
		d := get()
		Expect(k8sClient.Patch(ctx, d, client.RawPatch(types.StrategicMergePatchType, restartPatch))).To(Succeed())
		restarted := get()
		Expect(restarted.Spec.Template.Annotations).To(HaveKey("kubectl.kubernetes.io/restartedAt"))
		beforeGen := restarted.Generation

		// Reconcile and assert the restart is preserved and no new update is issued.
		_, err = rec.ReconcileBuilders(ctx, cluster, newBuilders())
		Expect(err).NotTo(HaveOccurred())
		final := get()
		Expect(final.Spec.Template.Annotations).To(HaveKey("kubectl.kubernetes.io/restartedAt"),
			"operator must not revert the restart annotation written by kubectl rollout restart")
		Expect(final.Generation).To(Equal(beforeGen),
			"preserving the restart annotation must not itself trigger a rollout (template churn)")
	})
})
