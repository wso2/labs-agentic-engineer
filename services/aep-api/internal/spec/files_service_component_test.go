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

// Service-tier cases for spec.FilesService, which aep-api's in-process design
// and validation adapters still call (the public Files API is gone; the AE
// Studio pod serves spec files). They run the service directly over the real
// gitfs engine and a real bare file:// origin (filesRig), so a stale baseSha is
// a real conflict and a batch is a real single commit.
package spec_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/spec"
)

const staleSHA = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"

func (r *filesRig) readSHA(t *testing.T, path string) string {
	t.Helper()
	fc, err := r.svc.ReadAt(context.Background(), filesTestOrg, filesTestProj, path, "")
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return fc.SHA
}

func (r *filesRig) apply(t *testing.T, req spec.ApplyRequest) (*spec.ApplyResult, []spec.Conflict, error) {
	t.Helper()
	return r.svc.Apply(context.Background(), filesTestOrg, filesTestProj, req)
}

func (r *filesRig) mustApply(t *testing.T, req spec.ApplyRequest) *spec.ApplyResult {
	t.Helper()
	res, _, err := r.apply(t, req)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	return res
}

func TestFilesService_Apply_StaleBaseSHA_ConflictNothingApplied(t *testing.T) {
	r := newFilesRig(t, map[string]string{"specs/requirements/prd.md": "v1"})
	headBefore := r.remote.HeadSHA(t)

	_, conflicts, err := r.apply(t, spec.ApplyRequest{Writes: []spec.WriteOp{
		{Path: "specs/requirements/prd.md", Content: "v2", BaseSHA: staleSHA},
	}})
	if !errors.Is(err, spec.ErrApplyConflict) {
		t.Fatalf("err = %v, want ErrApplyConflict", err)
	}
	want := spec.Conflict{
		Path: "specs/requirements/prd.md", BaseSHA: staleSHA,
		CurrentSHA: "28c218c44b49222f91536daf5b4d9871638edc8e", // git blob sha of "v1"
	}
	if len(conflicts) != 1 || conflicts[0] != want {
		t.Fatalf("conflicts = %+v, want [%+v]", conflicts, want)
	}
	if r.remote.HeadSHA(t) != headBefore {
		t.Error("HEAD advanced on a conflicting apply")
	}
	if got := r.remote.FileAt(t, "main", "specs/requirements/prd.md"); got != "v1" {
		t.Errorf("content mutated on conflict: %q", got)
	}
}

// One conflicting op rejects the whole batch: the valid delete must not land,
// and every conflict is collected.
func TestFilesService_Apply_BatchConflict_AllOrNothing(t *testing.T) {
	r := newFilesRig(t, map[string]string{
		"specs/requirements/prd.md":  "keep me",
		"specs/requirements/todo.md": "scratch",
	})
	todoSHA := r.readSHA(t, "specs/requirements/todo.md")
	headBefore := r.remote.HeadSHA(t)

	_, conflicts, err := r.apply(t, spec.ApplyRequest{
		Writes: []spec.WriteOp{
			{Path: "specs/requirements/prd.md", Content: "clobber", BaseSHA: staleSHA},
			{Path: "specs/design/domain-model.md", Content: "new", BaseSHA: staleSHA}, // absent + baseSha ⇒ conflict
		},
		Deletes: []spec.DeleteOp{{Path: "specs/requirements/todo.md", BaseSHA: todoSHA}}, // valid, must not apply
	})
	if !errors.Is(err, spec.ErrApplyConflict) {
		t.Fatalf("err = %v, want ErrApplyConflict", err)
	}
	if len(conflicts) != 2 {
		t.Fatalf("conflicts = %+v, want both collected", conflicts)
	}
	if r.remote.HeadSHA(t) != headBefore {
		t.Error("HEAD advanced on a conflicting batch")
	}
	if got := r.remote.FileAt(t, "main", "specs/requirements/todo.md"); got != "scratch" {
		t.Errorf("valid delete leaked through a conflicting batch: %q", got)
	}
}

func TestFilesService_Apply_PathRejections(t *testing.T) {
	r := newFilesRig(t, map[string]string{"specs/requirements/prd.md": "x"})
	for name, path := range map[string]string{
		"traversal":                      "specs/../etc/passwd",
		"non-specs":                      "README.md",
		"absolute":                       "/specs/x.md",
		"validation report is read-only": "tests/acceptance/report.json",
	} {
		_, _, err := r.apply(t, spec.ApplyRequest{Writes: []spec.WriteOp{{Path: path, Content: "x"}}})
		if !errors.Is(err, spec.ErrPathInvalid) {
			t.Errorf("%s: err = %v, want ErrPathInvalid", name, err)
		}
	}
}

