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

package targetcluster

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/alexandrevilain/controller-tools/pkg/discovery"
	"github.com/alexandrevilain/temporal-operator/api/v1beta1"
	internaldiscovery "github.com/alexandrevilain/temporal-operator/internal/discovery"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// ErrTargetClusterNotReady is returned when a referenced target cluster exists but can't be used
// yet. It is worth retrying.
var ErrTargetClusterNotReady = errors.New("target cluster not ready")

// Resolver turns a custom resource's target cluster reference into a usable *Target.
//
// Connections are expensive enough to be worth keeping: each carries a discovery cache and, for
// targets using watch-based drift detection, informers. They're keyed by the name of the
// TemporalTargetCluster and rebuilt when its kubeconfig changes.
type Resolver struct {
	local  *Target
	scheme *runtime.Scheme
	log    logr.Logger

	mu      sync.Mutex
	targets map[types.NamespacedName]*Target
	// watched records which (target, owner kind) pairs already have drift detection watches, so
	// reconciling the same custom resource twice doesn't start a second set of informers.
	watched map[string]struct{}
}

// NewResolver returns a Resolver whose unqualified target is local.
func NewResolver(local *Target, scheme *runtime.Scheme, log logr.Logger) *Resolver {
	return &Resolver{
		local:   local,
		scheme:  scheme,
		log:     log,
		targets: map[types.NamespacedName]*Target{},
		watched: map[string]struct{}{},
	}
}

// NewLocalTarget describes the cluster the manager watches as a Target, so that reconcilers can
// treat "no target reference" as just another target instead of a special case.
func NewLocalTarget(mgr manager.Manager, discoveryMgr discovery.Manager, availableAPIs *internaldiscovery.AvailableAPIs) *Target {
	return &Target{
		Client:        mgr.GetClient(),
		Config:        mgr.GetConfig(),
		Discovery:     discoveryMgr,
		AvailableAPIs: availableAPIs,
		local:         true,
		cache:         mgr.GetCache(),
	}
}

// Local returns the target used by custom resources that don't reference one.
func (r *Resolver) Local() *Target {
	return r.local
}

// For resolves the target cluster the given reference selects, reading the referenced
// TemporalTargetCluster and its kubeconfig with reader. A nil reference resolves to the local
// target, which is what every custom resource written before this feature existed gets.
//
// namespace is the namespace to look the reference up in when it doesn't name one itself.
func (r *Resolver) For(ctx context.Context, reader client.Reader, ref *v1beta1.ObjectReference, namespace string) (*Target, error) {
	if ref == nil || ref.Name == "" {
		return r.local, nil
	}

	name := types.NamespacedName{Namespace: ref.Namespace, Name: ref.Name}
	if name.Namespace == "" {
		name.Namespace = namespace
	}

	targetCluster := &v1beta1.TemporalTargetCluster{}
	if err := reader.Get(ctx, name, targetCluster); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("%w: %s does not exist", ErrTargetClusterNotReady, name)
		}
		return nil, fmt.Errorf("can't get target cluster %s: %w", name, err)
	}

	secret := &corev1.Secret{}
	secretName := types.NamespacedName{Namespace: name.Namespace, Name: targetCluster.Spec.KubeconfigSecretRef.Name}
	if err := reader.Get(ctx, secretName, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("%w: kubeconfig secret %s does not exist", ErrTargetClusterNotReady, secretName)
		}
		return nil, fmt.Errorf("can't get kubeconfig secret %s: %w", secretName, err)
	}

	kubeconfig, ok := secret.Data[targetCluster.GetKey()]
	if !ok {
		return nil, fmt.Errorf("%w: kubeconfig secret %s has no key %q",
			ErrTargetClusterNotReady, secretName, targetCluster.GetKey())
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// The connection is rebuilt whenever the credentials change, which is how a rotated kubeconfig
	// takes effect without restarting the operator.
	if existing, found := r.targets[name]; found {
		if existing.credentialsVersion == secret.ResourceVersion {
			existing.ResyncPeriod = targetCluster.GetResyncPeriod()
			return existing, nil
		}

		r.log.Info("target cluster credentials changed, reconnecting", "target", name)
		r.closeTarget(existing)
		delete(r.targets, name)
	}

	target, err := r.connect(name, targetCluster, kubeconfig, secret.ResourceVersion)
	if err != nil {
		return nil, err
	}

	r.targets[name] = target

	return target, nil
}

