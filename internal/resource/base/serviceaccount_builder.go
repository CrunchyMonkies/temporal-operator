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

package base

import (
	"fmt"

	"github.com/alexandrevilain/controller-tools/pkg/resource"
	"github.com/alexandrevilain/temporal-operator/api/v1beta1"
	"github.com/alexandrevilain/temporal-operator/internal/metadata"
	"go.temporal.io/server/common/primitives"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	gcpServiceAccountAnnotation = "iam.gke.io/gcp-service-account"
	awsRoleArnAnnotation        = "eks.amazonaws.com/role-arn"
)

// s3ArchivalServices are the temporal services which talk to the s3 archival bucket: the history
// service writes archives, and the frontends read them back. They are the only ones building an
// archiver provider in the temporal server, so annotating any other service account with an IAM
// role only forces IRSA onto pods that have no use for it — including the schema setup jobs, which
// share this builder.
var s3ArchivalServices = map[string]struct{}{
	string(primitives.FrontendService):         {},
	string(primitives.InternalFrontendService): {},
	string(primitives.HistoryService):          {},
}

var _ resource.Builder = (*ServiceAccountBuilder)(nil)

type ServiceAccountBuilder struct {
	serviceName string
	instance    *v1beta1.TemporalCluster
	scheme      *runtime.Scheme
}

func NewServiceAccountBuilder(serviceName string, instance *v1beta1.TemporalCluster, scheme *runtime.Scheme) *ServiceAccountBuilder {
	return &ServiceAccountBuilder{
		serviceName: serviceName,
		instance:    instance,
		scheme:      scheme,
	}
}

func (b *ServiceAccountBuilder) Build() client.Object {
	return &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:        b.instance.ChildResourceName(b.serviceName),
			Namespace:   b.instance.Namespace,
			Labels:      metadata.GetLabels(b.instance, b.serviceName, b.instance.Spec.Version, b.instance.Labels),
			Annotations: metadata.GetAnnotations(b.instance.Name, b.instance.Annotations),
		},
	}
}

func (b *ServiceAccountBuilder) Enabled() bool {
	return isBuilderEnabled(b.instance, b.serviceName)
}

// needsS3Access reports whether the service this service account belongs to reads from or writes
// to the archival bucket.
func (b *ServiceAccountBuilder) needsS3Access() bool {
	_, ok := s3ArchivalServices[b.serviceName]
	return ok
}

func (b *ServiceAccountBuilder) getIAMAnnotations() map[string]string {
	annotations := make(map[string]string)
	if b.instance.Spec.Archival.IsEnabled() &&
		b.instance.Spec.Archival.Provider != nil &&
		b.instance.Spec.Archival.Provider.S3.UsesIRSA() &&
		b.needsS3Access() {
		annotations[awsRoleArnAnnotation] = *b.instance.Spec.Archival.Provider.S3.RoleName
	}
	if b.instance.Spec.Persistence.DefaultStore.SQL != nil &&
		b.instance.Spec.Persistence.DefaultStore.SQL.GCPServiceAccount != nil {
		annotations[gcpServiceAccountAnnotation] = *b.instance.Spec.Persistence.DefaultStore.SQL.GCPServiceAccount
	}
	if b.instance.Spec.Persistence.VisibilityStore.SQL != nil &&
		b.instance.Spec.Persistence.VisibilityStore.SQL.GCPServiceAccount != nil {
		annotations[gcpServiceAccountAnnotation] = *b.instance.Spec.Persistence.VisibilityStore.SQL.GCPServiceAccount
	}
	if b.instance.Spec.Persistence.SecondaryVisibilityStore != nil &&
		b.instance.Spec.Persistence.SecondaryVisibilityStore.SQL != nil &&
		b.instance.Spec.Persistence.SecondaryVisibilityStore.SQL.GCPServiceAccount != nil {
		annotations[gcpServiceAccountAnnotation] = *b.instance.Spec.Persistence.SecondaryVisibilityStore.SQL.GCPServiceAccount
	}
	if b.instance.Spec.Persistence.AdvancedVisibilityStore != nil &&
		b.instance.Spec.Persistence.AdvancedVisibilityStore.SQL != nil &&
		b.instance.Spec.Persistence.AdvancedVisibilityStore.SQL.GCPServiceAccount != nil {
		annotations[gcpServiceAccountAnnotation] = *b.instance.Spec.Persistence.AdvancedVisibilityStore.SQL.GCPServiceAccount
	}

	return annotations
}

func (b *ServiceAccountBuilder) Update(object client.Object) error {
	sa := object.(*corev1.ServiceAccount)

	sa.Annotations = metadata.Merge(
		sa.Annotations,
		b.getIAMAnnotations(),
	)

	if err := controllerutil.SetControllerReference(b.instance, sa, b.scheme); err != nil {
		return fmt.Errorf("failed setting controller reference: %w", err)
	}

	return nil
}
