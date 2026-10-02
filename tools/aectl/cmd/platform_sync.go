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
	"os/exec"
	"strconv"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/wso2/aep/aectl/internal/ui"

	k8s "github.com/wso2/aep/aectl/internal/kubernetes"
)

var syncClientsNamespace string

var syncClientsCmd = &cobra.Command{
	Use:   "sync-clients",
	Short: "Seed missing Thunder client secrets and register the Thunder clients",
	Long: `Brings an already-installed platform up to date with the Thunder clients
this aectl knows about, without a full install:

  1. Writes only the MISSING generated aep/thunder-clients/* keys to OpenBao.
     Existing keys are never rotated, and no database or Thunder admin secret
     is touched.
  2. Nudges the ExternalSecrets that sync those keys so the cluster Secrets
     appear without waiting out their refresh interval.
  3. Registers or updates every Thunder client (idempotent).

'make dev-update' runs it after the helm upgrade. A client whose Secret is
still unavailable is skipped with a warning; the others register.`,
	RunE: runSyncClients,
}

func init() {
	platformCmd.AddCommand(syncClientsCmd)
	syncClientsCmd.Flags().StringVar(&syncClientsNamespace, "namespace", "wso2-aep", "Kubernetes namespace of the platform release")
	registerThunderFlags(syncClientsCmd)
}

func runSyncClients(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	k8sClient, err := k8s.NewClient(kubeconfig)
	if err != nil {
		return fmt.Errorf("connect to cluster: %w", err)
	}

	ui.Step("Seeding missing Thunder client secrets")
	session, err := openOpenBaoSession(ctx)
	if err != nil {
		return fmt.Errorf("connect to OpenBao: %w", err)
	}
	seeded, err := session.seedMissingThunderClientSecrets(ctx)
	session.stop()
	if err != nil {
		return fmt.Errorf("seed Thunder client secrets: %w", err)
	}
	if len(seeded) == 0 {
		ui.Success("All Thunder client secrets already seeded")
	}
	for _, p := range seeded {
		ui.Success("Seeded " + p)
	}
	if len(seeded) > 0 {
		forceSyncExternalSecrets(ctx, syncClientsNamespace, thunderSecretNames())
	}

	ui.Step("Registering Thunder OAuth clients")
	if err := doThunderSetup(ctx, k8sClient, syncClientsNamespace,
		viper.GetString("thunder.namespace"), consolePublicURL()); err != nil {
		return err
	}
	ui.Success("Thunder configured")
	return nil
}

// forceSyncExternalSecrets asks ESO to reconcile the named ExternalSecrets now.
// Best effort: a failure only means the next sync waits for its refresh
// interval, and doThunderSetup's own wait reports a Secret that never appears.
func forceSyncExternalSecrets(ctx context.Context, namespace string, names []string) {
	stamp := "force-sync=" + strconv.FormatInt(time.Now().Unix(), 10)
	for _, name := range names {
		args := []string{"annotate", "externalsecret", name, "-n", namespace, stamp, "--overwrite"}
		if kubeconfig != "" {
			args = append(args, "--kubeconfig", kubeconfig)
		}
		if out, err := exec.CommandContext(ctx, "kubectl", args...).CombinedOutput(); err != nil {
			ui.Warn(fmt.Sprintf("could not nudge ExternalSecret %s: %v: %s", name, err, out))
		}
	}
}
