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
	"maps"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/wso2/aep/aectl/internal/config"
	"github.com/wso2/aep/aectl/internal/ui"

	k8s "github.com/wso2/aep/aectl/internal/kubernetes"
)

var syncClientsNamespace string

var syncClientsCmd = &cobra.Command{
	Use:   "sync-clients",
	Short: "Seed missing client secrets; set the Postgres role and the platform IdP's clients from OpenBao",
	Long: `Brings an already-installed platform's secret holders in line with
OpenBao, without a full install and without rotating anything:

  0. Refuses, writing nothing, if OpenBao lacks any non-generated platform
     secret (a restarted in-memory OpenBao: re-run 'make dev-env', whose
     install restores every generated key from the cluster Secrets ESO last
     synced it into).
  1. Writes only the MISSING generated aep/idp-clients/* keys to OpenBao
     (create-only), each restored from its cluster Secret when one holds it.
  2. Makes ESO re-read OpenBao for every platform ExternalSecret and waits
     (90s) until each has synced, so the steps below read what OpenBao holds,
     not what ESO synced up to an hour ago.
  3. Sets the Postgres role's password to postgres-secrets (idempotent).
  4. Registers or updates every Thunder client (idempotent), skipping with a
     warning any whose Secret did not sync. With idp.clients asgardeo it
     creates the applications the organization lacks and checks the ones it
     has, with the admin credential of --idp-admin-client-id-file and
     --idp-admin-client-secret-file. With idp.clients imported it registers
     nothing.

'make dev-update' runs it after the helm upgrade.`,
	RunE: runSyncClients,
}

func init() {
	platformCmd.AddCommand(syncClientsCmd)
	syncClientsCmd.Flags().StringVar(&syncClientsNamespace, "namespace", "wso2-aep", "Kubernetes namespace of the platform release")
	registerThunderFlags(syncClientsCmd)
	registerIDPAdminFlags(syncClientsCmd)
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
	// setupClients registers the clients of the platform IdP except the
	// skipped ones: on a ThunderID aectl administers, or in an Asgardeo
	// organization. nil with imported clients, where aectl registers nothing.
	// idpName names the IdP in what the run prints.
	setupClients func(ctx context.Context, skip map[string]string) error
	idpName      string
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
		// What the seed wrote before it failed stays written, and the next
		// run finds it there and says nothing: warn now.
		warnRegeneratedOrgClientSecrets(seeded)
		return fmt.Errorf("seed client secrets: %w", err)
	}
	if len(seeded) == 0 {
		ui.Success("All client secrets already in OpenBao")
	}
	for _, line := range describeSeeded(seeded) {
		ui.Success(line)
	}
	warnRegeneratedOrgClientSecrets(seeded)

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

	if st.setupClients == nil {
		ui.Success("The platform IdP's clients are imported: none registered")
		return nil
	}
	ui.Step(fmt.Sprintf("Registering the %s OAuth clients", st.idpName))
	skip := clientsReadingUnsynced(unsynced)
	if err := st.setupClients(ctx, skip); err != nil {
		return err
	}
	reportClientSetup(st.idpName, skip)
	return nil
}

// The names of the IdPs aectl creates clients on, in what a run prints.
const (
	idpNameThunder  = "Thunder"
	idpNameAsgardeo = "Asgardeo"
)

// clientSetupOutcome is the closing line of a client registration on the IdP
// named idpName, and whether every client was registered. Skipped clients
// (clientID -> reason) are not on the platform IdP after the run, so the
// line names them and the command that registers them.
func clientSetupOutcome(idpName string, skip map[string]string) (msg string, complete bool) {
	if len(skip) == 0 {
		return idpName + " configured", true
	}
	ids := slices.Sorted(maps.Keys(skip))
	return fmt.Sprintf("%s configured, except %d client(s) that this run did not register: %s. Run '%s' when their Secret has synced.", idpName, len(ids), strings.Join(ids, ", "), syncClientsCommand()), false
}

// syncClientsCommand is the sync-clients command as an operator has to run
// it for the loaded config: with the two admin file flags where aectl
// creates the clients in an Asgardeo organization.
func syncClientsCommand() string {
	if platformIDPClients() == config.ClientsAsgardeo {
		return "aectl platform sync-clients --idp-admin-client-id-file <file> --idp-admin-client-secret-file <file>"
	}
	return "aectl platform sync-clients"
}

// installNotReadyRows are the rows of the closing panel of an install that
// skipped clients (clientID -> reason): the clients that are not on the
// platform IdP and the command that registers them.
func installNotReadyRows(consoleURL string, skip map[string]string) [][2]string {
	var rows [][2]string
	if consoleURL != "" {
		rows = append(rows, [2]string{"Console", ui.Orange(consoleURL)})
	}
	return append(rows,
		[2]string{"Status", ui.Yellow("NOT READY")},
		[2]string{"Not registered", strings.Join(slices.Sorted(maps.Keys(skip)), ", ")},
		[2]string{"Next step", syncClientsCommand()},
	)
}

// reportInstallOutcome prints the closing panel of `platform install`. An
// install that skipped clients is not reported as ready: what needs those
// clients does not work until they are registered.
func reportInstallOutcome(consoleURL string, skip map[string]string) {
	if len(skip) == 0 {
		ui.Ready(consoleURL)
		return
	}
	ui.Panel("AEP is installed, but clients are not registered", installNotReadyRows(consoleURL, skip))
}

// reportClientSetup prints clientSetupOutcome: a success line, or a warning
// when clients were skipped.
func reportClientSetup(idpName string, skip map[string]string) {
	msg, complete := clientSetupOutcome(idpName, skip)
	if complete {
		ui.Success(msg)
		return
	}
	ui.Warn(msg)
}

// warnRegeneratedOrgClientSecrets prints orgClientRepairWarning for a seed
// run, also for one that failed after it wrote.
func warnRegeneratedOrgClientSecrets(seeded []seededSecret) {
	if msg := orgClientRepairWarning(seeded); msg != "" {
		ui.Warn(msg)
	}
}

func runSyncClients(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	if err := refuseIDPAdminFlags(); err != nil {
		return err
	}

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
		// Thunder's client secrets are generated where missing. Imported ones
		// are restored from the cluster where the store lost them,
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
	}
	switch platformIDPClients() {
	case config.ClientsThunder:
		steps.idpName = idpNameThunder
		steps.setupClients = func(ctx context.Context, skip map[string]string) error {
			return doThunderSetup(ctx, k8sClient, syncClientsNamespace,
				viper.GetString("thunder.namespace"), consolePublicURL(), skip)
		}
	case config.ClientsAsgardeo:
		// The credential and the organization are checked first: a wrong
		// credential stops the command before it writes anything.
		idp, err := discoverPlatformIDP(ctx)
		if err != nil {
			return err
		}
		admin, err := preflightAsgardeo(ctx, idp.Public.TokenURL)
		if err != nil {
			return err
		}
		steps.idpName = idpNameAsgardeo
		steps.setupClients = func(ctx context.Context, skip map[string]string) error {
			return doAsgardeoSetup(ctx, k8sClient, syncClientsNamespace, consolePublicURL(), admin, session.put, skip, true)
		}
	}
	return syncClients(ctx, steps)
}
