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
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/term"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/wso2/aep/aepctl/internal/adminpb"
	"github.com/wso2/aep/aepctl/internal/config"
	k8s "github.com/wso2/aep/aepctl/internal/kubernetes"
)

const minOCVersion = "1.1.1"

var (
	initPlatformChart        string
	initPlatformVersion      string
	initPlatformRelease      string
	initPlatformNamespace    string
	initConsoleURL           string
	initAPIURL               string
	initWorkspacesAccessMode string
	initBuildPlaneNamespace  string
	initRegistryService      string
	initOCNamespace          string
	initSkipOCVersionCheck   bool
)

var initCmd = &cobra.Command{
	Use:   "install",
	Short: "Provision OpenBao, install the platform, and configure Thunder",
	Long: `Full AEP platform installation in one command:
  1. Waits for the OpenBao pod to be ready
  2. Provisions OpenBao and generates all secrets
  3. Installs the platform Helm chart
  4. Waits for all platform pods to be ready
  5. Registers AEP OAuth clients in Thunder
  6. Writes cluster config to the aep-cli-config ConfigMap

Pass the server URL via the --server flag:
  aep platform install --server http://aep-server.openchoreo.localhost:8080 [flags]`,
	// skipClusterConfig: the ConfigMap does not exist before this command runs.
	Annotations: map[string]string{"skipClusterConfig": "true"},
	RunE:        runAEPInit,
}

func init() {
	platformCmd.AddCommand(initCmd)
	initCmd.Flags().StringVar(&initPlatformChart, "platform-chart", "", "Local path to the platform Helm chart (for local/dev installs; takes precedence over --platform-version)")
	initCmd.Flags().StringVar(&initPlatformVersion, "platform-version", "latest", "Platform version to pull from GHCR (ignored when --platform-chart is set)")
	initCmd.Flags().StringVar(&initPlatformRelease, "platform-release", "aep-platform", "Helm release name for the platform chart")
	initCmd.Flags().StringVar(&initPlatformNamespace, "namespace", "wso2-aep", "Kubernetes namespace")
	initCmd.Flags().StringVar(&initConsoleURL, "console-url", "http://console.openchoreo.localhost:8080", "Public URL of the AEP console")
	initCmd.Flags().StringVar(&initAPIURL, "api-url", "http://api.openchoreo.localhost:8080", "Public URL of the AEP API")
	initCmd.Flags().StringVar(&initWorkspacesAccessMode, "workspaces-access-mode", "", "PVC access mode for the shared workspaces volume (e.g. ReadWriteOnce for local k3d, ReadWriteMany for production)")
	_ = viper.BindPFlag("platform.workspaces.access_mode", initCmd.Flags().Lookup("workspaces-access-mode"))
	initCmd.Flags().StringVar(&initBuildPlaneNamespace, "build-plane-namespace", "openchoreo-workflow-plane", "Namespace of the OpenChoreo build/workflow plane (must already exist, incl. its image registry)")
	initCmd.Flags().StringVar(&initRegistryService, "registry-service", "registry", "Name of the build-plane image registry Service (the coding-agent build pushes/pulls here)")
	initCmd.Flags().StringVar(&initOCNamespace, "oc-namespace", "openchoreo-system", "Namespace where OpenChoreo control-plane is installed")
	initCmd.Flags().BoolVar(&initSkipOCVersionCheck, "skip-oc-version-check", false, "Skip the OpenChoreo minimum version check (not recommended)")
	initCmd.Flags().String("oc-api-url", "", "In-cluster URL of the OpenChoreo platform API")
	_ = viper.BindPFlag("oc.api_url", initCmd.Flags().Lookup("oc-api-url"))
	initCmd.Flags().String("server", "", "AEP server gRPC URL")
	_ = viper.BindPFlag("server", initCmd.Flags().Lookup("server"))
	initCmd.Flags().String("webhook-delivery-url", "", "Public URL registered on each repo's webhook (e.g. https://webhook.example.com/api/v1/webhooks/github)")
	_ = viper.BindPFlag("webhook.delivery_url", initCmd.Flags().Lookup("webhook-delivery-url"))
	initCmd.Flags().String("cluster-gateway-proxy-url", "", "URL of the managed cluster-gateway-proxy service (production; omit to deploy the local stub)")
	_ = viper.BindPFlag("codingagent.cluster_gateway_proxy.url", initCmd.Flags().Lookup("cluster-gateway-proxy-url"))
	initCmd.Flags().String("secret-manager-api-url", "", "URL of the managed secret-manager API service (production; omit to deploy the local stub)")
	_ = viper.BindPFlag("codingagent.secret_manager_api.url", initCmd.Flags().Lookup("secret-manager-api-url"))
	registerThunderFlags(initCmd)
}

