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

package admintools

import (
	"testing"

	"github.com/alexandrevilain/temporal-operator/api/v1beta1"
	"github.com/alexandrevilain/temporal-operator/pkg/version"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDeploymentBuilder_AdminToolsImage asserts the admin tools deployment resolves its image the
// same way the schema jobs do: the user's tag when set, the tag matching spec.version otherwise.
func TestDeploymentBuilder_AdminToolsImage(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, v1beta1.AddToScheme(scheme))

	tests := []struct {
		name       string
		adminTools *v1beta1.TemporalAdminToolsSpec
		expected   string
	}{
		{
			name:       "follows spec.version when the tag is unset",
			adminTools: &v1beta1.TemporalAdminToolsSpec{Enabled: true},
			expected:   "temporalio/admin-tools:1.26",
		},
		{
			name:       "uses the user's image and tag when set",
			adminTools: &v1beta1.TemporalAdminToolsSpec{Enabled: true, Image: "registry.example.com/admin-tools", Version: "1.26.2-custom"},
			expected:   "registry.example.com/admin-tools:1.26.2-custom",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cluster := &v1beta1.TemporalCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "demo"},
				Spec: v1beta1.TemporalClusterSpec{
					Version:    version.MustNewVersionFromString("1.26.2"),
					AdminTools: tt.adminTools,
					Services: &v1beta1.ServicesSpec{
						Frontend: &v1beta1.ServiceSpec{Port: ptr.To[int32](7233)},
					},
				},
			}

			builder := NewDeploymentBuilder(cluster, scheme, "")
			deployment, ok := builder.Build().(*appsv1.Deployment)
			require.True(t, ok)
			require.NoError(t, builder.Update(deployment))

			containers := deployment.Spec.Template.Spec.Containers
			require.Len(t, containers, 1)
			assert.Equal(t, tt.expected, containers[0].Image)
			assert.Equal(t, cluster.AdminToolsImage(), containers[0].Image)
		})
	}
}
