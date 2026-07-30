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

// Package bootstrap installs the operator's own prerequisites into the cluster it watches.
//
// When the operator watches a remote cluster, that cluster holds the CustomResourceDefinitions
// and the admission webhook configurations, but the Helm release installing the operator runs
// against the cluster hosting it. Bootstrapping closes that gap: the chart renders what the
// watched cluster needs into a directory mounted in the operator's pod, and the operator applies
// it on startup, before its controllers start.
package bootstrap

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// fieldOwner identifies the operator as the owner of the fields it applies.
	fieldOwner = client.FieldOwner("temporal-operator")

	// crdEstablishedTimeout bounds how long we wait for applied CustomResourceDefinitions to
	// become servable. Controllers watching those kinds fail to start until they are.
	crdEstablishedTimeout = time.Minute
	crdEstablishedPolling = time.Second
)

// webhookConfigurationKinds are the kinds whose clientConfig needs a CA bundle. Cert-manager's
// ca-injector can't do it for us here: it only reconciles objects living in the same cluster it
// runs in, and in remote mode the operator's serving certificate is issued in another one.
var webhookConfigurationKinds = map[string]struct{}{
	"ValidatingWebhookConfiguration": {},
	"MutatingWebhookConfiguration":   {},
}

// scheme only needs the types we read back as typed objects: everything we apply goes through
// unstructured, which the client resolves using the api server's discovery instead.
var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(apiextensionsv1.AddToScheme(scheme))
}

// Apply applies every manifest found in dir to the cluster reachable through cfg.
//
// Manifests are applied server-side, so re-running it converges rather than conflicting with a
// previous run. Admission webhook configurations that carry no CA bundle get the one read from
// caBundleFile, when that file exists. Apply returns once any applied CustomResourceDefinition is
// established.
func Apply(ctx context.Context, cfg *rest.Config, dir string, caBundleFile string) error {
	logger := log.FromContext(ctx).WithName("bootstrap")

	objects, err := Load(dir)
	if err != nil {
		return err
	}

	if len(objects) == 0 {
		logger.Info("No manifests to apply", "dir", dir)
		return nil
	}

	caBundle, err := readCABundle(caBundleFile)
	if err != nil {
		return err
	}
	if caBundle == nil {
		logger.Info("No CA bundle available, applying webhook configurations as-is", "file", caBundleFile)
	}

	c, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		return fmt.Errorf("can't create bootstrap client: %w", err)
	}

	crds := []string{}
	for _, obj := range objects {
		if err := InjectCABundle(obj, caBundle); err != nil {
			return err
		}

		err := c.Patch(ctx, obj, client.Apply, fieldOwner, client.ForceOwnership)
		if err != nil {
			return fmt.Errorf("can't apply %s %s: %w", obj.GetKind(), obj.GetName(), err)
		}

		logger.Info("Applied manifest", "kind", obj.GetKind(), "name", obj.GetName())

		if obj.GetKind() == "CustomResourceDefinition" {
			crds = append(crds, obj.GetName())
		}
	}

	return waitForEstablishedCRDs(ctx, c, crds)
}

// Load reads every YAML manifest found in dir, non-recursively. Files that aren't YAML are
// ignored, which keeps it usable against a mounted ConfigMap: Kubernetes populates those with a
// hidden ..data symlink tree we don't want to walk twice.
func Load(dir string) ([]*unstructured.Unstructured, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("can't read bootstrap manifests directory: %w", err)
	}

	objects := []*unstructured.Unstructured{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		ext := filepath.Ext(entry.Name())
		if ext != ".yaml" && ext != ".yml" {
			continue
		}

		file, err := os.Open(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("can't open bootstrap manifest %s: %w", entry.Name(), err)
		}

		decoded, err := Decode(file)
		file.Close()
		if err != nil {
			return nil, fmt.Errorf("can't decode bootstrap manifest %s: %w", entry.Name(), err)
		}

		objects = append(objects, decoded...)
	}

	return objects, nil
}

