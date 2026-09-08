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

package e2e

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alexandrevilain/temporal-operator/api/v1beta1"
	"github.com/alexandrevilain/temporal-operator/pkg/temporal"
	"go.temporal.io/api/serviceerror"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"

	corev1 "k8s.io/api/core/v1"

	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/klient/wait/conditions"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"
)

const forceDeleteAnnotation = "temporal.io/force-delete"

func TestNamespaceCreation(t *testing.T) {
	var cluster *v1beta1.TemporalCluster
	var temporalNamespace *v1beta1.TemporalNamespace

	namespaceFature := features.New("namespace creation using CRD").
		Setup(func(ctx context.Context, _ *testing.T, cfg *envconf.Config) context.Context {
			namespace := GetNamespaceForFeature(ctx)

			var err error
			cluster, err = deployAndWaitForTemporalWithPostgres(ctx, cfg, namespace)
			if err != nil {
				t.Fatal(err)
			}
			return SetTemporalClusterForFeature(ctx, cluster)
		}).
		Assess("Temporal cluster created", AssertTemporalClusterReady()).
		Assess("Can create a temporal namespace", func(ctx context.Context, _ *testing.T, cfg *envconf.Config) context.Context {
			namespace := GetNamespaceForFeature(ctx)

			// create the temporal cluster client
			temporalNamespace = &v1beta1.TemporalNamespace{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: namespace},
				Spec: v1beta1.TemporalNamespaceSpec{
					ClusterRef: v1beta1.ObjectReference{
						Name: cluster.GetName(),
					},
					RetentionPeriod: &metav1.Duration{Duration: 24 * time.Hour},
				},
			}
			err := cfg.Client().Resources(namespace).Create(ctx, temporalNamespace)
			if err != nil {
				t.Fatal(err)
			}

			return ctx
		}).
		Assess("Namespace exists", func(ctx context.Context, _ *testing.T, cfg *envconf.Config) context.Context {
			connectAddr, closePortForward, err := forwardPortToTemporalFrontend(ctx, cfg, t, cluster)
			if err != nil {
				t.Fatal(err)
			}
			defer closePortForward()

			client := cfg.Client().Resources().GetControllerRuntimeClient()

			nsClient, err := temporal.GetClusterNamespaceClient(ctx, client, cluster, temporal.WithHostPort(connectAddr))
			if err != nil {
				t.Fatal(err)
			}

			err = wait.For(func(ctx context.Context) (done bool, err error) {
				// If no error while describing the namespace, it works.
				_, err = nsClient.Describe(ctx, temporalNamespace.GetName())
				if err != nil {
					var namespaceNotFoundError *serviceerror.NamespaceNotFound
					if errors.As(err, &namespaceNotFoundError) {
						return false, nil
					}

					return false, err
				}

				return true, nil
			}, wait.WithTimeout(5*time.Minute), wait.WithInterval(5*time.Second))
			if err != nil {
				t.Fatal(err)
			}

			return ctx
		}).
		Assess("Namespace can be deleted", func(ctx context.Context, _ *testing.T, cfg *envconf.Config) context.Context {
			namespace := GetNamespaceForFeature(ctx)

			err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				err := cfg.Client().Resources(namespace).Get(ctx, temporalNamespace.GetName(), temporalNamespace.GetNamespace(), temporalNamespace)
				if err != nil {
					return err
				}

				temporalNamespace.Spec.AllowDeletion = true
				return cfg.Client().Resources(namespace).Update(ctx, temporalNamespace)
			})
			if err != nil {
				t.Fatal(err)
			}

			// Wait for controller to set finalizer.
			err = wait.For(func(ctx context.Context) (done bool, err error) {
				err = cfg.Client().Resources(namespace).Get(ctx, temporalNamespace.GetName(), temporalNamespace.GetNamespace(), temporalNamespace)
				if err != nil {
					t.Fatal(err)
				}

				result := controllerutil.ContainsFinalizer(temporalNamespace, "deletion.finalizers.temporal.io")
				return result, nil
			}, wait.WithTimeout(2*time.Minute), wait.WithInterval(1*time.Second))
			if err != nil {
				t.Fatal(err)
			}

			err = cfg.Client().Resources(namespace).Delete(ctx, temporalNamespace)
			if err != nil {
				t.Fatal(err)
			}

			return ctx
		}).
		// Assess("Namespace delete in temporal", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
		// 	connectAddr, closePortForward, err := forwardPortToTemporalFrontend(ctx, cfg, t, cluster)
		// 	if err != nil {
		// 		t.Fatal(err)
		// 	}
		// 	defer closePortForward()

		// 	client := cfg.Client().Resources().GetControllerRuntimeClient()

		// 	tempClient, err := temporal.GetClusterClient(ctx, client, cluster, temporal.WithHostPort(connectAddr))
		// 	if err != nil {
		// 		t.Fatal(err)
		// 	}

		// 	// Wait for the client to return NamespaceNotFound error.
		// 	err = wait.For(func() (done bool, err error) {
		// 		list, err := tempClient.WorkflowService().ListNamespaces(ctx, &workflowservice.ListNamespacesRequest{
		// 			PageSize: 10,
		// 			NamespaceFilter: &namespace.NamespaceFilter{
		// 				IncludeDeleted: true,
		// 			},
		// 		})
		// 		if err != nil {
		// 			return false, err
		// 		}

		// 		t.Logf("Retrieved %d namespaces", len(list.Namespaces))

		// 		for _, namespace := range list.Namespaces {
		// 			if namespace.NamespaceInfo.Name == temporalNamespace.GetName() {
		// 				t.Logf("Found '%s' namespace: %s", temporalNamespace.GetName(), namespace.NamespaceInfo.GetState())
		// 				return namespace.NamespaceInfo.GetState() == enums.NAMESPACE_STATE_DELETED, nil
		// 			}
		// 		}

		// 		t.Logf("Namespace '%s' not found", temporalNamespace.GetName())

		// 		return true, nil
		// 	}, wait.WithTimeout(5*time.Minute), wait.WithInterval(5*time.Second))
		// 	if err != nil {
		// 		t.Fatal(err)
		// 	}

		// 	return ctx
		// }).
		Feature()

	testenv.Test(t, namespaceFature)
}

