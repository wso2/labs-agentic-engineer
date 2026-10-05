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
	"net/url"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/wso2/aep/aectl/internal/thunder"
	"github.com/wso2/aep/aectl/internal/ui"
)

// registerThunderFlags adds Thunder connection flags to cmd and binds each one
// to its corresponding Viper key. Precedence: flag > AEP_* env var > cluster ConfigMap > default.
func registerThunderFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.String("thunder-namespace", "", "Kubernetes namespace where Thunder is installed")
	f.String("thunder-url", "", "In-cluster URL of the Thunder service")
	f.String("thunder-public-url", "", "Public URL of Thunder — must match the JWT issuer configured in Thunder")

	_ = viper.BindPFlag("thunder.namespace", f.Lookup("thunder-namespace"))
	_ = viper.BindPFlag("thunder.url", f.Lookup("thunder-url"))
	_ = viper.BindPFlag("thunder.public_url", f.Lookup("thunder-public-url"))
}

// thunderClientDef describes a Thunder OAuth client to provision.
type thunderClientDef struct {
	clientID         string
	clientType       string   // "confidential" or "public"
	secretKey        string   // key in the client's K8s Secret (confidential only)
	redirectURIs     []string // public only
	noUserAttributes bool     // token carries no attributes at all (AE-only client)
	secretName       string   // K8s Secret holding secretKey; default aep-thunder-secrets
	// optional: a missing Secret skips this client with a warning instead of
	// failing the run, so its absence degrades nothing else.
	optional bool
	// vaultName is the generated vault key aep/thunder-clients/<vaultName>
	// the ExternalSecret syncs into secretKey (empty: not a generated key).
	vaultName string
}

// aepThunderClients is the canonical list of AEP OAuth clients to register in Thunder.
// Confidential clients read their pre-generated secret from a K8s Secret: the
// shared aep-thunder-secrets unless the client names its own (secretName).
// Public (PKCE) clients use the redirect URIs supplied at registration time.
var aepThunderClients = []thunderClientDef{
	{clientID: "openchoreo-workload-publisher-client", clientType: "confidential", vaultName: "oc-workload-publisher", secretKey: "OC_WORKLOAD_PUBLISHER_SECRET"},
	{clientID: observerReaderClientID, clientType: "confidential", vaultName: "oc-observer-reader", secretKey: "OC_OBSERVER_READER_SECRET"},
	{clientID: "aep-api-client", clientType: "confidential", vaultName: "aep-api-client", secretKey: "AEP_API_CLIENT_SECRET"},
	{clientID: "bff-git-service", clientType: "confidential", vaultName: "bff-git-service", secretKey: "BFF_TO_GIT_SERVICE_SECRET"},
	{clientID: "bff-remote-worker", clientType: "confidential", vaultName: "bff-remote-worker", secretKey: "BFF_TO_REMOTE_WORKER_SECRET"},
	{clientID: "local-dev-seeder", clientType: "confidential", vaultName: "local-dev-seeder", secretKey: "LOCAL_DEV_SEEDER_SECRET"},
	{clientID: "aep-system-client", clientType: "confidential", vaultName: "system-client", secretKey: "THUNDER_SYSTEM_CLIENT_SECRET"},
	{clientID: "openchoreo-rca-agent", clientType: "confidential", vaultName: "openchoreo-rca-agent", secretKey: "OC_RCA_AGENT_SECRET"},
	{clientID: "ae-studio-internal-client", clientType: "confidential", secretKey: "AE_STUDIO_INTERNAL_CLIENT_SECRET", secretName: aeStudioInternalSecretsName, noUserAttributes: true, optional: true, vaultName: "ae-studio-internal"},
	// Public PKCE clients — redirect URIs are filled in by doThunderSetup.
	{clientID: "aep-console-client", clientType: "public"},
	{clientID: "aep-cli-client", clientType: "public", redirectURIs: []string{"http://localhost", "http://127.0.0.1"}},
}

func (d thunderClientDef) secretNameOrDefault() string {
	if d.secretName != "" {
		return d.secretName
	}
	return thunderSecretsName
}

// secretOptional reports whether every client reading the K8s Secret name is
// optional, i.e. the run may continue without it.
func secretOptional(name string) bool {
	for _, d := range aepThunderClients {
		if d.clientType == "confidential" && d.secretNameOrDefault() == name && !d.optional {
			return false
		}
	}
	return true
}

