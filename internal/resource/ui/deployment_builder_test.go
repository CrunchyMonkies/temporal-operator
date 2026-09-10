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

package ui_test

import (
	"testing"

	"github.com/alexandrevilain/temporal-operator/api/v1beta1"
	"github.com/alexandrevilain/temporal-operator/internal/resource/ui"
	"github.com/alexandrevilain/temporal-operator/pkg/version"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
)

// newCluster returns a defaulted cluster running the ui, ready to be given to the
// deployment builder.
func newCluster(uiSpec *v1beta1.TemporalUISpec) *v1beta1.TemporalCluster {
	cluster := &v1beta1.TemporalCluster{
		TypeMeta: v1beta1.TemporalClusterTypeMeta,
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "demo",
		},
		Spec: v1beta1.TemporalClusterSpec{
			Version: version.MustNewVersionFromString("1.31.1"),
			UI:      uiSpec,
		},
	}
	cluster.Default()
	return cluster
}

func buildUIDeployment(t *testing.T, cluster *v1beta1.TemporalCluster) *appsv1.Deployment {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, v1beta1.AddToScheme(scheme))

	builder := ui.NewDeploymentBuilder(cluster, scheme, "fake-hash")
	deployment, ok := builder.Build().(*appsv1.Deployment)
	require.True(t, ok)
	require.NoError(t, builder.Update(deployment))

	return deployment
}

// envMap indexes the ui container's environment variables by name.
func envMap(t *testing.T, deployment *appsv1.Deployment) map[string]corev1.EnvVar {
	t.Helper()

	require.Len(t, deployment.Spec.Template.Spec.Containers, 1)
	env := deployment.Spec.Template.Spec.Containers[0].Env

	result := map[string]corev1.EnvVar{}
	for _, envVar := range env {
		_, found := result[envVar.Name]
		assert.False(t, found, "the ui container has a duplicated %s environment variable", envVar.Name)
		result[envVar.Name] = envVar
	}

	return result
}

