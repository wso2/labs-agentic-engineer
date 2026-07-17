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

	k8s "github.com/wso2/aep/aepctl/internal/kubernetes"
)

var (
	uninstallNamespace        string
	uninstallBootstrapRelease string
	uninstallPlatformRelease  string
	uninstallObsNamespace     string
	uninstallYes              bool
)

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove AEP from the cluster",
	Long: `Uninstalls both Helm releases, deletes PVCs, removes cluster-scoped RBAC,
and deletes the AEP namespace. This is destructive and irreversible.`,
	RunE: runUninstall,
}

func init() {
	platformCmd.AddCommand(uninstallCmd)
	uninstallCmd.Flags().StringVar(&uninstallNamespace, "namespace", "wso2-aep", "Kubernetes namespace where AEP is installed")
	uninstallCmd.Flags().StringVar(&uninstallBootstrapRelease, "bootstrap-release", "aep", "Helm release name of the bootstrap chart")
	uninstallCmd.Flags().StringVar(&uninstallPlatformRelease, "platform-release", "aep-platform", "Helm release name of the platform chart")
	uninstallCmd.Flags().StringVar(&uninstallObsNamespace, "obs-namespace", "openchoreo-observability-plane", "Namespace of the SRE/observability plane (installed by `aep sre install`)")
	uninstallCmd.Flags().BoolVarP(&uninstallYes, "yes", "y", false, "Skip confirmation prompt")
}

func runUninstall(cmd *cobra.Command, args []string) error {
	if !uninstallYes {
		fmt.Printf("This will permanently delete the AEP installation in namespace %q\n", uninstallNamespace)
		fmt.Printf("and the SRE/observability plane in namespace %q.\n", uninstallObsNamespace)
		fmt.Printf("Helm releases: %q, %q, observability-plane, observability-logs-opensearch\n", uninstallBootstrapRelease, uninstallPlatformRelease)
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

	step := func(msg string) { fmt.Printf("  %s\n", msg) }
	warn := func(msg string) { fmt.Printf("  warning: %s\n", msg) }

	fmt.Println("Uninstalling AEP...")

	// 1. Helm uninstall — platform first, then bootstrap.
	for _, release := range []string{uninstallPlatformRelease, uninstallBootstrapRelease} {
		step(fmt.Sprintf("helm uninstall %s -n %s", release, uninstallNamespace))
		out, err := exec.CommandContext(ctx, "helm", "uninstall", release, "-n", uninstallNamespace).CombinedOutput()
		if err != nil {
			warn(fmt.Sprintf("helm uninstall %s: %v — %s", release, err, strings.TrimSpace(string(out))))
		}
	}

	// 2. Delete PVCs (Helm never deletes these).
	step("Deleting PersistentVolumeClaims...")
	pvcs, err := client.CoreV1().PersistentVolumeClaims(uninstallNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		warn(fmt.Sprintf("list PVCs: %v", err))
	} else {
		for _, pvc := range pvcs.Items {
			if err := client.CoreV1().PersistentVolumeClaims(uninstallNamespace).Delete(
				ctx, pvc.Name, metav1.DeleteOptions{},
			); err != nil {
				warn(fmt.Sprintf("delete PVC %s: %v", pvc.Name, err))
			} else {
				step(fmt.Sprintf("  deleted PVC %s", pvc.Name))
			}
		}
	}

	// 3. Delete cluster-scoped RBAC (not namespace-scoped, may survive helm uninstall).
	for _, name := range []string{"aep-server", "aep-api"} {
		step(fmt.Sprintf("Deleting ClusterRole %s...", name))
		if err := client.RbacV1().ClusterRoles().Delete(ctx, name, metav1.DeleteOptions{}); err != nil {
			warn(fmt.Sprintf("delete ClusterRole %s: %v", name, err))
		}
		step(fmt.Sprintf("Deleting ClusterRoleBinding %s...", name))
		if err := client.RbacV1().ClusterRoleBindings().Delete(ctx, name, metav1.DeleteOptions{}); err != nil {
			warn(fmt.Sprintf("delete ClusterRoleBinding %s: %v", name, err))
		}
	}

	// 4. Delete the namespace (removes all remaining namespaced resources).
	step(fmt.Sprintf("Deleting namespace %s...", uninstallNamespace))
	if err := client.CoreV1().Namespaces().Delete(ctx, uninstallNamespace, metav1.DeleteOptions{}); err != nil {
		warn(fmt.Sprintf("delete namespace %s: %v", uninstallNamespace, err))
	}

	// 5. SRE / observability plane (installed by `aep sre install`). Best-effort:
	//    an install without the SRE stack must still uninstall cleanly. Doing this
	//    here means a later reinstall starts from a clean obs plane whose secrets
	//    match the freshly-provisioned OpenBao (avoids stale-secret / OAuth drift).
	fmt.Println("Removing SRE / observability plane...")
	for _, release := range []string{"observability-logs-opensearch", "observability-plane"} {
		step(fmt.Sprintf("helm uninstall %s -n %s", release, uninstallObsNamespace))
		if out, err := exec.CommandContext(ctx, "helm", "uninstall", release, "-n", uninstallObsNamespace).CombinedOutput(); err != nil {
			warn(fmt.Sprintf("helm uninstall %s: %v — %s", release, err, strings.TrimSpace(string(out))))
		}
	}
	// Cluster-scoped CRs created by `aep sre install` (survive namespace deletion).
	if applier, err := k8s.NewApplier(""); err != nil {
		warn(fmt.Sprintf("build applier for SRE CR cleanup: %v", err))
	} else {
		for _, cr := range []struct{ apiVersion, kind, name string }{
			{"openchoreo.dev/v1alpha1", "ClusterObservabilityPlane", "default"},
			{"openchoreo.dev/v1alpha1", "ClusterAuthzRoleBinding", "aep-observer-reader-binding"},
			{"openchoreo.dev/v1alpha1", "ClusterAuthzRole", "aep-observer-reader"},
			{"openchoreo.dev/v1alpha1", "ClusterAuthzRoleBinding", "rca-agent-dispatch-binding"},
			{"openchoreo.dev/v1alpha1", "ClusterAuthzRole", "rca-agent-dispatch"},
		} {
			step(fmt.Sprintf("Deleting %s %s...", cr.kind, cr.name))
			if err := applier.Delete(ctx, cr.apiVersion, cr.kind, "", cr.name); err != nil {
				warn(fmt.Sprintf("delete %s %s: %v", cr.kind, cr.name, err))
			}
		}
	}
	step(fmt.Sprintf("Deleting namespace %s...", uninstallObsNamespace))
	if err := client.CoreV1().Namespaces().Delete(ctx, uninstallObsNamespace, metav1.DeleteOptions{}); err != nil {
		warn(fmt.Sprintf("delete namespace %s: %v", uninstallObsNamespace, err))
	}

	fmt.Println("Done. AEP has been removed from the cluster.")
	return nil
}