func runAEPInit(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	if _, err := exec.LookPath("helm"); err != nil {
		return fmt.Errorf("helm is required but was not found in PATH\nInstall it from https://helm.sh/docs/intro/install/ and try again")
	}

	k8sClient, err := k8s.NewClient(kubeconfig)
	if err != nil {
		return fmt.Errorf("connect to cluster: %w", err)
	}

	// 0a. Prerequisite guard — verify the OpenChoreo version meets the minimum
	// requirement. Features used by the coding-agent build/deploy chain (e.g.
	// the workflow plane APIs) are only available from 1.1.1 onward; older
	// clusters produce cryptic failures deep in the pipeline.
	if !initSkipOCVersionCheck {
		if err := checkOCVersion(ctx, k8sClient, initOCNamespace, minOCVersion); err != nil {
			return err
		}
	}

	// 0b. Prerequisite guard — verify the OpenChoreo build plane + image registry
	// exist BEFORE installing anything. The coding-agent build/deploy chain
	// pushes built images to, and pulls them from, this registry; without it,
	// builds fail deep in the pipeline with an opaque publish/pull error. aepctl
	// does not provision it (that is the OpenChoreo cluster's responsibility) —
	// so fail fast here with an actionable message rather than half-installing.
	if err := checkBuildRegistry(ctx, k8sClient, initBuildPlaneNamespace, initRegistryService); err != nil {
		return err
	}

	// 1. Wait for OpenBao pod.
	if err := waitForOpenBaoPod(ctx, k8sClient, initPlatformNamespace); err != nil {
		return err
	}

	// 2. Prompt for secrets.
	anthropicKey, err := readMaskedInput("Anthropic API key")
	if err != nil {
		return fmt.Errorf("read Anthropic API key: %w", err)
	}
	if anthropicKey == "" {
		return fmt.Errorf("an Anthropic API key is required")
	}

	// 3. Provision OpenBao via the management server.
	client, ctx, closeConn, err := dialServer(ctx)
	if err != nil {
		return err
	}
	defer closeConn()

	_, _ = fmt.Fprintln(os.Stdout, "Provisioning OpenBao...")
	stream, err := client.Init(ctx, &adminpb.InitRequest{
		AnthropicApiKey: anthropicKey,
	})
	if err != nil {
		return fmt.Errorf("call Init: %w", err)
	}

	var complete *adminpb.InitComplete
	for {
		event, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("receive: %w", err)
		}
		switch p := event.Payload.(type) {
		case *adminpb.InitEvent_Progress:
			_, _ = fmt.Fprintf(os.Stdout, "  %s\n", p.Progress)
		case *adminpb.InitEvent_Error:
			return fmt.Errorf("server error: %s", p.Error)
		case *adminpb.InitEvent_Complete:
			complete = p.Complete
		}
	}

	if complete != nil && len(complete.UnsealKeys) > 0 {
		printOpenBaoCredentials(complete.UnsealKeys)
	}

	// 4. Install the platform chart.
	_, _ = fmt.Fprintln(os.Stdout, "Installing platform chart...")
	thunderURL := viper.GetString("thunder.url")
	helmArgs := []string{
		"install", initPlatformRelease,
		"-n", initPlatformNamespace,
		"--set", "console.publicURL=" + initConsoleURL,
		"--set", "aepApi.publicURL=" + initAPIURL,
		"--set", "console.thunderPublicURL=" + viper.GetString("thunder.public_url"),
		"--set", "thunder.adminURL=" + thunderURL,
		"--set", "thunder.jwksURL=" + thunderURL + "/oauth2/jwks",
		"--set", "platformAPI.baseURL=" + viper.GetString("oc.api_url"),
	}
	if initPlatformChart != "" {
		// Local chart path — used for dev/local testing.
		helmArgs = append([]string{helmArgs[0], helmArgs[1], initPlatformChart}, helmArgs[2:]...)
	} else {
		// Pull chart from GHCR.
		helmArgs = append([]string{helmArgs[0], helmArgs[1], "oci://ghcr.io/wso2/aep/charts/platform"}, helmArgs[2:]...)
		if initPlatformVersion != "latest" {
			helmArgs = append(helmArgs, "--version", initPlatformVersion)
		}
	}
	if mode := viper.GetString("platform.workspaces.access_mode"); mode != "" {
		helmArgs = append(helmArgs, "--set", "workspaces.accessMode="+mode)
	}
	// Coding-agent dispatch: deploy the local cluster-gateway-proxy stub (reads
	// pod logs/job status for live streaming + JobWatcher) unless disabled. Prod
	// installs set codingagent.local_stubs.enabled=false and supply the real
	// endpoint URLs instead (set them via flags or AEP_* env vars).
	helmArgs = append(helmArgs, "--set",
		fmt.Sprintf("codingAgentDispatch.localStubs.enabled=%t", viper.GetBool("codingagent.local_stubs.enabled")))
	if u := viper.GetString("codingagent.cluster_gateway_proxy.url"); u != "" {
		helmArgs = append(helmArgs, "--set", "codingAgentDispatch.clusterGatewayProxy.url="+u)
	}
	if u := viper.GetString("codingagent.secret_manager_api.url"); u != "" {
		helmArgs = append(helmArgs, "--set", "codingAgentDispatch.secretManagerApi.url="+u)
	}
	// GitHub webhook delivery: register deliveryURL on repos and, locally, deploy
	// the smee-client that forwards the smee.io channel into the cluster. Prod
	// sets webhook.local_smee.enabled=false and delivery_url to a real ingress.
	helmArgs = append(helmArgs, "--set",
		fmt.Sprintf("webhook.localSmee.enabled=%t", viper.GetBool("webhook.local_smee.enabled")))
	if u := viper.GetString("webhook.delivery_url"); u != "" {
		helmArgs = append(helmArgs, "--set", "webhook.deliveryURL="+u)
	}
	// Local OpenChoreo org-unit provisioning: create the per-org namespaced
	// ComponentTypes + api-configuration trait aep-api references (cloud does this
	// via platform-api ProvisionOrgUnit). Prod sets enabled=false.
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

	// 5. Wait for all platform pods.
	if err := waitForAllPodsReady(ctx, k8sClient, initPlatformNamespace, 10*time.Minute); err != nil {
		return err
	}

	// 6. Register AEP OAuth clients in Thunder.
	_, _ = fmt.Fprintln(os.Stdout, "Configuring Thunder OAuth clients...")
	if err := doThunderSetup(ctx, k8sClient, initPlatformNamespace,
		viper.GetString("thunder.namespace"),
		viper.GetString("thunder.url"),
		viper.GetString("thunder.config_map"),
		viper.GetString("thunder.deployment"),
		viper.GetString("thunder.admin_client_id"),
		initConsoleURL,
	); err != nil {
		return err
	}

	// 7. Persist non-sensitive config into the in-cluster ConfigMap so
	// subsequent aep commands can load it without any local config file.
	if err := writeClusterConfig(ctx, k8sClient, initPlatformNamespace); err != nil {
		return fmt.Errorf("write cluster config: %w", err)
	}

	_, _ = fmt.Fprintln(os.Stdout, "\nAEP is ready. Open the console to get started.")
	return nil
}

