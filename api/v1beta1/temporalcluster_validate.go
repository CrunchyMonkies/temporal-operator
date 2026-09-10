package v1beta1

import (
	"fmt"
	"net/url"
	"time"

	"github.com/alexandrevilain/temporal-operator/pkg/version"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func (m *MTLSSpec) Validate() (admission.Warnings, field.ErrorList) {
	var warns admission.Warnings
	var errs field.ErrorList

	if m == nil || m.Provider != CertManagerMTLSProvider {
		return nil, nil
	}

	if m.RenewBefore != nil {
		if m.RenewBefore.Duration < 5*time.Minute {
			errs = append(errs, field.Invalid(field.NewPath("spec.mTLS.renewBefore"), m.RenewBefore, "must be at least 5 minutes"))
		}
	}

	return warns, errs
}

// clientSecretEnvVarName is the ui's environment variable holding the OIDC client secret.
const clientSecretEnvVarName = "TEMPORAL_AUTH_CLIENT_SECRET"

// Validate checks that the ui's authentication configuration can be applied to the
// ui version the cluster runs.
func (s *TemporalUISpec) Validate() (admission.Warnings, field.ErrorList) {
	var warns admission.Warnings
	var errs field.ErrorList

	if s == nil || s.Auth == nil {
		return nil, nil
	}

	rootPath := field.NewPath("spec", "ui", "auth")

	// The auth configuration is only applied to the ui deployment, which isn't
	// created at all when the ui is disabled.
	if !s.Enabled {
		errs = append(errs, field.Forbidden(rootPath, "spec.ui.auth requires the ui to be enabled (spec.ui.enabled)"))
		return warns, errs
	}

	if s.Auth.OIDC == nil && len(s.Auth.ExtraEnv) == 0 {
		errs = append(errs, field.Required(rootPath, "please provide an oidc configuration or extra environment variables, or remove the auth configuration"))
	}

	// The ui version is a free-form image tag, so it may not be a semantic version.
	// In that case the operator can't tell which auth features the image supports:
	// it applies the whole configuration and warns the user.
	uiVersion, err := s.ParsedVersion()
	if err != nil {
		warns = append(warns, fmt.Sprintf("Can't parse the ui version %q as a semantic version, the operator can't check that this ui version supports the requested auth configuration.", s.Version))
	}

	if err == nil && !version.UISupportsAuth(uiVersion) {
		errs = append(errs, field.Forbidden(rootPath, fmt.Sprintf("temporal ui version < %s doesn't support configuring authentication using environment variables", version.UIV2_0_0.String())))
	}

	if oidc := s.Auth.OIDC; oidc != nil {
		oidcPath := rootPath.Child("oidc")

		errs = append(errs, validateURL(oidcPath.Child("providerUrl"), oidc.ProviderURL)...)
		errs = append(errs, validateURL(oidcPath.Child("redirectUrl"), oidc.RedirectURL)...)

		if oidc.ClientID == "" {
			errs = append(errs, field.Required(oidcPath.Child("clientId"), "please provide the oidc client id"))
		}

		if oidc.ClientSecretRef == nil {
			errs = append(errs, field.Required(oidcPath.Child("clientSecretRef"), "please provide a reference to the secret holding the oidc client secret"))
		} else if oidc.ClientSecretRef.Name == "" {
			errs = append(errs, field.Required(oidcPath.Child("clientSecretRef", "name"), "please provide the name of the secret holding the oidc client secret"))
		}

		if len(oidc.Scopes) > 0 && err == nil && !version.UISupportsAuthScopes(uiVersion) {
			errs = append(errs, field.Forbidden(oidcPath.Child("scopes"), fmt.Sprintf("temporal ui version < %s ignores the requested oidc scopes", version.UIV2_9_0.String())))
		}
	}

	for i, envVar := range s.Auth.ExtraEnv {
		envVarPath := rootPath.Child("extraEnv").Index(i)

		if envVar.Name == "" {
			errs = append(errs, field.Required(envVarPath.Child("name"), "please provide the environment variable name"))
		}

		// The oidc client secret is only accepted as a secret reference, never as a
		// plain value stored in the cluster's spec.
		if envVar.Name == clientSecretEnvVarName && envVar.Value != "" {
			errs = append(errs, field.Forbidden(envVarPath, fmt.Sprintf("%s can't be set to an inline value, use spec.ui.auth.oidc.clientSecretRef or a valueFrom secret reference", clientSecretEnvVarName)))
		}
	}

	return warns, errs
}

// validateURL ensures the provided value is set and is an absolute http(s) URL.
func validateURL(path *field.Path, value string) field.ErrorList {
	if value == "" {
		return field.ErrorList{field.Required(path, "please provide an URL")}
	}

	parsed, err := url.Parse(value)
	if err != nil {
		return field.ErrorList{field.Invalid(path, value, fmt.Sprintf("can't parse the provided URL: %v", err))}
	}

	if parsed.Scheme != "http" && parsed.Scheme != "https" || parsed.Host == "" {
		return field.ErrorList{field.Invalid(path, value, "please provide an absolute http(s) URL")}
	}

	return nil
}
