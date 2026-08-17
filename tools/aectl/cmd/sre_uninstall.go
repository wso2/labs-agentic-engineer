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
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	k8s "github.com/wso2/aep/aectl/internal/kubernetes"
)

var (
	sreUninstallObsNamespace string
	sreUninstallYes          bool
)

var sreUninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove what aep sre install deployed",
	Long: `Reverses exactly what aep sre install did:

  1. helm uninstall observability-logs-opensearch
  2. helm uninstall observability-plane
  3. Deletes cluster-scoped CRs (ClusterObservabilityPlane, ClusterAuthzRole,
     ClusterAuthzRoleBinding) that survive namespace deletion
  4. Deletes the observability namespace and all namespaced resources inside it
     (ESO SecretStore/ExternalSecrets, synced Secrets, ConfigMaps, etc.)

The AEP platform namespace (wso2-aep), OpenBao, the ESO controller, and all
AEP platform resources are not affected. Only ESO SecretStore and ExternalSecret
resources in the observability namespace are removed as part of step 4.`,
	RunE: runSreUninstall,
}

func init() {
	sreCmd.AddCommand(sreUninstallCmd)
	sreUninstallCmd.Flags().StringVar(&sreUninstallObsNamespace, "obs-namespace", "openchoreo-observability-plane", "Observability plane namespace to remove")
	sreUninstallCmd.Flags().BoolVarP(&sreUninstallYes, "yes", "y", false, "Skip confirmation prompt")
}

func runSreUninstall(cmd *cobra.Command, args []string) error {
	if !sreUninstallYes {
		fmt.Printf("This will permanently remove the SRE observability plane from namespace %q.\n", sreUninstallObsNamespace)
		fmt.Printf("The AEP platform, OpenBao, and ESO will NOT be affected.\n")
		fmt.Print("Type \"yes\" to confirm: ")
		scanner := bufio.NewScanner(os.Stdin)
		scanner.Scan()
		if strings.TrimSpace(scanner.Text()) != "yes" {
			fmt.Println("Aborted.")
			return nil
		}
	}

	ctx := context.Background()
	client, err := k8s.NewClient(kubeconfig)
	if err != nil {
		return fmt.Errorf("connect to cluster: %w", err)
	}
	applier, err := k8s.NewApplier(kubeconfig)
	if err != nil {
		return fmt.Errorf("build applier: %w", err)
	}

	warn := func(msg string) { fmt.Printf("  warning: %s\n", msg) }
	step := func(msg string) { fmt.Printf("  %s\n", msg) }

	// 1. Helm uninstall — logs-opensearch first (depends on opensearch).
	for _, release := range []string{"observability-logs-opensearch", "observability-plane"} {
		fmt.Printf("helm uninstall %s -n %s...\n", release, sreUninstallObsNamespace)
		out, err := exec.CommandContext(ctx, "helm", "uninstall", release, "-n", sreUninstallObsNamespace).CombinedOutput()
		if err != nil {
			warn(fmt.Sprintf("helm uninstall %s: %v — %s", release, err, strings.TrimSpace(string(out))))
		} else {
			step(fmt.Sprintf("helm uninstall %s: done", release))
		}
	}

	// 2. Cluster-scoped CRs — these survive namespace deletion and must be
	//    removed explicitly. They were created by the sreCRsTmpl apply in
	//    aep sre install (step 6).
	fmt.Println("Deleting cluster-scoped CRs...")
	for _, cr := range []struct{ apiVersion, kind, name string }{
		{"openchoreo.dev/v1alpha1", "ClusterObservabilityPlane", "default"},
		{"openchoreo.dev/v1alpha1", "ClusterAuthzRoleBinding", "aep-observer-reader-binding"},
		{"openchoreo.dev/v1alpha1", "ClusterAuthzRole", "aep-observer-reader"},
		{"openchoreo.dev/v1alpha1", "ClusterAuthzRoleBinding", "rca-agent-dispatch-binding"},
		{"openchoreo.dev/v1alpha1", "ClusterAuthzRole", "rca-agent-dispatch"},
	} {
		if err := applier.Delete(ctx, cr.apiVersion, cr.kind, "", cr.name); err != nil {
			warn(fmt.Sprintf("delete %s/%s: %v", cr.kind, cr.name, err))
		} else {
			step(fmt.Sprintf("deleted %s/%s", cr.kind, cr.name))
		}
	}

	// 3. Delete the observability namespace. This removes everything namespaced:
	//    ESO SecretStore + ExternalSecrets, synced Secrets (opensearch-admin-credentials,
	//    rca-agent-secret, observer-secret), cluster-gateway-ca ConfigMap, ConfigMap
	//    patches (observer-config, rca-agent-config), HTTPRoute, and any leftover Jobs.
	fmt.Printf("Deleting namespace %s...\n", sreUninstallObsNamespace)
	if err := client.CoreV1().Namespaces().Delete(ctx, sreUninstallObsNamespace, metav1.DeleteOptions{}); err != nil {
		warn(fmt.Sprintf("delete namespace %s: %v", sreUninstallObsNamespace, err))
	} else {
		step(fmt.Sprintf("namespace %s deleted", sreUninstallObsNamespace))
	}

	fmt.Println("\nDone. AEP platform, OpenBao, and the ESO controller are still running.")
	return nil
}