func TestNamespaceDeletionWhenClusterDoesNotExist(rt *testing.T) {
	feature := features.New("namespace can be deleted when temporal cluster does not exist").
		Setup(func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			namespace := GetNamespaceForFeature(ctx)

			// create TemporalNamespace
			temporalNamespace := &v1beta1.TemporalNamespace{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: namespace},
				Spec: v1beta1.TemporalNamespaceSpec{
					ClusterRef: v1beta1.ObjectReference{
						Name: doesNotExistName,
					},
					RetentionPeriod: &metav1.Duration{Duration: 24 * time.Hour},
				},
			}
			err := c.Client().Resources(namespace).Create(ctx, temporalNamespace)
			if err != nil {
				t.Fatal(err)
			}
			return SetTemporalNamespaceForFeature(ctx, temporalNamespace)
		}).
		Assess("TemporalCluster does not exist", func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			var temporalCluster = &v1beta1.TemporalCluster{}
			err := c.Client().Resources().Get(ctx, doesNotExistName, GetNamespaceForFeature(ctx), temporalCluster)
			if err == nil {
				t.Fatalf("found cluster: %v", temporalCluster)
			}

			return ctx
		}).
		Assess("TemporalNamespace can be deleted", func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			err := c.Client().Resources(GetNamespaceForFeature(ctx)).Delete(ctx, GetTemporalNamespaceForFeature(ctx))
			if err != nil {
				t.Fatalf("failed to delete namespace: %v", err)
			}
			return ctx
		}).
		Feature()

	testenv.Test(rt, feature)
}

