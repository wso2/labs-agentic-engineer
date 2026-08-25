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
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/spf13/viper"
	"k8s.io/client-go/kubernetes"

	"github.com/wso2/aep/aectl/internal/ui"
)

type gatewayIngressDeps struct {
	isConfigured    func(context.Context) (bool, error)
	discoverGateway func(context.Context) (name, namespace string, err error)
	applyConfig     func(ctx context.Context, gwName, gwNamespace, hostname string) error
	confirm         func(string) bool
	prompt          func(label, defaultValue string) (string, error)
	// hostnameOverride, when non-empty, bypasses the interactive hostname prompt
	// and confirmation — used by the CI path (gateway.hostname in config).
	hostnameOverride string
}

var defaultGatewayIngressDeps = gatewayIngressDeps{
	isConfigured:    isGatewayIngressConfigured,
	discoverGateway: discoverDataplaneGateway,
	applyConfig:     applyGatewayIngressConfig,
	confirm:         ui.Confirm,
	prompt:          ui.Prompt,
}

func isGatewayIngressConfigured(ctx context.Context) (bool, error) {
	cdpOK, err := checkGatewayIngress(ctx, "clusterdataplane", "default", "")
	if err != nil {
		return false, fmt.Errorf("read ClusterDataPlane: %w", err)
	}
	if !cdpOK {
		return false, nil
	}
	envOK, err := checkGatewayIngress(ctx, "environment", "development", "default")
	if err != nil {
		return false, fmt.Errorf("read Environment/development: %w", err)
	}
	return envOK, nil
}