// Decode reads a stream of YAML documents. Empty documents are skipped, so the leading or
// trailing separators template engines tend to emit are harmless.
func Decode(r io.Reader) ([]*unstructured.Unstructured, error) {
	decoder := utilyaml.NewYAMLOrJSONDecoder(r, 4096)

	objects := []*unstructured.Unstructured{}
	for {
		obj := &unstructured.Unstructured{}

		err := decoder.Decode(obj)
		if errors.Is(err, io.EOF) {
			return objects, nil
		}
		if err != nil {
			return nil, err
		}

		if len(obj.Object) == 0 {
			continue
		}

		objects = append(objects, obj)
	}
}

// InjectCABundle sets the CA bundle on every webhook of an admission webhook configuration that
// doesn't already declare one. Other kinds, and an empty caBundle, are left alone.
func InjectCABundle(obj *unstructured.Unstructured, caBundle []byte) error {
	if len(caBundle) == 0 {
		return nil
	}

	if _, ok := webhookConfigurationKinds[obj.GetKind()]; !ok {
		return nil
	}

	webhooks, found, err := unstructured.NestedSlice(obj.Object, "webhooks")
	if err != nil {
		return fmt.Errorf("can't read webhooks of %s %s: %w", obj.GetKind(), obj.GetName(), err)
	}
	if !found {
		return nil
	}

	for i, webhook := range webhooks {
		webhookMap, ok := webhook.(map[string]interface{})
		if !ok {
			return fmt.Errorf("unexpected webhook entry in %s %s", obj.GetKind(), obj.GetName())
		}

		existing, _, err := unstructured.NestedString(webhookMap, "clientConfig", "caBundle")
		if err != nil {
			return fmt.Errorf("can't read caBundle of %s %s: %w", obj.GetKind(), obj.GetName(), err)
		}
		if existing != "" {
			continue
		}

		err = unstructured.SetNestedField(webhookMap, encodeCABundle(caBundle), "clientConfig", "caBundle")
		if err != nil {
			return fmt.Errorf("can't set caBundle of %s %s: %w", obj.GetKind(), obj.GetName(), err)
		}

		webhooks[i] = webhookMap
	}

	err = unstructured.SetNestedSlice(obj.Object, webhooks, "webhooks")
	if err != nil {
		return fmt.Errorf("can't set webhooks of %s %s: %w", obj.GetKind(), obj.GetName(), err)
	}

	return nil
}

// waitForEstablishedCRDs blocks until the api server serves the provided CustomResourceDefinitions.
// Controllers watching those kinds fail to start while they aren't established yet.
func waitForEstablishedCRDs(ctx context.Context, c client.Client, names []string) error {
	if len(names) == 0 {
		return nil
	}

	logger := log.FromContext(ctx).WithName("bootstrap")
	logger.Info("Waiting for CustomResourceDefinitions to be established", "count", len(names))

	for _, name := range names {
		err := wait.PollUntilContextTimeout(ctx, crdEstablishedPolling, crdEstablishedTimeout, true,
			func(ctx context.Context) (bool, error) {
				crd := &apiextensionsv1.CustomResourceDefinition{}

				err := c.Get(ctx, types.NamespacedName{Name: name}, crd)
				if apierrors.IsNotFound(err) {
					return false, nil
				}
				if err != nil {
					return false, err
				}

				for _, condition := range crd.Status.Conditions {
					if condition.Type == apiextensionsv1.Established {
						return condition.Status == apiextensionsv1.ConditionTrue, nil
					}
				}

				return false, nil
			})
		if err != nil {
			return fmt.Errorf("customresourcedefinition %s was not established: %w", name, err)
		}
	}

	return nil
}

// encodeCABundle returns the representation the api server expects for a caBundle field. The typed
// field is a []byte, which marshals to base64, so an unstructured object has to hold it encoded.
func encodeCABundle(caBundle []byte) string {
	return base64.StdEncoding.EncodeToString(caBundle)
}

// readCABundle returns the PEM encoded CA bundle held by path, or nil when the file doesn't
// exist. A missing file is expected: the operator may serve a certificate whose CA the api server
// already trusts, or the webhook configurations may carry their own bundle.
func readCABundle(path string) ([]byte, error) {
	if path == "" {
		return nil, nil
	}

	content, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("can't read CA bundle: %w", err)
	}

	if len(content) == 0 {
		return nil, nil
	}

	return content, nil
}