func TestNamespaceDeletionWhenClusterDeleted(rt *testing.T) {
	feature := features.New("namespace can be deleted after a temporal cluster associated with it is also deleted").
		Setup(func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			// create TemporalCluster
			namespace := GetNamespaceForFeature(ctx)

			cluster, err := deployAndWaitForTemporalWithPostgres(ctx, c, namespace)
			if err != nil {
				t.Fatal(err)
			}
			return SetTemporalClusterForFeature(ctx, cluster)
		}).
		Setup(func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			// create TemporalNamespace
			namespace := GetNamespaceForFeature(ctx)
			cluster := GetTemporalClusterForFeature(ctx)

			temporalNamespace := &v1beta1.TemporalNamespace{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: namespace},
				Spec: v1beta1.TemporalNamespaceSpec{
					ClusterRef: v1beta1.ObjectReference{
						Name: cluster.GetName(),
					},
					RetentionPeriod: &metav1.Duration{Duration: 24 * time.Hour},
				},
			}
			err := c.Client().Resources(namespace).Create(ctx, temporalNamespace)
			if err != nil {
				t.Fatal(err)
			}
			return SetTemporalNamespaceForFeature(ctx, temporalNamespace)
		}).
		Assess("TemporalCluster can be deleted", func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			err := c.Client().Resources(GetNamespaceForFeature(ctx)).Delete(ctx, GetTemporalClusterForFeature(ctx))
			if err != nil {
				t.Fatalf("failed to delete: %v", err)
			}
			return ctx
		}).
		Assess("TemporalNamespace can be deleted", func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			err := c.Client().Resources(GetNamespaceForFeature(ctx)).Delete(ctx, GetTemporalNamespaceForFeature(ctx))
			if err != nil {
				t.Fatalf("failed to delete: %v", err)
			}
			return ctx
		}).
		Feature()

	testenv.Test(rt, feature)
}

// waitForNamespaceFinalizer waits for the operator to take ownership of the namespace's deletion.
func waitForNamespaceFinalizer(ctx context.Context, cfg *envconf.Config, temporalNamespace *v1beta1.TemporalNamespace) error {
	return wait.For(func(ctx context.Context) (bool, error) {
		err := cfg.Client().Resources(temporalNamespace.GetNamespace()).Get(ctx, temporalNamespace.GetName(), temporalNamespace.GetNamespace(), temporalNamespace)
		if err != nil {
			return false, err
		}

		return controllerutil.ContainsFinalizer(temporalNamespace, deletionFinalizer), nil
	}, wait.WithTimeout(2*time.Minute), wait.WithInterval(time.Second))
}

// waitForNamespaceDeleted waits for the object to actually go away, which only happens once the
// operator has dropped its finalizer: a namespace stuck on that finalizer never gets here.
func waitForNamespaceDeleted(ctx context.Context, cfg *envconf.Config, temporalNamespace *v1beta1.TemporalNamespace, timeout time.Duration) error {
	cond := conditions.New(cfg.Client().Resources()).ResourceDeleted(temporalNamespace)

	return wait.For(cond, wait.WithTimeout(timeout), wait.WithInterval(2*time.Second))
}

