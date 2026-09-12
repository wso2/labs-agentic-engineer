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

package spec

// Reads: the bundle at HEAD (the live draft) and at a `v*` tag (an approved
// version), plus the version list — all served by walking the repo tree via the
// Git Data API.

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestListDesignFiles_AtHead(t *testing.T) {
	t.Parallel()
	r := newRig(t, map[string]string{
		"specs/design/design.cell":                 "# system\n",
		"specs/design/components/svc/design.md":    "svc\n",
		"specs/design/components/svc/openapi.yaml": "openapi: 3.0.0\n",
		"specs/design/components/svc/design.json":  validComponentDesignJSON("svc"),
		"specs/requirements/prd.md":                "wrong subtree\n",
	})
	got, err := r.svc.ListDesignFiles(context.Background(), r.org, r.proj)
	if err != nil {
		t.Fatalf("ListDesignFiles: %v", err)
	}
	want := map[string]string{
		"design.cell":                 "# system\n",
		"components/svc/design.md":    "svc\n",
		"components/svc/openapi.yaml": "openapi: 3.0.0\n",
		"components/svc/design.json":  validComponentDesignJSON("svc"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("bundle = %v, want %v (recursive, design-scoped, keys relative to specs/design/)", got, want)
	}
}

// A version is read back at the name the USER gave it. `m1` is not `v<N>`, and
// the read used to demand that shape — so a build could not read the design of
// the version it had just cut.
func TestGetDesignAtTag_PinsAVersionTheUserNamed(t *testing.T) {
	t.Parallel()
	r := newRig(t, map[string]string{
		"specs/requirements/prd.md":               "spec\n",
		"specs/design/design.cell":                "# m1\n",
		"specs/design/components/svc/design.json": validComponentDesignJSON("svc"),
	})
	ctx := context.Background()
	if _, err := r.svc.SaveSpec(ctx, r.org, r.proj, SaveRequest{Name: "m1"}); err != nil {
		t.Fatalf("save spec: %v", err)
	}
	r.seed(map[string]string{"specs/design/design.cell": "# later\n"}, "draft")

	at, err := r.svc.GetDesignAtTag(ctx, r.org, r.proj, "m1")
	if err != nil {
		t.Fatalf("GetDesignAtTag(m1): %v", err)
	}
	if at["design.cell"] != "# m1\n" {
		t.Errorf("at m1 = %q, want the pinned design, not HEAD", at["design.cell"])
	}
}

// An absent tag is "not found", not "malformed": the name was answerable, the
// version simply is not there.
func TestGetDesignAtTag_MissingTag(t *testing.T) {
	t.Parallel()
	r := newRig(t, map[string]string{"specs/requirements/prd.md": "x\n"})
	_, err := r.svc.GetDesignAtTag(context.Background(), r.org, r.proj, "never-cut")
	if !errors.Is(err, ErrArtifactNotFound) {
		t.Fatalf("err = %v, want ErrArtifactNotFound for an absent tag", err)
	}
}

// The read applies the SAME name rule the save does, so a string that could
// never have become a version cannot be asked for either. Without it the name
// reaches ref resolution as `tags/<name>`, and `../heads/main` walks out of the
// tag namespace onto a branch — reading a MOVING ref while the caller believes
// it pinned a frozen version.
func TestGetDesignAtTag_RefusesANameNoVersionCouldCarry(t *testing.T) {
	t.Parallel()
	r := newRig(t, map[string]string{"specs/requirements/prd.md": "x\n"})
	ctx := context.Background()
	for _, name := range []string{"../heads/main", "has space", "", ".hidden", "ends.lock", "a..b"} {
		if _, err := r.svc.GetDesignAtTag(ctx, r.org, r.proj, name); !errors.Is(err, ErrInvalidVersionTag) {
			t.Errorf("GetDesignAtTag(%q) err = %v, want ErrInvalidVersionTag", name, err)
		}
	}
}

// GetDesignAtCommit pins the exact commit — the publish flow's read of the
// commit its apply just created, never a ref resolution that could lag.
func TestGetDesignAtCommit_PinsExactCommit(t *testing.T) {
	t.Parallel()
	r := newRig(t, map[string]string{"specs/design/design.cell": "# early\n"})
	pinned := r.headSHA()
	r.seed(map[string]string{"specs/design/design.cell": "# later\n"}, "later edit")

	at, err := r.svc.GetDesignAtCommit(context.Background(), r.org, r.proj, pinned)
	if err != nil {
		t.Fatalf("GetDesignAtCommit: %v", err)
	}
	if at["design.cell"] != "# early\n" {
		t.Errorf("at %s = %q, want the pinned commit's content (not HEAD)", pinned, at["design.cell"])
	}
}

// Branch-tip bundle reads freshen the mirror on every read: a commit made
// directly on the ORIGIN (an external writer) is visible immediately — there
// is no cache tier to go stale.
func TestListDesignFiles_SeesOriginAdvanceImmediately(t *testing.T) {
	t.Parallel()
	r := newRig(t, map[string]string{"specs/design/design.cell": "v1\n"})
	ctx := context.Background()

	got, err := r.svc.ListDesignFiles(ctx, r.org, r.proj)
	if err != nil || got["design.cell"] != "v1\n" {
		t.Fatalf("first read = (%v, %v), want v1", got, err)
	}

	r.seed(map[string]string{"specs/design/design.cell": "v2 external\n"}, "external edit")

	got, err = r.svc.ListDesignFiles(ctx, r.org, r.proj)
	if err != nil {
		t.Fatalf("second read: %v", err)
	}
	if got["design.cell"] != "v2 external\n" {
		t.Errorf("second read = %q, want the origin's new commit (fetch freshness)", got["design.cell"])
	}
}

// The agent writes component design.json (no per-component design.md since
// PR #70) — the assembler must surface those components, or the cell diagram
// and task reconciliation see an empty design.
func TestAssembleDesign_ComponentFromDesignJSON(t *testing.T) {
	t.Parallel()
	design, err := AssembleDesign(map[string]string{
		"design.cell": "---\nsourceSpec: v1\n---\n# Overview\n",
		"components/task-api/design.json": `{"name":"task-api","type":"service","version":"1.0.0",` +
			`"language":"go","buildpack":"go","appPath":"task-api","entrypoint":"main.go",` +
			`"exposure":"internet","dependencies":[` +
			`{"kind":"component","name":"web-ui"},` +
			`{"kind":"external","name":"stripe"}` +
			`],"description":"Task CRUD API"}`,
		"components/task-api/openapi.yaml": "openapi: 3.0.0\n",
		"components/web-ui/design.json": `{"name":"web-ui","type":"web-application","version":"1.0.0",` +
			`"language":"ts","buildpack":"node","appPath":"web-ui","entrypoint":"index.ts",` +
			`"exposure":"internet","dependencies":[],"description":"UI"}`,
	})
	if err != nil {
		t.Fatalf("AssembleDesign: %v", err)
	}
	if len(design.Components) != 2 {
		t.Fatalf("components = %d, want 2 (design.json-only components must assemble)", len(design.Components))
	}
	c := design.Components[0]
	if c.Name != "task-api" || c.ComponentType != "service" || c.Language != "go" {
		t.Errorf("component = %+v, want task-api/service/go from design.json", c)
	}
	if c.OpenAPISpec != "openapi: 3.0.0\n" {
		t.Errorf("OpenAPISpec = %q, want sibling openapi.yaml content", c.OpenAPISpec)
	}
	if c.Description != "Task CRUD API" {
		t.Errorf("description = %q, want design.json description", c.Description)
	}
	// Unified dependencies: ComponentDependsOn() derives the sibling components.
	if sib := c.ComponentDependsOn(); len(sib) != 1 || sib[0] != "web-ui" {
		t.Errorf("ComponentDependsOn = %v, want [web-ui]", sib)
	}
	// The external dependency survives verbatim as a kind=external entry.
	var externals []string
	for _, d := range c.Dependencies {
		if d.Kind == "external" {
			externals = append(externals, d.Name)
		}
	}
	if len(externals) != 1 || externals[0] != "stripe" {
		t.Errorf("external deps = %v, want [stripe]", externals)
	}
}

// design.json is the SOLE authored component model since the dependency-
// management migration: a component directory with only a legacy design.md
// (no design.json) is skipped, and a design.json alongside a stray design.md is
// the authority (the per-component design.md frontmatter path was retired).
func TestAssembleDesign_DesignJSONOnly_LegacyMdSkipped(t *testing.T) {
	t.Parallel()
	design, err := AssembleDesign(map[string]string{
		"design.cell":                 "# o\n",
		"components/svc/design.md":    "---\ntype: web-app\nlanguage: python\n---\nlegacy body\n",
		"components/svc/design.json":  validComponentDesignJSON("svc"),
		"components/legacy/design.md": "---\ntype: service\nlanguage: java\n---\nold-style component\n",
	})
	if err != nil {
		t.Fatalf("AssembleDesign: %v", err)
	}
	// Only svc assembles (it has design.json); the legacy design.md-only dir is skipped.
	if len(design.Components) != 1 {
		t.Fatalf("components = %d, want 1 (design.json-only; legacy design.md-only dir skipped)", len(design.Components))
	}
	svc := design.Components[0]
	if svc.Name != "svc" || svc.ComponentType != "service" || svc.Language != "go" {
		t.Errorf("svc = %+v, want design.json values (svc/service/go), not legacy md (web-app/python)", svc)
	}
}

// A malformed design.json fails the assemble loudly (same strictness as broken
// design.md frontmatter) — never a silently missing component.
func TestAssembleDesign_MalformedDesignJSONErrors(t *testing.T) {
	t.Parallel()
	_, err := AssembleDesign(map[string]string{
		"design.cell":                "# o\n",
		"components/bad/design.json": "{not json",
	})
	if err == nil {
		t.Fatal("want error for malformed design.json, got nil")
	}
}
