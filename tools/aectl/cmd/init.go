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
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/term"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/wso2/aep/aectl/internal/bootstrap"
	"github.com/wso2/aep/aectl/internal/config"
	k8s "github.com/wso2/aep/aectl/internal/kubernetes"
	"github.com/wso2/aep/aectl/internal/openbao"
)

const (
	minOCVersion = "1.1.1"

	ocOpenBaoNamespace = "openbao"
	ocOpenBaoRelease   = "openbao"
	ocOpenBaoSA        = "external-secrets-openbao"

	// OC's built-in openchoreo-secret-writer-role grants create/read/update/delete
	// on secret/data/* — aep/* paths are covered. The upstream role binds any SA
	// in the openbao namespace, so external-secrets-openbao qualifies directly.
	ocWriteRole = "openchoreo-secret-writer-role"
)

var (
	initPlatformChart       string
	initPlatformVersion     string
	initPlatformRelease     string
	initPlatformNamespace   string
	initConsoleURL          string
	initAPIURL              string
	initBuildPlaneNamespace string
	initRegistryService     string
	initOCNamespace         string
	initSkipOCVersionCheck  bool
	initOpenBaoDirect       bool
	initReuseSecrets        bool
)

var initCmd = &cobra.Command{
	Use:   "install",
	Short: "Provision OpenBao secrets, install the platform, and configure Thunder",
	Long: `Full AEP platform installation in one command:
  1. Validates the cluster config (imported via 'aectl platform config import')
  2. Seeds all platform secrets into OpenChoreo's built-in OpenBao instance
  3. Installs or upgrades the platform Helm chart (idempotent)
  4. Waits for all platform pods to be ready
  5. Registers AEP OAuth clients in Thunder

Run 'aectl platform config import --config <file>' before this command.
All configuration values are read from the imported ConfigMap — no
hardcoded defaults are used.`,
	RunE: runAEPInit,
}

func init() {
	platformCmd.AddCommand(initCmd)
	initCmd.Flags().StringVar(&initPlatformChart, "platform-chart", "", "Local path to the platform Helm chart (for local/dev installs; takes precedence over --platform-version)")
	initCmd.Flags().StringVar(&initPlatformVersion, "platform-version", "latest", "Platform version to pull from GHCR (ignored when --platform-chart is set)")
	initCmd.Flags().StringVar(&initPlatformRelease, "platform-release", "aep-platform", "Helm release name for the platform chart")
	initCmd.Flags().StringVar(&initPlatformNamespace, "namespace", "wso2-aep", "Kubernetes namespace")
	initCmd.Flags().StringVar(&initConsoleURL, "console-url", "http://console.openchoreo.localhost:8080", "Public URL of the AEP console")
	initCmd.Flags().StringVar(&initAPIURL, "api-url", "http://api.openchoreo.localhost:8080", "Public URL of the AEP API")
	initCmd.Flags().StringVar(&initBuildPlaneNamespace, "build-plane-namespace", "openchoreo-workflow-plane", "Namespace of the OpenChoreo build/workflow plane (must already exist, incl. its image registry)")
	initCmd.Flags().StringVar(&initRegistryService, "registry-service", "registry", "Name of the build-plane image registry Service (the coding-agent build pushes/pulls here)")
	initCmd.Flags().StringVar(&initOCNamespace, "oc-namespace", "", "Namespace where OpenChoreo control-plane is installed (overrides config)")
	_ = viper.BindPFlag("oc.system_namespace", initCmd.Flags().Lookup("oc-namespace"))
	initCmd.Flags().BoolVar(&initSkipOCVersionCheck, "skip-oc-version-check", false, "Skip the OpenChoreo minimum version check (not recommended)")
	initCmd.Flags().String("oc-api-url", "", "In-cluster URL of the OpenChoreo platform API (overrides config)")
	_ = viper.BindPFlag("oc.api_url", initCmd.Flags().Lookup("oc-api-url"))
	initCmd.Flags().String("webhook-delivery-url", "", "Public URL registered on each repo's webhook (overrides config)")
	_ = viper.BindPFlag("webhook.delivery_url", initCmd.Flags().Lookup("webhook-delivery-url"))
	initCmd.Flags().BoolVar(&initOpenBaoDirect, "openbao-direct", false, "Enable OpenBao-direct secrets delivery — injects OPENBAO_ADDR/TOKEN into aep-api (required for local/OSS installs)")
	_ = viper.BindPFlag("codingagent.openbao_direct.enabled", initCmd.Flags().Lookup("openbao-direct"))
	initCmd.Flags().String("openbao-addr", "", "In-cluster URL of the OpenBao service (overrides config)")
	_ = viper.BindPFlag("openbao.addr", initCmd.Flags().Lookup("openbao-addr"))
	initCmd.Flags().BoolVar(&initReuseSecrets, "reuse-secrets", false, "Skip secret prompts and reuse secrets already seeded in OpenBao (for reinstall or upgrade)")
	registerThunderFlags(initCmd)
}