func TestFilesService_Apply_SizeCap(t *testing.T) {
	r := newFilesRig(t, map[string]string{"specs/requirements/prd.md": "x"})
	huge := strings.Repeat("A", (5<<20)+1)
	_, _, err := r.apply(t, spec.ApplyRequest{Writes: []spec.WriteOp{{Path: "specs/requirements/big.md", Content: huge}}})
	if !errors.Is(err, spec.ErrPathInvalid) {
		t.Errorf("err = %v, want ErrPathInvalid for an oversized file", err)
	}
}

// A batch that writes specs/design/design.cell also lands a design.json
// skeleton for every deployable component the cell declares that has none, in
// the SAME commit. Non-deployable nodes get no directory; an existing
// design.json is untouched.
func TestFilesService_Apply_ScaffoldsComponentsFromCell(t *testing.T) {
	existing := `{"name":"lunch-api","type":"service","version":"0.1.0","language":"Go","buildpack":"docker","appPath":"lunch-api","entrypoint":"deployment/service","exposure":"intranet","dependencies":[],"description":"hand-written"}`
	r := newFilesRig(t, map[string]string{"specs/design/components/lunch-api/design.json": existing})

	cell := "component lunch-api service\n" +
		"component lunch-web web-application\n" +
		"component slack-notifier service\n" +
		"component orders-db database\n"
	r.mustApply(t, spec.ApplyRequest{
		Writes:  []spec.WriteOp{{Path: "specs/design/design.cell", Content: cell}},
		Message: "design cell",
	})

	for id, wantType := range map[string]string{"lunch-web": "web-application", "slack-notifier": "service"} {
		var parsed map[string]any
		content := r.remote.FileAt(t, "main", "specs/design/components/"+id+"/design.json")
		if err := json.Unmarshal([]byte(content), &parsed); err != nil {
			t.Fatalf("scaffolded %s design.json not JSON: %v", id, err)
		}
		if parsed["name"] != id || parsed["type"] != wantType {
			t.Errorf("scaffold %s = name %v type %v", id, parsed["name"], parsed["type"])
		}
		if parsed["language"] == "" || parsed["description"] == "" {
			t.Errorf("scaffold %s missing enrichable defaults: %v", id, parsed)
		}
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(r.remote.FileAt(t, "main", "specs/design/components/lunch-api/design.json")), &got); err != nil {
		t.Fatalf("existing design.json parse: %v", err)
	}
	if got["language"] != "Go" || got["description"] != "hand-written" {
		t.Errorf("existing enrichment clobbered: %v", got)
	}
	if _, err := r.svc.ReadAt(context.Background(), filesTestOrg, filesTestProj, "specs/design/components/orders-db/design.json", ""); !errors.Is(err, spec.ErrFileNotFound) {
		t.Errorf("orders-db dir should not exist: err = %v", err)
	}
}

// The authored `stories` claim (#369) survives a later cell save; scaffolds are
// born without one.
func TestFilesService_Apply_AuthoredStoriesSurviveCellSave(t *testing.T) {
	enriched := `{"name":"lunch-api","type":"service","version":"0.1.0","language":"Go","buildpack":"docker","appPath":"lunch-api","entrypoint":"deployment/service","exposure":"intranet","dependencies":[],"description":"hand-written","stories":[9]}`
	r := newFilesRig(t, map[string]string{"specs/design/components/lunch-api/design.json": enriched})

	r.mustApply(t, spec.ApplyRequest{
		Writes:  []spec.WriteOp{{Path: "specs/design/design.cell", Content: "component lunch-api service\ncomponent lunch-web web-application\n"}},
		Message: "design cell",
	})

	var scaffold, existing map[string]any
	if err := json.Unmarshal([]byte(r.remote.FileAt(t, "main", "specs/design/components/lunch-web/design.json")), &scaffold); err != nil {
		t.Fatalf("scaffold parse: %v", err)
	}
	if _, present := scaffold["stories"]; present {
		t.Errorf("scaffold carries stories = %v, want the agent to author it", scaffold["stories"])
	}
	if err := json.Unmarshal([]byte(r.remote.FileAt(t, "main", "specs/design/components/lunch-api/design.json")), &existing); err != nil {
		t.Fatalf("existing parse: %v", err)
	}
	if got, _ := json.Marshal(existing["stories"]); string(got) != "[9]" {
		t.Errorf("authored stories = %s, want [9] untouched", got)
	}
	if existing["description"] != "hand-written" || existing["language"] != "Go" {
		t.Errorf("cell save clobbered enrichment: %v", existing)
	}
}

