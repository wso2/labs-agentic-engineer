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
	"text/template"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"

	k8s "github.com/wso2/aep/aectl/internal/kubernetes"
	"github.com/wso2/aep/aectl/internal/ui"
)

// SRE (RCA) agent install: the OpenChoreo SRE agent, wired to hand incidents to
// AEP. Secrets flow OpenBao->ESO (never plaintext), and the RCA->AEP handoff
// targets in-cluster Service DNS.
//
// Two modes, picked by what the cluster already has:
//   - an observability plane is installed (e.g. by setup-env-for-aectl.sh):
//     aectl adopts it. It enables the plane's own SRE agent at the plane's
//     installed chart version, so the plane is never replaced by a second
//     release of another version, and leaves the plane's secrets, logs chart
//     and ClusterObservabilityPlane to whoever installed them.
//   - none is installed: aectl installs the plane and logs charts itself at
//     --obs-plane-version / --obs-logs-version.
//
// The agent's LLM key/model/base URL and its AEP handoff token are pushed by
// aep-api into the AE-owned sre-agent-aep Secret (rca.extraEnvs) for the
// --org this command wires up; this command does not carry any LLM
// credential itself. Prerequisite: `aectl platform install` (it registers
// the openchoreo-rca-agent Thunder client and seeds the OpenBao secrets this
// reads).

var (
	sreNamespace       string // where AEP + OpenBao live (secret source)
	sreObsNamespace    string // observability plane namespace
	sreObsPlaneVersion string
	sreObsLogsVersion  string
	sreRcaImageRepo    string
	sreRcaImageTag     string
	sreRcaPullPolicy   string
	sreAdapterImage    string
	sreAEHandoff       bool
	sreObserverHost    string
	sreRcaHost         string
	sreMCPHost         string
	sreMCPPort         int
	sreAssetsRoot      string
	sreOrg             string
	srePlatformStore   string
	sreSkipOCVerCheck  bool
	// Passed through to the `aectl platform update` this command runs
	// in-process to flip sreAgent.* on the AEP platform release. Mirrors
	// `platform update`'s own --platform-chart/--version: required (one or
	// the other) so that release is never upgraded to an unpinned chart just
	// because `sre install` happened to run (see runSreInstall's early check).
	srePlatformChart   string
	srePlatformVersion string
	// Install-time SRE model seed (Task A2): when llmAPIKeyFile+llmModel are
	// both given, this command writes them (plus llmBaseURL) into the
	// sre-model-seed Secret and tells the platform release about it, so
	// aep-api can bring the SRE agent's model connection up without a
	// Console/API save. See resolveSreModelSeed.
	sreLLMAPIKeyFile string
	sreLLMModel      string
	sreLLMBaseURL    string
)

var sreCmd = &cobra.Command{
	Use:   "sre",
	Short: "Manage the SRE (RCA) agent + observability plane",
}

var sreInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install the observability plane + SRE/RCA agent and wire the AEP handoff",
	Long: `Detects the OpenChoreo Observability Plane and SRE/RCA agent; warns and
installs them if missing (idempotent), then applies the AEP integration:
obs-namespace secrets (via OpenBao/ESO), the alert->RCA auto-trigger + AEP
handoff wiring, the authz grants, and the observability CRs.

Run 'aectl init' first — it registers the openchoreo-rca-agent Thunder client and
seeds the OpenBao secrets this command reads via External Secrets.

Requires --platform-chart or --platform-version: this command also flips
sreAgent.* on the AEP platform release (org, obs namespace, RCA deployment
name, Secret, MCP hostname) via an internal 'aectl platform update', and a
pinned chart source keeps that call from silently upgrading the release to
whatever is latest on GHCR.`,
	RunE: runSreInstall,
}

