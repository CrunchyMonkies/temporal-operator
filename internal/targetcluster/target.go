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

// Package targetcluster lets the operator create resources in a kubernetes cluster other than the
// one it watches.
//
// Every reconcile resolves a Target: either the local one, meaning the cluster the operator watches
// and the custom resource lives in, or a remote one described by a TemporalTargetCluster. Callers
// take the same code path in both cases; what differs is which client the resources are written
// through, and whether they can carry an owner reference.
package targetcluster

import (
	"context"
	"time"

	"github.com/alexandrevilain/controller-tools/pkg/discovery"
	internaldiscovery "github.com/alexandrevilain/temporal-operator/internal/discovery"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Target is a kubernetes cluster the operator creates resources in.
type Target struct {
	// Name identifies the TemporalTargetCluster describing this target. It is empty for the local
	// target, which no resource describes.
	Name types.NamespacedName
	// Client talks to the target's api server. It reads straight from the api server rather than
	// through a cache: a target may only ever be reconciled a handful of times, so keeping an
	// informer per kind warm for it would cost more than it saves.
	Client client.Client
	// Config is the target's api server connection, for the few callers that need to build their
	// own client from it.
	Config *rest.Config
	// Discovery answers whether an optional API is served by this target.
	Discovery discovery.Manager
	// AvailableAPIs records which optional APIs this target serves. A TemporalCluster relying on
	// one that isn't available here can't be fully reconciled.
	AvailableAPIs *internaldiscovery.AvailableAPIs
	// ResyncPeriod is how often resources managed here should be reconciled even when nothing
	// happened, or 0 when drift is noticed through watches instead.
	ResyncPeriod time.Duration

	// local records whether this is the cluster the operator watches. Resources created in the
	// local target can own each other; resources created elsewhere can't.
	local bool
	// cache backs the drift detection watches, and is nil unless they're in use.
	cache cache.Cache
	// credentialsVersion is the resourceVersion of the Secret this target's connection was built
	// from, so rotating the kubeconfig rebuilds the connection.
	credentialsVersion string
	// stop tears down this target's cache.
	stop context.CancelFunc
}

// IsLocal reports whether this target is the cluster the operator watches, which is also the
// cluster the custom resources live in.
//
// It is the deciding fact for ownership: an owner reference is only meaningful inside a single
// cluster. Pointed at a custom resource living in another one, the target's garbage collector
// resolves it to nothing and deletes the resource as orphaned.
func (t *Target) IsLocal() bool {
	return t.local
}

// Cache returns the cache backing this target's drift detection watches, or nil when it has none.
func (t *Target) Cache() cache.Cache {
	return t.cache
}

// String returns a name for this target usable in logs and error messages.
func (t *Target) String() string {
	if t.local {
		return "local"
	}
	return t.Name.String()
}
