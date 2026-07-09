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

// COMPONENT tier (bff-component-testing.md §4): the REAL skills services behind
// the REAL production handler chain — global middleware → faked auth at the
// auth.WithClaims seam → orgensure → Huma parsing/validation → the tenant gate in
// ENFORCE → mapSkillError — driven in-process via the componenttest harness. The
// SkillService/SkillMutationService/SkillImportService run for real over the
// gitfs Workspace engine + one REAL bare file:// origin per org
// (skills.NewComponentStore, the export_test.go handle around
// repo_store_test.go's engine-backed host); NOTHING is faked below the git
// plumbing — every request fetches, reads, and commits genuine git objects.
//
// Read-body shapes are asserted by FIELD SET against the harvested goldens
// (testdata/harvest/golden/). Both goldens and served responses would carry
// Huma's `$schema`, but this edge sets SchemasPath="" (api/huma.go) so responses
// omit it; it is normalised away on both sides so the comparison asserts the
// skills feature's own field set.
//
// External test package: the harness imports api, which imports skills — an
// in-package test file would be an import cycle (mirrors project/design/orgcreds).
package skills_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/api"
	"github.com/wso2/aep/aep-api/internal/feature/skills"
	"github.com/wso2/aep/aep-api/internal/platform/auth"
	"github.com/wso2/aep/aep-api/internal/platform/componenttest"
)

const (
	base      = "/api/v1/skills"
	goldenDir = "../../../testdata/harvest/golden"
)

// newHarness assembles the real chain around the REAL skills services (all
// three) over one engine-backed store, and returns the store so a test can
// drive repo state (e.g. DriftBuiltin for the updates badge).
func newHarness(t *testing.T) (*componenttest.Harness, *skills.ComponentStore) {
	t.Helper()
	store := skills.NewComponentStore(t)
	h := componenttest.New(t, componenttest.Options{Deps: api.HumaDeps{
		SkillSvc:         store.Svc,
		SkillMutationSvc: skills.NewSkillMutationService(store.Svc),
		SkillImportSvc:   skills.NewSkillImportService(store.Svc),
	}})
	return h, store
}

func skillMD(name, extra string) string {
	return "---\nname: " + name + "\ndescription: A component-test skill.\n" + extra + "metadata:\n  aep.version: \"1\"\n---\n\n# " + name + "\n\nbody\n"
}

// --- golden helpers ----------------------------------------------------------

func dropSchema(keys []string) []string {
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		if k != "$schema" {
			out = append(out, k)
		}
	}
	return out
}

func assertObjectFieldSetMatchesGolden(t *testing.T, body, goldenName string) {
	t.Helper()
	got := dropSchema(componenttest.FieldSet(t, body))
	want := dropSchema(componenttest.GoldenFieldSet(t, filepath.Join(goldenDir, goldenName)))
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("%s field set drifted:\n got %v\nwant %v", goldenName, got, want)
	}
}

// arrayFieldElementKeys returns element[0]'s sorted keys for the named array
// field of a JSON object body (the list/updates responses nest their array
// under a key; componenttest.GoldenFieldSet is object-only).
func arrayFieldElementKeys(t *testing.T, raw []byte, field string) []string {
	t.Helper()
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("not a JSON object: %v\n%s", err, raw)
	}
	var arr []map[string]any
	if err := json.Unmarshal(obj[field], &arr); err != nil {
		t.Fatalf("field %q is not a JSON array of objects: %v", field, err)
	}
	if len(arr) == 0 {
		t.Fatalf("field %q is empty; need at least one element", field)
	}
	keys := make([]string, 0, len(arr[0]))
	for k := range arr[0] {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func readGolden(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(goldenDir, name))
	if err != nil {
		t.Fatalf("read golden %s: %v", name, err)
	}
	return raw
}

// claimsHeader mirrors componenttest's private auth header (internal/platform/
// componenttest/auth.go). The harness has no multipart request builder, so the
// import test hand-builds its request and must stamp this header itself.
const claimsHeader = "X-Componenttest-Claims"