func init() {
	rootCmd.AddCommand(sreCmd)
	sreCmd.AddCommand(sreInstallCmd)

	f := sreInstallCmd.Flags()
	f.StringVar(&sreNamespace, "namespace", "wso2-aep", "Namespace where AEP + OpenBao are installed")
	f.StringVar(&sreObsNamespace, "obs-namespace", "openchoreo-observability-plane", "Observability plane namespace")
	f.StringVar(&sreObsPlaneVersion, "obs-plane-version", "1.3.0", "openchoreo-observability-plane chart version, when no plane is installed yet (an installed plane keeps its own)")
	f.StringVar(&sreObsLogsVersion, "obs-logs-version", "0.5.3", "observability-logs-opensearch chart version, when no plane is installed yet")
	f.StringVar(&sreRcaImageRepo, "rca-image-repo", "ghcr.io/openchoreo/sre-agent", "RCA/SRE agent image repository")
	f.StringVar(&sreRcaImageTag, "rca-image-tag", "v1.3.0@sha256:25e5e6c423d049460f4a60d95497747c2a99b02713faf8523a38ef8e6a85c599", "RCA/SRE agent image tag (digest-pinned so both architectures resolve the same build)")
	f.StringVar(&sreRcaPullPolicy, "rca-image-pull-policy", "IfNotPresent", "RCA/SRE agent image pull policy")
	f.StringVar(&sreAdapterImage, "adapter-image", "docker.io/tharindulak/observability-logs-opensearch-adapter:0.5.1-case-insensitive", "logs-adapter image (repo:tag)")
	f.BoolVar(&sreAEHandoff, "ae-handoff", true, "Enable the RCA->AEP coding-agent handoff (mounts the SRE remediation extension)")
	f.StringVar(&sreObserverHost, "observer-hostname", "observer.openchoreo.localhost", "Observer gateway hostname")
	f.StringVar(&sreRcaHost, "rca-hostname", "rca-agent.openchoreo.localhost", "RCA agent gateway hostname")
	f.StringVar(&sreMCPHost, "mcp-hostname", "aep-mcp.openchoreo.localhost", "aep-mcp-server's https hostname on the control-plane gateway (must match the platform chart's sreAgent.mcpHostname)")
	f.IntVar(&sreMCPPort, "mcp-port", 8443, "https port of the control-plane gateway listener aep-mcp-server is routed on (k3d publishes 8443)")
	f.StringVar(&sreAssetsRoot, "assets-root", "", "AE repository checkout holding the SRE extension assets (deployments/sre-agent-extensions, services/aep-mcp-server/skills); default: search upward from the working directory")
	f.StringVar(&sreOrg, "org", "", "OpenChoreo org whose SRE agent this install wires up (aep-api pushes that org's LLM key/model and handoff token into the agent's Secret)")
	f.StringVar(&srePlatformStore, "platform-secret-store", "aep-platform", "ClusterSecretStore the platform chart installs for the aep/* OpenBao paths")
	f.StringVar(&srePlatformChart, "platform-chart", "", "Local path to the AEP platform chart, for the internal `aectl platform update` that flips sreAgent.* (mirrors `platform update`'s own --platform-chart; one of --platform-chart/--platform-version is required)")
	f.StringVar(&srePlatformVersion, "platform-version", "", "AEP platform chart version, for the internal `aectl platform update` that flips sreAgent.* (mirrors `platform update`'s own --version; one of --platform-chart/--platform-version is required)")
	f.StringVar(&sreLLMAPIKeyFile, "llm-api-key-file", "", "Path to a file holding the org's SRE model API key, seeded into the sre-model-seed Secret at install so aep-api can bring the SRE agent up without a Console/API save (the key is read from this file, never taken as a flag value; must be given with --llm-model)")
	f.StringVar(&sreLLMModel, "llm-model", "", "Model name for the install-time SRE model seed (e.g. gpt-5.4; must be given with --llm-api-key-file)")
	f.StringVar(&sreLLMBaseURL, "llm-base-url", "https://api.openai.com/v1", "OpenAI-compatible base URL for the install-time SRE model seed")
	f.String("oc-api-url", "", "In-cluster OpenChoreo platform API URL (overrides config)")
	_ = viper.BindPFlag("oc.api_url", f.Lookup("oc-api-url"))
	f.BoolVar(&sreSkipOCVerCheck, "skip-oc-version-check", false, "Skip the OpenChoreo minimum version check (not recommended)")
	_ = sreInstallCmd.MarkFlagRequired("org")
}

// sreParams holds everything the value/manifest templates need.
type sreParams struct {
	ObsNamespace, PlatformSecretStore                         string
	OCApiURL, ThunderJwksURL, ThunderTokenURL, ThunderAuthURL string
	RcaImageRepo, RcaImageTag, RcaPullPolicy                  string
	AdapterRepo, AdapterTag                                   string
	ObserverHost, RcaHost                                     string
	// Handoff wiring. AEMCPURL is aep-mcp-server's MCP endpoint as the agent
	// reaches it (sreMCPURL), used both for the rendered remediation
	// mcp.json and as the rca.extraEnvs AEP_MCP_URL value (the agent's
	// RCA_LLM_API_KEY/RCA_MODEL_NAME/RCA_LLM_BASE_URL/AEP_MCP_TOKEN come from
	// the sre-agent-aep Secret aep-api owns instead, via secretKeyRef).
	RcaServiceURL, AEMCPURL string
	// MCPHostname is aep-mcp-server's https hostname (--mcp-hostname, the
	// same one AEMCPURL was built from), passed to the platform release as
	// sreAgent.mcpHostname so the chart and this command can't drift.
	MCPHostname string
	AEHandoff   bool
	// Org is the OpenChoreo org whose observability-plane SRE agent aep-api's
	// reconciler pushes the LLM key/model and handoff token to.
	Org string
	// RcaName is the RCA/SRE agent Deployment name, set from
	// findSREAgentDeployment's discovery (the chart can rename it, e.g.
	// ai-rca-agent -> sre-agent in 1.2.0). AEPNamespace is the AEP namespace
	// (--namespace) aep-api's ServiceAccount lives in. Both feed
	// sreAgentPushRoleTmpl and the platform chart's
	// sreAgent.{deployment,org,namespace,secret,mcpHostname} values.
	RcaName, AEPNamespace string
	// ForceSync is a per-run value (unix seconds) written into every
	// ExternalSecret's force-sync annotation. ESO re-syncs an ExternalSecret
	// whenever its spec changes, so bumping this on every run makes a re-run
	// with an otherwise-unchanged spec still trigger a fresh sync instead of
	// waiting out the 1h refreshInterval — see waitForExternalSecretRefresh.
	ForceSync string
}

