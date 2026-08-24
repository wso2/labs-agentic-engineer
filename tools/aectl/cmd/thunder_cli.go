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
	f.String("thunder-config-map", "", "Name of Thunder's runtime ConfigMap")
	f.String("thunder-deployment", "", "Name of Thunder's Deployment")
	f.String("thunder-public-url", "", "Public URL of Thunder — must match the JWT issuer configured in Thunder")

	_ = viper.BindPFlag("thunder.namespace", f.Lookup("thunder-namespace"))
	_ = viper.BindPFlag("thunder.url", f.Lookup("thunder-url"))
	_ = viper.BindPFlag("thunder.config_map", f.Lookup("thunder-config-map"))
	_ = viper.BindPFlag("thunder.deployment", f.Lookup("thunder-deployment"))
	_ = viper.BindPFlag("thunder.public_url", f.Lookup("thunder-public-url"))
}

// thunderClientDef describes a Thunder OAuth client to provision.
type thunderClientDef struct {
	clientID     string
	clientType   string   // "confidential" or "public"
	secretKey    string   // key in aep-thunder-secrets (confidential only)
	redirectURIs []string // public only
}

// aepThunderClients is the canonical list of AEP OAuth clients to register in Thunder.
// Confidential clients read their pre-generated secret from the aep-thunder-secrets K8s Secret.
// Public (PKCE) clients use the redirect URIs supplied at registration time.
var aepThunderClients = []thunderClientDef{
	{clientID: "openchoreo-workload-publisher-client", clientType: "confidential", secretKey: "OC_WORKLOAD_PUBLISHER_SECRET"},
	{clientID: "openchoreo-observer-resource-reader-client", clientType: "confidential", secretKey: "OC_OBSERVER_READER_SECRET"},
	{clientID: "aep-api-client", clientType: "confidential", secretKey: "AEP_API_CLIENT_SECRET"},
	{clientID: "bff-git-service", clientType: "confidential", secretKey: "BFF_TO_GIT_SERVICE_SECRET"},
	{clientID: "bff-remote-worker", clientType: "confidential", secretKey: "BFF_TO_REMOTE_WORKER_SECRET"},
	{clientID: "local-dev-seeder", clientType: "confidential", secretKey: "LOCAL_DEV_SEEDER_SECRET"},
	{clientID: "aep-system-client", clientType: "confidential", secretKey: "THUNDER_SYSTEM_CLIENT_SECRET"},
	{clientID: "openchoreo-rca-agent", clientType: "confidential", secretKey: "OC_RCA_AGENT_SECRET"},
	// Public PKCE clients — redirect URIs are filled in by doThunderSetup.
	{clientID: "aep-console-client", clientType: "public"},
	{clientID: "aep-cli-client", clientType: "public", redirectURIs: []string{"http://localhost", "http://127.0.0.1"}},
}

const (
	thunderSecretsName  = "aep-thunder-secrets"
	thunderSystemClient = "aep-system-client"
)

// doThunderSetup registers all AEP OAuth clients in Thunder and patches its CORS
// configuration. It port-forwards to Thunder directly rather than waiting for the
// thunder-app-operator to reconcile ThunderApplication CRs.
func doThunderSetup(
	ctx context.Context,
	k8sClient *kubernetes.Clientset,
	platformNamespace, thunderNamespace, consoleURL, thunderConfigMap, thunderDeployment string,
) error {
	// 1. Read client secrets from the ESO-synced aep-thunder-secrets K8s Secret.
	//    ESO may take a few seconds after pod readiness to complete its first sync,
	//    so retry for up to 60s before failing.
	sp := ui.NewSpinner("Waiting for Thunder client secrets (ESO sync)")
	sp.Start()
	clientSecrets, err := waitForThunderSecrets(ctx, k8sClient, platformNamespace, 60*time.Second)
	if err != nil {
		sp.Fail("Thunder client secrets not available")
		return fmt.Errorf("read Thunder client secrets from %s/%s: %w", platformNamespace, thunderSecretsName, err)
	}
	sp.Success("Thunder client secrets ready")

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
	client, err := thunder.New(ctx, localURL, adminClientID, adminClientSecret)
	if err != nil {
		sp.Fail("Authentication failed")
		return fmt.Errorf("authenticate with Thunder: %w", err)
	}
	sp.Success("Authenticated")

	// 4. Register all OAuth clients — one spinner per client so each resolves to ✓.
	for i, def := range aepThunderClients {
		clientSp := ui.NewSpinner(fmt.Sprintf("Registering OAuth clients (%d/%d) — %s", i+1, len(aepThunderClients), def.clientID))
		clientSp.Start()

		app := thunder.DesiredApp{
			ClientID:   def.clientID,
			ClientType: def.clientType,
		}
		if def.clientType == "confidential" {
			secret, ok := clientSecrets[def.secretKey]
			if !ok || secret == "" {
				clientSp.Fail(fmt.Sprintf("Secret key %q missing from %s", def.secretKey, thunderSecretsName))
				return fmt.Errorf("secret key %q missing from %s/%s", def.secretKey, platformNamespace, thunderSecretsName)
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
	}

	// 5. Ensure the system client is assigned to the aep-system role so it can manage resources.
	sp = ui.NewSpinner("Assigning system client to aep-system role")
	sp.Start()
	if err := client.AssignAdminRole(ctx, thunderSystemClient); err != nil {
		sp.Fail("Role assignment failed")
		return fmt.Errorf("assign admin role to %q: %w", thunderSystemClient, err)
	}
	sp.Success("System client role assigned")

	// 6. Patch Thunder's CORS config so the console SPA can make browser-side OAuth requests.
	ui.Step("Patching Thunder CORS configuration")
	if err := thunder.PatchCORS(ctx, k8sClient, consoleURL, thunderNamespace, thunderConfigMap, thunderDeployment); err != nil {
		ui.Warn(fmt.Sprintf("CORS patch failed (%v) — add %s manually if needed", err, consoleURL))
	} else {
		ui.Detail("Thunder CORS configured")
	}

	ui.Detail("Thunder setup complete")
	return nil
}

// waitForThunderSecrets retries reading the aep-thunder-secrets K8s Secret until
// it exists and is non-empty, or until timeout expires.
func waitForThunderSecrets(ctx context.Context, k8sClient *kubernetes.Clientset, namespace string, timeout time.Duration) (map[string]string, error) {
	deadline := time.Now().Add(timeout)
	for {
		sec, err := k8sClient.CoreV1().Secrets(namespace).Get(ctx, thunderSecretsName, metav1.GetOptions{})
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