func runAEPInit(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Pre-flight: all config values must come from the imported ConfigMap.
	// PersistentPreRunE has already loaded it; fail fast if any required key is missing.
	if errs := config.ValidateLoaded(); len(errs) > 0 {
		_, _ = fmt.Fprintln(os.Stderr, "Missing or invalid config. Run 'aectl platform config import --config <file>' first:")
		for _, e := range errs {
			_, _ = fmt.Fprintf(os.Stderr, "  %s\n", e)
		}
		return fmt.Errorf("config validation failed")
	}

	if _, err := exec.LookPath("helm"); err != nil {
		return fmt.Errorf("helm is required but was not found in PATH\nInstall it from https://helm.sh/docs/intro/install/ and try again")
	}
	if _, err := exec.LookPath("kubectl"); err != nil {
		return fmt.Errorf("kubectl is required but was not found in PATH\nInstall it from https://kubernetes.io/docs/tasks/tools/ and try again")
	}

	k8sClient, err := k8s.NewClient(kubeconfig)
	if err != nil {
		return fmt.Errorf("connect to cluster: %w", err)
	}

	ocNamespace := viper.GetString("oc.system_namespace")
	if !initSkipOCVersionCheck {
		if err := checkOCVersion(ctx, k8sClient, ocNamespace, minOCVersion); err != nil {
			return err
		}
	}

	if err := checkBuildRegistry(ctx, k8sClient, initBuildPlaneNamespace, initRegistryService); err != nil {
		return err
	}

	openBaoDirect := viper.GetBool("codingagent.openbao_direct.enabled")

	// 1. Secrets: either reuse existing OpenBao secrets or prompt + provision fresh ones.
	if initReuseSecrets {
		_, _ = fmt.Fprintln(os.Stdout, "Verifying existing OpenBao secrets...")
		if err := verifyOpenBaoSecrets(ctx); err != nil {
			return fmt.Errorf("reuse-secrets verification failed: %w\nRemove --reuse-secrets to run a fresh install", err)
		}
		_, _ = fmt.Fprintln(os.Stdout, "Existing secrets verified — skipping provisioning.")
	} else {
		anthropicKey := strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY"))
		if anthropicKey == "" {
			var err error
			anthropicKey, err = readMaskedInput("Anthropic API key")
			if err != nil {
				return fmt.Errorf("read Anthropic API key: %w", err)
			}
			if anthropicKey == "" {
				return fmt.Errorf("an Anthropic API key is required")
			}
		}

		if openBaoDirect && os.Getenv("AEP_OPENBAO_TOKEN") == "" {
			obToken, err := readMaskedInput("OpenBao token (Enter = use default \"root\")")
			if err != nil {
				return fmt.Errorf("read OpenBao token: %w", err)
			}
			if obToken != "" {
				viper.Set("openbao.token", obToken)
			}
		}

		thunderSecret := strings.TrimSpace(os.Getenv("AEP_THUNDER_ADMIN_CLIENT_SECRET"))
		if thunderSecret == "" {
			var err error
			thunderSecret, err = readMaskedInput("Thunder admin client secret")
			if err != nil {
				return fmt.Errorf("read Thunder admin client secret: %w", err)
			}
			if thunderSecret == "" {
				return fmt.Errorf("a Thunder admin client secret is required")
			}
		}
		viper.Set("thunder.admin_client_secret", thunderSecret)

		adminClientID := viper.GetString("thunder.admin_client_id")
		adminClientSecret := viper.GetString("thunder.admin_client_secret")
		if adminClientID == "" {
			return fmt.Errorf("thunder.admin_client_id is not set — run 'aectl platform config import' first or set it in ~/.aectl/config.yaml")
		}
		if adminClientSecret == "" {
			return fmt.Errorf("thunder.admin_client_secret is not set — set it via AEP_THUNDER_ADMIN_CLIENT_SECRET or re-run without --reuse-secrets")
		}
		_, _ = fmt.Fprintln(os.Stdout, "Provisioning OpenBao secrets...")
		if err := provisionOpenBao(ctx, anthropicKey, adminClientID, adminClientSecret); err != nil {
			return fmt.Errorf("provision OpenBao: %w", err)
		}
	}

	// 3. Install the platform chart.
	_, _ = fmt.Fprintln(os.Stdout, "Installing platform chart...")
	if err := deleteOrphanedResources(ctx); err != nil {
		return fmt.Errorf("clean up legacy resources: %w", err)
	}
	thunderURL := viper.GetString("thunder.url")
	helmArgs := []string{
		"upgrade", "--install", initPlatformRelease,
		"-n", initPlatformNamespace,
		"--create-namespace",
		"--set", "console.publicURL=" + initConsoleURL,
		"--set", "aepApi.publicURL=" + initAPIURL,
		"--set", "console.thunderPublicURL=" + viper.GetString("thunder.public_url"),
		"--set", "thunder.adminURL=" + thunderURL,
		"--set", "thunder.jwksURL=" + thunderURL + "/oauth2/jwks",
		"--set", "platformAPI.baseURL=" + viper.GetString("oc.api_url"),
		// thunder-app-operator subchart: forward the same Thunder admin URL so the
		// operator talks to the same endpoint as the rest of the platform.
		// Credentials come from the ESO-synced Secret (written to OpenBao by
		// provisionOpenBao above — never passed via --set).
		"--set", "thunder-app-operator.thunder.adminURL=" + thunderURL,
		"--set", "thunder-app-operator.thunder.existingSecret=" + config.ThunderOperatorCredsSecret,
	}
	// helm upgrade --install <release> <chart> [flags]
	// Chart must be inserted after "upgrade", "--install", <release> (index 3).
	if initPlatformChart != "" {
		helmArgs = append(helmArgs[:3:3], append([]string{initPlatformChart}, helmArgs[3:]...)...)
	} else {
		helmArgs = append(helmArgs[:3:3], append([]string{"oci://ghcr.io/wso2/aep/charts/aep-platform"}, helmArgs[3:]...)...)
		if initPlatformVersion != "latest" {
			helmArgs = append(helmArgs, "--version", initPlatformVersion)
		}
	}
	if mode := viper.GetString("platform.workspaces.access_mode"); mode != "" {
		helmArgs = append(helmArgs, "--set", "workspaces.accessMode="+mode)
	}
	helmArgs = append(helmArgs, "--set",
		fmt.Sprintf("codingAgentDispatch.openBaoDirect.enabled=%t", openBaoDirect))
	if openBaoDirect {
		helmArgs = append(helmArgs, "--set", "openbao.addr="+viper.GetString("openbao.addr"))
		helmArgs = append(helmArgs, "--set", "openbao.token="+viper.GetString("openbao.token"))
	}
	helmArgs = append(helmArgs, "--set",
		fmt.Sprintf("webhook.localSmee.enabled=%t", viper.GetBool("webhook.local_smee.enabled")))
	if u := viper.GetString("webhook.delivery_url"); u != "" {
		helmArgs = append(helmArgs, "--set", "webhook.deliveryURL="+u)
	}
	helmArgs = append(helmArgs, "--set",
		fmt.Sprintf("localOrgProvisioning.enabled=%t", viper.GetBool("oc.local_org_provisioning.enabled")))
	if ns := viper.GetString("oc.org_namespace"); ns != "" {
		helmArgs = append(helmArgs, "--set", "localOrgProvisioning.orgNamespace="+ns)
	}
	var helmOut bytes.Buffer
	helmCmd := exec.CommandContext(ctx, "helm", helmArgs...)
	helmCmd.Stdout = &helmOut
	helmCmd.Stderr = &helmOut
	if err := helmCmd.Run(); err != nil {
		return fmt.Errorf("helm install platform: %w\n%s", err, helmOut.String())
	}
	_, _ = fmt.Fprintln(os.Stdout, "Platform chart installed.")

	// 4. Wait for all platform pods.
	if err := waitForAllPodsReady(ctx, k8sClient, initPlatformNamespace, 10*time.Minute); err != nil {
		return err
	}

	// 5. Wait for the thunder-app-operator to provision all platform OAuth clients,
	// then patch Thunder's CORS configuration for the console.
	_, _ = fmt.Fprintln(os.Stdout, "Configuring Thunder OAuth clients...")
	if err := doThunderSetup(ctx, k8sClient, initPlatformNamespace,
		viper.GetString("thunder.namespace"),
		initConsoleURL,
		viper.GetString("thunder.config_map"),
		viper.GetString("thunder.deployment"),
	); err != nil {
		return err
	}

	_, _ = fmt.Fprintln(os.Stdout, "\nAEP is ready. Open the console to get started.")
	return nil
}