// tests/acceptance/report.json is the one readable non-specs/ path; any other
// non-specs/ path stays refused.
func TestFilesService_ReadAt_ValidationReportAllowListed(t *testing.T) {
	report := `{"schemaVersion":1,"criteria":[]}`
	r := newFilesRig(t, map[string]string{
		"tests/acceptance/report.json": report,
		"README.md":                    "root",
	})
	fc, err := r.svc.ReadAt(context.Background(), filesTestOrg, filesTestProj, "tests/acceptance/report.json", "")
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	if fc.Content != report || fc.Path != "tests/acceptance/report.json" || fc.SHA == "" {
		t.Fatalf("read report wrong: %+v", fc)
	}
	if _, err := r.svc.ReadAt(context.Background(), filesTestOrg, filesTestProj, "README.md", ""); !errors.Is(err, spec.ErrPathInvalid) {
		t.Errorf("read README.md: err = %v, want ErrPathInvalid (the allow-list is exact)", err)
	}
}

func (r *filesRig) bundle(t *testing.T, prefix, at string) *spec.FileBundle {
	t.Helper()
	b, err := r.svc.Bundle(context.Background(), filesTestOrg, filesTestProj, prefix, at)
	if err != nil {
		t.Fatalf("bundle(%q, %q): %v", prefix, at, err)
	}
	return b
}

// A path the read gate refuses is OMITTED from a bundle, not an error: the tree
// also holds application code.
func TestFilesService_Bundle_OmitsWhatTheReadGateRefuses(t *testing.T) {
	r := newFilesRig(t, map[string]string{
		"specs/requirements/prd.md": "req",
		"README.md":                 "readme",
		"src/main.go":               "package main",
		".github/workflows/ci.yml":  "on: push",
	})
	got := r.bundle(t, "", "")
	if len(got.Files) != 1 || got.Files[0].Path != "specs/requirements/prd.md" {
		t.Fatalf("bundle leaked past the read gate: %+v", got.Files)
	}
	if got.Files[0].SHA != r.readSHA(t, "specs/requirements/prd.md") {
		t.Errorf("bundle sha disagrees with the per-file read")
	}
	if want := r.mirrorRevParse(t, "refs/heads/main"); got.CommitSHA != want {
		t.Errorf("commitSha = %s, want the branch tip %s", got.CommitSHA, want)
	}
}

func TestFilesService_Bundle_ReadsAtAPinnedCommit(t *testing.T) {
	r := newFilesRig(t, map[string]string{"specs/requirements/prd.md": "v1"})
	before := r.bundle(t, "specs/", "")

	r.mustApply(t, spec.ApplyRequest{Writes: []spec.WriteOp{
		{Path: "specs/requirements/prd.md", Content: "v2", BaseSHA: r.readSHA(t, "specs/requirements/prd.md")},
	}})

	if head := r.bundle(t, "specs/", ""); head.Files[0].Content != "v2" {
		t.Fatalf("unpinned bundle = %q, want the new tip content", head.Files[0].Content)
	}
	pinned := r.bundle(t, "specs/", before.CommitSHA)
	if pinned.CommitSHA != before.CommitSHA || pinned.Files[0].Content != "v1" {
		t.Errorf("pinned bundle = %s/%q, want %s/v1", pinned.CommitSHA, pinned.Files[0].Content, before.CommitSHA)
	}
}

// `at` takes an object name and nothing else; a revision expression would turn
// a prefix read into a browser over the repo's history.
func TestFilesService_Bundle_RejectsARevisionExpressionAsRef(t *testing.T) {
	r := newFilesRig(t, map[string]string{"specs/requirements/prd.md": "v1"})
	for _, at := range []string{"main", "HEAD~1", "refs/heads/main"} {
		if _, err := r.svc.Bundle(context.Background(), filesTestOrg, filesTestProj, "", at); err == nil {
			t.Errorf("at=%s: want an error", at)
		}
	}
}