func TestDeploymentBuilderAuthEnvironmentVariables(t *testing.T) {
	oidc := func() *v1beta1.TemporalUIOIDCAuthSpec {
		return &v1beta1.TemporalUIOIDCAuthSpec{
			ProviderURL:     "https://keycloak.example.com/realms/temporal",
			ClientID:        "temporal-ui",
			ClientSecretRef: &v1beta1.SecretKeyReference{Name: "ui-oidc", Key: "secret"},
			Scopes:          []string{"openid", "profile", "email"},
			RedirectURL:     "https://temporal.example.com/auth/sso/callback",
		}
	}

	tests := map[string]struct {
		ui              *v1beta1.TemporalUISpec
		expectedEnv     map[string]string
		expectedFromRef map[string]corev1.SecretKeySelector
		absentEnv       []string
	}{
		"no auth configuration": {
			ui: &v1beta1.TemporalUISpec{Enabled: true},
			absentEnv: []string{
				"TEMPORAL_AUTH_ENABLED", "TEMPORAL_AUTH_TYPE", "TEMPORAL_AUTH_PROVIDER_URL",
				"TEMPORAL_AUTH_CLIENT_ID", "TEMPORAL_AUTH_CLIENT_SECRET", "TEMPORAL_AUTH_CALLBACK_URL",
				"TEMPORAL_AUTH_SCOPES",
			},
		},
		"oidc configuration": {
			ui: &v1beta1.TemporalUISpec{
				Enabled: true,
				Auth:    &v1beta1.TemporalUIAuthSpec{OIDC: oidc()},
			},
			expectedEnv: map[string]string{
				"TEMPORAL_AUTH_ENABLED":      "true",
				"TEMPORAL_AUTH_TYPE":         "oidc",
				"TEMPORAL_AUTH_PROVIDER_URL": "https://keycloak.example.com/realms/temporal",
				"TEMPORAL_AUTH_CLIENT_ID":    "temporal-ui",
				"TEMPORAL_AUTH_CALLBACK_URL": "https://temporal.example.com/auth/sso/callback",
				"TEMPORAL_AUTH_SCOPES":       "openid,profile,email",
			},
			expectedFromRef: map[string]corev1.SecretKeySelector{
				"TEMPORAL_AUTH_CLIENT_SECRET": {
					LocalObjectReference: corev1.LocalObjectReference{Name: "ui-oidc"},
					Key:                  "secret",
				},
			},
		},
		"oidc configuration without client secret key": {
			ui: &v1beta1.TemporalUISpec{
				Enabled: true,
				Auth: &v1beta1.TemporalUIAuthSpec{
					OIDC: func() *v1beta1.TemporalUIOIDCAuthSpec {
						o := oidc()
						o.ClientSecretRef = &v1beta1.SecretKeyReference{Name: "ui-oidc"}
						return o
					}(),
				},
			},
			expectedFromRef: map[string]corev1.SecretKeySelector{
				"TEMPORAL_AUTH_CLIENT_SECRET": {
					LocalObjectReference: corev1.LocalObjectReference{Name: "ui-oidc"},
					Key:                  "clientSecret",
				},
			},
		},
		"oidc scopes are left out for ui versions ignoring them": {
			ui: &v1beta1.TemporalUISpec{
				Enabled: true,
				Version: "2.8.0",
				Auth:    &v1beta1.TemporalUIAuthSpec{OIDC: oidc()},
			},
			expectedEnv: map[string]string{
				"TEMPORAL_AUTH_ENABLED":   "true",
				"TEMPORAL_AUTH_CLIENT_ID": "temporal-ui",
			},
			absentEnv: []string{"TEMPORAL_AUTH_SCOPES"},
		},
		"oidc scopes are set for the first ui version reading them": {
			ui: &v1beta1.TemporalUISpec{
				Enabled: true,
				Version: "2.9.0",
				Auth:    &v1beta1.TemporalUIAuthSpec{OIDC: oidc()},
			},
			expectedEnv: map[string]string{"TEMPORAL_AUTH_SCOPES": "openid,profile,email"},
		},
		"oidc scopes are set for non semver ui versions": {
			ui: &v1beta1.TemporalUISpec{
				Enabled: true,
				Version: "latest",
				Auth:    &v1beta1.TemporalUIAuthSpec{OIDC: oidc()},
			},
			expectedEnv: map[string]string{"TEMPORAL_AUTH_SCOPES": "openid,profile,email"},
		},
		"extra env only": {
			ui: &v1beta1.TemporalUISpec{
				Enabled: true,
				Auth: &v1beta1.TemporalUIAuthSpec{
					ExtraEnv: []corev1.EnvVar{
						{Name: "TEMPORAL_AUTH_ENABLED", Value: "true"},
						{Name: "TEMPORAL_AUTH_PROVIDER_URL", Value: "https://accounts.google.com"},
					},
				},
			},
			expectedEnv: map[string]string{
				"TEMPORAL_AUTH_ENABLED":      "true",
				"TEMPORAL_AUTH_PROVIDER_URL": "https://accounts.google.com",
			},
			absentEnv: []string{"TEMPORAL_AUTH_TYPE", "TEMPORAL_AUTH_CLIENT_SECRET"},
		},
		"extra env overrides the computed variables": {
			ui: &v1beta1.TemporalUISpec{
				Enabled: true,
				Auth: &v1beta1.TemporalUIAuthSpec{
					OIDC: oidc(),
					ExtraEnv: []corev1.EnvVar{
						{Name: "TEMPORAL_AUTH_LABEL", Value: "keycloak"},
						{Name: "TEMPORAL_AUTH_TYPE", Value: "custom"},
					},
				},
			},
			expectedEnv: map[string]string{
				"TEMPORAL_AUTH_LABEL": "keycloak",
				"TEMPORAL_AUTH_TYPE":  "custom",
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(tt *testing.T) {
			deployment := buildUIDeployment(tt, newCluster(test.ui))
			env := envMap(tt, deployment)

			// The variables the operator always sets must not be lost.
			assert.Contains(tt, env, "TEMPORAL_ADDRESS")
			assert.Contains(tt, env, "TEMPORAL_UI_PORT")

			for name, value := range test.expectedEnv {
				assert.Equal(tt, value, env[name].Value, "unexpected value for %s", name)
			}

			for name, selector := range test.expectedFromRef {
				require.NotNil(tt, env[name].ValueFrom, "%s must come from a secret reference", name)
				require.NotNil(tt, env[name].ValueFrom.SecretKeyRef)
				assert.Equal(tt, selector, *env[name].ValueFrom.SecretKeyRef)
				assert.Empty(tt, env[name].Value, "%s must never be inlined", name)
			}

			for _, name := range test.absentEnv {
				assert.NotContains(tt, env, name)
			}
		})
	}
}

// TestDeploymentBuilderAuthDisabledUI ensures the auth configuration is ignored when
// the ui is disabled, the webhook rejects that combination anyway.
func TestDeploymentBuilderAuthDisabledUI(t *testing.T) {
	cluster := newCluster(&v1beta1.TemporalUISpec{
		Enabled: false,
		Auth: &v1beta1.TemporalUIAuthSpec{
			ExtraEnv: []corev1.EnvVar{{Name: "TEMPORAL_AUTH_ENABLED", Value: "true"}},
		},
	})

	assert.NotContains(t, envMap(t, buildUIDeployment(t, cluster)), "TEMPORAL_AUTH_ENABLED")
}