// sreMCPURL is aep-mcp-server's MCP endpoint as the SRE agent reaches it:
// https through the control-plane gateway (the platform chart's
// aep-mcp-server HTTPRoute), because the agent's extension loader sends the
// Authorization header only to https URLs. Port 443 is left implicit.
func sreMCPURL(host string, port int) string {
	if port == 443 {
		return "https://" + host + "/mcp"
	}
	return fmt.Sprintf("https://%s:%d/mcp", host, port)
}

func runSreInstall(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	if _, err := exec.LookPath("helm"); err != nil {
		return fmt.Errorf("helm is required but was not found in PATH\nInstall it from https://helm.sh/docs/intro/install/ and try again")
	}

	// This command flips sreAgent.* on the AEP platform release via `aectl
	// platform update`'s own code path; without an explicit chart source that
	// call falls back to the unversioned OCI chart, silently upgrading the
	// platform release to whatever is latest on GHCR. Fail fast rather than
	// risk that.
	if srePlatformChart == "" && srePlatformVersion == "" {
		return fmt.Errorf("--platform-chart or --platform-version is required (pins the platform chart this command's internal `aectl platform update` upgrades — without one it would silently pull the latest unpinned chart from GHCR)")
	}

	// Resolve+validate the install-time SRE model seed before touching the
	// cluster, so a bad --llm-* flag combination fails fast rather than
	// after a partially-applied install.
	seed, err := resolveSreModelSeed(sreLLMAPIKeyFile, sreLLMModel, sreLLMBaseURL)
	if err != nil {
		return err
	}

	client, err := k8s.NewClient(kubeconfig)
	if err != nil {
		return fmt.Errorf("connect to cluster: %w", err)
	}

	if err := enforceOCVersion(ctx, client, viper.GetString("oc.system_namespace"), sreSkipOCVerCheck); err != nil {
		return err
	}

	applier, err := k8s.NewApplier(kubeconfig)
	if err != nil {
		return fmt.Errorf("build applier: %w", err)
	}

	thunderURL := viper.GetString("thunder.url")
	p := sreParams{
		ObsNamespace:        sreObsNamespace,
		PlatformSecretStore: srePlatformStore,
		OCApiURL:            viper.GetString("oc.api_url"),
		ThunderJwksURL:      thunderURL + "/oauth2/jwks",
		ThunderTokenURL:     thunderURL + "/oauth2/token",
		ThunderAuthURL:      viper.GetString("thunder.public_url"),
		RcaImageRepo:        sreRcaImageRepo,
		RcaImageTag:         sreRcaImageTag,
		RcaPullPolicy:       sreRcaPullPolicy,
		ObserverHost:        sreObserverHost,
		RcaHost:             sreRcaHost,
		AEHandoff:           sreAEHandoff,
		Org:                 sreOrg,
		AEPNamespace:        sreNamespace,
		ForceSync:           strconv.FormatInt(time.Now().Unix(), 10),
	}
	p.AEMCPURL = sreMCPURL(sreMCPHost, sreMCPPort)
	p.MCPHostname = sreMCPHost
	// Split on the LAST colon so a registry port (registry:5000/img:tag) is
	// kept in the repo; image tags never contain a colon.
	if i := strings.LastIndex(sreAdapterImage, ":"); i > 0 && i < len(sreAdapterImage)-1 {
		p.AdapterRepo, p.AdapterTag = sreAdapterImage[:i], sreAdapterImage[i+1:]
	} else {
		return fmt.Errorf("--adapter-image must be repo:tag, got %q", sreAdapterImage)
	}

	// Resolve the handoff extension before touching the cluster, so a missing
	// checkout fails fast instead of leaving a half-installed plane.
	var assets sreExtensionAssets
	if sreAEHandoff {
		if assets, err = loadSreExtensionAssets(sreAssetsRoot); err != nil {
			return err
		}
	}

	// 1. Detect the plane.
	plane, adopt, err := installedObsPlane(ctx, sreObsNamespace)
	if err != nil {
		return err
	}
	if adopt {
		ui.Detail(fmt.Sprintf("Observability plane %q (chart %s) found in %q — enabling its SRE agent at that version.", plane.Name, plane.Version, sreObsNamespace))
	} else {
		ui.Warn(fmt.Sprintf("No observability plane in %q — installing chart %s.", sreObsNamespace, sreObsPlaneVersion))
	}

	// 2. Namespace + cluster-gateway-ca + secrets via OpenBao->ESO.
	if err := ensureNamespace(ctx, client, sreObsNamespace); err != nil {
		return err
	}
	ui.Step("Applying obs-namespace ExternalSecrets (OpenBao->ESO)")
	// Earlier aectl versions authored their own obs-namespace SecretStore,
	// logging in as an OpenBao role nothing creates; it never became Ready.
	if err := applier.Delete(ctx, "external-secrets.io/v1", "SecretStore", sreObsNamespace, "openbao"); err != nil {
		return fmt.Errorf("remove the legacy openbao SecretStore: %w", err)
	}
	if err := applyTemplate(ctx, applier, "sre-secrets", sreObsNamespace, sreAgentSecretsTmpl, p); err != nil {
		return fmt.Errorf("apply secrets: %w (did you run `aectl platform install`?)", err)
	}
	wantSecrets := []string{"rca-agent-secret"}
	if adopt {
		applied := time.Now()
		if err := applyTemplate(ctx, applier, "sre-observer-secret", sreObsNamespace, sreObserverClientSecretTmpl, p); err != nil {
			return fmt.Errorf("apply observer client secret: %w", err)
		}
		// The Secret already exists with the plane installer's value, so wait
		// for ESO to rewrite it rather than for it to appear.
		if err := waitForExternalSecretRefresh(ctx, applier, sreObsNamespace, "observer-secret", applied, 2*time.Minute); err != nil {
			return err
		}
	} else {
		// The obs-plane chart mounts a cluster-gateway-ca ConfigMap into its
		// cluster-agent pod but does not create it (same as the DP/WF planes).
		// Without it the cluster-agent sits in ContainerCreating forever.
		if err := ensureClusterGatewayCA(ctx, client, sreObsNamespace); err != nil {
			return fmt.Errorf("cluster-gateway-ca: %w", err)
		}
		if err := applyTemplate(ctx, applier, "sre-plane-secrets", sreObsNamespace, srePlaneSecretsTmpl, p); err != nil {
			return fmt.Errorf("apply plane secrets: %w", err)
		}
		wantSecrets = append(wantSecrets, "opensearch-admin-credentials", "observer-secret")
	}
	// ESO must materialise these before the charts start.
	for _, s := range wantSecrets {
		if err := waitForSecret(ctx, client, sreObsNamespace, s, 2*time.Minute); err != nil {
			return fmt.Errorf("%w\nESO did not sync %q — check the ExternalSecrets, the %s ClusterSecretStore, and that `aectl platform install` seeded OpenBao", err, s, srePlatformStore)
		}
	}
	ui.Success("Secrets synced")

	// 3. AE-owned Secret, create-only. Ahead of the Helm step below: the RCA
	// container's rca.extraEnvs secretKeyRefs need sre-agent-aep to exist
	// before it starts (its actual content is unset until aep-api's
	// reconciler pushes into it — that's fine, the container just waits).
	ui.Step("Applying the AE-owned SRE agent Secret")
	if err := ensureSREAgentSecret(ctx, client, sreObsNamespace, p); err != nil {
		return fmt.Errorf("ensure sre-agent-aep secret: %w", err)
	}

	// 4. Helm: enable the SRE agent on the installed plane, or install both
	// OpenChoreo charts.
	if adopt {
		if err := helmEnableSREAgent(ctx, p, plane); err != nil {
			return err
		}
		if _, err := client.AppsV1().DaemonSets(sreObsNamespace).Get(ctx, "fluent-bit", metav1.GetOptions{}); err != nil {
			ui.Warn(fmt.Sprintf("No fluent-bit DaemonSet in %q — container logs are not collected, so log alerts never fire.", sreObsNamespace))
		}
	} else {
		if err := helmInstallObsPlane(ctx, p); err != nil {
			return err
		}
		if err := helmInstallObsLogs(ctx, p); err != nil {
			return err
		}
	}
	agentDeploy, err := findSREAgentDeployment(ctx, client, sreObsNamespace)
	if err != nil {
		return err
	}
	p.RcaServiceURL = fmt.Sprintf("http://%s:8080", agentDeploy)
	// The chart may have renamed the Deployment (ai-rca-agent -> sre-agent in
	// 1.2.0); use the name actually running.
	p.RcaName = agentDeploy

	// 5. Push Role/RoleBinding scoped to the discovered Deployment name, then
	// flip the platform release's sreAgent.* values so aep-api's reconciler
	// knows where to push the agent's LLM key/model/base URL and handoff
	// token, and so the aep-mcp-server route/grant/policy the agent's
	// extension needs render.
	ui.Step("Applying aep-api's push Role and the platform sreAgent.* wiring")
	if err := applyTemplate(ctx, applier, "sre-push-role", sreObsNamespace, sreAgentPushRoleTmpl, p); err != nil {
		return fmt.Errorf("apply aep-api-sre-push Role/RoleBinding: %w", err)
	}
	// Write the install-time SRE model seed Secret before the platform
	// update below tells aep-api its name (sreAgent.seed.secretName), so the
	// Secret always exists by the time aep-api's reconciler could look for
	// it.
	seedSecretName := ""
	seedHash := ""
	if seed != nil {
		ui.Step("Writing the SRE model seed Secret")
		if err := ensureSREModelSeedSecret(ctx, client, sreNamespace, *seed); err != nil {
			return fmt.Errorf("write %s secret: %w", sreModelSeedSecretName, err)
		}
		seedSecretName = sreModelSeedSecretName
		seedHash = sreModelSeedHash(*seed)
	}
	if err := updatePlatformSreAgent(ctx, p, srePlatformChart, srePlatformVersion, seedSecretName, seedHash); err != nil {
		return fmt.Errorf("wire sreAgent.* on the platform release: %w", err)
	}
	ui.Success("Push Role and platform sreAgent.* wiring applied")

	// 6. Best-effort readiness (do NOT wait on the RCA/SRE agent deployment —
	// it stays unwired until step 7 patches observer-config with
	// RCA_SERVICE_URL). Warn (don't abort) so name/version drift in the
	// upstream charts can't wedge the install.
	waitForDeployment(ctx, client, sreObsNamespace, "observer", 5*time.Minute)
	waitForDeployment(ctx, client, sreObsNamespace, "controller-manager", 5*time.Minute)

	// 7. Alert->RCA auto-trigger + AEP handoff wiring (post-helm ConfigMap
	// patches; the charts don't expose all these keys). In-cluster URLs.
	ui.Step("Wiring alert->RCA auto-trigger + AEP handoff")
	if err := patchConfigMap(ctx, client, sreObsNamespace, "observer-config", map[string]string{
		"LOGS_ADAPTER_ENABLED":     "true",
		"RCA_SERVICE_URL":          p.RcaServiceURL,
		"ALERT_SUPPRESSION_WINDOW": "1h",
	}); err != nil {
		return err
	}
	_ = rolloutRestart(ctx, client, sreObsNamespace, "observer")
	if sreAEHandoff {
		ui.Step("Wiring the remediation agent's SRE-agent extensions (mcp.json/CONTEXT.md/skill)")
		if err := applyExtensionsConfigMap(ctx, client, sreObsNamespace, assets, p.AEMCPURL); err != nil {
			return fmt.Errorf("apply sre-agent-extensions configmap: %w", err)
		}
		_ = rolloutRestart(ctx, client, sreObsNamespace, agentDeploy)
		ui.Detail(fmt.Sprintf("AE handoff: enabled (mcp=%s, assets=%s)", p.AEMCPURL, assets.RootHint))
	} else {
		ui.Detail("AE handoff: disabled (--ae-handoff=false)")
	}

	// 8. Authz grants, plus the route and ClusterObservabilityPlane CR for a
	// plane aectl installed.
	ui.Step("Applying authz grants")
	if err := applyTemplate(ctx, applier, "sre-grants", sreObsNamespace, sreGrantsTmpl, p); err != nil {
		return fmt.Errorf("apply grants: %w", err)
	}
	if !adopt {
		ui.Step("Applying HTTPRoute and ClusterObservabilityPlane")
		if err := applyTemplate(ctx, applier, "sre-crs", sreObsNamespace, srePlaneCRsTmpl, p); err != nil {
			return fmt.Errorf("apply CRs: %w", err)
		}
	}

	// 9. OpenSearch index-template bootstrap (detect + self-heal).
	ui.Step("Running OpenSearch index-template bootstrap job")
	if err := k8s.RunJob(ctx, client, openSearchBootstrapJob(sreObsNamespace), os.Stdout); err != nil {
		ui.Warn(fmt.Sprintf("index-template bootstrap job did not complete cleanly: %v", err))
		ui.Detail("Log-based alerts may misbehave until the container-logs template maps log as 'wildcard'.")
	}

	printSreCompletion(p, seed)
	return nil
}