// verifyOpenBaoSecrets confirms all required secret paths exist in OpenBao.
// Used by --reuse-secrets to ensure a previous install seeded everything before
// skipping the provisioning step.
func verifyOpenBaoSecrets(ctx context.Context) error {
	pfCmd, err := openbao.PortForward(ctx, ocOpenBaoNamespace, ocOpenBaoRelease, kubeconfig)
	if err != nil {
		return err
	}
	defer func() { _ = pfCmd.Process.Kill() }()

	baseURL := "http://localhost:" + openbao.LocalPort
	if err := openbao.WaitForReachable(ctx, baseURL, 30*time.Second); err != nil {
		return fmt.Errorf("OpenBao not reachable: %w", err)
	}

	saToken, err := openbao.GetSAToken(ctx, ocOpenBaoNamespace, ocOpenBaoSA, kubeconfig)
	if err != nil {
		return err
	}
	token, err := openbao.KubernetesLogin(ctx, baseURL, ocWriteRole, saToken)
	if err != nil {
		return err
	}

	required := []string{
		"aep/anthropic-api-key",
		"aep/postgres-password",
		"aep/task-signing-key",
		"aep/oauth-state-key",
		"aep/agents-jwt-secret",
		"aep/webhook-secret",
		"aep/opensearch-username",
		"aep/opensearch-password",
		"aep/thunder-admin/client-id",
		"aep/thunder-admin/client-secret",
		"aep/thunder-clients/oc-workload-publisher",
		"aep/thunder-clients/oc-observer-reader",
		"aep/thunder-clients/aep-api-client",
		"aep/thunder-clients/bff-git-service",
		"aep/thunder-clients/bff-remote-worker",
		"aep/thunder-clients/local-dev-seeder",
		"aep/thunder-clients/system-client",
		"aep/thunder-clients/openchoreo-rca-agent",
	}

	var missing []string
	for _, path := range required {
		_, status, err := openbao.Req(ctx, "GET", baseURL, token, "/v1/secret/data/"+path, nil)
		if err != nil {
			return fmt.Errorf("check secret %s: %w", path, err)
		}
		if status == 404 {
			missing = append(missing, path)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("the following secrets are not in OpenBao:\n  %s", strings.Join(missing, "\n  "))
	}
	return nil
}

// provisionOpenBao seeds all platform secrets into OC's built-in OpenBao instance.
// Authenticates via Kubernetes auth using openchoreo-secret-writer-role, which OC's
// postStart already binds to any SA in the openbao namespace — no custom role needed.
func provisionOpenBao(ctx context.Context, anthropicKey, thunderAdminClientID, thunderAdminClientSecret string) error {
	progress := func(msg string) { _, _ = fmt.Fprintf(os.Stdout, "  %s\n", msg) }

	progress("Port-forwarding to OpenBao...")
	pfCmd, err := openbao.PortForward(ctx, ocOpenBaoNamespace, ocOpenBaoRelease, kubeconfig)
	if err != nil {
		return err
	}
	defer func() { _ = pfCmd.Process.Kill() }()

	baseURL := "http://localhost:" + openbao.LocalPort
	if err := openbao.WaitForReachable(ctx, baseURL, 30*time.Second); err != nil {
		return fmt.Errorf("OpenBao not reachable via port-forward: %w", err)
	}

	progress("Authenticating via Kubernetes auth...")
	saToken, err := openbao.GetSAToken(ctx, ocOpenBaoNamespace, ocOpenBaoSA, kubeconfig)
	if err != nil {
		return err
	}
	token, err := openbao.KubernetesLogin(ctx, baseURL, ocWriteRole, saToken)
	if err != nil {
		return err
	}

	progress("Generating secrets...")

	postgresPassword, err := bootstrap.GeneratePassword(32)
	if err != nil {
		return fmt.Errorf("generate postgres password: %w", err)
	}
	signingKey, err := bootstrap.GenerateRSAPrivateKey()
	if err != nil {
		return fmt.Errorf("generate signing key: %w", err)
	}
	oauthStateKey, err := bootstrap.GeneratePassword(32)
	if err != nil {
		return fmt.Errorf("generate oauth state key: %w", err)
	}
	agentsJWTSecret, err := bootstrap.GeneratePassword(32)
	if err != nil {
		return fmt.Errorf("generate agents JWT secret: %w", err)
	}
	webhookSecret, err := bootstrap.GeneratePassword(32)
	if err != nil {
		return fmt.Errorf("generate webhook secret: %w", err)
	}
	openSearchPassword, err := bootstrap.GeneratePassword(24)
	if err != nil {
		return fmt.Errorf("generate opensearch password: %w", err)
	}

	thunderClientNames := []string{
		"oc-workload-publisher",
		"oc-observer-reader",
		"aep-api-client",
		"bff-git-service",
		"bff-remote-worker",
		"local-dev-seeder",
		"system-client",
		"openchoreo-rca-agent",
	}
	// fixedClientSecrets: clients whose secret an OpenChoreo component bakes in
	// as a fixed default and cannot be told a random value.
	fixedClientSecrets := map[string]string{
		"oc-workload-publisher": "openchoreo-workload-publisher-secret",
	}
	thunderClientSecrets := make(map[string]string, len(thunderClientNames))
	for _, name := range thunderClientNames {
		if fixed, ok := fixedClientSecrets[name]; ok {
			thunderClientSecrets[name] = fixed
			continue
		}
		s, err := bootstrap.GeneratePassword(32)
		if err != nil {
			return fmt.Errorf("generate thunder client secret %s: %w", name, err)
		}
		thunderClientSecrets[name] = s
	}

	secrets := []struct{ path, value string }{
		{"aep/anthropic-api-key", anthropicKey},
		{"aep/postgres-password", postgresPassword},
		{"aep/task-signing-key", signingKey},
		{"aep/oauth-state-key", oauthStateKey},
		{"aep/agents-jwt-secret", agentsJWTSecret},
		{"aep/webhook-secret", webhookSecret},
		{"aep/opensearch-username", "admin"},
		{"aep/opensearch-password", openSearchPassword},
		{"aep/thunder-admin/client-id", thunderAdminClientID},
		{"aep/thunder-admin/client-secret", thunderAdminClientSecret},
	}
	for _, name := range thunderClientNames {
		secrets = append(secrets, struct{ path, value string }{
			"aep/thunder-clients/" + name, thunderClientSecrets[name],
		})
	}

	progress("Writing secrets to OpenBao...")
	for _, sec := range secrets {
		if _, err := openbao.Must(ctx, "PUT", baseURL, token, "/v1/secret/data/"+sec.path, map[string]interface{}{
			"data": map[string]interface{}{"value": sec.value},
		}); err != nil {
			return fmt.Errorf("write %s: %w", sec.path, err)
		}
		progress(fmt.Sprintf("  wrote secret/data/%s", sec.path))
	}

	progress("OpenBao provisioned successfully.")
	return nil
}

// verifyOpenBaoSecrets confirms all required secret paths exist in OpenBao.
// Used by --reuse-secrets to ensure a previous install seeded everything before
// skipping the provisioning step.
// deleteOrphanedResources removes cluster resources that may have been created
// by legacy setup scripts (setup-aep.sh) without Helm ownership labels. Helm
// refuses to adopt them on install, so we delete and let the chart recreate.
func deleteOrphanedResources(ctx context.Context) error {
	// If setup-aep.sh or setup-local.sh installed the thunder-app-operator as a
	// standalone Helm release, its ClusterRole/ClusterRoleBinding are owned by
	// that release and block the platform chart from installing the subchart.
	// Uninstall it first — the subchart takes over ownership in wso2-aep.
	helmStatusArgs := []string{"status", "thunder-app-operator", "-n", "thunder-app-operator-system"}
	if kubeconfig != "" {
		helmStatusArgs = append(helmStatusArgs, "--kubeconfig", kubeconfig)
	}
	if out := exec.CommandContext(ctx, "helm", helmStatusArgs...).Run(); out == nil {
		_, _ = fmt.Fprintln(os.Stdout, "  Removing standalone thunder-app-operator (replacing with platform subchart)...")
		helmUninstallArgs := []string{"uninstall", "thunder-app-operator", "-n", "thunder-app-operator-system"}
		if kubeconfig != "" {
			helmUninstallArgs = append(helmUninstallArgs, "--kubeconfig", kubeconfig)
		}
		if out, err := exec.CommandContext(ctx, "helm", helmUninstallArgs...).CombinedOutput(); err != nil {
			return fmt.Errorf("uninstall standalone thunder-app-operator: %w\n%s", err, out)
		}
	}

	// cluster-scoped: clusterauthzrolebinding, clustertrait
	// namespaced:     secretstore (lives in initPlatformNamespace)
	resources := []struct {
		kind, name, namespace string
	}{
		{"clusterauthzrolebinding", "aep-api-client-binding", ""},
		{"clustertrait", "api-configuration", ""},
		// Old namespaced SecretStore replaced by ClusterSecretStore aep-platform.
		{"secretstore", "openbao", initPlatformNamespace},
	}
	for _, r := range resources {
		args := []string{"delete", r.kind, r.name, "--ignore-not-found"}
		if r.namespace != "" {
			args = append(args, "-n", r.namespace)
		}
		if kubeconfig != "" {
			args = append([]string{"--kubeconfig", kubeconfig}, args...)
		}
		if out, err := exec.CommandContext(ctx, "kubectl", args...).CombinedOutput(); err != nil {
			return fmt.Errorf("delete %s/%s: %w: %s", r.kind, r.name, err, out)
		}
	}
	return nil
}

func checkOCVersion(ctx context.Context, client *kubernetes.Clientset, namespace, minVersion string) error {
	if _, err := client.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{}); err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("OpenChoreo namespace %q not found: "+
				"AEP requires OpenChoreo >= %s; provision it first, or pass --oc-namespace if yours differs",
				namespace, minVersion)
		}
		return fmt.Errorf("check OpenChoreo namespace %q: %w", namespace, err)
	}

	deps, err := client.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/part-of=openchoreo",
	})
	if err != nil {
		return fmt.Errorf("list OpenChoreo deployments in %q: %w", namespace, err)
	}

	for _, d := range deps.Items {
		ver, ok := d.Labels["app.kubernetes.io/version"]
		if !ok || ver == "" {
			continue
		}
		ver = strings.TrimPrefix(ver, "v")
		ok, err := versionAtLeast(ver, minVersion)
		if err != nil {
			return fmt.Errorf("parse OpenChoreo version %q: %w", ver, err)
		}
		if !ok {
			return fmt.Errorf("OpenChoreo version %s is below the minimum required version %s: "+
				"upgrade OpenChoreo to %s or later before running `aectl platform install`",
				ver, minVersion, minVersion)
		}
		return nil
	}

	return fmt.Errorf("could not determine OpenChoreo version from deployments in namespace %q: "+
		"ensure OpenChoreo >= %s is installed, or pass --skip-oc-version-check to bypass this check",
		namespace, minVersion)
}

