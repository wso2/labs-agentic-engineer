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

package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"k8s.io/client-go/kubernetes"

	"github.com/wso2/aep/aectl/internal/thunder"
)

// registerThunderFlags adds Thunder connection flags to cmd and binds each one
// to its corresponding Viper key. Precedence: flag > AEP_* env var > cluster ConfigMap > default.
func registerThunderFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.String("thunder-namespace", "", "Kubernetes namespace where Thunder is installed")
	f.String("thunder-url", "", "In-cluster URL of the Thunder service")
	f.String("thunder-config-map", "", "Name of Thunder's runtime ConfigMap")
	f.String("thunder-deployment", "", "Name of Thunder's Deployment")
	f.String("thunder-public-url", "", "Public URL of Thunder — must match the JWT issuer configured in Thunder")

	_ = viper.BindPFlag("thunder.namespace", f.Lookup("thunder-namespace"))
	_ = viper.BindPFlag("thunder.url", f.Lookup("thunder-url"))
	_ = viper.BindPFlag("thunder.config_map", f.Lookup("thunder-config-map"))
	_ = viper.BindPFlag("thunder.deployment", f.Lookup("thunder-deployment"))
	_ = viper.BindPFlag("thunder.public_url", f.Lookup("thunder-public-url"))
}

// doThunderSetup waits for all platform ThunderApplication CRs (deployed by
// the platform Helm chart) to be reconciled by the thunder-app-operator, then
// patches Thunder's CORS configuration.
//
// Client registration is now driven declaratively: Helm applies the
// ThunderApplication CRs and the operator reconciles them against Thunder's
// admin API using the PE-provisioned system client credentials. This function
// only waits for that to complete.
func doThunderSetup(
	ctx context.Context,
	k8sClient *kubernetes.Clientset,
	platformNamespace, thunderNamespace, consoleURL, thunderConfigMap, thunderDeployment string,
) error {
	step := func(msg string) { _, _ = fmt.Fprintf(os.Stdout, "  %s\n", msg) }

	// Poll ThunderApplication CR statuses until all are ready.
	_, _ = fmt.Fprintf(os.Stdout, "Waiting for Thunder OAuth clients to be provisioned")
	timeout := 5 * time.Minute
	deadline := time.Now().Add(timeout)
	var lastKubectlErr error
	for {
		args := kubectlArgs("get", "thunderapplications",
			"-n", platformNamespace,
			"-o", `jsonpath={range .items[*]}{.metadata.name}=={.status.ready}{"\n"}{end}`)
		out, err := exec.CommandContext(ctx, "kubectl", args...).Output()
		if err != nil {
			lastKubectlErr = err
		} else {
			lastKubectlErr = nil
			if len(strings.TrimSpace(string(out))) > 0 {
				lines := strings.Split(strings.TrimSpace(string(out)), "\n")
				allReady := true
				for _, line := range lines {
					if !strings.HasSuffix(line, "==true") {
						allReady = false
						break
					}
				}
				if allReady {
					_, _ = fmt.Fprintln(os.Stdout, " ready")
					break
				}
			}
		}
		if time.Now().After(deadline) {
			_, _ = fmt.Fprintln(os.Stdout)
			if lastKubectlErr != nil {
				return fmt.Errorf("timed out after %s waiting for ThunderApplications in namespace %s: last kubectl error: %w",
					timeout, platformNamespace, lastKubectlErr)
			}
			return fmt.Errorf("timed out after %s waiting for ThunderApplications to be ready in namespace %s",
				timeout, platformNamespace)
		}
		_, _ = fmt.Fprintf(os.Stdout, ".")
		select {
		case <-ctx.Done():
			_, _ = fmt.Fprintln(os.Stdout)
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}

	// Patch Thunder's CORS configuration so the console can make browser-side
	// OAuth requests. This still uses direct k8s access (ConfigMap + Deployment
	// restart) rather than Thunder's API.
	step("Patching Thunder CORS configuration")
	if err := thunder.PatchCORS(ctx, k8sClient, consoleURL, thunderNamespace, thunderConfigMap, thunderDeployment); err != nil {
		step(fmt.Sprintf("Warning: CORS patch failed (%v). Add %s manually if needed.", err, consoleURL))
	} else {
		step("Thunder CORS configured")
	}

	step("Thunder setup complete")
	return nil
}

// kubectlArgs prepends --kubeconfig if a non-default kubeconfig is configured.
func kubectlArgs(args ...string) []string {
	if kubeconfig != "" {
		return append([]string{"--kubeconfig", kubeconfig}, args...)
	}
	return args
}