// Forget drops any connection held for the named target cluster, stopping its informers. Call it
// when a TemporalTargetCluster is deleted; the next reference to it reconnects from scratch.
func (r *Resolver) Forget(name types.NamespacedName) {
	r.mu.Lock()
	defer r.mu.Unlock()

	target, found := r.targets[name]
	if !found {
		return
	}

	r.closeTarget(target)
	delete(r.targets, name)
}

// connect builds a target's api server connection. The caller must hold r.mu.
func (r *Resolver) connect(name types.NamespacedName, targetCluster *v1beta1.TemporalTargetCluster, kubeconfig []byte, credentialsVersion string) (*Target, error) {
	restConfig, err := RestConfigFromKubeconfig(kubeconfig, targetCluster.Spec.Context)
	if err != nil {
		return nil, fmt.Errorf("%w: %s has an unusable kubeconfig: %w", ErrTargetClusterNotReady, name, err)
	}

	// Resources are read straight from the target's api server rather than through a cache: the
	// operator may only ever touch a handful of namespaces there, and an informer per managed kind
	// would keep far more in memory than those reads cost.
	targetClient, err := client.New(restConfig, client.Options{Scheme: r.scheme})
	if err != nil {
		return nil, fmt.Errorf("%w: can't build a client for %s: %w", ErrTargetClusterNotReady, name, err)
	}

	discoveryMgr, err := discovery.NewManager(restConfig, r.scheme)
	if err != nil {
		return nil, fmt.Errorf("%w: can't discover the apis served by %s: %w", ErrTargetClusterNotReady, name, err)
	}

	availableAPIs, err := internaldiscovery.FindAvailableAPIs(r.log.WithValues("target", name), discoveryMgr)
	if err != nil {
		return nil, fmt.Errorf("%w: can't discover the apis served by %s: %w", ErrTargetClusterNotReady, name, err)
	}

	target := &Target{
		Name:               name,
		Client:             targetClient,
		Config:             restConfig,
		Discovery:          discoveryMgr,
		AvailableAPIs:      availableAPIs,
		ResyncPeriod:       targetCluster.GetResyncPeriod(),
		credentialsVersion: credentialsVersion,
	}

	if targetCluster.Spec.DriftDetection != v1beta1.ResyncTargetClusterDriftDetection {
		if err := r.startCache(target); err != nil {
			return nil, err
		}
	}

	return target, nil
}

// startCache gives a target the cache its drift detection watches read from.
//
// The cache runs on its own context rather than being handed to the manager: the manager can start
// runnables after it has started, but it can't stop one, and this cache has to go away when the
// target's credentials are rotated. Its context is therefore rooted in the background rather than
// in the manager's, and teardown is Resolver's job — on shutdown the process exits, taking any
// still-running cache with it.
func (r *Resolver) startCache(target *Target) error {
	targetCache, err := cache.New(target.Config, cache.Options{Scheme: r.scheme})
	if err != nil {
		return fmt.Errorf("%w: can't build a cache for %s: %w", ErrTargetClusterNotReady, target.Name, err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	target.cache = targetCache
	target.stop = cancel

	go func() {
		if err := targetCache.Start(ctx); err != nil {
			r.log.Error(err, "target cluster cache stopped", "target", target.Name)
		}
	}()

	return nil
}

// closeTarget releases a target's resources. The caller must hold r.mu.
//
// Stopping the cache also silences the drift detection watches reading from it, so their
// registrations are forgotten too: the next reconcile re-registers them against the replacement
// cache.
func (r *Resolver) closeTarget(target *Target) {
	if target.stop != nil {
		target.stop()
	}

	prefix := target.Name.String() + "/"
	for key := range r.watched {
		if strings.HasPrefix(key, prefix) {
			delete(r.watched, key)
		}
	}
}

// RestConfigFromKubeconfig builds an api server connection from a kubeconfig, optionally using a
// named context from it instead of its current one.
func RestConfigFromKubeconfig(kubeconfig []byte, contextName string) (*rest.Config, error) {
	apiConfig, err := clientcmd.Load(kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("can't parse kubeconfig: %w", err)
	}

	if contextName != "" {
		if _, found := apiConfig.Contexts[contextName]; !found {
			return nil, fmt.Errorf("kubeconfig has no context named %q", contextName)
		}
	}

	overrides := &clientcmd.ConfigOverrides{CurrentContext: contextName}

	restConfig, err := clientcmd.NewDefaultClientConfig(*apiConfig, overrides).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("can't build a client config from kubeconfig: %w", err)
	}

	return restConfig, nil
}