// TestNamespaceDeletionAndRecreation walks the whole lifecycle twice under the same name: a
// deletion that leaves the object stuck on its finalizer, or that leaves anything behind on the
// temporal server, shows up as the second creation never becoming ready.
func TestNamespaceDeletionAndRecreation(rt *testing.T) {
	const namespaceName = "recreated"

	feature := features.New("namespace can be created, deleted then created again").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			namespace := GetNamespaceForFeature(ctx)

			cluster, err := deployAndWaitForTemporalWithPostgres(ctx, cfg, namespace)
			if err != nil {
				t.Fatal(err)
			}

			return SetTemporalClusterForFeature(ctx, cluster)
		}).
		Assess("Temporal cluster created", AssertTemporalClusterReady()).
		Assess("Can create a deletable temporal namespace", createDeletableNamespace(namespaceName)).
		Assess("Namespace is ready", AssertTemporalNamespaceReady()).
		Assess("Namespace holds the deletion finalizer", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			if err := waitForNamespaceFinalizer(ctx, cfg, GetTemporalNamespaceForFeature(ctx)); err != nil {
				t.Fatal(err)
			}

			return ctx
		}).
		Assess("Namespace can be deleted", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			temporalNamespace := GetTemporalNamespaceForFeature(ctx)

			err := cfg.Client().Resources(GetNamespaceForFeature(ctx)).Delete(ctx, temporalNamespace)
			if err != nil {
				t.Fatalf("failed to delete namespace: %v", err)
			}

			// The object going away is the assertion: it can only happen once the operator has
			// deleted the namespace on the temporal server and dropped its own finalizer.
			if err := waitForNamespaceDeleted(ctx, cfg, temporalNamespace, 5*time.Minute); err != nil {
				t.Fatalf("namespace was not released: %v", err)
			}

			return ctx
		}).
		Assess("Namespace can be created again under the same name", createDeletableNamespace(namespaceName)).
		Assess("Re-created namespace is ready", AssertTemporalNamespaceReady()).
		Assess("Re-created namespace can be deleted", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			temporalNamespace := GetTemporalNamespaceForFeature(ctx)

			if err := waitForNamespaceFinalizer(ctx, cfg, temporalNamespace); err != nil {
				t.Fatal(err)
			}

			err := cfg.Client().Resources(GetNamespaceForFeature(ctx)).Delete(ctx, temporalNamespace)
			if err != nil {
				t.Fatalf("failed to delete namespace: %v", err)
			}

			if err := waitForNamespaceDeleted(ctx, cfg, temporalNamespace, 5*time.Minute); err != nil {
				t.Fatalf("namespace was not released: %v", err)
			}

			return ctx
		}).
		Feature()

	testenv.Test(rt, feature)
}

// createDeletableNamespace creates a namespace the operator is allowed to delete, and puts it in
// the feature's context.
func createDeletableNamespace(name string) features.Func {
	return func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
		namespace := GetNamespaceForFeature(ctx)
		cluster := GetTemporalClusterForFeature(ctx)

		temporalNamespace := &v1beta1.TemporalNamespace{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
			Spec: v1beta1.TemporalNamespaceSpec{
				ClusterRef:      v1beta1.ObjectReference{Name: cluster.GetName()},
				RetentionPeriod: &metav1.Duration{Duration: 24 * time.Hour},
				AllowDeletion:   true,
			},
		}
		if err := cfg.Client().Resources(namespace).Create(ctx, temporalNamespace); err != nil {
			t.Fatal(err)
		}

		return SetTemporalNamespaceForFeature(ctx, temporalNamespace)
	}
}