// checkGatewayIngress returns true when the named resource has
// spec.gateway.ingress.external set to a non-empty name. Pass an empty
// namespace for cluster-scoped resources.
func checkGatewayIngress(ctx context.Context, kind, name, namespace string) (bool, error) {
	args := []string{"get", kind, name, "-o", "json"}
	if namespace != "" {
		args = append(args, "-n", namespace)
	}
	out, err := execKubectl(ctx, args...)
	if err != nil {
		s := strings.ToLower(err.Error())
		if strings.Contains(s, "not found") || strings.Contains(s, "notfound") {
			return false, nil
		}
		return false, err
	}
	var obj struct {
		Spec struct {
			Gateway struct {
				Ingress struct {
					External *struct {
						Name string `json:"name"`
					} `json:"external"`
				} `json:"ingress"`
			} `json:"gateway"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(out, &obj); err != nil {
		return false, fmt.Errorf("parse %s/%s: %w", kind, name, err)
	}
	return obj.Spec.Gateway.Ingress.External != nil &&
		obj.Spec.Gateway.Ingress.External.Name != "", nil
}

func discoverDataplaneGateway(ctx context.Context) (string, string, error) {
	const ns = "openchoreo-data-plane"
	out, err := execKubectl(ctx, "get", "gateway", "-n", ns,
		"-o", "jsonpath={.items[0].metadata.name}")
	if err != nil {
		return "", "", fmt.Errorf("discover data-plane gateway: %w", err)
	}
	name := strings.TrimSpace(string(out))
	if name == "" {
		name = "gateway-default"
	}
	return name, ns, nil
}

func applyGatewayIngressConfig(ctx context.Context, gwName, gwNamespace, hostname string) error {
	patch := fmt.Sprintf(
		`{"spec":{"gateway":{"ingress":{"external":{"name":%q,"namespace":%q,"http":{"host":%q,"listenerName":"http","port":19080}}}}}}`,
		gwName, gwNamespace, hostname,
	)
	if _, err := execKubectl(ctx, "patch", "clusterdataplane", "default", "--type=merge", "-p", patch); err != nil {
		return fmt.Errorf("patch ClusterDataPlane: %w", err)
	}
	if _, err := execKubectl(ctx, "patch", "environment", "development", "-n", "default", "--type=merge", "-p", patch); err != nil {
		return fmt.Errorf("patch Environment: %w", err)
	}
	return nil
}

// execKubectl runs kubectl with the global --kubeconfig flag prepended when set.
func execKubectl(ctx context.Context, args ...string) ([]byte, error) {
	if kubeconfig != "" {
		args = append([]string{"--kubeconfig", kubeconfig}, args...)
	}
	cmd := exec.CommandContext(ctx, "kubectl", args...)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("%w\n%s", err, ee.Stderr)
		}
		return nil, err
	}
	return out, nil
}

func checkOrConfigureGatewayIngress(ctx context.Context, _ *kubernetes.Clientset) error {
	deps := defaultGatewayIngressDeps
	deps.hostnameOverride = viper.GetString("gateway.hostname")
	return runGatewayIngressCheck(ctx, deps)
}

func runGatewayIngressCheck(ctx context.Context, deps gatewayIngressDeps) error {
	sp := ui.NewSpinner("Checking external gateway ingress")
	sp.Start()

	ok, err := deps.isConfigured(ctx)
	if err != nil {
		sp.Fail("Gateway ingress check failed")
		return err
	}
	if ok {
		sp.Success("External gateway ingress configured")
		return nil
	}
	sp.Fail("External gateway ingress not configured")

	// CI path: hostname provided via config — auto-configure without prompting.
	if deps.hostnameOverride != "" {
		fmt.Println()
		gwName, gwNamespace, err := deps.discoverGateway(ctx)
		if err != nil {
			return fmt.Errorf("discover data-plane gateway: %w", err)
		}
		sp2 := ui.NewSpinner(fmt.Sprintf("Configuring external gateway ingress (%s)", deps.hostnameOverride))
		sp2.Start()
		if err := deps.applyConfig(ctx, gwName, gwNamespace, deps.hostnameOverride); err != nil {
			sp2.Fail("Failed to configure gateway ingress")
			return err
		}
		sp2.Success(fmt.Sprintf("External gateway ingress configured (ClusterDataPlane + Environment/development: %s → %s, port 19080)", deps.hostnameOverride, gwName))
		return nil
	}

	// Interactive path.
	fmt.Println()
	ui.Warn("Components with external endpoints will fail to render without a configured gateway ingress.")
	ui.Detail("ComponentType templates use gateway.ingress.external to generate HTTPRoute hostnames.")
	fmt.Println()

	gwName, gwNamespace, err := deps.discoverGateway(ctx)
	if err != nil {
		return fmt.Errorf("discover data-plane gateway: %w", err)
	}
	ui.Detail(fmt.Sprintf("Found data-plane gateway: %s (namespace: %s)", gwName, gwNamespace))

	hostname, err := deps.prompt(
		"External hostname for component endpoints",
		"openchoreoapis.localhost",
	)
	if err != nil {
		return fmt.Errorf("read hostname: %w", err)
	}

	fmt.Println()
	if !deps.confirm(fmt.Sprintf("Configure ClusterDataPlane and Environment/development with gateway %s/%s and host %s?", gwNamespace, gwName, hostname)) {
		fmt.Println()
		ui.Warn("Gateway ingress not configured — skipping")
		ui.Detail("Configure it manually and re-run 'aectl platform install':")
		ui.Detail(fmt.Sprintf(
			`kubectl patch clusterdataplane default --type=merge -p '{"spec":{"gateway":{"ingress":{"external":{"name":%q,"namespace":%q,"http":{"host":%q,"listenerName":"http","port":19080}}}}}}'`,
			gwName, gwNamespace, hostname,
		))
		ui.Detail(fmt.Sprintf(
			`kubectl patch environment development -n default --type=merge -p '{"spec":{"gateway":{"ingress":{"external":{"name":%q,"namespace":%q,"http":{"host":%q,"listenerName":"http","port":19080}}}}}}'`,
			gwName, gwNamespace, hostname,
		))
		fmt.Println()
		return fmt.Errorf("external gateway ingress is required for components with external endpoints")
	}

	fmt.Println()
	sp2 := ui.NewSpinner("Configuring external gateway ingress")
	sp2.Start()
	if err := deps.applyConfig(ctx, gwName, gwNamespace, hostname); err != nil {
		sp2.Fail("Failed to configure gateway ingress")
		return err
	}
	sp2.Success(fmt.Sprintf("External gateway ingress configured (ClusterDataPlane + Environment/development: %s → %s, port 19080)", hostname, gwName))
	return nil
}
