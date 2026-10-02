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

// The apply cases of services/aep-api/internal/spec/files_component_test.go,
// moved with the apply core; the aep-api copy is deleted in phase 4.

package files_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/wso2/aep/ae-studio-tools/internal/files"
	"github.com/wso2/aep/ae-studio-tools/internal/repo/repotest"
)

const staleSHA = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"

func bundleSHAs(t *testing.T, a *applyRig) map[string]string {
	t.Helper()
	b, err := a.reader.Bundle(ctx, "greeter", "specs/", "")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, f := range b.Files {
		out[f.Path] = f.SHA
	}
	return out
}

func TestApply_MultiWriteAndDelete_SingleCommit(t *testing.T) {
	a, origin := newApplier(t, map[string]string{
		"specs/requirements/prd.md":  "old",
		"specs/requirements/todo.md": "scratch",
	})
	shas := bundleSHAs(t, a)
	headBefore := origin.HeadSHA(t)

	res, _, err := a.Apply(ctx, "greeter", files.ApplyRequest{
		Writes: []files.WriteOp{
			{Path: "specs/requirements/prd.md", Content: "new", BaseSHA: shas["specs/requirements/prd.md"]},
			{Path: "specs/design/domain-model.md", Content: "# Design"}, // baseSha omitted ⇒ create
		},
		Deletes: []files.DeleteOp{{Path: "specs/requirements/todo.md", BaseSHA: shas["specs/requirements/todo.md"]}},
		Message: "from test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.CommitSHA == "" || len(res.Files) != 2 {
		t.Fatalf("apply result wrong: %+v", res)
	}
	if got := origin.Git(t, "rev-parse", res.CommitSHA+"^"); got != headBefore {
		t.Fatalf("parent = %s, want exactly one commit on %s", got, headBefore)
	}
	if got := origin.FileAt(t, repotest.Branch, "specs/requirements/prd.md"); got != "new" {
		t.Errorf("prd.md = %q, want new", got)
	}
	if got := origin.FileAt(t, repotest.Branch, "specs/design/domain-model.md"); got != "# Design" {
		t.Errorf("domain-model.md = %q", got)
	}
	if _, err := a.reader.ReadAt(ctx, "greeter", "specs/requirements/todo.md", ""); !errors.Is(err, files.ErrFileNotFound) {
		t.Errorf("todo.md still present: %v", err)
	}
}

func TestApply_StaleBaseSHA_ConflictNothingApplied(t *testing.T) {
	a, origin := newApplier(t, map[string]string{"specs/requirements/prd.md": "v1"})
	headBefore := origin.HeadSHA(t)

	_, conflicts, err := a.Apply(ctx, "greeter", files.ApplyRequest{Writes: []files.WriteOp{
		{Path: "specs/requirements/prd.md", Content: "v2", BaseSHA: staleSHA},
	}})
	if !errors.Is(err, files.ErrApplyConflict) {
		t.Fatalf("err = %v, want ErrApplyConflict", err)
	}
	// currentSha is the git blob sha of "v1"; baseSha echoes the stale one.
	want := []files.Conflict{{Path: "specs/requirements/prd.md", BaseSHA: staleSHA, CurrentSHA: "28c218c44b49222f91536daf5b4d9871638edc8e"}}
	if !reflect.DeepEqual(conflicts, want) {
		t.Fatalf("conflicts = %+v, want %+v", conflicts, want)
	}
	if origin.HeadSHA(t) != headBefore {
		t.Error("HEAD advanced on a conflicting apply")
	}
}

// A batch where only ONE op conflicts is rejected wholesale: the valid delete
// must not be applied (all-or-nothing), and every conflict is collected.
func TestApply_BatchConflict_AllOrNothing_CollectsAllConflicts(t *testing.T) {
	a, origin := newApplier(t, map[string]string{
		"specs/requirements/prd.md":  "keep me",
		"specs/requirements/todo.md": "scratch",
	})
	shas := bundleSHAs(t, a)
	headBefore := origin.HeadSHA(t)

	_, conflicts, err := a.Apply(ctx, "greeter", files.ApplyRequest{
		Writes: []files.WriteOp{
			{Path: "specs/requirements/prd.md", Content: "clobber", BaseSHA: staleSHA},
			{Path: "specs/design/domain-model.md", Content: "new", BaseSHA: staleSHA}, // absent + baseSha set ⇒ conflict too
		},
		Deletes: []files.DeleteOp{{Path: "specs/requirements/todo.md", BaseSHA: shas["specs/requirements/todo.md"]}},
	})
	if !errors.Is(err, files.ErrApplyConflict) || len(conflicts) != 2 {
		t.Fatalf("err = %v, conflicts = %+v, want ALL 2 collected", err, conflicts)
	}
	if origin.HeadSHA(t) != headBefore {
		t.Error("HEAD advanced on a conflicting batch")
	}
}

func TestApply_BaseSHAOmittedButExists_Conflict(t *testing.T) {
	a, _ := newApplier(t, map[string]string{"specs/requirements/prd.md": "exists"})
	_, conflicts, err := a.Apply(ctx, "greeter", files.ApplyRequest{Writes: []files.WriteOp{
		{Path: "specs/requirements/prd.md", Content: "clobber"}, // no baseSha ⇒ must-not-exist
	}})
	if !errors.Is(err, files.ErrApplyConflict) || len(conflicts) != 1 || conflicts[0].BaseSHA != "" || conflicts[0].CurrentSHA == "" {
		t.Fatalf("err = %v, conflicts = %+v, want one must-not-exist conflict", err, conflicts)
	}
}

func TestApply_DeleteOfAMissingFileConflicts(t *testing.T) {
	a, _ := newApplier(t, map[string]string{"specs/requirements/prd.md": "x"})
	_, conflicts, err := a.Apply(ctx, "greeter", files.ApplyRequest{Deletes: []files.DeleteOp{{Path: "specs/requirements/gone.md"}}})
	if !errors.Is(err, files.ErrApplyConflict) || len(conflicts) != 1 || conflicts[0].CurrentSHA != "" {
		t.Fatalf("err = %v, conflicts = %+v", err, conflicts)
	}
}

func TestApply_RequestRejections(t *testing.T) {
	a, origin := newApplier(t, map[string]string{"specs/requirements/prd.md": "x"})
	head := origin.HeadSHA(t)
	huge := strings.Repeat("A", (5<<20)+1)
	cases := map[string]files.ApplyRequest{
		"empty":             {},
		"traversal":         {Writes: []files.WriteOp{{Path: "specs/../etc/passwd", Content: "x"}}},
		"non-specs":         {Writes: []files.WriteOp{{Path: "README.md", Content: "x"}}},
		"absolute":          {Writes: []files.WriteOp{{Path: "/specs/x.md", Content: "x"}}},
		"read-only escape":  {Writes: []files.WriteOp{{Path: "tests/acceptance/report.json", Content: "{}"}}},
		"size cap":          {Writes: []files.WriteOp{{Path: "specs/requirements/big.md", Content: huge}}},
		"duplicate write":   {Writes: []files.WriteOp{{Path: "specs/a.md", Content: "1"}, {Path: "specs/a.md", Content: "2"}}},
		"write and delete":  {Writes: []files.WriteOp{{Path: "specs/requirements/prd.md", Content: "1"}}, Deletes: []files.DeleteOp{{Path: "specs/requirements/prd.md"}}},
		"delete non-specs":  {Deletes: []files.DeleteOp{{Path: "README.md"}}},
		"non-canonical del": {Deletes: []files.DeleteOp{{Path: "specs//prd.md"}}},
	}
	for name, req := range cases {
		if _, _, err := a.Apply(ctx, "greeter", req); !errors.Is(err, files.ErrPathInvalid) {
			t.Errorf("%s: err = %v, want ErrPathInvalid", name, err)
		}
	}
	if origin.HeadSHA(t) != head {
		t.Fatal("a refused request committed")
	}
}

// A batch byte-identical to the tip commits nothing: Changed is false and the
// commit is the unchanged tip.
func TestApply_IdenticalContentIsNoChange(t *testing.T) {
	a, origin := newApplier(t, map[string]string{"specs/requirements/prd.md": "same"})
	shas := bundleSHAs(t, a)
	head := origin.HeadSHA(t)
	res, _, err := a.Apply(ctx, "greeter", files.ApplyRequest{Writes: []files.WriteOp{
		{Path: "specs/requirements/prd.md", Content: "same", BaseSHA: shas["specs/requirements/prd.md"]},
	}})
	if err != nil || res.Changed || res.CommitSHA != head || origin.HeadSHA(t) != head {
		t.Fatalf("res = %+v, err = %v; want no commit at %s", res, err, head)
	}
}

func TestApply_UnknownProject(t *testing.T) {
	a, _ := newApplier(t, nil)
	if _, _, err := a.Apply(ctx, "nope", files.ApplyRequest{Writes: []files.WriteOp{{Path: "specs/a.md", Content: "a"}}}); err == nil {
		t.Fatal("an unknown project applied")
	}
}

// ---- soft validation -----------------------------------------------------

func TestApply_WarningsNonBlocking(t *testing.T) {
	a, origin := newApplier(t, nil)
	res, _, err := a.Apply(ctx, "greeter", files.ApplyRequest{Writes: []files.WriteOp{
		{Path: "specs/design/components/foo/design.json", Content: "{ not valid json"},
	}})
	if err != nil {
		t.Fatalf("invalid json must NOT block apply: %v", err)
	}
	if len(res.Warnings) != 1 || res.Warnings[0].Code != "INVALID_JSON" {
		t.Fatalf("expected one INVALID_JSON warning: %+v", res.Warnings)
	}
	if got := origin.FileAt(t, repotest.Branch, "specs/design/components/foo/design.json"); got != "{ not valid json" {
		t.Errorf("file not committed: %q", got)
	}
}

func TestApply_SchemaViolationWarning_NonBlocking(t *testing.T) {
	a, origin := newApplier(t, nil)
	// Valid JSON + valid schema, but name != component directory ("bar" != "foo").
	valid := `{"name":"bar","type":"service","version":"1","language":"go","buildpack":"go","appPath":".","entrypoint":"m","exposure":"intranet","connections":[],"description":"d"}`
	res, _, err := a.Apply(ctx, "greeter", files.ApplyRequest{Writes: []files.WriteOp{
		{Path: "specs/design/components/foo/design.json", Content: valid},
	}})
	if err != nil {
		t.Fatalf("schema violation must NOT block apply: %v", err)
	}
	if len(res.Warnings) != 1 || res.Warnings[0].Code != "SCHEMA_VIOLATION" {
		t.Fatalf("expected one SCHEMA_VIOLATION warning: %+v", res.Warnings)
	}
	if got := origin.FileAt(t, repotest.Branch, "specs/design/components/foo/design.json"); got != valid {
		t.Errorf("file not committed despite warning: %q", got)
	}
}

func TestApply_InvalidSecurityDesignWarns(t *testing.T) {
	a, _ := newApplier(t, nil)
	res, _, err := a.Apply(ctx, "greeter", files.ApplyRequest{Writes: []files.WriteOp{
		{Path: "specs/design/security.json", Content: `{"version":3}`},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Warnings) == 0 || res.Warnings[0].Path != "specs/design/security.json" || res.Warnings[0].Code == "" {
		t.Fatalf("warnings = %+v, want a coded security.json warning", res.Warnings)
	}
}

// The security design's coverage notices ride the soft-warning channel; the
// apply path assembles the bundle the batch lands and judges that.
func TestApply_SecurityDesignCoverageWarnings(t *testing.T) {
	fixture := func(rel string) string {
		t.Helper()
		body, err := os.ReadFile("../securityspec/testdata/" + rel)
		if err != nil {
			t.Fatalf("read fixture %s — layout drift?: %v", rel, err)
		}
		return string(body)
	}
	catalog := fixture("expense-tracker.json")
	a, origin := newApplier(t, map[string]string{
		"specs/design/security.json":                       catalog,
		"specs/design/design.cell":                         fixture("expense-tracker/design.cell"),
		"specs/design/components/expense-api/openapi.yaml": fixture("expense-tracker/expense-api.openapi.yaml"),
	})
	withOrphan := strings.Replace(catalog,
		`{ "handle": "submit", "description": "Create and send a claim" }`,
		`{ "handle": "submit", "description": "Create and send a claim" },
        { "handle": "archive", "description": "Archive an old claim" }`, 1)
	if withOrphan == catalog {
		t.Fatal("fixture reworded — the orphan handle was never added")
	}
	res, _, err := a.Apply(ctx, "greeter", files.ApplyRequest{Writes: []files.WriteOp{{
		Path: "specs/design/security.json", Content: withOrphan, BaseSHA: bundleSHAs(t, a)["specs/design/security.json"],
	}}})
	if err != nil {
		t.Fatalf("a coverage warning must never block apply: %v", err)
	}
	byCode := map[string]files.Warning{}
	for _, w := range res.Warnings {
		byCode[w.Code] = w
	}
	unused, ok := byCode["SECURITY_HANDLE_USED_NOWHERE"]
	if !ok {
		t.Fatalf("the orphan handle must reach the warnings channel: %+v", res.Warnings)
	}
	if unused.Path != "specs/design/security.json" || !strings.Contains(unused.Message, "claims:archive") {
		t.Errorf("warning = %+v, want the catalog's repo path and the handle", unused)
	}
	if _, ok := byCode["SECURITY_ASSIGN_TO_DIRECTORY_CHECKED"]; !ok {
		t.Errorf("the assignTo INFO note must not be dropped: %+v", res.Warnings)
	}
	if got := origin.FileAt(t, repotest.Branch, "specs/design/security.json"); got != withOrphan {
		t.Error("the catalog was not committed despite the notices being non-blocking")
	}
}

func TestApply_NoSecurityNoticesForANonDesignWrite(t *testing.T) {
	a, _ := newApplier(t, map[string]string{"specs/requirements/prd.md": "# PRD\n"})
	res, _, err := a.Apply(ctx, "greeter", files.ApplyRequest{Writes: []files.WriteOp{{Path: "specs/requirements/notes.md", Content: "hello"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Warnings) != 0 {
		t.Errorf("a requirements write must carry no design notices: %+v", res.Warnings)
	}
}

// ---- shas and concurrency ------------------------------------------------

// The returned commit is origin's tip, and each returned blob sha is what a
// later read serves — the Room folds them into its next baseShas.
func TestApply_ShaConsistency_OriginAndReadBack(t *testing.T) {
	a, origin := newApplier(t, map[string]string{"specs/requirements/prd.md": "v1"})
	res, _, err := a.Apply(ctx, "greeter", files.ApplyRequest{Writes: []files.WriteOp{
		{Path: "specs/requirements/prd.md", Content: "v2", BaseSHA: bundleSHAs(t, a)["specs/requirements/prd.md"]},
		{Path: "specs/design/domain-model.md", Content: "# Design"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if tip := origin.HeadSHA(t); res.CommitSHA != tip {
		t.Errorf("commitSha %s != origin tip %s", res.CommitSHA, tip)
	}
	read := bundleSHAs(t, a)
	for _, f := range res.Files {
		if read[f.Path] != f.SHA {
			t.Errorf("%s: apply returned sha %s, read returns %s", f.Path, f.SHA, read[f.Path])
		}
	}
}

// Two concurrent applies to DISJOINT paths both land: the loser of the push
// race re-runs its (still valid) preconditions on the winner's commit.
func TestApply_ConcurrentDisjointApplies_BothLand(t *testing.T) {
	a, origin := newApplier(t, map[string]string{"specs/requirements/prd.md": "seed"})
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i, p := range []string{"specs/design/a.md", "specs/design/b.md"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, errs[i] = a.Apply(ctx, "greeter", files.ApplyRequest{Writes: []files.WriteOp{{Path: p, Content: p}}})
		}()
	}
	wg.Wait()
	if errs[0] != nil || errs[1] != nil {
		t.Fatalf("errs = %v, want both to land", errs)
	}
	for _, p := range []string{"specs/design/a.md", "specs/design/b.md"} {
		if got := origin.FileAt(t, repotest.Branch, p); got != p {
			t.Errorf("%s = %q", p, got)
		}
	}
}

// Two concurrent applies to the SAME path with the same baseSha: exactly one
// lands, the other gets the clean conflict — never a lost update.
func TestApply_ConcurrentSamePath_OneLandsOneConflicts(t *testing.T) {
	const path = "specs/requirements/prd.md"
	a, origin := newApplier(t, map[string]string{path: "seed"})
	base := bundleSHAs(t, a)[path]
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i, content := range []string{"first", "second"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, errs[i] = a.Apply(ctx, "greeter", files.ApplyRequest{Writes: []files.WriteOp{{Path: path, Content: content, BaseSHA: base}}})
		}()
	}
	wg.Wait()
	ok, conflict := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, files.ErrApplyConflict):
			conflict++
		}
	}
	if ok != 1 || conflict != 1 {
		t.Fatalf("errs = %v, want exactly one success and one conflict", errs)
	}
	if got := origin.FileAt(t, repotest.Branch, path); got != "first" && got != "second" {
		t.Errorf("content = %q, want the single winner's write", got)
	}
}

// ---- scaffold ------------------------------------------------------------

// A batch that writes design.cell also lands a design.json skeleton for every
// deployable component the cell declares that has none yet — in the SAME
// commit. Non-deployable nodes get none; an existing design.json is untouched.
func TestApply_ScaffoldsComponentsFromCell(t *testing.T) {
	existing := `{"name":"lunch-api","type":"service","version":"0.1.0","language":"Go","buildpack":"docker","appPath":"lunch-api","entrypoint":"deployment/service","exposure":"intranet","dependencies":[],"description":"hand-written"}`
	a, origin := newApplier(t, map[string]string{"specs/design/components/lunch-api/design.json": existing})

	cell := "component lunch-api service\n" +
		"component lunch-web web-application\n" +
		"component slack-notifier service\n" +
		"component orders-db database\n"
	res, _, err := a.Apply(ctx, "greeter", files.ApplyRequest{
		Writes:  []files.WriteOp{{Path: "specs/design/design.cell", Content: cell}},
		Message: "design cell",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"specs/design/components/lunch-web/design.json",
		"specs/design/components/slack-notifier/design.json",
		"specs/design/design.cell",
	}
	if got := changedPathsIn(t, origin, res.CommitSHA); !sameStrings(got, want) {
		t.Fatalf("commit changed %v, want %v", got, want)
	}
	for id, wantType := range map[string]string{"lunch-web": "web-application", "slack-notifier": "service"} {
		var parsed map[string]any
		if err := json.Unmarshal([]byte(origin.FileAt(t, repotest.Branch, "specs/design/components/"+id+"/design.json")), &parsed); err != nil {
			t.Fatalf("scaffolded %s design.json not JSON: %v", id, err)
		}
		if parsed["name"] != id || parsed["type"] != wantType || parsed["language"] == "" || parsed["description"] == "" {
			t.Errorf("scaffold %s = %v", id, parsed)
		}
		if _, present := parsed["stories"]; present {
			t.Errorf("scaffold %s carries stories, want the agent to author them", id)
		}
	}
	if got := origin.FileAt(t, repotest.Branch, "specs/design/components/lunch-api/design.json"); got != existing {
		t.Errorf("existing enrichment clobbered: %s", got)
	}
	scaffolded := 0
	for _, w := range res.Warnings {
		if strings.HasPrefix(w.Message, "scaffolded from design.cell") {
			scaffolded++
		}
	}
	if scaffolded != 2 {
		t.Errorf("warnings = %+v, want one scaffold note per skeleton", res.Warnings)
	}
}

// A component design.json the same batch writes is not scaffolded over.
func TestApply_ScaffoldSkipsAComponentInTheBatch(t *testing.T) {
	a, origin := newApplier(t, nil)
	mine := `{"name":"lunch-web","authored":true}`
	if _, _, err := a.Apply(ctx, "greeter", files.ApplyRequest{Writes: []files.WriteOp{
		{Path: "specs/design/design.cell", Content: "component lunch-web web-application\n"},
		{Path: "specs/design/components/lunch-web/design.json", Content: mine},
	}}); err != nil {
		t.Fatal(err)
	}
	if got := origin.FileAt(t, repotest.Branch, "specs/design/components/lunch-web/design.json"); got != mine {
		t.Fatalf("design.json = %s, want the batch's own", got)
	}
}

// The authored `stories` claim survives a later cell save.
func TestApply_AuthoredStoriesSurviveCellSave(t *testing.T) {
	enriched := `{"name":"lunch-api","type":"service","version":"0.1.0","language":"Go","buildpack":"docker","appPath":"lunch-api","entrypoint":"deployment/service","exposure":"intranet","dependencies":[],"description":"hand-written","stories":[9]}`
	a, origin := newApplier(t, map[string]string{"specs/design/components/lunch-api/design.json": enriched})
	if _, _, err := a.Apply(ctx, "greeter", files.ApplyRequest{Writes: []files.WriteOp{
		{Path: "specs/design/design.cell", Content: "component lunch-api service\ncomponent lunch-web web-application\n"},
	}}); err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(origin.FileAt(t, repotest.Branch, "specs/design/components/lunch-api/design.json")), &parsed); err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(parsed["stories"]); got != "[9]" || parsed["description"] != "hand-written" {
		t.Errorf("cell save clobbered enrichment: %v", parsed)
	}
}

// A cell whose frontmatter is unterminated scaffolds nothing.
func TestApply_MalformedCellScaffoldsNothing(t *testing.T) {
	a, origin := newApplier(t, nil)
	res, _, err := a.Apply(ctx, "greeter", files.ApplyRequest{Writes: []files.WriteOp{
		{Path: "specs/design/design.cell", Content: "---\nsourceSpec: x\ncomponent lunch-web web-application\n"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got := changedPathsIn(t, origin, res.CommitSHA); len(got) != 1 {
		t.Fatalf("commit changed %v, want only the cell", got)
	}
}