func printSreCompletion(p sreParams, seed *sreModelSeed) {
	ui.Success("SRE agent + observability plane installed")
	ui.Section("Security Note")
	ui.Detail(fmt.Sprintf("AE handoff is %s. RCA feeds pod logs to an LLM (prompt-injection", onOff(p.AEHandoff)))
	ui.Detail("surface), and a fired alert can drive automated code changes. Set")
	ui.Detail("--ae-handoff=false to disable the handoff.")
	ui.Detail("RCA/logs-adapter images are non-WSO2 registries (pin/mirror for prod).")
	ui.Section("Next Steps")
	ui.Detail("Create an ObservabilityAlertRule per component you want auto-RCA on")
	ui.Detail("(component UID + name labels, incident.enabled, triggerAiRca: true).")
	ui.Detail("Guide: docs/developer-guide/sre-handoff-runbook.md")
	if seed != nil {
		ui.Detail(fmt.Sprintf("SRE model seed written (model %s @ %s); aep-api applies it within ~1 minute unless the org already has an SRE model connection — check Settings → SRE agent model or kubectl -n %s get deploy sre-agent", seed.Model, seed.BaseURL, p.ObsNamespace))
	}
	fmt.Println()
}

func onOff(b bool) string {
	if b {
		return "ON"
	}
	return "OFF"
}