// clientsToRegister is the confidential and public clients doThunderSetup
// registers: every client except those in skip (clientID -> reason) and the
// optional ones whose K8s Secret is unavailable. The rest still register.
func clientsToRegister(defs []thunderClientDef, secretsByName map[string]map[string]string, skip map[string]string) []thunderClientDef {
	var out []thunderClientDef
	for _, d := range defs {
		if _, skipped := skip[d.clientID]; skipped {
			continue
		}
		if d.optional && d.clientType == "confidential" && len(secretsByName[d.secretNameOrDefault()]) == 0 {
			continue
		}
		out = append(out, d)
	}
	return out
}

// thunderSecretNames lists the distinct K8s Secrets the confidential clients
// read their secrets from.
func thunderSecretNames() []string {
	var names []string
	seen := map[string]bool{}
	for _, d := range aepThunderClients {
		if d.clientType != "confidential" || seen[d.secretNameOrDefault()] {
			continue
		}
		seen[d.secretNameOrDefault()] = true
		names = append(names, d.secretNameOrDefault())
	}
	return names
}

const (
	thunderSecretsName = "aep-thunder-secrets"
	// aeStudioInternalSecretsName holds only the AE-only client secret, so a
	// missing vault key degrades nothing else.
	aeStudioInternalSecretsName = "aep-ae-studio-internal-secrets"
	thunderSystemClient         = "aep-system-client"
)

// doThunderSetup registers all AEP OAuth clients in Thunder. It port-forwards
// to Thunder directly rather than waiting for the thunder-app-operator to
// reconcile ThunderApplication CRs. Thunder's CORS configuration is handled by
// the aep-platform chart's own TrafficPolicy (templates/thunder/cors-policy.yaml),
// not by this function. skipClients (clientID -> reason) are left untouched with
// a warning; install passes nil.
func doThunderSetup(
	ctx context.Context,
	k8sClient *kubernetes.Clientset,
	platformNamespace, thunderNamespace, consoleURL string,
	skipClients map[string]string,
) error {
	// 1. Read client secrets from the ESO-synced K8s Secrets (aep-thunder-secrets,
	//    plus the AE-only client's own). ESO may take a few seconds after pod
	//    readiness to complete its first sync, so retry for up to 60s before
	//    failing. A Secret only optional clients read is warned about and its
	//    clients are skipped; the rest still register.
	sp := ui.NewSpinner("Waiting for Thunder client secrets (ESO sync)")
	sp.Start()
	secretsByName := map[string]map[string]string{}
	var err error
	var skippedSecrets []string
	for _, name := range thunderSecretNames() {
		secretsByName[name], err = waitForSecretData(ctx, k8sClient, platformNamespace, name, 60*time.Second)
		if err != nil && secretOptional(name) {
			skippedSecrets = append(skippedSecrets, name)
			continue
		}
		if err != nil {
			sp.Fail("Thunder client secrets not available")
			return fmt.Errorf("read Thunder client secrets from %s/%s: %w", platformNamespace, name, err)
		}
	}
	sp.Success("Thunder client secrets ready")
	for _, name := range skippedSecrets {
		ui.Warn(fmt.Sprintf("Secret %s/%s is not available: skipping the Thunder clients that read it (seed its vault key, then re-run)", platformNamespace, name))
	}

	// 2. Port-forward to Thunder.
	sp = ui.NewSpinner("Connecting to Thunder")
	sp.Start()
	pf, err := thunder.PortForward(ctx, thunderNamespace, kubeconfig)
	if err != nil {
		sp.Fail("Port-forward failed")
		return fmt.Errorf("port-forward to Thunder: %w", err)
	}
	defer pf.Stop()

	localURL := "http://localhost:" + pf.Port
	if err := thunder.WaitForReachable(ctx, localURL, 2*time.Minute, pf); err != nil {
		sp.Fail("Thunder unreachable")
		return fmt.Errorf("thunder not reachable via port-forward: %w", err)
	}
	sp.Success("Connected to Thunder")

	// 3. Authenticate with the Thunder admin client.
	sp = ui.NewSpinner("Authenticating with Thunder")
	sp.Start()
	adminClientID := viper.GetString("thunder.admin_client_id")
	adminClientSecret := viper.GetString("thunder.admin_client_secret")
	// The System resource server is named by Thunder's PUBLIC URL, not the
	// port-forward this client talks over.
	systemResource := thunder.SystemResourceIdentifier(viper.GetString("thunder.public_url"))
	client, err := thunder.New(ctx, localURL, adminClientID, adminClientSecret, systemResource)
	if err != nil {
		sp.Fail("Authentication failed")
		return fmt.Errorf("authenticate with Thunder: %w", err)
	}
	sp.Success("Authenticated")

	// 4. Register all OAuth clients — one spinner per client so each resolves to ✓.
	for id, reason := range skipClients {
		ui.Warn(fmt.Sprintf("Skipped %s: %s", id, reason))
	}
	toRegister := clientsToRegister(aepThunderClients, secretsByName, skipClients)
	for i, def := range toRegister {
		clientSp := ui.NewSpinner(fmt.Sprintf("Registering OAuth clients (%d/%d) — %s", i+1, len(toRegister), def.clientID))
		clientSp.Start()

		app := thunder.DesiredApp{
			ClientID:         def.clientID,
			ClientType:       def.clientType,
			NoUserAttributes: def.noUserAttributes,
		}
		if def.clientType == "confidential" {
			secretName := def.secretNameOrDefault()
			secret, ok := secretsByName[secretName][def.secretKey]
			if !ok || secret == "" {
				clientSp.Fail(fmt.Sprintf("Secret key %q missing from %s", def.secretKey, secretName))
				return fmt.Errorf("secret key %q missing from %s/%s", def.secretKey, platformNamespace, secretName)
			}
			app.ClientSecret = secret
		} else {
			// Public client — set redirect URIs from the definition or the console URL.
			if len(def.redirectURIs) > 0 {
				app.RedirectURIs = def.redirectURIs
			} else {
				if consoleURL == "" {
					clientSp.Fail(fmt.Sprintf("Cannot register %s: console URL is required for public clients with no redirect URIs", def.clientID))
					return fmt.Errorf("register Thunder client %q: console URL must be set to derive the redirect URI", def.clientID)
				}
				parsed, parseErr := url.Parse(consoleURL)
				if parseErr != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
					sp.Fail(fmt.Sprintf("Cannot register %s: console URL %q is not a valid absolute http/https URL", def.clientID, consoleURL))
					return fmt.Errorf("register Thunder client %q: console URL %q must be an absolute http or https URL", def.clientID, consoleURL)
				}
				// aep-console-client: redirect URI is the console's /callback endpoint.
				app.RedirectURIs = []string{consoleURL + "/callback"}
			}
		}

		if err := client.EnsureApplication(ctx, app); err != nil {
			clientSp.Fail(fmt.Sprintf("Failed to register %s", def.clientID))
			return fmt.Errorf("register Thunder client %q: %w", def.clientID, err)
		}
		clientSp.Success(def.clientID)
		if def.clientID == observerReaderClientID {
			// Thunder now holds aep/thunder-clients/oc-observer-reader for the
			// observer's client, so the plane's observer must read that one.
			syncObserverClientSecret(ctx, k8sClient)
		}
	}

	// 5. Ensure the system client is assigned to the aep-system role so it can manage resources.
	sp = ui.NewSpinner("Assigning system client to aep-system role")
	sp.Start()
	if err := client.AssignAdminRole(ctx, thunderSystemClient); err != nil {
		sp.Fail("Role assignment failed")
		return fmt.Errorf("assign admin role to %q: %w", thunderSystemClient, err)
	}
	sp.Success("System client role assigned")

	ui.Detail("Thunder setup complete")
	return nil
}

