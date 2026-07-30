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
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// RegisterWatches makes the given controller react to changes to the resources it manages in
// target, so drift there is corrected as promptly as it would be in the local cluster.
//
// It does nothing for the local target, whose changes the controller's own Owns() watches already
// cover, and nothing for a target using resync-based drift detection, which trades promptness for
// not holding a watch connection open. Registration is idempotent per target and owner kind.
//
// The first call for a target blocks while the informers it starts fill their caches, so callers
// should treat a failure as retryable rather than fatal.
func (r *Resolver) RegisterWatches(ctx context.Context, target *Target, c controller.Controller, kind string) error {
	if target.IsLocal() || target.cache == nil {
		return nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	key := fmt.Sprintf("%s/%s", target.Name, kind)
	if _, found := r.watched[key]; found {
		return nil
	}

	for _, obj := range ManagedKinds(target) {
		src := source.Kind(target.cache, obj, ownerHandler(kind))
		if err := c.Watch(src); err != nil {
			return fmt.Errorf("can't watch %T in %s: %w", obj, target, err)
		}
	}

	r.watched[key] = struct{}{}

	r.log.Info("watching target cluster for drift", "target", target.Name, "kind", kind)

	return nil
}

// ownerHandler maps a change to a resource in a target cluster back to the custom resource that
// owns it. Where the local path follows an owner reference, this follows the identity labels
// DecorateBuilders stamped on, which is the only link that survives crossing clusters.
func ownerHandler(kind string) handler.TypedEventHandler[client.Object, reconcile.Request] {
	return handler.EnqueueRequestsFromMapFunc(func(_ context.Context, obj client.Object) []reconcile.Request {
		owner, ok := OwnerFromLabels(obj.GetLabels(), kind)
		if !ok {
			return nil
		}

		return []reconcile.Request{{NamespacedName: owner}}
	})
}
