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
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFakeHelmScript installs a shell script named "helm" that dispatches on
// its first argument (the subcommand), returning canned output for each of
// the subcommands this file's tests exercise. Prepends the temp dir to PATH.
func writeFakeHelmScript(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "helm"), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatalf("write fake helm: %v", err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
}

func TestGetReleaseVersion(t *testing.T) {
	writeFakeHelmScript(t, `echo '{"name":"thunder-default-default","version":"1.0.0"}'`)
	version, err := GetReleaseVersion(context.Background(), "", "thunder-default-default", "thunder-default-default")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "1.0.0" {
		t.Errorf("version = %q, want 1.0.0", version)
	}
}

func TestGetReleaseVersion_EmptyVersionIsError(t *testing.T) {
	writeFakeHelmScript(t, `echo '{"name":"r","version":""}'`)
	if _, err := GetReleaseVersion(context.Background(), "", "r", "ns"); err == nil {
		t.Fatal("expected an error for an empty chart version")
	}
}

func TestGetReleaseVersion_HelmFailure(t *testing.T) {
	writeFakeHelmScript(t, `echo "release not found" >&2; exit 1`)
	if _, err := GetReleaseVersion(context.Background(), "", "r", "ns"); err == nil {
		t.Fatal("expected an error when helm exits non-zero")
	}
}

func TestGetReleaseValuesYAML(t *testing.T) {
	writeFakeHelmScript(t, `printf 'deployment:\n  replicaCount: 1\n'`)
	values, err := GetReleaseValuesYAML(context.Background(), "", "r", "ns")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(values, "replicaCount: 1") {
		t.Errorf("values = %q, missing expected content", values)
	}
}

func TestTemplateChart_ArgsAndValuesFile(t *testing.T) {
	dir := t.TempDir()
	captured := filepath.Join(dir, "captured-values.yaml")
	argsFile := filepath.Join(dir, "args.txt")
	script := `printf '%s\n' "$@" > ` + argsFile + `
prev=""
for a in "$@"; do
  if [ "$prev" = "-f" ]; then cp "$a" ` + captured + `; fi
  prev="$a"
done
echo "kind: Job"
`
	if err := os.WriteFile(filepath.Join(dir, "helm"), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatalf("write fake helm: %v", err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	spec := ChartSpec{
		ReleaseName: "thunder-default-default",
		Chart:       "oci://ghcr.io/thunder-id/helm-charts/thunderid",
		Version:     "1.0.0",
		Namespace:   "thunder-default-default",
		SetStrings:  []string{"bootstrap.configMap.name=thunder-default-default-aep-bootstrap"},
		SetJSON:     []string{`bootstrap.configMap.files=["80-aep-system-client.yaml"]`},
	}
	out, err := TemplateChart(context.Background(), "", spec, "deployment:\n  replicaCount: 1\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(string(out), "kind: Job") {
		t.Errorf("output = %q, missing expected rendered content", out)
	}

	argsData, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read captured args: %v", err)
	}
	args := strings.Split(strings.TrimSpace(string(argsData)), "\n")
	for _, want := range []string{"template", "thunder-default-default", spec.Chart, "-n", "thunder-default-default", "--version", "1.0.0",
		"--set-string", "bootstrap.configMap.name=thunder-default-default-aep-bootstrap",
		"--set-json", `bootstrap.configMap.files=["80-aep-system-client.yaml"]`} {
		if !containsArg(args, want) {
			t.Errorf("args %v missing %q", args, want)
		}
	}
	if containsArg(args, "--create-namespace") || containsArg(args, "--wait") || containsArg(args, "--install") {
		t.Errorf("args %v carry install-only flags — helm template does not accept them", args)
	}

	valuesContent, err := os.ReadFile(captured)
	if err != nil {
		t.Fatalf("read captured values file: %v", err)
	}
	if string(valuesContent) != "deployment:\n  replicaCount: 1\n" {
		t.Errorf("values file content = %q, want the base values passed in", valuesContent)
	}
}

func TestTemplateChart_NoValuesFileWhenBaseValuesEmpty(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args.txt")
	script := `printf '%s\n' "$@" > ` + argsFile + "\n"
	if err := os.WriteFile(filepath.Join(dir, "helm"), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatalf("write fake helm: %v", err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	if _, err := TemplateChart(context.Background(), "", ChartSpec{ReleaseName: "r", Chart: "c", Namespace: "ns"}, ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	argsData, _ := os.ReadFile(argsFile)
	if strings.Contains(string(argsData), "-f") {
		t.Errorf("args %q contain -f despite an empty base values string", argsData)
	}
}

func TestTemplateChart_FailureIncludesStderr(t *testing.T) {
	writeFakeHelmScript(t, `echo "template: bad document" >&2; exit 1`)
	if _, err := TemplateChart(context.Background(), "", ChartSpec{ReleaseName: "r", Chart: "c", Namespace: "ns"}, ""); err == nil {
		t.Fatal("expected an error on helm failure")
	} else if !strings.Contains(err.Error(), "bad document") {
		t.Errorf("error %q does not include helm stderr", err.Error())
	}
}

func containsArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}
