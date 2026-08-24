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

	"github.com/wso2/aep/aectl/internal/addons"
)

// writeFakeHelm installs a shell script named "helm" into a temp directory and
// prepends that directory to PATH. The script writes its arguments (one per
// line) to a file, then exits 0. When the environment variable HELM_FAIL=1 is
// set, it exits 1 and writes "helm: simulated failure" to stderr instead.
// The returned function reads and returns the recorded args on demand.
func writeFakeHelm(t *testing.T) (readArgs func() []string) {
	t.Helper()
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "helm-args.txt")
	script := "#!/bin/sh\n" +
		`printf '%s\n' "$@" > ` + argsFile + "\n" +
		"if [ \"$HELM_FAIL\" = \"1\" ]; then printf 'helm: simulated failure\\n' >&2; exit 1; fi\n"
	if err := os.WriteFile(filepath.Join(dir, "helm"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake helm: %v", err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	return func() []string {
		data, err := os.ReadFile(argsFile)
		if err != nil {
			t.Fatalf("read helm args file: %v", err)
		}
		var args []string
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			if line != "" {
				args = append(args, line)
			}
		}
		return args
	}
}

// containsSeq reports whether slice contains sub as a contiguous subsequence.
func containsSeq(slice, sub []string) bool {
	for i := 0; i+len(sub) <= len(slice); i++ {
		match := true
		for j := range sub {
			if slice[i+j] != sub[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

var baseOp = addons.OperatorSpec{
	ReleaseName: "test-op",
	Chart:       "oci://example.com/test-chart",
	Namespace:   "test-ns",
	DisplayName: "test-operator",
}

func TestInstallOperator_BaseArgs(t *testing.T) {
	readArgs := writeFakeHelm(t)
	if err := InstallOperator(context.Background(), "", baseOp); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	args := readArgs()
	for _, want := range [][]string{
		{"upgrade", "--install"},
		{"--install", "test-op", "oci://example.com/test-chart"},
		{"-n", "test-ns"},
		{"--create-namespace"},
		{"--wait"},
		{"--timeout", "5m"},
	} {
		if !containsSeq(args, want) {
			t.Errorf("args %v missing subsequence %v", args, want)
		}
	}
}

func TestInstallOperator_WithVersion(t *testing.T) {
	readArgs := writeFakeHelm(t)
	op := baseOp
	op.Version = "1.2.3"
	if err := InstallOperator(context.Background(), "", op); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !containsSeq(readArgs(), []string{"--version", "1.2.3"}) {
		t.Errorf("--version 1.2.3 not found in args")
	}
}

func TestInstallOperator_NoVersionFlag(t *testing.T) {
	readArgs := writeFakeHelm(t)
	if err := InstallOperator(context.Background(), "", baseOp); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, a := range readArgs() {
		if a == "--version" {
			t.Error("--version flag present but OperatorSpec.Version is empty")
		}
	}
}

func TestInstallOperator_WithSets(t *testing.T) {
	readArgs := writeFakeHelm(t)
	op := baseOp
	op.Sets = []string{"key1=val1", "key2=val2"}
	if err := InstallOperator(context.Background(), "", op); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	args := readArgs()
	if !containsSeq(args, []string{"--set", "key1=val1"}) {
		t.Errorf("--set key1=val1 not found in %v", args)
	}
	if !containsSeq(args, []string{"--set", "key2=val2"}) {
		t.Errorf("--set key2=val2 not found in %v", args)
	}
}

func TestInstallOperator_KubeconfigForwarded(t *testing.T) {
	readArgs := writeFakeHelm(t)
	if err := InstallOperator(context.Background(), "/tmp/test.yaml", baseOp); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !containsSeq(readArgs(), []string{"--kubeconfig", "/tmp/test.yaml"}) {
		t.Errorf("--kubeconfig not forwarded in args")
	}
}

func TestInstallOperator_NoKubeconfigWhenEmpty(t *testing.T) {
	readArgs := writeFakeHelm(t)
	if err := InstallOperator(context.Background(), "", baseOp); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, a := range readArgs() {
		if a == "--kubeconfig" {
			t.Error("--kubeconfig present but kubeconfig arg was empty")
		}
	}
}

func TestInstallOperator_FailureIncludesOutput(t *testing.T) {
	writeFakeHelm(t)
	t.Setenv("HELM_FAIL", "1")
	err := InstallOperator(context.Background(), "", baseOp)
	if err == nil {
		t.Fatal("expected error on helm failure, got nil")
	}
	if !strings.Contains(err.Error(), "simulated failure") {
		t.Errorf("error %q does not include helm output", err.Error())
	}
}
