// Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
//
// WSO2 LLC. licenses this file to you under the Apache License,
// Version 2.0 (the "License"); you may not use this file except
// in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

// Command thunder-app-operator reconciles ThunderApplication custom resources
// into OAuth clients on the platform Thunder IdP. It runs a single-replica
// controller-runtime manager (leader election off) that watches all
// namespaces, drives each CR toward its Thunder application via the operator's
// system OAuth2 client, and publishes the assigned client_id back as a
// ConfigMap.
package main

import (
	"os"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	thunderv1alpha1 "github.com/wso2/aep/thunder-app-operator/api/v1alpha1"
	"github.com/wso2/aep/thunder-app-operator/internal/controller"
	"github.com/wso2/aep/thunder-app-operator/internal/thunder"
)

// Environment variables that configure the operator's Thunder admin client.
// The helm chart wires these onto the deployment (see the chart's values.yaml
// and deployment template); defaults keep local `go run` usable against a
// port-forwarded Thunder.
const (
	envThunderAdminURL     = "THUNDER_ADMIN_URL"
	envThunderClientID     = "THUNDER_SYSTEM_CLIENT_ID"
	envThunderClientSecret = "THUNDER_SYSTEM_CLIENT_SECRET"

	defaultThunderAdminURL = "http://thunder-service.thunder.svc.cluster.local:8090"
)

// scheme carries every type the manager's client reads or writes: the
// aep.wso2.com CRD types plus core/v1 (ConfigMaps, Events).
var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(thunderv1alpha1.AddToScheme(scheme))
}

func main() {
	// zap logger, wired as controller-runtime's global logr sink so the
	// reconciler's log.FromContext(ctx) lands in the same stream.
	ctrl.SetLogger(zap.New(zap.UseDevMode(false)))
	setupLog := ctrl.Log.WithName("setup")

	adminURL := getEnvOr(envThunderAdminURL, defaultThunderAdminURL)
	clientID := os.Getenv(envThunderClientID)
	clientSecret := os.Getenv(envThunderClientSecret)
	if clientID == "" || clientSecret == "" {
		setupLog.Error(nil, "missing Thunder system credentials",
			"want", envThunderClientID+" and "+envThunderClientSecret)
		os.Exit(1)
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		// Single replica by design — leader election off keeps the pod from
		// waiting on a lease it would always win.
		LeaderElection: false,
		// Metrics bind is disabled: nothing scrapes it in the local stack and
		// exposing a port would only add surface. Re-enable per real cluster.
		Metrics:                metricsserver.Options{BindAddress: "0"},
		HealthProbeBindAddress: ":8081",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	thunderClient := thunder.New(thunder.Config{
		BaseURL:      adminURL,
		ClientID:     clientID,
		ClientSecret: clientSecret,
	})

	if err := (&controller.Reconciler{
		Client:  mgr.GetClient(),
		Scheme:  mgr.GetScheme(),
		Thunder: thunderClient,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to set up controller", "controller", "ThunderApplication")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting thunder-app-operator", "thunderAdminURL", adminURL)
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "manager exited with error")
		os.Exit(1)
	}
}

// getEnvOr returns the environment variable value or a fallback when unset/empty.
func getEnvOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
