package v1beta1

import (
	"fmt"
	"time"

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

// ValidatePProf ensures the pprof endpoint doesn't collide with any other port the
// operator opens in the temporal services containers. All services share the same
// pprof port, so a collision with any of them is rejected.
func (s *TemporalClusterSpec) ValidatePProf() (admission.Warnings, field.ErrorList) {
	if !s.PProf.IsEnabled() {
		return nil, nil
	}

	var errs field.ErrorList

	pprofPort := s.PProf.GetPort()
	path := field.NewPath("spec", "pprof", "port")

	reportCollision := func(with string) {
		errs = append(errs, field.Invalid(path, pprofPort, fmt.Sprintf("collides with the %s port", with)))
	}

	if s.Metrics.IsEnabled() &&
		s.Metrics.Prometheus != nil &&
		s.Metrics.Prometheus.ListenPort != nil &&
		*s.Metrics.Prometheus.ListenPort == pprofPort {
		reportCollision("metrics")
	}

	if s.Services == nil {
		return nil, errs
	}

	services := []struct {
		name string
		spec *ServiceSpec
	}{
		{"frontend", s.Services.Frontend},
		{"history", s.Services.History},
		{"matching", s.Services.Matching},
		{"worker", s.Services.Worker},
	}
	if s.Services.InternalFrontend.IsEnabled() {
		services = append(services, struct {
			name string
			spec *ServiceSpec
		}{"internalFrontend", &s.Services.InternalFrontend.ServiceSpec})
	}

	for _, service := range services {
		if service.spec == nil {
			continue
		}
		ports := []struct {
			name string
			port *int32
		}{
			{"rpc", service.spec.Port},
			{"membership", service.spec.MembershipPort},
			{"http", service.spec.HTTPPort},
		}
		for _, p := range ports {
			if p.port != nil && *p.port == pprofPort {
				reportCollision(fmt.Sprintf("%s service %s", service.name, p.name))
			}
		}
	}

	return nil, errs
}
