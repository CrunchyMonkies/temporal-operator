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

package ui

import (
	"strings"

	"github.com/alexandrevilain/temporal-operator/api/v1beta1"
	"github.com/alexandrevilain/temporal-operator/pkg/version"
	corev1 "k8s.io/api/core/v1"
)

const (
	// defaultOIDCClientSecretKey is the secret key holding the OIDC client secret
	// when the user didn't provide one.
	defaultOIDCClientSecretKey = "clientSecret"

	authEnabledEnvVarName      = "TEMPORAL_AUTH_ENABLED"
	authTypeEnvVarName         = "TEMPORAL_AUTH_TYPE"
	authProviderURLEnvVarName  = "TEMPORAL_AUTH_PROVIDER_URL"
	authClientIDEnvVarName     = "TEMPORAL_AUTH_CLIENT_ID"
	authClientSecretEnvVarName = "TEMPORAL_AUTH_CLIENT_SECRET"
	authCallbackURLEnvVarName  = "TEMPORAL_AUTH_CALLBACK_URL"
	authScopesEnvVarName       = "TEMPORAL_AUTH_SCOPES"

	oidcAuthType = "oidc"
)

// GetAuthEnvironmentVariables returns the environment variables configuring the ui's
// authentication for the provided cluster.
//
// The ui image renders its configuration file from those variables at container start,
// see the ui-server's docker config template.
func GetAuthEnvironmentVariables(instance *v1beta1.TemporalCluster) []corev1.EnvVar {
	spec := instance.Spec.UI
	if !spec.AuthEnabled() {
		return nil
	}

	vars := []corev1.EnvVar{}

	if oidc := spec.Auth.OIDC; oidc != nil {
		clientSecretKey := defaultOIDCClientSecretKey
		if oidc.ClientSecretRef != nil && oidc.ClientSecretRef.Key != "" {
			clientSecretKey = oidc.ClientSecretRef.Key
		}

		vars = append(vars,
			corev1.EnvVar{
				Name:  authEnabledEnvVarName,
				Value: "true",
			},
			corev1.EnvVar{
				Name:  authTypeEnvVarName,
				Value: oidcAuthType,
			},
			corev1.EnvVar{
				Name:  authProviderURLEnvVarName,
				Value: oidc.ProviderURL,
			},
			corev1.EnvVar{
				Name:  authClientIDEnvVarName,
				Value: oidc.ClientID,
			},
			corev1.EnvVar{
				Name:  authCallbackURLEnvVarName,
				Value: oidc.RedirectURL,
			},
		)

		if oidc.ClientSecretRef != nil {
			// The client secret is never inlined in the cluster's spec, it always comes
			// from a secret the user owns.
			vars = append(vars, corev1.EnvVar{
				Name: authClientSecretEnvVarName,
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: oidc.ClientSecretRef.Name,
						},
						Key: clientSecretKey,
					},
				},
			})
		}

		// The scopes environment variable is only read by ui-server >= 2.9.0, older
		// images hardcode the requested scopes in their config template. Setting it
		// for them would be misleading, so it's left out.
		if len(oidc.Scopes) > 0 && uiSupportsAuthScopes(spec) {
			vars = append(vars, corev1.EnvVar{
				Name:  authScopesEnvVarName,
				Value: strings.Join(oidc.Scopes, ","),
			})
		}
	}

	// The escape hatch wins over the variables computed from the typed configuration,
	// so users can tweak any of them without having to override the whole deployment.
	return mergeEnv(vars, spec.Auth.ExtraEnv)
}

// uiSupportsAuthScopes reports whether the ui version the cluster runs reads the
// requested OIDC scopes from its environment.
//
// The version is a free-form image tag: when it isn't a semantic version the
// operator can't tell which features the image supports, so it assumes the image
// is recent enough. The webhook warns users about such versions on admission.
func uiSupportsAuthScopes(spec *v1beta1.TemporalUISpec) bool {
	v, err := spec.ParsedVersion()
	if err != nil {
		return true
	}
	return version.UISupportsAuthScopes(v)
}

// mergeEnv returns the base environment variables with the provided overrides applied.
// Overrides sharing their name with a base variable replace it in place, the others are
// appended. This keeps the ui container free of duplicated environment variables, whose
// resolution order isn't guaranteed by kubernetes.
func mergeEnv(base, overrides []corev1.EnvVar) []corev1.EnvVar {
	result := make([]corev1.EnvVar, len(base))
	copy(result, base)

	for _, override := range overrides {
		index := -1
		for i, envVar := range result {
			if envVar.Name == override.Name {
				index = i
				break
			}
		}

		if index >= 0 {
			result[index] = override
			continue
		}

		result = append(result, override)
	}

	return result
}
