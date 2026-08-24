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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/spf13/cobra"

	"github.com/wso2/aep/aectl/internal/config"
	k8s "github.com/wso2/aep/aectl/internal/kubernetes"
	"github.com/wso2/aep/aectl/internal/ui"
)

var (
	uninstallNamespace       string
	uninstallPlatformRelease string
	uninstallPurgeConfig     bool
	uninstallYes             bool
)

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove what aep platform install deployed",
	Long: `Uninstalls the platform Helm chart and deletes its PersistentVolumeClaims.

This command reverses exactly what aep platform install did:
  - helm uninstall <platform-release>
  - deletes workspaces PVCs (Helm never removes these)
  - optionally deletes the aep-cli-config ConfigMap (--purge-config)

What it does NOT touch:
  - OpenBao secrets or ESO-synced Secrets
  - the wso2-aep namespace itself

To also remove the SRE observability plane use aep sre uninstall (when available).`,
	RunE: runUninstall,
}

func init() {
	platformCmd.AddCommand(uninstallCmd)
	uninstallCmd.Flags().StringVar(&uninstallNamespace, "namespace", "wso2-aep", "Kubernetes namespace where the platform chart is installed")
	uninstallCmd.Flags().StringVar(&uninstallPlatformRelease, "platform-release", "aep-platform", "Helm release name of the platform chart")
	uninstallCmd.Flags().BoolVar(&uninstallPurgeConfig, "purge-config", false, "Also delete the aep-cli-config ConfigMap written by aep platform configure / install")
	uninstallCmd.Flags().BoolVarP(&uninstallYes, "yes", "y", false, "Skip confirmation prompt")
}

func runUninstall(cmd *cobra.Command, args []string) error {
	if !uninstallYes {
		fmt.Printf("\n  This will uninstall chart %q from namespace %q\n", uninstallPlatformRelease, uninstallNamespace)
		fmt.Printf("  and delete all workspaces PVCs in that namespace.\n")
		if uninstallPurgeConfig {
			fmt.Printf("  The aep-cli-config ConfigMap will also be deleted.\n")
		}
		fmt.Printf("  %s OpenBao secrets and ESO are NOT affected.\n\n", ui.Gray("note:"))
		fmt.Print("  Type \"yes\" to confirm: ")
		scanner := bufio.NewScanner(os.Stdin)
		scanner.Scan()
		if strings.TrimSpace(scanner.Text()) != "yes" {
			ui.Print("Aborted.")
			return nil
		}
		fmt.Println()
	}

	ctx := context.Background()
	client, err := k8s.NewClient(kubeconfig)
	if err != nil {
		return fmt.Errorf("connect to cluster: %w", err)
	}

	ui.Step(fmt.Sprintf("Uninstalling platform chart %q", uninstallPlatformRelease))
	out, err := exec.CommandContext(ctx, "helm", "uninstall", uninstallPlatformRelease, "-n", uninstallNamespace).CombinedOutput()
	if err != nil {
		ui.Warn(fmt.Sprintf("helm uninstall %s: %v — %s", uninstallPlatformRelease, err, strings.TrimSpace(string(out))))
	} else {
		ui.Success(fmt.Sprintf("Chart %q uninstalled", uninstallPlatformRelease))
	}

	ui.Step("Deleting workspaces PVCs")
	pvcs, err := client.CoreV1().PersistentVolumeClaims(uninstallNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/part-of=wso2-aep",
	})
	if err != nil {
		ui.Warn(fmt.Sprintf("list PVCs in %s: %v", uninstallNamespace, err))
	} else {
		deleted := 0
		for _, pvc := range pvcs.Items {
			if err := client.CoreV1().PersistentVolumeClaims(uninstallNamespace).Delete(
				ctx, pvc.Name, metav1.DeleteOptions{},
			); err != nil {
				ui.Warn(fmt.Sprintf("delete PVC %s: %v", pvc.Name, err))
			} else {
				ui.Detail(fmt.Sprintf("deleted PVC %s", pvc.Name))
				deleted++
			}
		}
		if len(pvcs.Items) == 0 {
			ui.Detail("no PVCs found")
		} else if deleted < len(pvcs.Items) {
			ui.Warn(fmt.Sprintf("Deleted %d/%d PVC(s) — %d failed, manual cleanup may be required", deleted, len(pvcs.Items), len(pvcs.Items)-deleted))
		} else {
			ui.Success(fmt.Sprintf("Deleted %d PVC(s)", deleted))
		}
	}

	if uninstallPurgeConfig {
		ui.Step("Deleting aep-cli-config ConfigMap")
		err := client.CoreV1().ConfigMaps(uninstallNamespace).Delete(ctx, config.ConfigMapName, metav1.DeleteOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			ui.Warn(fmt.Sprintf("delete %s: %v", config.ConfigMapName, err))
		} else if apierrors.IsNotFound(err) {
			ui.Detail(fmt.Sprintf("%s not found — nothing to delete", config.ConfigMapName))
		} else {
			ui.Success(fmt.Sprintf("Deleted %s", config.ConfigMapName))
		}
	}

	fmt.Printf("\n  %s Done. %s\n\n", ui.Green("✓"), ui.Gray("OpenBao secrets and ESO are NOT affected."))
	return nil
}
