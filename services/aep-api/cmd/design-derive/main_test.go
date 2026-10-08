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

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const repoCatalog = "../../../../deployments/single-cluster/resource-types"

func copyTree(t *testing.T, src string) string {
	t.Helper()
	dst := t.TempDir()
	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, body, 0o644)
	})
	if err != nil {
		t.Fatalf("copy %s: %v", src, err)
	}
	return dst
}

// production/ holds design.json files as the platform committed them; input/ is
// the same with derived fields removed.
func TestDeriveDir_ReproducesProductionByteForByte(t *testing.T) {
	t.Parallel()
	dir := copyTree(t, "testdata/track-each-hire9665/input")

	changed, err := deriveDir(context.Background(), dir, "track-each-hire9665", repoCatalog)
	if err != nil {
		t.Fatalf("deriveDir: %v", err)
	}
	components := []string{"onboarding-api", "onboarding-webapp"}
	if len(changed) != len(components) {
		t.Fatalf("want both components rewritten, got %v", changed)
	}
	for _, c := range components {
		got, err := os.ReadFile(filepath.Join(dir, "components", c, "design.json"))
		if err != nil {
			t.Fatal(err)
		}
		want, err := os.ReadFile(filepath.Join("testdata/track-each-hire9665/production/components", c, "design.json"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s: derived design.json differs from production\n--- got\n%s\n--- want\n%s", c, got, want)
		}
	}

	again, err := deriveDir(context.Background(), dir, "track-each-hire9665", repoCatalog)
	if err != nil || len(again) != 0 {
		t.Fatalf("a second run must change nothing: changed=%v err=%v", again, err)
	}
}

func writeDesign(t *testing.T, deps string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "design.cell"), []byte("title T\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	comp := filepath.Join(dir, "components", "orders-api")
	if err := os.MkdirAll(comp, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"name":"orders-api","type":"service","dependencies":` + deps + "}\n"
	if err := os.WriteFile(filepath.Join(comp, "design.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func runCLI(args ...string) (code int, stdout, stderr string) {
	var out, errOut bytes.Buffer
	code = run(context.Background(), args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestRun_UnknownResourceTypeRefusesWithProductionText(t *testing.T) {
	t.Parallel()
	dir := writeDesign(t, `[{"kind":"platform-resource","name":"receipts","resourceType":"object-storage"}]`)
	before, _ := os.ReadFile(filepath.Join(dir, "components/orders-api/design.json"))

	code, _, stderr := runCLI("--design-dir", dir, "--project", "shop", "--resource-types", repoCatalog)
	if code != 1 {
		t.Fatalf("exit = %d, want 1 (stderr %q)", code, stderr)
	}
	for _, s := range []string{"resourceType is not installed on this cluster", `receipts ("object-storage")`, "available: postgres-cnpg, thunder-app"} {
		if !strings.Contains(stderr, s) {
			t.Errorf("stderr %q missing %q", stderr, s)
		}
	}
	after, _ := os.ReadFile(filepath.Join(dir, "components/orders-api/design.json"))
	if !bytes.Equal(before, after) {
		t.Error("a refusal must not write the design")
	}
}

func TestRun_AuthConflictRefuses(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "design.cell"), []byte("title T\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	comp := filepath.Join(dir, "components", "orders-api")
	if err := os.MkdirAll(comp, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"name":"orders-api","type":"service","exposesAPI":{"auth":"service-required"},` +
		`"dependencies":[{"kind":"platform-resource","name":"user-auth","resourceType":"thunder-app"}]}` + "\n"
	if err := os.WriteFile(filepath.Join(comp, "design.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := runCLI("--design-dir", dir, "--project", "shop", "--resource-types", repoCatalog)
	if code != 1 || !strings.Contains(stderr, "explicitly sets a conflicting exposesAPI.auth") || !strings.Contains(stderr, `component "orders-api"`) {
		t.Fatalf("exit = %d stderr = %q, want 1 and the conflict", code, stderr)
	}
}

func TestRun_PrintsChangedThenNoChange(t *testing.T) {
	t.Parallel()
	dir := writeDesign(t, `[{"kind":"platform-resource","name":"orders-db","resourceType":"postgres-cnpg"}]`)

	code, stdout, stderr := runCLI("--design-dir", dir, "--project", "shop", "--resource-types", repoCatalog)
	if code != 0 || stdout != "derived: components/orders-api/design.json\n" {
		t.Fatalf("first run: exit %d stdout %q stderr %q", code, stdout, stderr)
	}
	code, stdout, _ = runCLI("--design-dir", dir, "--project", "shop", "--resource-types", repoCatalog)
	if code != 0 || stdout != "derived: no change\n" {
		t.Fatalf("second run: exit %d stdout %q", code, stdout)
	}
}

func TestRun_NoDesignIsANoOp(t *testing.T) {
	t.Parallel()
	code, stdout, _ := runCLI("--design-dir", t.TempDir(), "--project", "shop", "--resource-types", repoCatalog)
	if code != 0 || stdout != "derived: no change\n" {
		t.Fatalf("exit %d stdout %q", code, stdout)
	}
}

func TestRun_UsageErrors(t *testing.T) {
	t.Parallel()
	if code, _, _ := runCLI("--project", "shop"); code != 2 {
		t.Errorf("missing --design-dir: exit %d, want 2", code)
	}
	dir := writeDesign(t, `[]`)
	if code, _, _ := runCLI("--design-dir", dir, "--project", "shop", "--resource-types", t.TempDir()); code != 2 {
		t.Errorf("empty catalog: exit %d, want 2", code)
	}
}

// `go test` runs in this package's directory.
func TestFindRepoCatalog(t *testing.T) {
	t.Parallel()
	got, err := findRepoCatalog()
	if err != nil {
		t.Fatalf("findRepoCatalog: %v", err)
	}
	want, _ := filepath.Abs(repoCatalog)
	if got != want {
		t.Fatalf("found %s, want %s", got, want)
	}
}