// writeClusterConfig creates or updates the aep-cli-config ConfigMap with the
// non-sensitive viper values set during this init run. Sensitive values (e.g.
// thunder.admin_client_secret) are intentionally excluded — they are read at
// runtime from the ESO-synced aep-thunder-secrets Secret.
func writeClusterConfig(ctx context.Context, client *kubernetes.Clientset, namespace string) error {
	data := make(map[string]string, len(config.ConfigMapKeys))
	for _, k := range config.ConfigMapKeys {
		data[k] = viper.GetString(k)
	}

	existing, err := client.CoreV1().ConfigMaps(namespace).Get(ctx, config.ConfigMapName, metav1.GetOptions{})
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("get %s: %w", config.ConfigMapName, err)
		}
		_, err = client.CoreV1().ConfigMaps(namespace).Create(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      config.ConfigMapName,
				Namespace: namespace,
				Labels:    map[string]string{"app.kubernetes.io/managed-by": "aepctl"},
			},
			Data: data,
		}, metav1.CreateOptions{})
		return err
	}

	existing.Data = data
	_, err = client.CoreV1().ConfigMaps(namespace).Update(ctx, existing, metav1.UpdateOptions{})
	return err
}

// checkOCVersion fails fast when the installed OpenChoreo version is below the
// required minimum. It reads the app.kubernetes.io/version label from any
// Deployment in the OC namespace that carries app.kubernetes.io/part-of=openchoreo.
// A missing namespace or no versioned Deployment is treated as an unrecognised
// installation and also fails.
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
		// Strip a leading "v" if present (e.g. "v1.2.0" → "1.2.0").
		ver = strings.TrimPrefix(ver, "v")
		ok, err := versionAtLeast(ver, minVersion)
		if err != nil {
			return fmt.Errorf("parse OpenChoreo version %q: %w", ver, err)
		}
		if !ok {
			return fmt.Errorf("OpenChoreo version %s is below the minimum required version %s: "+
				"upgrade OpenChoreo to %s or later before running `aep init`",
				ver, minVersion, minVersion)
		}
		return nil
	}

	return fmt.Errorf("could not determine OpenChoreo version from deployments in namespace %q: "+
		"ensure OpenChoreo >= %s is installed, or pass --skip-oc-version-check to bypass this check",
		namespace, minVersion)
}

