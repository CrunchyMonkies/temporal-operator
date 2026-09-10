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
)

var _ = Describe("Maintenance mode", func() {
	It("scales the services to 0 and back to spec when toggled", func() {
		ctx := context.Background()
		dm, err := discovery.NewManager(cfg, scheme.Scheme)
		Expect(err).NotTo(HaveOccurred())
		rec := &reconciler.Reconciler{Client: k8sClient, Scheme: scheme.Scheme, Recorder: record.NewFakeRecorder(100), Discovery: dm}

		replicas := int32(2)
		cluster := &v1beta1.TemporalCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "maintenance", Namespace: "default"},
			Spec: v1beta1.TemporalClusterSpec{
				Version:          version.MustNewVersionFromString("1.24.1"),
				Image:            "temporalio/server",
				NumHistoryShards: 1,
				Services: &v1beta1.ServicesSpec{
					Frontend: &v1beta1.ServiceSpec{Replicas: &replicas},
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

		key := types.NamespacedName{Name: cluster.ChildResourceName("frontend"), Namespace: "default"}
		getReplicas := func() int32 {
			d := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, key, d)).To(Succeed())
			Expect(d.Spec.Replicas).NotTo(BeNil())
			return *d.Spec.Replicas
		}

		// Baseline: the deployment follows the spec.
		_, err = rec.ReconcileBuilders(ctx, cluster, newBuilders())
		Expect(err).NotTo(HaveOccurred())
		Expect(getReplicas()).To(Equal(replicas))

		// Enter maintenance mode: services scale to 0.
		cluster.Spec.Maintenance = &v1beta1.MaintenanceSpec{Enabled: true}
		Expect(k8sClient.Update(ctx, cluster)).To(Succeed())
		_, err = rec.ReconcileBuilders(ctx, cluster, newBuilders())
		Expect(err).NotTo(HaveOccurred())
		Expect(getReplicas()).To(Equal(int32(0)), "maintenance mode must scale services to 0")

		// Leave maintenance mode: services scale back to the spec.
		cluster.Spec.Maintenance = nil
		Expect(k8sClient.Update(ctx, cluster)).To(Succeed())
		_, err = rec.ReconcileBuilders(ctx, cluster, newBuilders())
		Expect(err).NotTo(HaveOccurred())
		Expect(getReplicas()).To(Equal(replicas), "leaving maintenance mode must restore the spec replicas")
	})
})