// TestNamespaceDeletionWhenClusterIsNotReady covers the namespace whose cluster exists but never
// becomes healthy — the state the deletion path used to be unreachable in, leaving the object
// stuck on its finalizer with hand-editing the only way out.
func TestNamespaceDeletionWhenClusterIsNotReady(rt *testing.T) {
	feature := features.New("namespace holding the deletion finalizer can be released when its cluster is not ready").
		Setup(func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			namespace := GetNamespaceForFeature(ctx)

			// No postgres is deployed for this cluster, so it never reaches a ready state and its
			// frontend never answers.
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "postgres-password", Namespace: namespace},
				StringData: map[string]string{"PASSWORD": "temporal"},
			}
			if err := c.Client().Resources(namespace).Create(ctx, secret); err != nil {
				t.Fatal(err)
			}

			cluster := &v1beta1.TemporalCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "unhealthy", Namespace: namespace},
				Spec: v1beta1.TemporalClusterSpec{
					NumHistoryShards: 1,
					Version:          defaultVersion,
					Persistence: v1beta1.TemporalPersistenceSpec{
						DefaultStore:    unreachableDatastore("temporal"),
						VisibilityStore: unreachableDatastore("temporal_visibility"),
					},
				},
			}
			if err := c.Client().Resources(namespace).Create(ctx, cluster); err != nil {
				t.Fatal(err)
			}

			return SetTemporalClusterForFeature(ctx, cluster)
		}).
		Setup(func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			namespace := GetNamespaceForFeature(ctx)

			// The finalizer is set up front: it is what a namespace carries once it has been
			// reconciled, and the operator cannot add it while its cluster is unhealthy.
			temporalNamespace := &v1beta1.TemporalNamespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test",
					Namespace:  namespace,
					Finalizers: []string{deletionFinalizer},
				},
				Spec: v1beta1.TemporalNamespaceSpec{
					ClusterRef:      v1beta1.ObjectReference{Name: GetTemporalClusterForFeature(ctx).GetName()},
					RetentionPeriod: &metav1.Duration{Duration: 24 * time.Hour},
					AllowDeletion:   true,
				},
			}
			if err := c.Client().Resources(namespace).Create(ctx, temporalNamespace); err != nil {
				t.Fatal(err)
			}

			return SetTemporalNamespaceForFeature(ctx, temporalNamespace)
		}).
		Assess("TemporalCluster is not ready", func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			cluster := GetTemporalClusterForFeature(ctx)

			err := c.Client().Resources().Get(ctx, cluster.GetName(), GetNamespaceForFeature(ctx), cluster)
			if err != nil {
				t.Fatal(err)
			}

			if cluster.IsReady() {
				t.Fatal("cluster became ready, the test cannot cover the unhealthy case")
			}

			return ctx
		}).
		Assess("Deletion is not silently skipped while the namespace may still exist on the server", func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			temporalNamespace := GetTemporalNamespaceForFeature(ctx)

			err := c.Client().Resources(GetNamespaceForFeature(ctx)).Delete(ctx, temporalNamespace)
			if err != nil {
				t.Fatalf("failed to delete namespace: %v", err)
			}

			// The frontend is unreachable, so the operator cannot confirm the temporal-side
			// deletion and must keep the finalizer rather than drop it on a guess.
			err = waitForNamespaceDeleted(ctx, c, temporalNamespace.DeepCopy(), 30*time.Second)
			if err == nil {
				t.Fatal("namespace was released without the temporal-side deletion")
			}

			return ctx
		}).
		Assess("Force delete annotation releases the namespace", func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			namespace := GetNamespaceForFeature(ctx)
			temporalNamespace := GetTemporalNamespaceForFeature(ctx)

			err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				err := c.Client().Resources(namespace).Get(ctx, temporalNamespace.GetName(), namespace, temporalNamespace)
				if err != nil {
					return err
				}

				annotations := temporalNamespace.GetAnnotations()
				if annotations == nil {
					annotations = map[string]string{}
				}
				annotations[forceDeleteAnnotation] = "true"
				temporalNamespace.SetAnnotations(annotations)

				return c.Client().Resources(namespace).Update(ctx, temporalNamespace)
			})
			if err != nil {
				t.Fatal(err)
			}

			if err := waitForNamespaceDeleted(ctx, c, temporalNamespace, 2*time.Minute); err != nil {
				t.Fatalf("namespace was not released: %v", err)
			}

			return ctx
		}).
		Feature()

	testenv.Test(rt, feature)
}

// unreachableDatastore describes a database nothing is listening on.
func unreachableDatastore(name string) *v1beta1.DatastoreSpec {
	return &v1beta1.DatastoreSpec{
		SQL: &v1beta1.SQLSpec{
			User:            "temporal",
			PluginName:      "postgres12",
			DatabaseName:    name,
			ConnectAddr:     "postgres.does-not-exist:5432",
			ConnectProtocol: "tcp",
		},
		PasswordSecretRef: &v1beta1.SecretKeyReference{
			Name: "postgres-password",
			Key:  "PASSWORD",
		},
	}
}