// versionAtLeast reports whether version >= minimum, comparing major.minor.patch
// numerically. Both strings must be of the form "X.Y.Z".
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
	return true, nil // equal
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

// checkBuildRegistry fails fast (before any install) when the OpenChoreo build
// plane or its image registry Service is absent. The coding-agent build →
// publish → deploy chain depends on this registry; aepctl assumes a
// pre-provisioned OpenChoreo cluster and does not create it.
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
				"provision the OpenChoreo build plane's registry before running `aep init`, "+
				"or pass --registry-service if yours is named differently", service, namespace)
		}
		return fmt.Errorf("check registry Service %q/%q: %w", namespace, service, err)
	}
	return nil
}

// waitForOpenBaoPod blocks until the OpenBao pod is Running. Does NOT wait for
// Ready — the readiness probe fails until after init completes, so waiting for
// Ready would deadlock.
func waitForOpenBaoPod(ctx context.Context, client *kubernetes.Clientset, namespace string) error {
	_, _ = fmt.Fprintf(os.Stdout, "Waiting for OpenBao pod")
	for {
		pods, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: "app.kubernetes.io/name=aep-openbao",
		})
		if err == nil && len(pods.Items) > 0 {
			pod := pods.Items[0]
			if pod.Status.Phase == "Running" && len(pod.Status.ContainerStatuses) > 0 {
				started := pod.Status.ContainerStatuses[0].Started
				if started != nil && *started {
					_, _ = fmt.Fprintln(os.Stdout, " ready")
					return nil
				}
			}
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

// waitForAllPodsReady blocks until every pod in the namespace is Ready, or the
// timeout elapses.
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

func printOpenBaoCredentials(unsealKeys []string) {
	_, _ = fmt.Fprintln(os.Stdout, "\n+------------------------------------------------------------------+")
	_, _ = fmt.Fprintln(os.Stdout, "|  STORE THESE SECURELY - they cannot be retrieved later           |")
	_, _ = fmt.Fprintln(os.Stdout, "|  Unseal keys: need 3 of 5 to unseal after pod restart            |")
	_, _ = fmt.Fprintln(os.Stdout, "+------------------------------------------------------------------+")
	for i, k := range unsealKeys {
		_, _ = fmt.Fprintf(os.Stdout, "  Key %d: %s\n", i+1, k)
	}
	_, _ = fmt.Fprintln(os.Stdout, "+------------------------------------------------------------------+")
	_, _ = fmt.Fprintln(os.Stdout)
}

// readMaskedInput prompts on stderr and reads hidden input from the terminal.
func readMaskedInput(prompt string) (string, error) {
	_, _ = fmt.Fprintf(os.Stderr, "%s: ", prompt)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	_, _ = fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}