func versionAtLeast(version, minimum string) (bool, error) {
	vParts, err := splitVersion(version)
	if err != nil {
		return false, fmt.Errorf("version %q: %w", version, err)
	}
	mParts, err := splitVersion(minimum)
	if err != nil {
		return false, fmt.Errorf("minimum %q: %w", minimum, err)
	}
	for i := 0; i < 3; i++ {
		if vParts[i] > mParts[i] {
			return true, nil
		}
		if vParts[i] < mParts[i] {
			return false, nil
		}
	}
	return true, nil
}

func splitVersion(v string) ([3]int, error) {
	parts := strings.SplitN(v, ".", 3)
	if len(parts) != 3 {
		return [3]int{}, fmt.Errorf("expected major.minor.patch, got %q", v)
	}
	var out [3]int
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return [3]int{}, fmt.Errorf("non-numeric segment %q", p)
		}
		out[i] = n
	}
	return out, nil
}

func checkBuildRegistry(ctx context.Context, client *kubernetes.Clientset, namespace, service string) error {
	if _, err := client.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{}); err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("OpenChoreo build plane namespace %q not found: "+
				"AEP requires a pre-provisioned OpenChoreo build/workflow plane with its image registry "+
				"(the coding-agent build pipeline pushes and pulls images there); "+
				"provision it first, or pass --build-plane-namespace if yours differs", namespace)
		}
		return fmt.Errorf("check build plane namespace %q: %w", namespace, err)
	}
	if _, err := client.CoreV1().Services(namespace).Get(ctx, service, metav1.GetOptions{}); err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("build image registry Service %q not found in namespace %q: "+
				"the coding-agent build pipeline needs an in-cluster image registry (publish + deploy-time pull); "+
				"provision the OpenChoreo build plane's registry before running `aectl platform install`, "+
				"or pass --registry-service if yours is named differently", service, namespace)
		}
		return fmt.Errorf("check registry Service %q/%q: %w", namespace, service, err)
	}
	return nil
}

