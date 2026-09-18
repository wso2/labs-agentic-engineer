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

package helm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
)

// GetReleaseVersion returns the chart version an existing Helm release is
// actually running. Used to re-render that release's own chart consistently
// with what is deployed, rather than trusting this process's own pinned
// version constant to still match a release it did not install.
func GetReleaseVersion(ctx context.Context, kubeconfig, release, namespace string) (string, error) {
	args := []string{"get", "metadata", release, "-n", namespace, "-o", "json"}
	if kubeconfig != "" {
		args = append(args, "--kubeconfig", kubeconfig)
	}
	out, err := exec.CommandContext(ctx, "helm", args...).Output()
	if err != nil {
		return "", fmt.Errorf("helm get metadata %s: %w", release, err)
	}
	var meta struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(out, &meta); err != nil {
		return "", fmt.Errorf("parse helm metadata for %s: %w", release, err)
	}
	if meta.Version == "" {
		return "", fmt.Errorf("helm metadata for %s reported an empty chart version", release)
	}
	return meta.Version, nil
}

// GetReleaseValuesYAML returns the live, user-supplied values of an existing
// Helm release (the same input `helm get values` without `--all` returns —
// exactly the --set/--values a prior install/upgrade passed, not the chart's
// merged defaults). Used to re-render that release's chart with precisely
// what it is actually configured with, so a re-render changes only what the
// caller explicitly overrides on top and nothing else silently reverts to a
// chart default.
func GetReleaseValuesYAML(ctx context.Context, kubeconfig, release, namespace string) (string, error) {
	args := []string{"get", "values", release, "-n", namespace, "-o", "yaml"}
	if kubeconfig != "" {
		args = append(args, "--kubeconfig", kubeconfig)
	}
	out, err := exec.CommandContext(ctx, "helm", args...).Output()
	if err != nil {
		return "", fmt.Errorf("helm get values %s: %w", release, err)
	}
	return string(out), nil
}

// TemplateChart runs `helm template` for the given ChartSpec against
// baseValuesYAML (typically an existing release's own live values, from
// GetReleaseValuesYAML) plus any Sets/SetStrings/SetJSON overrides in spec,
// and returns the rendered multi-document YAML. Nothing is applied to the
// cluster — the caller decides what to do with the output (e.g. extract and
// run a single Job from it). spec.ValuesYAML, Version-defaulting, and
// install-only flags (--create-namespace, --wait, --timeout) are irrelevant
// here and ignored; only ReleaseName/Chart/Version/Namespace/Sets/SetStrings/
// SetJSON are read.
func TemplateChart(ctx context.Context, kubeconfig string, spec ChartSpec, baseValuesYAML string) ([]byte, error) {
	args := []string{"template", spec.ReleaseName, spec.Chart, "-n", spec.Namespace}
	if spec.Version != "" {
		args = append(args, "--version", spec.Version)
	}
	if baseValuesYAML != "" {
		f, err := os.CreateTemp("", "aectl-helm-template-values-*.yaml")
		if err != nil {
			return nil, fmt.Errorf("create temp values file: %w", err)
		}
		defer func() { _ = os.Remove(f.Name()) }()
		if _, err := f.WriteString(baseValuesYAML); err != nil {
			_ = f.Close()
			return nil, fmt.Errorf("write temp values file: %w", err)
		}
		if err := f.Close(); err != nil {
			return nil, fmt.Errorf("close temp values file: %w", err)
		}
		args = append(args, "-f", f.Name())
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
	if kubeconfig != "" {
		args = append(args, "--kubeconfig", kubeconfig)
	}

	var out, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "helm", args...)
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%w\n%s", err, stderr.String())
	}
	return out.Bytes(), nil
}