// waitForSecretData retries reading an ESO-synced K8s Secret until it exists
// and is non-empty, or until timeout expires. ESO's first sync of a freshly
// created ExternalSecret is not instant, so a caller reading the Secret right
// after the Helm install that created it needs to tolerate a short gap.
func waitForSecretData(ctx context.Context, k8sClient *kubernetes.Clientset, namespace, secretName string, timeout time.Duration) (map[string]string, error) {
	deadline := time.Now().Add(timeout)
	for {
		// Bounded per attempt: ctx itself may be an undeadlined
		// context.Background() (see runAEPInit), so without this a single
		// hung Get would block past the deadline check below instead of
		// being canceled and retried or reported as a timeout.
		getCtx, cancel := context.WithTimeout(ctx, timeout)
		sec, err := k8sClient.CoreV1().Secrets(namespace).Get(getCtx, secretName, metav1.GetOptions{})
		cancel()
		if err == nil && len(sec.Data) > 0 {
			out := make(map[string]string, len(sec.Data))
			for k, v := range sec.Data {
				out[k] = string(v)
			}
			return out, nil
		}
		if err != nil && !apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("get secret: %w", err)
		}
		if time.Now().After(deadline) {
			if err != nil {
				return nil, fmt.Errorf("timed out after %s: %w", timeout, err)
			}
			return nil, fmt.Errorf("timed out after %s — secret exists but is empty", timeout)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(6 * time.Second):
		}
	}
}
