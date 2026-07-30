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

package controllers

import (
	"github.com/alexandrevilain/controller-tools/pkg/discovery"
	"github.com/alexandrevilain/controller-tools/pkg/reconciler"
	"github.com/alexandrevilain/temporal-operator/internal/targetcluster"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Base struct {
	// Client reads and writes the custom resources themselves, and so always addresses the cluster
	// the operator watches — never a target cluster, which holds no custom resources.
	client.Client
	Scheme *runtime.Scheme
	// Recorder emits events onto the custom resources, and so also belongs to the watched cluster.
	Recorder record.EventRecorder

	Jobs       *reconciler.JosbReconciler
	Reconciler *reconciler.Reconciler
}

func New(crclient client.Client, scheme *runtime.Scheme, recorder record.EventRecorder, discoveryMgr discovery.Manager) Base {
	return Base{
		Client:   crclient,
		Scheme:   scheme,
		Recorder: recorder,
		Jobs: &reconciler.JosbReconciler{
			Client:   crclient,
			Scheme:   scheme,
			Recorder: recorder,
		},
		Reconciler: &reconciler.Reconciler{
			Client:    crclient,
			Scheme:    scheme,
			Recorder:  recorder,
			Discovery: discoveryMgr,
		},
	}
}

// forTarget returns a copy of b whose resource and job reconcilers write to the given target
// cluster instead of the watched one.
//
// Client and Recorder are deliberately left alone: the custom resource being reconciled, its
// status and its events all live in the watched cluster no matter where its resources go. For the
// local target this is a no-op in effect — the target's client *is* the watched cluster's — which
// is what lets callers use it unconditionally.
func (b Base) forTarget(target *targetcluster.Target) Base {
	if target.IsLocal() {
		return b
	}

	return Base{
		Client:   b.Client,
		Scheme:   b.Scheme,
		Recorder: b.Recorder,
		Jobs: &reconciler.JosbReconciler{
			Client:   target.Client,
			Scheme:   b.Scheme,
			Recorder: b.Recorder,
		},
		Reconciler: &reconciler.Reconciler{
			Client:    target.Client,
			Scheme:    b.Scheme,
			Recorder:  b.Recorder,
			Discovery: target.Discovery,
		},
	}
}
