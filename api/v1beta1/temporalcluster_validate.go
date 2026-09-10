package v1beta1

import (
	"time"

	corev1 "k8s.io/api/core/v1"

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

// ExposedOutsideCluster returns true if the service spec makes the kubernetes Service
// reachable from outside of the kubernetes cluster.
func (s *ServiceResourceSpec) ExposedOutsideCluster() bool {
	if s == nil || s.Type == nil {
		return false
	}
	return *s.Type == corev1.ServiceTypeNodePort || *s.Type == corev1.ServiceTypeLoadBalancer
}

// Validate validates the kubernetes Service customizations.
func (s *ServiceResourceSpec) Validate(rootPath *field.Path) (admission.Warnings, field.ErrorList) {
	var warns admission.Warnings
	var errs field.ErrorList

	if s == nil {
		return nil, nil
	}

	if s.NodePort != nil && !s.ExposedOutsideCluster() {
		errs = append(errs,
			field.Invalid(
				rootPath.Child("nodePort"),
				*s.NodePort,
				"nodePort can only be set if the service type is NodePort or LoadBalancer",
			),
		)
	}

	return warns, errs
}