func waitForAllPodsReady(ctx context.Context, client *kubernetes.Clientset, namespace string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	_, _ = fmt.Fprintf(os.Stdout, "Waiting for platform pods")

	var lastNotReady []string
	for {
		pods, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			_, _ = fmt.Fprintln(os.Stdout)
			return fmt.Errorf("list pods: %w", err)
		}

		var notReady []string
		for _, p := range pods.Items {
			ready := false
			switch p.Status.Phase {
			case "Succeeded":
				ready = true
			case "Running":
				ready = true
				for _, c := range p.Status.ContainerStatuses {
					if !c.Ready {
						ready = false
						break
					}
				}
			}
			if !ready {
				notReady = append(notReady, p.Name)
			}
		}

		if len(pods.Items) > 0 && len(notReady) == 0 {
			_, _ = fmt.Fprintln(os.Stdout, " ready")
			return nil
		}
		lastNotReady = notReady

		if time.Now().After(deadline) {
			_, _ = fmt.Fprintln(os.Stdout)
			return fmt.Errorf("timed out after %s waiting for pods: %s", timeout, strings.Join(lastNotReady, ", "))
		}

		_, _ = fmt.Fprintf(os.Stdout, ".")
		select {
		case <-ctx.Done():
			_, _ = fmt.Fprintln(os.Stdout)
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

func readMaskedInput(prompt string) (string, error) {
	_, _ = fmt.Fprintf(os.Stderr, "%s: ", prompt)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	_, _ = fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}