// ── helpers ──────────────────────────────────────────────────────────────────

func applyTemplate(ctx context.Context, applier *k8s.Applier, fieldManager, ns, tmpl string, p sreParams) error {
	t, err := template.New(fieldManager).Parse(tmpl)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, p); err != nil {
		return err
	}
	return applier.ApplyYAML(ctx, "aectl-sre", ns, buf.String())
}

// ensureSREAgentSecret applies sreAgentAEOwnedSecretTmpl only when
// sre-agent-aep does not already exist. aep-api's reconciler owns its actual
// content (LLM key/model/base URL, handoff token); a re-run of `aectl sre
// install` must never clobber values it already pushed.
func ensureSREAgentSecret(ctx context.Context, client kubernetes.Interface, ns string, p sreParams) error {
	if _, err := client.CoreV1().Secrets(ns).Get(ctx, "sre-agent-aep", metav1.GetOptions{}); err == nil {
		return nil // already present; aep-api may have pushed real values here — never overwrite
	} else if !apierrors.IsNotFound(err) {
		return fmt.Errorf("get sre-agent-aep secret: %w", err)
	}

	t, err := template.New("sre-agent-secret").Parse(sreAgentAEOwnedSecretTmpl)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, p); err != nil {
		return err
	}
	var sec corev1.Secret
	if err := yaml.Unmarshal(buf.Bytes(), &sec); err != nil {
		return fmt.Errorf("decode sre-agent-aep secret template: %w", err)
	}
	if _, err := client.CoreV1().Secrets(ns).Create(ctx, &sec, metav1.CreateOptions{}); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return nil // race: another install created it first
		}
		return fmt.Errorf("create sre-agent-aep secret: %w", err)
	}
	return nil
}

