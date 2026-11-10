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

package base_test

import (
	"testing"

	"github.com/alexandrevilain/temporal-operator/api/v1beta1"
	"github.com/alexandrevilain/temporal-operator/internal/resource/base"
	"github.com/alexandrevilain/temporal-operator/internal/resource/persistence"
	"github.com/alexandrevilain/temporal-operator/pkg/version"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/server/common/primitives"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
)

const awsRoleArnAnnotation = "eks.amazonaws.com/role-arn"

func newArchivalCluster(t *testing.T, s3 *v1beta1.S3Archiver) *v1beta1.TemporalCluster {
	t.Helper()

	cluster := &v1beta1.TemporalCluster{
		TypeMeta:   v1beta1.TemporalClusterTypeMeta,
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: v1beta1.TemporalClusterSpec{
			Version:          version.MustNewVersionFromString("1.24.3"),
			NumHistoryShards: 1,
			Persistence: v1beta1.TemporalPersistenceSpec{
				DefaultStore:    &v1beta1.DatastoreSpec{SQL: &v1beta1.SQLSpec{PluginName: "postgres12"}},
				VisibilityStore: &v1beta1.DatastoreSpec{SQL: &v1beta1.SQLSpec{PluginName: "postgres12"}},
			},
		},
	}

	if s3 != nil {
		cluster.Spec.Archival = &v1beta1.ClusterArchivalSpec{
			Enabled:  true,
			Provider: &v1beta1.ArchivalProvider{S3: s3},
		}
	}

	cluster.Default()

	return cluster
}

// buildServiceAccount runs the builder the way the reconciler does, and returns the resulting
// service account.
func buildServiceAccount(t *testing.T, serviceName string, cluster *v1beta1.TemporalCluster) *corev1.ServiceAccount {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, v1beta1.AddToScheme(scheme))

	builder := base.NewServiceAccountBuilder(serviceName, cluster, scheme)
	sa, ok := builder.Build().(*corev1.ServiceAccount)
	require.True(t, ok)
	require.NoError(t, builder.Update(sa))

	return sa
}

// TestServiceAccountS3IAMAnnotation covers which service accounts get the IRSA role annotation:
// only the services which actually reach the archival bucket, and only when a role name asks for
// IRSA in the first place.
func TestServiceAccountS3IAMAnnotation(t *testing.T) {
	roleName := "arn:aws:iam::123456789012:role/temporal-archival"

	// Every service account the operator creates for a cluster, including the one the schema setup
	// jobs run as.
	allServiceNames := []string{
		string(primitives.FrontendService),
		string(primitives.InternalFrontendService),
		string(primitives.HistoryService),
		string(primitives.MatchingService),
		string(primitives.WorkerService),
		persistence.ServiceNameSuffix,
	}

	tests := map[string]struct {
		s3 *v1beta1.S3Archiver
		// annotated lists the service accounts expected to carry the role annotation.
		annotated []string
	}{
		"irsa annotates only the services accessing the bucket": {
			s3: &v1beta1.S3Archiver{
				Region:   "eu-west-1",
				RoleName: ptr.To(roleName),
			},
			annotated: []string{
				string(primitives.FrontendService),
				string(primitives.InternalFrontendService),
				string(primitives.HistoryService),
			},
		},
		"the default credential chain annotates nothing": {
			s3: &v1beta1.S3Archiver{
				Region:                "eu-west-1",
				UseDefaultCredentials: true,
			},
		},
		"static credentials annotate nothing": {
			s3: &v1beta1.S3Archiver{
				Region: "eu-west-1",
				Credentials: &v1beta1.S3Credentials{
					AccessKeyIDRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "archival-credentials"},
						Key:                  "AWS_ACCESS_KEY_ID",
					},
					SecretAccessKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "archival-credentials"},
						Key:                  "AWS_SECRET_ACCESS_KEY",
					},
				},
			},
		},
		"no archival annotates nothing": {},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			expected := map[string]struct{}{}
			for _, serviceName := range test.annotated {
				expected[serviceName] = struct{}{}
			}

			cluster := newArchivalCluster(t, test.s3)

			for _, serviceName := range allServiceNames {
				sa := buildServiceAccount(t, serviceName, cluster)

				if _, ok := expected[serviceName]; ok {
					assert.Equal(t, roleName, sa.Annotations[awsRoleArnAnnotation], "service account %s should assume the archival role", serviceName)
					continue
				}

				assert.NotContains(t, sa.Annotations, awsRoleArnAnnotation, "service account %s should not be forced onto IRSA", serviceName)
			}
		})
	}
}
