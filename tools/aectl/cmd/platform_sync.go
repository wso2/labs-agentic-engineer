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
	"crypto/sha256"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/wso2/aep/aectl/internal/ui"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	k8s "github.com/wso2/aep/aectl/internal/kubernetes"
)

var syncClientsNamespace string

var syncClientsCmd = &cobra.Command{
	Use:   "sync-clients",
	Short: "Seed missing Thunder client secrets and register the Thunder clients",
	Long: `Brings an already-installed platform up to date with the Thunder clients
this aectl knows about, without a full install:

  0. Refuses, writing nothing, if OpenBao lacks any non-generated platform
     secret (a wiped store needs a full install, not a top-up).
  1. Writes only the MISSING generated aep/thunder-clients/* keys to OpenBao
     (create-only). Existing keys are never rotated, and no database or
     Thunder admin secret is touched.
  2. Nudges the ExternalSecrets that sync those keys and waits (90s) until
     the cluster Secrets hold the seeded values.
  3. Registers or updates every Thunder client (idempotent), skipping with a
     warning any whose Secret did not catch up.

'make dev-update' runs it after the helm upgrade. A client whose Secret is
still unavailable is skipped with a warning; the others register.`,
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
	// nudge asks ESO to sync the named ExternalSecrets now.
	nudge func(ctx context.Context, names []string)
	// awaitSynced waits until the cluster Secrets hold the seeded values and
	// returns the clients (clientID -> reason) whose Secret did not catch up.
	awaitSynced func(ctx context.Context, seeded []seededSecret) map[string]string
	// setupThunder registers the Thunder clients except the skipped ones.
	setupThunder func(ctx context.Context, skip map[string]string) error
}

// errOpenBaoWiped is returned when non-generated vault paths are missing:
// seeding fresh generated keys then would rotate every client secret behind
// the cluster Secrets' back, so nothing is written.
func errOpenBaoWiped(missing []string) error {
	return fmt.Errorf("OpenBao is missing %d platform secret(s) (%s): it looks wiped or never installed, so seeding fresh Thunder client secrets would rotate them out from under the running platform.\nNothing was written. Recover with a full 'aectl platform install' (see deployments/README.md), not sync-clients",
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

	ui.Step("Seeding missing Thunder client secrets")
	seeded, err := st.seed(ctx)
	if err != nil {
		return fmt.Errorf("seed Thunder client secrets: %w", err)
	}
	if len(seeded) == 0 {
		ui.Success("All Thunder client secrets already seeded")
	}
	for _, p := range seeded {
		ui.Success("Seeded " + p.path)
	}

	var skip map[string]string
	if len(seeded) > 0 {
		st.nudge(ctx, thunderSecretNames())
		skip = st.awaitSynced(ctx, seeded)
	}

	ui.Step("Registering Thunder OAuth clients")
	if err := st.setupThunder(ctx, skip); err != nil {
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

	return syncClients(ctx, syncClientsSteps{
		missingRequired: session.missingRequiredPaths,
		seed:            session.seedMissingThunderClientSecrets,
		nudge: func(ctx context.Context, names []string) {
			forceSyncExternalSecrets(ctx, syncClientsNamespace, names)
		},
		awaitSynced: func(ctx context.Context, seeded []seededSecret) map[string]string {
			return awaitSeededSecretsSynced(ctx, k8sClient, syncClientsNamespace, seeded, seededSyncTimeout)
		},
		setupThunder: func(ctx context.Context, skip map[string]string) error {
			return doThunderSetup(ctx, k8sClient, syncClientsNamespace,
				viper.GetString("thunder.namespace"), consolePublicURL(), skip)
		},
	})
}

// seededSyncTimeout bounds the wait for ESO to carry freshly seeded keys into
// the cluster Secrets.
const seededSyncTimeout = 90 * time.Second

// awaitSeededSecretsSynced waits until, for every seeded key a client reads,
// the cluster Secret holds the seeded value (compared by hash; values are
// never logged). Registering a client before that would hand Thunder the
// stale cluster value while ESO is about to sync the new one, an effective
// rotation. It returns the clients whose Secret did not catch up in time
// (clientID -> reason), which the caller skips.
func awaitSeededSecretsSynced(ctx context.Context, c kubernetes.Interface, namespace string, seeded []seededSecret, timeout time.Duration) map[string]string {
	pending := map[string]thunderClientDef{}
	want := map[string][sha256.Size]byte{}
	for _, sec := range seeded {
		for _, d := range aepThunderClients {
			if d.vaultName != "" && "aep/thunder-clients/"+d.vaultName == sec.path {
				pending[d.clientID] = d
				want[d.clientID] = sha256.Sum256([]byte(sec.value))
			}
		}
	}
	deadline := time.Now().Add(timeout)
	for {
		for id, d := range pending {
			sec, err := c.CoreV1().Secrets(namespace).Get(ctx, d.secretNameOrDefault(), metav1.GetOptions{})
			if err != nil {
				continue
			}
			if sha256.Sum256(sec.Data[d.secretKey]) == want[id] {
				delete(pending, id)
			}
		}
		if len(pending) == 0 || time.Now().After(deadline) || ctx.Err() != nil {
			break
		}
		select {
		case <-ctx.Done():
		case <-time.After(3 * time.Second):
		}
	}
	skip := map[string]string{}
	for id, d := range pending {
		skip[id] = fmt.Sprintf("Secret %s/%s did not sync the newly seeded key within %s", namespace, d.secretNameOrDefault(), timeout)
	}
	return skip
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