// sreAgentPlatformUpdateConfig builds the platformUpdateConfig that flips
// sreAgent.* on the AEP platform release. Split out from
// updatePlatformSreAgent so the (sreParams, chartPath, chartVersion) ->
// platformUpdateConfig mapping — in particular that the chart source always
// reaches the config — is unit-testable without shelling out to helm.
// seedSecretName is sreModelSeedSecretName when this run wrote the
// sre-model-seed Secret, "" otherwise: an install run without --llm-* flags
// must leave sreAgent.seed.secretName untouched (Never clear an
// already-seeded org's value just because a later run didn't pass the
// flags). seedHash is sreModelSeedHash of that same seed (Task A3), set only
// alongside seedSecretName: it lands on aep-api's pod-template annotation so
// a changed seed (a rotated key, a different model) rolls aep-api's pods —
// without it, env sourced from a Secret is fixed at pod start and a Secret
// rewrite alone never rolls the workload, so aep-api keeps reading the old
// seed.
func sreAgentPlatformUpdateConfig(p sreParams, chartPath, chartVersion, seedSecretName, seedHash string) platformUpdateConfig {
	sets := []string{
		"sreAgent.enabled=true",
		"sreAgent.org=" + p.Org,
		"sreAgent.namespace=" + p.ObsNamespace,
		"sreAgent.deployment=" + p.RcaName,
		"sreAgent.secret=sre-agent-aep",
		"sreAgent.mcpHostname=" + p.MCPHostname,
	}
	if seedSecretName != "" {
		sets = append(sets, "sreAgent.seed.secretName="+seedSecretName)
	}
	if seedHash != "" {
		sets = append(sets, "sreAgent.seed.hash="+seedHash)
	}
	return platformUpdateConfig{
		Namespace:    p.AEPNamespace,
		Release:      defaultPlatformRelease,
		ChartPath:    chartPath,
		ChartVersion: chartVersion,
		HelmSets:     sets,
	}
}

// updatePlatformSreAgent flips sreAgent.* on the AEP platform release by
// calling platformUpdate directly (the same function `aectl platform
// update`'s cobra RunE calls) — no shared package-level state with that
// command, and this call's own ctx is honored rather than a fresh
// context.Background(). chartPath/chartVersion pin the chart source
// (runSreInstall requires one of them up front) so this never falls back to
// platformUpdate's own unpinned-OCI default. seedSecretName/seedHash are
// sreAgentPlatformUpdateConfig's own parameters, forwarded as-is.
func updatePlatformSreAgent(ctx context.Context, p sreParams, chartPath, chartVersion, seedSecretName, seedHash string) error {
	return platformUpdate(ctx, sreAgentPlatformUpdateConfig(p, chartPath, chartVersion, seedSecretName, seedHash))
}

// ensureClusterGatewayCA copies the cluster gateway CA cert from the
// control-plane's cluster-gateway-ca Secret into a same-named ConfigMap in the
// obs namespace (mirrors setup scripts' create_plane_cert_resources). The
// obs-plane chart's cluster-agent mounts this but does not create it.
func ensureClusterGatewayCA(ctx context.Context, client *kubernetes.Clientset, ns string) error {
	const cpNamespace = "openchoreo-control-plane"
	sec, err := client.CoreV1().Secrets(cpNamespace).Get(ctx, "cluster-gateway-ca", metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("read cluster-gateway-ca secret in %s: %w", cpNamespace, err)
	}
	ca := sec.Data["ca.crt"]
	if len(ca) == 0 {
		return fmt.Errorf("cluster-gateway-ca secret in %s has no ca.crt", cpNamespace)
	}
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster-gateway-ca", Namespace: ns},
		Data:       map[string]string{"ca.crt": string(ca)},
	}
	if _, err := client.CoreV1().ConfigMaps(ns).Create(ctx, cm, metav1.CreateOptions{}); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return err
		}
		if _, err := client.CoreV1().ConfigMaps(ns).Update(ctx, cm, metav1.UpdateOptions{}); err != nil {
			return err
		}
	}
	return nil
}

