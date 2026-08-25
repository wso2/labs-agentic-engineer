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
	"encoding/json"
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

	"github.com/wso2/aep/aectl/internal/addons"
	"github.com/wso2/aep/aectl/internal/bootstrap"
	"github.com/wso2/aep/aectl/internal/config"
	aectlhelm "github.com/wso2/aep/aectl/internal/helm"
	k8s "github.com/wso2/aep/aectl/internal/kubernetes"
	"github.com/wso2/aep/aectl/internal/openbao"
	"github.com/wso2/aep/aectl/internal/ui"
)

var platformCmd = &cobra.Command{
	Use:   "platform",
	Short: "Manage the AEP platform installation",
	Long: `Commands for installing, configuring, updating, and removing the AEP
platform on a Kubernetes cluster.`,
}

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
	rootCmd.AddCommand(platformCmd)
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

	if errs := config.ValidateLoaded(); len(errs) > 0 {
		ui.Fail("Missing or invalid config. Run 'aectl platform config import --config <file>' first:")
		for _, e := range errs {
			ui.Detail(e)
		}
		return fmt.Errorf("config validation failed")
	}
	ui.Success("Config loaded")

	if _, err := exec.LookPath("helm"); err != nil {
		return fmt.Errorf("helm not found in PATH — install from https://helm.sh/docs/intro/install/")
	}
	ui.Success("helm " + shortToolVersion("helm", "version", "--short"))

	if _, err := exec.LookPath("kubectl"); err != nil {
		return fmt.Errorf("kubectl not found in PATH — install from https://kubernetes.io/docs/tasks/tools/")
	}
	ui.Success("kubectl " + shortToolVersion("kubectl", "version", "--client", "--short"))

	k8sClient, err := k8s.NewClient(kubeconfig)
	if err != nil {
		return fmt.Errorf("connect to cluster: %w", err)
	}

	ocNamespace := viper.GetString("oc.system_namespace")
	if !initSkipOCVersionCheck {
		if err := checkOCVersion(ctx, k8sClient, ocNamespace, minOCVersion); err != nil {
			return err
		}
		ui.Success(fmt.Sprintf("OpenChoreo ≥ %s", minOCVersion))
	} else {
		ui.Warn("OpenChoreo version check skipped")
	}

	if err := checkBuildRegistry(ctx, k8sClient, initBuildPlaneNamespace, initRegistryService); err != nil {
		return err
	}
	ui.Success(fmt.Sprintf("Build registry %s/%s", initBuildPlaneNamespace, initRegistryService))

	if err := checkAPIPlatform(ctx, k8sClient); err != nil {
		return err
	}

	if err := checkOrConfigureGatewayIngress(ctx, k8sClient); err != nil {
		return err
	}

	openBaoDirect := viper.GetBool("codingagent.openbao_direct.enabled")

	if initReuseSecrets {
		fmt.Println()
		sp := ui.NewSpinner("Verifying existing OpenBao secrets")
		sp.Start()
		if err := verifyOpenBaoSecrets(ctx); err != nil {
			sp.Fail("Secret verification failed")
			return fmt.Errorf("reuse-secrets verification failed: %w\nRemove --reuse-secrets to run a fresh install", err)
		}
		sp.Success("Existing secrets verified")
	} else {
		fmt.Println()
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
		} else {
			ui.Success("Anthropic API key (from env)")
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
		} else {
			ui.Success("Thunder admin client secret (from env)")
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

		fmt.Println()
		if err := provisionOpenBao(ctx, anthropicKey, adminClientID, adminClientSecret); err != nil {
			return fmt.Errorf("provision OpenBao: %w", err)
		}
	}

	fmt.Println()
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
	}
	// Chart source: local path takes precedence, otherwise OCI registry.
	// Must be inserted at index 3: after "upgrade", "--install", <release>.
	chartLabel := "aep-platform"
	if initPlatformChart != "" {
		helmArgs = append(helmArgs[:3:3], append([]string{initPlatformChart}, helmArgs[3:]...)...)
		chartLabel = initPlatformChart
	} else {
		helmArgs = append(helmArgs[:3:3], append([]string{"oci://ghcr.io/wso2/aep/charts/aep-platform"}, helmArgs[3:]...)...)
		if initPlatformVersion != "latest" {
			helmArgs = append(helmArgs, "--version", initPlatformVersion)
			chartLabel = "aep-platform@" + initPlatformVersion
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

	helmSp := ui.NewSpinner(fmt.Sprintf("Installing %s", chartLabel))
	helmSp.Start()
	var helmOut bytes.Buffer
	helmCmd := exec.CommandContext(ctx, "helm", helmArgs...)
	helmCmd.Stdout = &helmOut
	helmCmd.Stderr = &helmOut
	if err := helmCmd.Run(); err != nil {
		helmSp.Fail("Helm install failed")
		return fmt.Errorf("helm install platform: %w\n%s", err, helmOut.String())
	}
	helmSp.Success(fmt.Sprintf("%s installed", chartLabel))

	if err := waitForAllPodsReady(ctx, k8sClient, initPlatformNamespace, 10*time.Minute); err != nil {
		return err
	}

	fmt.Println()
	ui.Step("Registering Thunder OAuth clients")
	if err := doThunderSetup(ctx, k8sClient, initPlatformNamespace,
		viper.GetString("thunder.namespace"),
		initConsoleURL,
		viper.GetString("thunder.config_map"),
		viper.GetString("thunder.deployment"),
	); err != nil {
		return err
	}
	ui.Success("Thunder configured")

	platformVersion := initPlatformVersion
	if platformVersion == "latest" {
		// Resolve the concrete chart version of the platform release just
		// installed, so addon operators are pinned to a matching --version.
		// A failure here must abort: silently installing operators without a
		// version pin would pull whatever the registry defaults to.
		platformVersion, err = resolvedPlatformVersion(ctx, initPlatformNamespace, initPlatformRelease)
		if err != nil {
			return fmt.Errorf("resolve platform version for addon operators: %w", err)
		}
	}
	if err := installAddons(ctx, k8sClient, platformVersion); err != nil {
		return err
	}
	ui.Ready(initConsoleURL)
	return nil
}

// manifestApplier is the subset of kubernetes.Applier used by the addon flow.
type manifestApplier interface {
	ApplyYAML(ctx context.Context, fieldManager, defaultNamespace, manifests string) error
	Exists(ctx context.Context, apiVersion, kind, namespace, name string) (bool, error)
}

// addonDeps holds the external calls that installAddons depends on, allowing
// them to be replaced in tests without modifying any shared package state.
type addonDeps struct {
	multiSelect     func(string, []ui.SelectItem) ([]bool, bool)
	confirm         func(string) bool
	installOperator func(context.Context, string, addons.OperatorSpec) error
	newApplier      func(string) (manifestApplier, error)
	// waitForSecrets blocks until all named Secrets exist in namespace or the
	// context deadline is exceeded. Nil skips the wait (tests that do not
	// exercise credential synchronization may omit this dep).
	waitForSecrets func(ctx context.Context, namespace string, names []string) error
}

var defaultAddonDeps = addonDeps{
	multiSelect:     ui.MultiSelect,
	confirm:         ui.Confirm,
	installOperator: aectlhelm.InstallOperator,
	newApplier: func(kc string) (manifestApplier, error) {
		return k8s.NewApplier(kc)
	},
}

// installAddons presents the optional addon selector and applies chosen addons.
// It installs any required operators first, prompting for confirmation, then
// applies addon manifests. Addons whose operator failed are skipped.
// platformVersion is used as the Helm --version for operators whose OperatorSpec.Version is empty.
func installAddons(ctx context.Context, client *kubernetes.Clientset, platformVersion string) error {
	deps := defaultAddonDeps
	deps.waitForSecrets = func(ctx context.Context, namespace string, names []string) error {
		return waitForSecretsReady(ctx, client, namespace, names, 3*time.Minute)
	}
	return runAddonInstall(ctx, platformVersion, deps)
}

func runAddonInstall(ctx context.Context, platformVersion string, deps addonDeps) error {
	if len(addons.Available) == 0 {
		return nil
	}

	// Phase 0: addon selection
	items := make([]ui.SelectItem, len(addons.Available))
	for i, a := range addons.Available {
		items[i] = ui.SelectItem{Label: a.Label, Description: a.Description}
	}

	selected, confirmed := deps.multiSelect("Optional platform resources", items)
	if !confirmed {
		return nil
	}

	var chosen []addons.Addon
	for i, a := range addons.Available {
		if selected[i] {
			chosen = append(chosen, a)
		}
	}
	if len(chosen) == 0 {
		return nil
	}

	// Phase 1: operator installation
	operatorFailed := map[string]error{}

	var withOp []addons.Addon
	for _, a := range chosen {
		if a.Operator.ReleaseName != "" {
			withOp = append(withOp, a)
		}
	}

	if len(withOp) > 0 {
		fmt.Println()
		fmt.Printf("  %s\n", ui.Gray("The following operators will be installed:"))
		for _, a := range withOp {
			ui.Detail(fmt.Sprintf("%-28s (for %s)", a.Operator.DisplayName, a.Label))
		}
		fmt.Println()

		if !deps.confirm("Install these operators?") {
			ui.Warn("Operator installation skipped — addons will not be applied")
			fmt.Println()
			return nil
		}

		for _, a := range withOp {
			op := a.Operator
			if op.Version == "" && platformVersion != "" {
				op.Version = platformVersion
			}

			if len(op.PreManifests) > 0 {
				preSp := ui.NewSpinner(fmt.Sprintf("Preparing %s prerequisites", op.DisplayName))
				preSp.Start()
				preApplier, err := deps.newApplier(kubeconfig)
				if err != nil {
					preSp.Fail(fmt.Sprintf("Failed to prepare %s prerequisites", op.DisplayName))
					operatorFailed[a.Operator.ReleaseName] = fmt.Errorf("build applier for %s pre-manifests: %w", op.ReleaseName, err)
					continue
				}
				var preErr error
				for _, m := range op.PreManifests {
					if preErr = preApplier.ApplyYAML(ctx, "aectl", "", m); preErr != nil {
						break
					}
				}
				if preErr != nil {
					preSp.Fail(fmt.Sprintf("Failed to prepare %s prerequisites", op.DisplayName))
					operatorFailed[a.Operator.ReleaseName] = fmt.Errorf("apply %s pre-manifests: %w", op.ReleaseName, preErr)
					continue
				}
				preSp.Success(fmt.Sprintf("%s prerequisites applied", op.DisplayName))
			}

			if len(op.WaitForSecrets) > 0 && deps.waitForSecrets != nil {
				waitSp := ui.NewSpinner(fmt.Sprintf("Waiting for %s credentials", op.DisplayName))
				waitSp.Start()
				if err := deps.waitForSecrets(ctx, op.Namespace, op.WaitForSecrets); err != nil {
					waitSp.Fail(fmt.Sprintf("%s credentials not ready", op.DisplayName))
					operatorFailed[a.Operator.ReleaseName] = fmt.Errorf("wait for %s secrets: %w", op.ReleaseName, err)
					continue
				}
				waitSp.Success(fmt.Sprintf("%s credentials ready", op.DisplayName))
			}

			sp := ui.NewSpinner(fmt.Sprintf("Installing %s...", op.DisplayName))
			sp.Start()
			if err := deps.installOperator(ctx, kubeconfig, op); err != nil {
				sp.Fail(fmt.Sprintf("%s install failed", op.DisplayName))
				operatorFailed[a.Operator.ReleaseName] = err
			} else {
				sp.Success(fmt.Sprintf("%s installed", op.DisplayName))
			}
		}

		if len(operatorFailed) > 0 {
			fmt.Println()
			for _, a := range withOp {
				label := fmt.Sprintf("%-24s", a.Operator.ReleaseName+":")
				if operatorFailed[a.Operator.ReleaseName] != nil {
					ui.Fail(label + " failed — skipping " + a.Label)
				} else {
					ui.Success(label + " installed")
				}
			}
			fmt.Println()
		}
	}

	// Phase 2: manifest application (skips addons whose operator failed).
	// The applier is built lazily so a fully-failed operator run avoids a
	// kubeconfig read entirely.
	var applier manifestApplier
	any := false
	for _, a := range chosen {
		if a.Operator.ReleaseName != "" && operatorFailed[a.Operator.ReleaseName] != nil {
			continue
		}
		if applier == nil {
			ap, err := deps.newApplier(kubeconfig)
			if err != nil {
				return fmt.Errorf("build applier: %w", err)
			}
			applier = ap
		}
		any = true
		sp := ui.NewSpinner(fmt.Sprintf("Applying %s", a.Label))
		sp.Start()
		for _, manifest := range a.Manifests {
			if err := applier.ApplyYAML(ctx, "aectl", "", manifest); err != nil {
				sp.Fail(fmt.Sprintf("Failed to apply %s", a.Label))
				return fmt.Errorf("apply addon %s: %w", a.ID, err)
			}
		}
		for _, v := range a.VerifyResources {
			ok, err := applier.Exists(ctx, v.APIVersion, v.Kind, v.Namespace, v.Name)
			if err != nil {
				sp.Fail(fmt.Sprintf("Verification failed for %s/%s", v.Kind, v.Name))
				return fmt.Errorf("verify addon %s: %w", a.ID, err)
			}
			if !ok {
				sp.Fail(fmt.Sprintf("%s/%s not found after apply", v.Kind, v.Name))
				return fmt.Errorf("addon %s: %s/%s was not created", a.ID, v.Kind, v.Name)
			}
		}
		sp.Success(fmt.Sprintf("%s applied", a.Label))
		for _, v := range a.VerifyResources {
			ui.Detail(fmt.Sprintf("%s/%s", v.Kind, v.Name))
		}
	}

	if any {
		fmt.Println()
	}
	return nil
}

// resolvedPlatformVersion queries the installed Helm release to find the chart
// version that was actually deployed. Used when --platform-version is "latest"
// so that add-on operators released on the same cadence can use the same version.
// Returns an empty string on any failure — the caller falls back gracefully.
// resolvedPlatformVersion returns the concrete chart version of the named Helm
// release. It uses `helm get metadata`, which targets an exact release (unlike
// `helm list -f`, whose regex filter can match several releases) and reports the
// chart version directly (rather than parsing it out of a "<name>-<version>"
// string, which breaks when the chart name differs from the release name). Helm
// and parse errors are propagated; an empty or mismatched result is an error.
func resolvedPlatformVersion(ctx context.Context, namespace, release string) (string, error) {
	args := []string{"get", "metadata", release, "-n", namespace, "-o", "json"}
	if kubeconfig != "" {
		args = append(args, "--kubeconfig", kubeconfig)
	}
	out, err := exec.CommandContext(ctx, "helm", args...).Output()
	if err != nil {
		return "", fmt.Errorf("helm get metadata %s: %w", release, err)
	}
	return parsePlatformVersion(out, release)
}

// parsePlatformVersion extracts and validates the chart version from the JSON
// output of `helm get metadata -o json`. It is separated from the exec call so
// the parsing and validation rules can be unit-tested against fixtures.
func parsePlatformVersion(out []byte, release string) (string, error) {
	var meta struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(out, &meta); err != nil {
		return "", fmt.Errorf("parse helm metadata for %s: %w", release, err)
	}
	// Guard against querying (or helm returning) a different release than asked.
	if meta.Name != release {
		return "", fmt.Errorf("helm metadata returned release %q, expected %q", meta.Name, release)
	}
	if meta.Version == "" {
		return "", fmt.Errorf("helm metadata for %s reported an empty chart version", release)
	}
	return meta.Version, nil
}

// shortToolVersion returns the first line of a tool's version output.
// Returns "unknown" if the command fails.
func shortToolVersion(name string, args ...string) string {
	out, err := exec.Command(name, args...).Output()
	if err != nil {
		return "unknown"
	}
	line := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]
	return strings.TrimSpace(line)
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
func provisionOpenBao(ctx context.Context, anthropicKey, thunderAdminClientID, thunderAdminClientSecret string) error {
	sp := ui.NewSpinner("Connecting to OpenBao")
	sp.Start()

	pfCmd, err := openbao.PortForward(ctx, ocOpenBaoNamespace, ocOpenBaoRelease, kubeconfig)
	if err != nil {
		sp.Fail("Port-forward failed")
		return err
	}
	defer func() { _ = pfCmd.Process.Kill() }()

	baseURL := "http://localhost:" + openbao.LocalPort
	if err := openbao.WaitForReachable(ctx, baseURL, 30*time.Second); err != nil {
		sp.Fail("OpenBao unreachable")
		return fmt.Errorf("OpenBao not reachable via port-forward: %w", err)
	}

	sp.Update("Authenticating")
	saToken, err := openbao.GetSAToken(ctx, ocOpenBaoNamespace, ocOpenBaoSA, kubeconfig)
	if err != nil {
		sp.Fail("Authentication failed")
		return err
	}
	token, err := openbao.KubernetesLogin(ctx, baseURL, ocWriteRole, saToken)
	if err != nil {
		sp.Fail("Authentication failed")
		return err
	}

	sp.Update("Generating secrets")

	postgresPassword, err := bootstrap.GeneratePassword(32)
	if err != nil {
		sp.Fail("Secret generation failed")
		return fmt.Errorf("generate postgres password: %w", err)
	}
	signingKey, err := bootstrap.GenerateRSAPrivateKey()
	if err != nil {
		sp.Fail("Secret generation failed")
		return fmt.Errorf("generate signing key: %w", err)
	}
	oauthStateKey, err := bootstrap.GeneratePassword(32)
	if err != nil {
		sp.Fail("Secret generation failed")
		return fmt.Errorf("generate oauth state key: %w", err)
	}
	agentsJWTSecret, err := bootstrap.GeneratePassword(32)
	if err != nil {
		sp.Fail("Secret generation failed")
		return fmt.Errorf("generate agents JWT secret: %w", err)
	}
	webhookSecret, err := bootstrap.GeneratePassword(32)
	if err != nil {
		sp.Fail("Secret generation failed")
		return fmt.Errorf("generate webhook secret: %w", err)
	}
	openSearchPassword, err := bootstrap.GeneratePassword(24)
	if err != nil {
		sp.Fail("Secret generation failed")
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
			sp.Fail("Secret generation failed")
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
		// system-client ID is a known constant but stored in OpenBao so the
		// thunder-app-operator can source both credentials from the same ESO Secret.
		{"aep/thunder-clients/system-client-id", "aep-system-client"},
	}
	for _, name := range thunderClientNames {
		secrets = append(secrets, struct{ path, value string }{
			"aep/thunder-clients/" + name, thunderClientSecrets[name],
		})
	}

	sp.Update(fmt.Sprintf("Writing %d secrets", len(secrets)))
	for i, sec := range secrets {
		sp.Update(fmt.Sprintf("Writing secrets (%d/%d)", i+1, len(secrets)))
		if _, err := openbao.Must(ctx, "PUT", baseURL, token, "/v1/secret/data/"+sec.path, map[string]interface{}{
			"data": map[string]interface{}{"value": sec.value},
		}); err != nil {
			sp.Fail(fmt.Sprintf("Failed writing %s", sec.path))
			return fmt.Errorf("write %s: %w", sec.path, err)
		}
	}

	sp.Success(fmt.Sprintf("%d secrets provisioned", len(secrets)))
	return nil
}

// deleteOrphanedResources removes cluster resources that may have been created
// by legacy setup scripts (setup-aep.sh) without Helm ownership labels. Helm
// refuses to adopt them on install, so we delete and let the chart recreate.
func deleteOrphanedResources(ctx context.Context) error {
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
			// If the CRD itself is not registered the resource type is unknown;
			// that means the legacy resource was never created, so skip it.
			if strings.Contains(string(out), "the server doesn't have a resource type") {
				continue
			}
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
	// Drop pre-release ("1.2.0-rc.1" → "1.2.0") and build metadata
	// ("1.2.0+build.5" → "1.2.0") before parsing so that OC deployments
	// with non-stable version labels are not incorrectly rejected.
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.SplitN(v, ".", 3)
	// Normalise shorthand ("1.2" → "1.2.0").
	for len(parts) < 3 {
		parts = append(parts, "0")
	}
	var out [3]int
	for i, p := range parts[:3] {
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
	sp := ui.NewSpinner("Waiting for platform pods to be ready")
	sp.Start()

	deadline := time.Now().Add(timeout)
	var lastNotReady []string
	for {
		pods, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			sp.Fail("Failed to list pods")
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
			sp.Success(fmt.Sprintf("All %d pods ready", len(pods.Items)))
			return nil
		}
		lastNotReady = notReady

		if len(pods.Items) > 0 {
			sp.Update(fmt.Sprintf("Waiting for pods — %d/%d ready", len(pods.Items)-len(notReady), len(pods.Items)))
		}

		if time.Now().After(deadline) {
			sp.Fail("Timed out waiting for pods")
			return fmt.Errorf("timed out after %s waiting for pods: %s", timeout, strings.Join(lastNotReady, ", "))
		}

		select {
		case <-ctx.Done():
			sp.Fail("Cancelled")
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

// waitForSecretsReady polls until all named Secrets exist in namespace or
// timeout is exceeded. Returns nil once every Secret is present.
func waitForSecretsReady(ctx context.Context, client *kubernetes.Clientset, namespace string, names []string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		var missing []string
		for _, name := range names {
			if _, err := client.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{}); err != nil {
				if apierrors.IsNotFound(err) {
					missing = append(missing, name)
				} else {
					return fmt.Errorf("check secret %s/%s: %w", namespace, name, err)
				}
			}
		}
		if len(missing) == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for secrets in %s: %s", namespace, strings.Join(missing, ", "))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

func readMaskedInput(prompt string) (string, error) {
	_, _ = fmt.Fprintf(os.Stderr, "  %s %s: ", ui.Gray("◇"), prompt)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	_, _ = fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}
