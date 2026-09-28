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

// Package helm wraps the helm CLI for operator and chart installation.
package helm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"

	"github.com/wso2/aep/aectl/internal/addons"
)

// InstallOperator runs `helm upgrade --install` for the given OperatorSpec.
// kubeconfig is forwarded via --kubeconfig; an empty string uses the default.
// On failure, combined stdout+stderr from helm is included in the returned error.
func InstallOperator(ctx context.Context, kubeconfig string, op addons.OperatorSpec) error {
	args := []string{
		"upgrade", "--install", op.ReleaseName, op.Chart,
		"-n", op.Namespace, "--create-namespace",
		"--wait", "--timeout", "5m",
	}
	if op.Version != "" {
		args = append(args, "--version", op.Version)
	}
	for _, s := range op.Sets {
		args = append(args, "--set", s)
	}
	return run(ctx, kubeconfig, args)
}

// ChartSpec describes an application/environment Helm release — as opposed to
// OperatorSpec's narrower "cluster-wide controller" framing. It additionally
// supports a values file and --set-json, both of which InstallOperator has no
// use for today (a plain --set has always been enough for an operator).
type ChartSpec struct {
	// ReleaseName is the Helm release name.
	ReleaseName string
	// Chart is the OCI or local chart reference.
	Chart string
	// Version is the chart version string; empty omits --version.
	Version string
	// Namespace is the target namespace for the release.
	Namespace string
	// ValuesYAML, when non-empty, is written to a temp file and passed via
	// --values. Preferred over a wall of --set for structured values: Helm
	// silently ignores an unknown --set path, so a typo in a deeply nested
	// key installs a release that looks correct and trusts the wrong value.
	ValuesYAML string
	// Sets is optional "key=value" pairs passed as --set flags.
	Sets []string
	// SetStrings is optional "key=value" pairs passed as --set-string flags
	// (values Helm would otherwise coerce, e.g. a numeric-looking password).
	SetStrings []string
	// SetJSON is optional "key=jsonvalue" pairs passed as --set-json flags
	// (e.g. a bootstrap file list).
	SetJSON []string
	// Timeout is the --timeout value; empty defaults to "5m".
	Timeout string
}

// InstallChart runs `helm upgrade --install` for the given ChartSpec.
// kubeconfig is forwarded via --kubeconfig; an empty string uses the default.
// On failure, combined stdout+stderr from helm is included in the returned error.
func InstallChart(ctx context.Context, kubeconfig string, spec ChartSpec) error {
	timeout := spec.Timeout
	if timeout == "" {
		timeout = "5m"
	}
	args := []string{
		"upgrade", "--install", spec.ReleaseName, spec.Chart,
		"-n", spec.Namespace, "--create-namespace",
		"--wait", "--timeout", timeout,
	}
	if spec.Version != "" {
		args = append(args, "--version", spec.Version)
	}
	if spec.ValuesYAML != "" {
		f, err := os.CreateTemp("", "aectl-helm-values-*.yaml")
		if err != nil {
			return fmt.Errorf("create temp values file: %w", err)
		}
		defer func() { _ = os.Remove(f.Name()) }()
		if _, err := f.WriteString(spec.ValuesYAML); err != nil {
			_ = f.Close()
			return fmt.Errorf("write temp values file: %w", err)
		}
		if err := f.Close(); err != nil {
			return fmt.Errorf("close temp values file: %w", err)
		}
		args = append(args, "--values", f.Name())
	}
	for _, s := range spec.Sets {
		args = append(args, "--set", s)
	}
	for _, s := range spec.SetStrings {
		args = append(args, "--set-string", s)
	}
	for _, s := range spec.SetJSON {
		args = append(args, "--set-json", s)
	}
	return run(ctx, kubeconfig, args)
}

// ReleaseDeployed reports whether a Helm release exists in the "deployed"
// state, as opposed to a bare `helm status` succeeding for a release in ANY
// state — a release left "failed" or "pending-install" by an earlier run is
// not something a caller should treat as a working instance and bind to.
// Returns false, nil if no release by that name exists at all.
func ReleaseDeployed(ctx context.Context, kubeconfig, release, namespace string) (bool, error) {
	args := []string{"status", release, "-n", namespace, "-o", "json"}
	if kubeconfig != "" {
		args = append(args, "--kubeconfig", kubeconfig)
	}
	out, err := exec.CommandContext(ctx, "helm", args...).Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && bytes.Contains(ee.Stderr, []byte("not found")) {
			return false, nil
		}
		return false, fmt.Errorf("helm status %s: %w", release, err)
	}
	var status struct {
		Info struct {
			Status string `json:"status"`
		} `json:"info"`
	}
	if err := json.Unmarshal(out, &status); err != nil {
		return false, fmt.Errorf("parse helm status for %s: %w", release, err)
	}
	return status.Info.Status == "deployed", nil
}

func run(ctx context.Context, kubeconfig string, args []string) error {
	if kubeconfig != "" {
		args = append(args, "--kubeconfig", kubeconfig)
	}
	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, "helm", args...)
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w\n%s", err, out.String())
	}
	return nil
}