func ensureNamespace(ctx context.Context, client *kubernetes.Clientset, ns string) error {
	_, err := client.CoreV1().Namespaces().Get(ctx, ns, metav1.GetOptions{})
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}
	_, err = client.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: ns},
	}, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create namespace %s: %w", ns, err)
	}
	return nil
}

func waitForSecret(ctx context.Context, client *kubernetes.Clientset, ns, name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if _, err := client.CoreV1().Secrets(ns).Get(ctx, name, metav1.GetOptions{}); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for secret %s/%s", ns, name)
		}
		time.Sleep(3 * time.Second)
	}
}

// waitForExternalSecretRefresh waits until ESO has synced the ExternalSecret
// at or after since: for a Secret that already exists, the point at which it
// holds the ExternalSecret's current source.
func waitForExternalSecretRefresh(ctx context.Context, applier *k8s.Applier, ns, name string, since time.Time, timeout time.Duration) error {
	// refreshTime has second precision.
	since = since.Truncate(time.Second)
	deadline := time.Now().Add(timeout)
	for {
		es, err := applier.Get(ctx, "external-secrets.io/v1", "ExternalSecret", ns, name)
		if err != nil {
			return err
		}
		if es != nil && externalSecretSyncedSince(es, since) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for ExternalSecret %s/%s to sync", ns, name)
		}
		time.Sleep(3 * time.Second)
	}
}

// externalSecretSyncedSince reports whether es is Ready with a refresh at or
// after since.
func externalSecretSyncedSince(es *unstructured.Unstructured, since time.Time) bool {
	refreshed, _, _ := unstructured.NestedString(es.Object, "status", "refreshTime")
	t, err := time.Parse(time.RFC3339, refreshed)
	if err != nil || t.Before(since) {
		return false
	}
	conds, _, _ := unstructured.NestedSlice(es.Object, "status", "conditions")
	for _, c := range conds {
		m, ok := c.(map[string]interface{})
		if ok && m["type"] == "Ready" && m["status"] == "True" {
			return true
		}
	}
	return false
}

// waitForDeployment is best-effort: it warns on timeout rather than failing,
// since upstream chart resource names can drift across versions.
func waitForDeployment(ctx context.Context, client *kubernetes.Clientset, ns, name string, timeout time.Duration) {
	sp := ui.NewSpinner(fmt.Sprintf("Waiting for deployment/%s", name))
	sp.Start()
	deadline := time.Now().Add(timeout)
	for {
		d, err := client.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
		if err == nil && d.Status.AvailableReplicas >= 1 {
			sp.Success(fmt.Sprintf("%s available", name))
			return
		}
		if time.Now().After(deadline) {
			sp.Stop()
			ui.Warn(fmt.Sprintf("%s not Available within %s — continuing (check it manually)", name, timeout))
			return
		}
		time.Sleep(5 * time.Second)
	}
}

func patchConfigMap(ctx context.Context, client *kubernetes.Clientset, ns, name string, data map[string]string) error {
	var b strings.Builder
	b.WriteString(`{"data":{`)
	first := true
	for k, v := range data {
		if !first {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, "%q:%q", k, v)
		first = false
	}
	b.WriteString("}}")
	if _, err := client.CoreV1().ConfigMaps(ns).Patch(ctx, name, types.MergePatchType, []byte(b.String()), metav1.PatchOptions{}); err != nil {
		return fmt.Errorf("patch configmap %s/%s: %w", ns, name, err)
	}
	return nil
}

func rolloutRestart(ctx context.Context, client *kubernetes.Clientset, ns, name string) error {
	patch := fmt.Sprintf(`{"spec":{"template":{"metadata":{"annotations":{"aectl.wso2.com/restartedAt":%q}}}}}`,
		time.Now().UTC().Format(time.RFC3339))
	_, err := client.AppsV1().Deployments(ns).Patch(ctx, name, types.StrategicMergePatchType, []byte(patch), metav1.PatchOptions{})
	return err
}

func helmInstallObsPlane(ctx context.Context, p sreParams) error {
	vals, cleanup, err := writeTempValues("obs-plane", sreObsPlaneValuesTmpl, p)
	if err != nil {
		return err
	}
	defer cleanup()
	args := []string{
		"upgrade", "--install", "observability-plane",
		"oci://ghcr.io/openchoreo/helm-charts/openchoreo-observability-plane",
		"--namespace", p.ObsNamespace, "--create-namespace",
		"--version", sreObsPlaneVersion,
		"--values", vals, "--timeout", "10m",
	}
	return runSREAgentHelm(ctx, "obs-plane", args)
}

