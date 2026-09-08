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

package kubernetes

import (
	"reflect"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

// mergeConflictHint is appended to override validation errors: a merged element
// holding several mutually exclusive fields always comes from a strategic merge
// patch which merged an override element into an existing one carrying the same
// merge key.
const mergeConflictHint = `the override was merged into an existing element with the same name; ` +
	`explicitly set the conflicting field to null in the override, or use "$patch": "replace"`

// validatePatchedPodSpec reports the mutually-exclusive field constraints that an
// override merge can violate. Strategic merge patch merges two elements sharing the
// same merge key field by field, which happily produces an EnvVar carrying both
// value and valueFrom, or a Volume carrying two sources. The API server rejects
// those on apply; reporting them here fails the reconcile with a message naming the
// offending field and the way out.
func validatePatchedPodSpec(spec *corev1.PodSpec, path *field.Path) field.ErrorList {
	errs := field.ErrorList{}

	containerLists := []struct {
		name       string
		containers []corev1.Container
	}{
		{"initContainers", spec.InitContainers},
		{"containers", spec.Containers},
	}

	for _, list := range containerLists {
		for i, container := range list.containers {
			containerPath := path.Child(list.name).Index(i)
			errs = append(errs, validatePatchedEnv(container.Env, containerPath.Child("env"))...)
		}
	}

	for i, container := range spec.EphemeralContainers {
		containerPath := path.Child("ephemeralContainers").Index(i)
		errs = append(errs, validatePatchedEnv(container.Env, containerPath.Child("env"))...)
	}

	for i, volume := range spec.Volumes {
		volumePath := path.Child("volumes").Index(i)
		if sources := setFields(volume.VolumeSource); len(sources) > 1 {
			errs = append(errs, field.Invalid(volumePath, strings.Join(sources, ", "),
				`may not specify more than one volume source: `+mergeConflictHint))
		}
	}

	return errs
}

func validatePatchedEnv(env []corev1.EnvVar, path *field.Path) field.ErrorList {
	errs := field.ErrorList{}

	for i, envVar := range env {
		envVarPath := path.Index(i)

		if envVar.Value != "" && envVar.ValueFrom != nil {
			errs = append(errs, field.Invalid(envVarPath.Child("valueFrom"), envVar.Name,
				`may not be specified when "value" is not empty: `+mergeConflictHint))

			continue
		}

		if envVar.ValueFrom != nil {
			if sources := setFields(*envVar.ValueFrom); len(sources) > 1 {
				errs = append(errs, field.Invalid(envVarPath.Child("valueFrom"), strings.Join(sources, ", "),
					`may not specify more than one environment variable source: `+mergeConflictHint))
			}
		}
	}

	return errs
}

// setFields returns the json names of the non-nil pointer fields of the provided
// struct. It's used to count how many members of a union-like struct are set.
func setFields(union any) []string {
	names := []string{}

	value := reflect.ValueOf(union)
	for i := range value.NumField() {
		if value.Field(i).Kind() != reflect.Ptr || value.Field(i).IsNil() {
			continue
		}

		name := value.Type().Field(i).Name
		if tag, ok := value.Type().Field(i).Tag.Lookup("json"); ok {
			if jsonName, _, _ := strings.Cut(tag, ","); jsonName != "" {
				name = jsonName
			}
		}

		names = append(names, name)
	}

	return names
}