// --- read surface ------------------------------------------------------------

func TestSkillsComponent_List_MatchesGolden(t *testing.T) {
	t.Parallel()
	h, _ := newHarness(t)

	resp := h.AsOrg("acme").Get(base)
	if resp.Code != 200 {
		t.Fatalf("list: want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	// Top-level object field set ({skills}) and the element field set of skills[0].
	assertObjectFieldSetMatchesGolden(t, resp.Body.String(), "get_org_skills.json")
	gotElem := arrayFieldElementKeys(t, resp.Body.Bytes(), "skills")
	wantElem := arrayFieldElementKeys(t, readGolden(t, "get_org_skills.json"), "skills")
	if strings.Join(gotElem, ",") != strings.Join(wantElem, ",") {
		t.Fatalf("skills[] element field set drifted:\n got %v\nwant %v", gotElem, wantElem)
	}

	// Semantics: the seeded built-ins are present, read-only.
	var obj struct {
		Skills []map[string]any `json:"skills"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &obj); err != nil {
		t.Fatalf("decode: %v", err)
	}
	var sawGo bool
	for _, s := range obj.Skills {
		if s["name"] == "go" {
			sawGo = true
			if s["kind"] != "org" || s["editable"] != false {
				t.Fatalf("go summary: %v", s)
			}
		}
	}
	if !sawGo {
		t.Fatalf("expected the `go` built-in in the list: %s", resp.Body.String())
	}
}

// TestSkillsComponent_PlatformSkillsReadOnlySurface: platform-kind skills
// (generation-flow guidance) list and resolve READ-ONLY on the skills page —
// org admins can inspect the guidance the generation agents run with — but
// never mutate; their names stay reserved against user creation.
func TestSkillsComponent_PlatformSkillsReadOnlySurface(t *testing.T) {
	t.Parallel()
	h, _ := newHarness(t)

	// Listed, editable=false, all five present.
	resp := h.AsOrg("acme").Get(base)
	if resp.Code != 200 {
		t.Fatalf("list: want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	var obj struct {
		Skills []map[string]any `json:"skills"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &obj); err != nil {
		t.Fatalf("decode: %v", err)
	}
	platform := 0
	for _, s := range obj.Skills {
		if s["kind"] == "platform" {
			platform++
			if s["editable"] != false {
				t.Fatalf("platform skill must be read-only: %v", s)
			}
		}
	}
	if platform != 5 {
		t.Fatalf("want 5 platform skills in the list, got %d: %s", platform, resp.Body.String())
	}

	// Resolvable read-only…
	resp = h.AsOrg("acme").Get(base + "/high-level-architecture")
	if resp.Code != 200 {
		t.Fatalf("get platform: want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	if m := decodeObject(t, resp.Body.Bytes()); m["kind"] != "platform" || m["editable"] != false {
		t.Fatalf("platform get-one projection: %v", m)
	}

	// …but never mutable.
	if resp := h.AsOrg("acme").Delete(base + "/task-planning"); resp.Code != 403 {
		t.Fatalf("delete platform: want 403, got %d body=%s", resp.Code, resp.Body.String())
	}
	upd := `{"skillMd":` + jsonString(skillMD("task-planning", "")) + `}`
	if resp := h.AsOrg("acme").Put(base+"/task-planning", upd); resp.Code != 403 {
		t.Fatalf("update platform: want 403, got %d body=%s", resp.Code, resp.Body.String())
	}

	// The name stays reserved: creating a same-named custom skill conflicts.
	body := `{"name":"task-planning","skillMd":` + jsonString(skillMD("task-planning", "")) + `,"references":{}}`
	if resp := h.AsOrg("acme").Post(base, body); resp.Code != 409 {
		t.Fatalf("create over platform name: want 409, got %d body=%s", resp.Code, resp.Body.String())
	}
}

func TestSkillsComponent_GetOne_MatchesGolden(t *testing.T) {
	t.Parallel()
	h, _ := newHarness(t)

	resp := h.AsOrg("acme").Get(base + "/go")
	if resp.Code != 200 {
		t.Fatalf("get go: want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	assertObjectFieldSetMatchesGolden(t, resp.Body.String(), "get_org_skill.json")

	var got map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["name"] != "go" || got["kind"] != "org" || got["editable"] != false {
		t.Fatalf("get-one semantics drifted: %s", resp.Body.String())
	}
}

func TestSkillsComponent_GetOne_NotFound_404(t *testing.T) {
	t.Parallel()
	h, _ := newHarness(t)

	resp := h.AsOrg("acme").Get(base + "/no-such-skill-xyz")
	if resp.Code != 404 {
		t.Fatalf("missing skill: want 404, got %d body=%s", resp.Code, resp.Body.String())
	}
	if p := componenttest.DecodeProblem(t, resp.Body.String()); p.Detail != "skill not found" {
		t.Fatalf("404 detail: %q", p.Detail)
	}
}

func TestSkillsComponent_GetOne_InvalidSlug_400(t *testing.T) {
	t.Parallel()
	h, _ := newHarness(t)

	// Uppercase fails validate.Slug in the handler before the service is touched.
	resp := h.AsOrg("acme").Get(base + "/NotASlug")
	if resp.Code != 400 {
		t.Fatalf("invalid slug: want 400, got %d body=%s", resp.Code, resp.Body.String())
	}
	if p := componenttest.DecodeProblem(t, resp.Body.String()); !strings.HasPrefix(p.Detail, "name:") {
		t.Fatalf("400 detail: %q", p.Detail)
	}
}

func TestSkillsComponent_Updates_MatchesGolden(t *testing.T) {
	t.Parallel()
	h, store := newHarness(t)

	// Fresh: repo seeded at embed versions → no updates, but the op still emits
	// the golden's top-level shape with an empty (non-nil) updates array.
	resp := h.AsOrg("acme").Get(base + "/updates")
	if resp.Code != 200 {
		t.Fatalf("updates (fresh): want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	assertObjectFieldSetMatchesGolden(t, resp.Body.String(), "get_org_skills_updates.json")
	if m := decodeObject(t, resp.Body.Bytes()); m["count"] != float64(0) {
		t.Fatalf("fresh updates count: want 0, got %v", m["count"])
	}

	// Plant a stale built-in so the badge reports one update, then assert the
	// element field set against the golden's updates[0].
	store.DriftOrg("acme", "go", skillMD("go", ""))
	resp = h.AsOrg("acme").Get(base + "/updates")
	if resp.Code != 200 {
		t.Fatalf("updates (stale): want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	if m := decodeObject(t, resp.Body.Bytes()); m["count"] != float64(1) {
		t.Fatalf("stale updates count: want 1, got %v (body=%s)", m["count"], resp.Body.String())
	}
	gotElem := arrayFieldElementKeys(t, resp.Body.Bytes(), "updates")
	wantElem := arrayFieldElementKeys(t, readGolden(t, "get_org_skills_updates.json"), "updates")
	if strings.Join(gotElem, ",") != strings.Join(wantElem, ",") {
		t.Fatalf("updates[] element field set drifted:\n got %v\nwant %v", gotElem, wantElem)
	}
}

func TestSkillsComponent_Sync(t *testing.T) {
	t.Parallel()
	h, _ := newHarness(t)

	// First read seeds the repo; a subsequent sync is a no-op (0 updated).
	if resp := h.AsOrg("acme").Get(base); resp.Code != 200 {
		t.Fatalf("seed via list: %d", resp.Code)
	}
	resp := h.AsOrg("acme").Post(base+"/sync", "")
	if resp.Code != 200 {
		t.Fatalf("sync: want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	m := decodeObject(t, resp.Body.Bytes())
	if m["status"] != "synced" || m["updated"] != float64(0) {
		t.Fatalf("sync body: %v", m)
	}
}

// --- write surface -----------------------------------------------------------

func TestSkillsComponent_Create_HappyThenGet(t *testing.T) {
	t.Parallel()
	h, _ := newHarness(t)

	body := `{"name":"cool-skill","skillMd":` + jsonString(skillMD("cool-skill", "")) + `,"references":{}}`
	resp := h.AsOrg("acme").Post(base, body)
	if resp.Code != 201 {
		t.Fatalf("create: want 201, got %d body=%s", resp.Code, resp.Body.String())
	}
	// Same on-wire field set as the get-one golden (a created skill IS a skill),
	// with editable=true for a custom skill.
	assertObjectFieldSetMatchesGolden(t, resp.Body.String(), "get_org_skill.json")
	if m := decodeObject(t, resp.Body.Bytes()); m["kind"] != "custom" || m["editable"] != true {
		t.Fatalf("created projection: %v", m)
	}

	// Round-trip: the skill is now resolvable through the real read path.
	resp = h.AsOrg("acme").Get(base + "/cool-skill")
	if resp.Code != 200 {
		t.Fatalf("get created: want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	if m := decodeObject(t, resp.Body.Bytes()); m["name"] != "cool-skill" || m["kind"] != "custom" || m["editable"] != true {
		t.Fatalf("read-back projection: %v", m)
	}
}

func TestSkillsComponent_Create_WithoutReferences_201(t *testing.T) {
	t.Parallel()
	h, _ := newHarness(t)

	// The console's create dialog only sends {name, skillMd} — a skill without
	// references is legitimate, so the field must be OPTIONAL in the schema
	// (this pinned a live 422 found by the 2026-07-08 e2e).
	body := `{"name":"no-refs-skill","skillMd":` + jsonString(skillMD("no-refs-skill", "")) + `}`
	resp := h.AsOrg("acme").Post(base, body)
	if resp.Code != 201 {
		t.Fatalf("create without references: want 201, got %d body=%s", resp.Code, resp.Body.String())
	}
	if m := decodeObject(t, resp.Body.Bytes()); m["kind"] != "custom" || m["editable"] != true {
		t.Fatalf("created projection: %v", m)
	}
}

func TestSkillsComponent_Create_MissingBody_422(t *testing.T) {
	t.Parallel()
	h, _ := newHarness(t)

	// Required schema fields absent → Huma validation 422, never reaches the service.
	resp := h.AsOrg("acme").Post(base, `{}`)
	if resp.Code != 422 {
		t.Fatalf("empty body: want 422, got %d body=%s", resp.Code, resp.Body.String())
	}
	if p := componenttest.DecodeProblem(t, resp.Body.String()); len(p.Errors) == 0 {
		t.Fatalf("422 must carry validation errors: %s", resp.Body.String())
	}
}

func TestSkillsComponent_Create_InvalidSkillMD_400(t *testing.T) {
	t.Parallel()
	h, _ := newHarness(t)

	// Well-formed request body, but the SKILL.md has no frontmatter → the service
	// returns a SkillValidationError → mapSkillError 400.
	body := `{"name":"cool-skill","skillMd":"no frontmatter here","references":{}}`
	resp := h.AsOrg("acme").Post(base, body)
	if resp.Code != 400 {
		t.Fatalf("invalid skillMd: want 400, got %d body=%s", resp.Code, resp.Body.String())
	}
	if p := componenttest.DecodeProblem(t, resp.Body.String()); !strings.Contains(p.Detail, "skill validation failed") {
		t.Fatalf("400 detail: %q", p.Detail)
	}
}

func TestSkillsComponent_Create_CollisionWithBuiltin_409(t *testing.T) {
	t.Parallel()
	h, _ := newHarness(t)

	// A custom skill reusing a built-in name collides.
	body := `{"name":"go","skillMd":` + jsonString(skillMD("go", "")) + `,"references":{}}`
	resp := h.AsOrg("acme").Post(base, body)
	if resp.Code != 409 {
		t.Fatalf("collision: want 409, got %d body=%s", resp.Code, resp.Body.String())
	}
	if p := componenttest.DecodeProblem(t, resp.Body.String()); p.Detail != "skill name already in use" {
		t.Fatalf("409 detail: %q", p.Detail)
	}
}

func TestSkillsComponent_Update_Builtin_403(t *testing.T) {
	t.Parallel()
	h, _ := newHarness(t)

	body := `{"skillMd":` + jsonString(skillMD("go", "")) + `,"references":{}}`
	resp := h.AsOrg("acme").Put(base+"/go", body)
	if resp.Code != 403 {
		t.Fatalf("update builtin: want 403, got %d body=%s", resp.Code, resp.Body.String())
	}
	if p := componenttest.DecodeProblem(t, resp.Body.String()); p.Detail != "built-in skills are read-only" {
		t.Fatalf("403 detail: %q", p.Detail)
	}
}

func TestSkillsComponent_Update_Missing_404(t *testing.T) {
	t.Parallel()
	h, _ := newHarness(t)

	body := `{"skillMd":` + jsonString(skillMD("ghost-skill", "")) + `,"references":{}}`
	resp := h.AsOrg("acme").Put(base+"/ghost-skill", body)
	if resp.Code != 404 {
		t.Fatalf("update missing: want 404, got %d body=%s", resp.Code, resp.Body.String())
	}
	if p := componenttest.DecodeProblem(t, resp.Body.String()); p.Detail != "skill not found" {
		t.Fatalf("404 detail: %q", p.Detail)
	}
}

func TestSkillsComponent_Delete_Builtin_403(t *testing.T) {
	t.Parallel()
	h, _ := newHarness(t)

	resp := h.AsOrg("acme").Delete(base + "/go")
	if resp.Code != 403 {
		t.Fatalf("delete builtin: want 403, got %d body=%s", resp.Code, resp.Body.String())
	}
}

func TestSkillsComponent_Delete_CustomRoundTrip(t *testing.T) {
	t.Parallel()
	h, _ := newHarness(t)

	// Create a custom skill, then delete it, then confirm it is gone.
	create := `{"name":"delete-me","skillMd":` + jsonString(skillMD("delete-me", "")) + `,"references":{}}`
	if resp := h.AsOrg("acme").Post(base, create); resp.Code != 201 {
		t.Fatalf("seed create: %d %s", resp.Code, resp.Body.String())
	}
	resp := h.AsOrg("acme").Delete(base + "/delete-me")
	if resp.Code != 200 {
		t.Fatalf("delete custom: want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	if m := decodeObject(t, resp.Body.Bytes()); m["status"] != "deleted" || m["name"] != "delete-me" {
		t.Fatalf("delete body: %v", m)
	}
	if resp := h.AsOrg("acme").Get(base + "/delete-me"); resp.Code != 404 {
		t.Fatalf("after delete: want 404, got %d", resp.Code)
	}
}

func TestSkillsComponent_Delete_Missing_404(t *testing.T) {
	t.Parallel()
	h, _ := newHarness(t)

	resp := h.AsOrg("acme").Delete(base + "/ghost-skill")
	if resp.Code != 404 {
		t.Fatalf("delete missing: want 404, got %d body=%s", resp.Code, resp.Body.String())
	}
}

// --- import (multipart) ------------------------------------------------------

func TestSkillsComponent_Import_Happy(t *testing.T) {
	t.Parallel()
	h, _ := newHarness(t)

	tgz := makeTarGz(t, "cool-import", skillMD("cool-import", ""))
	resp := postSkillTarball(t, h, "acme", "cool-import.tgz", tgz)
	if resp.Code != 201 {
		t.Fatalf("import: want 201, got %d body=%s", resp.Code, resp.Body.String())
	}
	m := decodeObject(t, resp.Body.Bytes())
	if m["name"] != "cool-import" || m["kind"] != "imported" {
		t.Fatalf("import result: %v", m)
	}
	if _, ok := m["warnings"]; !ok {
		t.Fatalf("import result must carry warnings: %v", m)
	}

	// Round-trip: the imported skill is resolvable.
	if resp := h.AsOrg("acme").Get(base + "/cool-import"); resp.Code != 200 {
		t.Fatalf("get imported: want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
}

func TestSkillsComponent_Import_InvalidTarball_400(t *testing.T) {
	t.Parallel()
	h, _ := newHarness(t)

	resp := postSkillTarball(t, h, "acme", "bad.tgz", []byte("this is not a gzip stream"))
	if resp.Code != 400 {
		t.Fatalf("bad tarball: want 400, got %d body=%s", resp.Code, resp.Body.String())
	}
	if p := componenttest.DecodeProblem(t, resp.Body.String()); !strings.Contains(p.Detail, "skill validation failed") {
		t.Fatalf("400 detail: %q", p.Detail)
	}
}

// --- 503 (service not configured) + auth ------------------------------------

func TestSkillsComponent_Create_MutationUnconfigured_503(t *testing.T) {
	t.Parallel()
	// SkillSvc wired for reads, but no mutation service → create is 503.
	store := skills.NewComponentStore(t)
	h := componenttest.New(t, componenttest.Options{Deps: api.HumaDeps{SkillSvc: store.Svc}})

	body := `{"name":"cool-skill","skillMd":` + jsonString(skillMD("cool-skill", "")) + `,"references":{}}`
	resp := h.AsOrg("acme").Post(base, body)
	if resp.Code != 503 {
		t.Fatalf("create w/o mutation svc: want 503, got %d body=%s", resp.Code, resp.Body.String())
	}
}

func TestSkillsComponent_NoAuth_401(t *testing.T) {
	t.Parallel()
	h, _ := newHarness(t)

	// Tokenless org-scoped request → the gate's ENFORCE no-claims 401 (NOT the
	// verifier's rejection; the harness fakes the verifier away — see §3).
	resp := h.NoAuth().Get(base)
	if resp.Code != 401 {
		t.Fatalf("no-auth: want gate 401, got %d body=%s", resp.Code, resp.Body.String())
	}
	p := componenttest.DecodeProblem(t, resp.Body.String())
	if p.Status != 401 || len(p.Errors) == 0 || !strings.Contains(p.Errors[0].Message, "authentication required") {
		t.Fatalf("gate 401 problem shape: %s", resp.Body.String())
	}
}

// --- helpers -----------------------------------------------------------------

func decodeObject(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decode object: %v\n%s", err, raw)
	}
	return m
}

// jsonString quotes+escapes s as a JSON string literal for inline body building.
func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// makeTarGz builds a gzip tarball containing <topDir>/SKILL.md.
func makeTarGz(t *testing.T, topDir, skillMD string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	write := func(name string, dir bool, content string) {
		hdr := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(content))}
		if dir {
			hdr.Typeflag = tar.TypeDir
			hdr.Size = 0
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if !dir {
			if _, err := tw.Write([]byte(content)); err != nil {
				t.Fatal(err)
			}
		}
	}
	write(topDir+"/", true, "")
	write(topDir+"/SKILL.md", false, skillMD)
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// postSkillTarball drives the multipart import op through the real handler. The
// componenttest harness has no multipart request builder, so this hand-builds
// the request and stamps the harness's claims header directly (auth.go) — the
// same seam AsOrg uses, just for a non-JSON body.
func postSkillTarball(t *testing.T, h *componenttest.Harness, org, filename string, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, base+"/import", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	claims := auth.Claims{OuHandle: org, OuId: org + "-ouid", Subject: "componenttest-user"}
	raw, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set(claimsHeader, string(raw))

	rec := httptest.NewRecorder()
	h.Handler.ServeHTTP(rec, req)
	return rec
}