// runSREAgentHelm runs an observability-plane `helm upgrade` that renders the
// SRE agent: through aectl's post-renderer (the stock chart has no
// extraVolumes for the extension and CA bundle mounts), plus
// --force-conflicts on Helm v4. That is a v4 (server-side apply) flag v3
// rejects as unknown; it only matters on re-runs where the post-helm
// ConfigMap patches claimed chart-owned fields under a different field
// manager, a v4-only concern.
func runSREAgentHelm(ctx context.Context, label string, args []string) error {
	major := helmMajorVersion(ctx)
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate aectl for the helm post-renderer: %w", err)
	}
	flags, env, cleanup, err := sreHelmPostRenderer(major, exe)
	if err != nil {
		return fmt.Errorf("prepare the helm post-renderer: %w", err)
	}
	defer cleanup()
	args = append(args, flags...)
	if major >= 4 {
		args = append(args, "--force-conflicts")
	}
	return runHelmWithEnv(ctx, label, env, args...)
}

// helmMajorVersion returns the installed helm's major version. Best-effort:
// on any error it returns 0, which callers treat as pre-v4 behaviour.
func helmMajorVersion(ctx context.Context) int {
	out, err := exec.CommandContext(ctx, "helm", "version", "--short").Output()
	if err != nil {
		return 0
	}
	v := strings.TrimPrefix(strings.TrimSpace(string(out)), "v")
	major, _, _ := strings.Cut(v, ".")
	n, err := strconv.Atoi(major)
	if err != nil {
		return 0
	}
	return n
}

// helmEnableSREAgent turns on the SRE agent of a plane aectl did not install:
// that release, at its own chart version, with its values kept.
func helmEnableSREAgent(ctx context.Context, p sreParams, plane obsPlaneRelease) error {
	vals, cleanup, err := writeTempValues("sre-agent", sreAgentValuesTmpl, p)
	if err != nil {
		return err
	}
	defer cleanup()
	args := []string{
		"upgrade", plane.Name,
		"oci://ghcr.io/openchoreo/helm-charts/" + obsPlaneChart,
		"--namespace", p.ObsNamespace,
		"--version", plane.Version,
		"--reuse-values",
		"--values", vals, "--timeout", "10m",
	}
	return runSREAgentHelm(ctx, "obs-plane (SRE agent)", args)
}

func helmInstallObsLogs(ctx context.Context, p sreParams) error {
	vals, cleanup, err := writeTempValues("obs-logs", sreObsLogsValuesTmpl, p)
	if err != nil {
		return err
	}
	defer cleanup()
	return runHelm(ctx, "obs-logs",
		"upgrade", "--install", "observability-logs-opensearch",
		"oci://ghcr.io/openchoreo/helm-charts/observability-logs-opensearch",
		"--namespace", p.ObsNamespace, "--create-namespace",
		"--version", sreObsLogsVersion,
		"--values", vals, "--timeout", "15m")
}

func runHelm(ctx context.Context, label string, args ...string) error {
	return runHelmWithEnv(ctx, label, nil, args...)
}

// runHelmWithEnv runs helm with env ("KEY=value") added to aectl's own
// environment.
func runHelmWithEnv(ctx context.Context, label string, env []string, args ...string) error {
	ui.Step(fmt.Sprintf("Installing %s chart", label))
	var out bytes.Buffer
	c := exec.CommandContext(ctx, "helm", args...)
	if len(env) > 0 {
		c.Env = append(os.Environ(), env...)
	}
	c.Stdout = &out
	c.Stderr = &out
	if err := c.Run(); err != nil {
		return fmt.Errorf("helm %s: %w\n%s", label, err, out.String())
	}
	ui.Success(fmt.Sprintf("%s chart installed", label))
	return nil
}

func writeTempValues(prefix, tmpl string, p sreParams) (string, func(), error) {
	t, err := template.New(prefix).Parse(tmpl)
	if err != nil {
		return "", nil, err
	}
	f, err := os.CreateTemp("", "aectl-"+prefix+"-*.yaml")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.Remove(f.Name()) }
	if err := t.Execute(f, p); err != nil {
		_ = f.Close()
		cleanup()
		return "", nil, err
	}
	if err := f.Close(); err != nil {
		cleanup()
		return "", nil, err
	}
	return f.Name(), cleanup, nil
}

// openSearchBootstrapJob mirrors setup-observability.sh step 6: verify the
// container-logs index template maps `log` as wildcard and delete any indices
// created with a wrong mapping so they get recreated correctly.
func openSearchBootstrapJob(ns string) *batchv1.Job {
	backoff := int32(5)
	ttl := int32(600)
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "opensearch-bootstrap-templates", Namespace: ns},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoff,
			TTLSecondsAfterFinished: &ttl,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyOnFailure,
					Containers: []corev1.Container{{
						Name:  "bootstrap",
						Image: "curlimages/curl:8.10.1",
						Env: []corev1.EnvVar{
							{Name: "OS_HOST", Value: "opensearch"},
							{Name: "OS_PORT", Value: "9200"},
							{Name: "OS_USER", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "opensearch-admin-credentials"}, Key: "username"}}},
							{Name: "OS_PASS", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "opensearch-admin-credentials"}, Key: "password"}}},
						},
						Command: []string{"/bin/sh", "-c"},
						Args:    []string{openSearchBootstrapScript},
					}},
				},
			},
		},
	}
}
