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

package main

import (
	"context"
	"flag"
	"os"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	istionetworkingv1beta1 "istio.io/client-go/pkg/apis/networking/v1beta1"
	istiosecurityv1beta1 "istio.io/client-go/pkg/apis/security/v1beta1"

	"github.com/alexandrevilain/controller-tools/pkg/discovery"
	temporaliov1beta1 "github.com/alexandrevilain/temporal-operator/api/v1beta1"
	"github.com/alexandrevilain/temporal-operator/controllers"
	"github.com/alexandrevilain/temporal-operator/internal/bootstrap"
	internaldiscovery "github.com/alexandrevilain/temporal-operator/internal/discovery"
	"github.com/alexandrevilain/temporal-operator/internal/targetcluster"
	"github.com/alexandrevilain/temporal-operator/webhooks"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	//+kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(certmanagerv1.AddToScheme(scheme))
	utilruntime.Must(istiosecurityv1beta1.AddToScheme(scheme))
	utilruntime.Must(istionetworkingv1beta1.AddToScheme(scheme))
	utilruntime.Must(temporaliov1beta1.AddToScheme(scheme))
	utilruntime.Must(monitoringv1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	var (
		metricsAddr             string
		enableLeaderElection    bool
		probeAddr               string
		kubeconfigContext       string
		leaderElectionNamespace string
		bootstrapManifestsDir   string
		bootstrapCABundleFile   string
	)

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")
	// Note: the "kubeconfig" flag is registered by controller-runtime itself, see
	// sigs.k8s.io/controller-runtime/pkg/client/config.
	flag.StringVar(&kubeconfigContext, "kubeconfig-context", "",
		"The context to use from the kubeconfig. Defaults to its current-context.")
	flag.StringVar(&leaderElectionNamespace, "leader-election-namespace", "",
		"The namespace holding the leader election lease. Defaults to the namespace the operator runs in, "+
			"which is only correct when the watched cluster is the one hosting the operator. It must be set when "+
			"the operator watches a remote cluster.")
	flag.StringVar(&bootstrapManifestsDir, "bootstrap-manifests-dir", "",
		"Directory holding manifests to apply to the watched cluster on startup, before controllers start. "+
			"Used to install the operator's CustomResourceDefinitions and admission webhook configurations into a "+
			"remote cluster. Disabled when empty.")
	flag.StringVar(&bootstrapCABundleFile, "bootstrap-ca-bundle-file", "/tmp/k8s-webhook-server/serving-certs/ca.crt",
		"File holding the PEM encoded CA bundle injected into bootstrapped admission webhook configurations "+
			"that don't already carry one. Ignored when it doesn't exist.")

	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	restConfig, err := config.GetConfigWithContext(kubeconfigContext)
	if err != nil {
		setupLog.Error(err, "unable to load kubeconfig")
		os.Exit(1)
	}

	// The watched cluster may not know about our CRDs and webhooks yet. Bootstrap them before the
	// manager starts, so API discovery and the controllers' watches see them.
	if bootstrapManifestsDir != "" {
		err := bootstrap.Apply(context.Background(), restConfig, bootstrapManifestsDir, bootstrapCABundleFile)
		if err != nil {
			setupLog.Error(err, "unable to bootstrap the watched cluster")
			os.Exit(1)
		}
	}

	mgr, err := ctrl.NewManager(restConfig, ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: metricsAddr,
		},
		HealthProbeBindAddress:  probeAddr,
		LeaderElection:          enableLeaderElection,
		LeaderElectionID:        "0cfcfa11.temporal.io",
		LeaderElectionNamespace: leaderElectionNamespace,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	discoveryManager, err := discovery.NewManager(mgr.GetConfig(), scheme)
	if err != nil {
		setupLog.Error(err, "unable to discover available apis")
		os.Exit(1)
	}

	availableAPIs, err := internaldiscovery.FindAvailableAPIs(setupLog, discoveryManager)
	if err != nil {
		setupLog.Error(err, "unable to discover available apis")
		os.Exit(1)
	}

	// The local target is the cluster the manager watches. Custom resources that don't name a target
	// cluster resolve to it, which is what keeps single-cluster operation unchanged.
	resolver := targetcluster.NewResolver(
		targetcluster.NewLocalTarget(mgr, discoveryManager, availableAPIs),
		scheme,
		ctrl.Log.WithName("targetcluster"),
	)

	if err = (&controllers.TemporalClusterReconciler{
		// GetEventRecorderFor is deprecated in controller-runtime v0.23 in favour of
		// GetEventRecorder, but the replacement returns events.EventRecorder rather
		// than record.EventRecorder. Those are different interfaces and switching
		// moves event emission from the core v1 API group to events.k8s.io/v1, which
		// is an observable behaviour change. Deferred to its own change.
		Base:          controllers.New(mgr.GetClient(), mgr.GetScheme(), mgr.GetEventRecorderFor("cluster-controller"), discoveryManager), //nolint:staticcheck // SA1019: see note above.
		AvailableAPIs: availableAPIs,
		Resolver:      resolver,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Cluster")
		os.Exit(1)
	}

	if err = (&webhooks.TemporalClusterWebhook{
		AvailableAPIs: availableAPIs,
	}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "TemporalCluster")
		os.Exit(1)
	}

	if err = (&controllers.TemporalClusterClientReconciler{
		// GetEventRecorderFor is deprecated in controller-runtime v0.23 in favour of
		// GetEventRecorder, but the replacement returns events.EventRecorder rather
		// than record.EventRecorder. Those are different interfaces and switching
		// moves event emission from the core v1 API group to events.k8s.io/v1, which
		// is an observable behaviour change. Deferred to its own change.
		Base:          controllers.New(mgr.GetClient(), mgr.GetScheme(), mgr.GetEventRecorderFor("clusterclient-controller"), discoveryManager), //nolint:staticcheck // SA1019: see note above.
		AvailableAPIs: availableAPIs,
		Resolver:      resolver,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ClusterClient")
		os.Exit(1)
	}

	if err = (&controllers.TemporalTargetClusterReconciler{
		// GetEventRecorderFor is deprecated in controller-runtime v0.23 in favour of
		// GetEventRecorder, but the replacement returns events.EventRecorder rather
		// than record.EventRecorder. Those are different interfaces and switching
		// moves event emission from the core v1 API group to events.k8s.io/v1, which
		// is an observable behaviour change. Deferred to its own change.
		Base:     controllers.New(mgr.GetClient(), mgr.GetScheme(), mgr.GetEventRecorderFor("targetcluster-controller"), discoveryManager), //nolint:staticcheck // SA1019: see note above.
		Resolver: resolver,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "TargetCluster")
		os.Exit(1)
	}

	if err = (&controllers.TemporalNamespaceReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Resolver: resolver,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Namespace")
		os.Exit(1)
	}

	if err = (&controllers.TemporalScheduleReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Resolver: resolver,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Schedule")
		os.Exit(1)
	}
	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
