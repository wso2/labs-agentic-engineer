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
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/wso2/aep/aectl/internal/ui"

	k8s "github.com/wso2/aep/aectl/internal/kubernetes"
)

var syncClientsNamespace string

var syncClientsCmd = &cobra.Command{
	Use:   "sync-clients",
	Short: "Seed missing Thunder client secrets; set the Postgres role and Thunder clients from OpenBao",
	Long: `Brings an already-installed platform's secret holders in line with
OpenBao, without a full install and without rotating anything:

  0. Refuses, writing nothing, if OpenBao lacks any non-generated platform
     secret (a restarted in-memory OpenBao: re-run 'make dev-env', whose
     install restores every generated key from the cluster Secrets ESO last
     synced it into).
  1. Writes only the MISSING generated aep/thunder-clients/* keys to OpenBao
     (create-only), each restored from its cluster Secret when one holds it.
  2. Makes ESO re-read OpenBao for every platform ExternalSecret and waits
     (90s) until each has synced, so the steps below read what OpenBao holds,
     not what ESO synced up to an hour ago.
  3. Sets the Postgres role's password to postgres-secrets (idempotent).
  4. Registers or updates every Thunder client (idempotent), skipping with a
     warning any whose Secret did not sync.

'make dev-update' runs it after the helm upgrade.`,
	RunE: runSyncClients,
}

func init() {
	platformCmd.AddCommand(syncClientsCmd)
	syncClientsCmd.Flags().StringVar(&syncClientsNamespace, "namespace", "wso2-aep", "Kubernetes namespace of the platform release")
	registerThunderFlags(syncClientsCmd)
}

// syncClientsSteps are the cluster-touching steps of sync-clients, injectable
// so the sequencing is unit-testable.
type syncClientsSteps struct {
	// missingRequired lists the non-generated OpenBao paths that are absent.
	missingRequired func(ctx context.Context) ([]string, error)
	// seed writes the missing generated keys and returns what it wrote.
	seed func(ctx context.Context) ([]seededSecret, error)
	// syncSecrets makes ESO re-read OpenBao and returns the platform Secrets
	// that did not catch up (Secret name -> reason).
	syncSecrets func(ctx context.Context) (map[string]string, error)
	// syncPostgres sets the Postgres role's password from postgres-secrets,
	// refusing while that Secret is unsynced.
	syncPostgres func(ctx context.Context, unsynced map[string]string) error
	// setupThunder registers the Thunder clients except the skipped ones.
	// nil on Asgardeo, where aectl registers nothing.
	setupThunder func(ctx context.Context, skip map[string]string) error
}

// errOpenBaoWiped is returned when non-generated vault paths are missing:
// the store restarted (it is in memory) or was never installed. Nothing is
// written: sync-clients has no source for the install's inputs.
func errOpenBaoWiped(missing []string) error {
	return fmt.Errorf("OpenBao is missing %d platform secret(s) (%s): it looks wiped (the local OpenBao is in memory) or never installed.\nNothing was written. Re-run 'make dev-env' (or 'aectl platform install'): it writes the install's inputs again and restores every generated key from the cluster Secret ESO last synced it into, so nothing rotates. See Local OpenBao wipe recovery in deployments/README.md",
		len(missing), strings.Join(missing, ", "))
}

func syncClients(ctx context.Context, st syncClientsSteps) error {
	missing, err := st.missingRequired(ctx)
	if err != nil {
		return fmt.Errorf("check OpenBao secrets: %w", err)
	}
	if len(missing) > 0 {
		return errOpenBaoWiped(missing)
	}

	ui.Step("Seeding missing client secrets")
	seeded, err := st.seed(ctx)
	if err != nil {
		return fmt.Errorf("seed client secrets: %w", err)
	}
	if len(seeded) == 0 {
		ui.Success("All client secrets already in OpenBao")
	}
	for _, line := range describeSeeded(seeded) {
		ui.Success(line)
	}

	ui.Step("Syncing platform Secrets from OpenBao")
	unsynced, err := st.syncSecrets(ctx)
	if err != nil {
		return fmt.Errorf("sync platform Secrets: %w", err)
	}
	if len(unsynced) == 0 {
		ui.Success("Platform Secrets in sync with OpenBao")
	}
	for name, reason := range unsynced {
		ui.Warn(fmt.Sprintf("%s: %s", name, reason))
	}

	if err := st.syncPostgres(ctx, unsynced); err != nil {
		return fmt.Errorf("sync postgres password: %w", err)
	}

	if st.setupThunder == nil {
		ui.Success("Platform IdP is Asgardeo: its clients are pre-created, none registered")
		return nil
	}
	ui.Step("Registering Thunder OAuth clients")
	if err := st.setupThunder(ctx, clientsReadingUnsynced(unsynced)); err != nil {
		return err
	}
	ui.Success("Thunder configured")
	return nil
}

func runSyncClients(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	k8sClient, err := k8s.NewClient(kubeconfig)
	if err != nil {
		return fmt.Errorf("connect to cluster: %w", err)
	}
	session, err := openOpenBaoSession(ctx)
	if err != nil {
		return fmt.Errorf("connect to OpenBao: %w", err)
	}
	defer session.stop()

	steps := syncClientsSteps{
		missingRequired: session.missingRequiredPaths,
		// Thunder's client secrets are generated where missing. Asgardeo's
		// are imported: restored from the cluster where the store lost them,
		// and required.
		seed: func(ctx context.Context) ([]seededSecret, error) {
			recover, err := newClusterSecretRecovery(ctx, syncClientsNamespace)
			if err != nil {
				return nil, err
			}
			st := session.seedStore(ctx, recover)
			restored, err := requireImportedClientSecrets(st)
			if err != nil {
				return restored, err
			}
			seeded, err := seedGeneratedSecrets(generatedClientSecrets(), st)
			return append(restored, seeded...), err
		},
		syncSecrets: func(ctx context.Context) (map[string]string, error) {
			return syncPlatformExternalSecrets(ctx, syncClientsNamespace)
		},
		syncPostgres: func(ctx context.Context, unsynced map[string]string) error {
			return syncPostgresPassword(ctx, k8sClient, syncClientsNamespace, unsynced)
		},
		setupThunder: func(ctx context.Context, skip map[string]string) error {
			return doThunderSetup(ctx, k8sClient, syncClientsNamespace,
				viper.GetString("thunder.namespace"), consolePublicURL(), skip)
		},
	}
	if onAsgardeo() {
		steps.setupThunder = nil
	}
	return syncClients(ctx, steps)
}
