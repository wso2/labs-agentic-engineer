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

// The version facts over REAL git: real annotated tags with real creation
// dates, real trees, a real deletion. The unit tests next door pin the rules
// over hand-built listings; this pins that the rules meet git — that a tag's
// date parses out of `for-each-ref`, that `Spec <name>` is what marks a
// version, and that a removed directory reads as a removed row rather than as
// nothing at all.

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
)

const ordersDesignJSON = `{
  "name": "orders-api",
  "type": "service",
  "dependencies": []
}`

// The same component, now declaring a platform resource. A resource owns no
// directory, so this is the only way one can be seen arriving.
const ordersDesignJSONWithResource = `{
  "name": "orders-api",
  "type": "service",
  "dependencies": [
    {
      "kind": "platform-resource",
      "name": "orders-cache",
      "resourceType": "redis"
    }
  ]
}`

// tagAt creates an annotated tag on the origin's tip with an explicit date, so
// ordering is decided by the clock the test sets rather than by the second the
// test happens to run in.
func tagAt(t *testing.T, r *rig, name, message, date string) {
	t.Helper()
	c := exec.Command("git", "--git-dir="+r.remote.Dir(),
		"tag", "-a", name, "-m", message, r.headSHA())
	c.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_SYSTEM="+os.DevNull,
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@aep.test",
		"GIT_COMMITTER_DATE="+date)
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("tag %s: %v\n%s", name, err, out)
	}
}

func factRows(t *testing.T, facts VersionFacts) map[string]VersionChange {
	t.Helper()
	out := make(map[string]VersionChange, len(facts.Changes))
	for _, c := range facts.Changes {
		out[c.Name] = c
	}
	return out
}

func TestBuildVersionFactsOverRealGit(t *testing.T) {
	ctx := context.Background()
	r := newRig(t, map[string]string{
		"README.md":                 "hello\n",
		"specs/requirements/prd.md": "# PRD\n\nfirst\n",
		"specs/design/components/orders-api/design.json":          ordersDesignJSON,
		"specs/design/dependencies/legacy-mailer/dependency.json": `{"name":"legacy-mailer"}`,
	})

	// --- a project with no version yet -------------------------------------
	facts, err := r.svc.BuildVersionFacts(ctx, r.org, r.proj)
	if err != nil {
		t.Fatalf("BuildVersionFacts: %v", err)
	}
	if facts.CurrentVersion != "" || facts.SuggestedVersion != "v1" || facts.SpecUnchanged {
		t.Fatalf("fresh project = %+v, want no current version, v1 suggested, not unchanged", facts)
	}
	if rows := factRows(t, facts); len(rows) != 2 ||
		rows["orders-api"].State != VersionChangeNew ||
		rows["legacy-mailer"].State != VersionChangeNew {
		t.Fatalf("first build rows = %+v, want the component and the dependency, both new", facts.Changes)
	}

	// --- the version is cut -------------------------------------------------
	tagAt(t, r, "v1", specTagSubject+"v1", "2026-01-01T10:00:00+00:00")

	facts, err = r.svc.BuildVersionFacts(ctx, r.org, r.proj)
	if err != nil {
		t.Fatalf("BuildVersionFacts after tag: %v", err)
	}
	if facts.CurrentVersion != "v1" || facts.SuggestedVersion != "v2" {
		t.Fatalf("after v1 = %+v, want current v1 and v2 suggested", facts)
	}
	if !facts.SpecUnchanged || len(facts.Changes) != 0 {
		t.Fatalf("after v1 = %+v, want the tree unchanged and no rows", facts)
	}

	// --- the spec moves: one edit, one addition, one deletion ---------------
	r.seed(map[string]string{
		"specs/requirements/prd.md":                      "# PRD\n\nfirst\nsecond\n",
		"specs/design/components/orders-api/design.json": ordersDesignJSONWithResource,
		"specs/design/components/reports-web/design.json": `{
  "name": "reports-web",
  "type": "web-application",
  "dependencies": []
}`,
		// Not part of the spec: it must not produce a row.
		"README.md": "hello again\n",
	}, "edit the spec")
	r.remote.Remove(t, "drop the mailer", "specs/design/dependencies/legacy-mailer/dependency.json")

	facts, err = r.svc.BuildVersionFacts(ctx, r.org, r.proj)
	if err != nil {
		t.Fatalf("BuildVersionFacts after the edit: %v", err)
	}
	if facts.SpecUnchanged {
		t.Fatalf("the spec tree moved but reads unchanged: %+v", facts)
	}
	rows := factRows(t, facts)
	want := map[string]string{
		"orders-api":    VersionChangeChanged,
		"reports-web":   VersionChangeNew,
		"legacy-mailer": VersionChangeRemoved,
		"orders-cache":  VersionChangeNew,
	}
	for name, state := range want {
		if got, listed := rows[name]; !listed || got.State != state {
			t.Errorf("%s = %+v (listed=%v), want %s", name, got, listed, state)
		}
	}
	if len(rows) != len(want) {
		t.Errorf("rows = %+v, want exactly %d — README.md is not part of the spec, and the edited PRD is not a row",
			facts.Changes, len(want))
	}
	if rows["orders-cache"].Kind != VersionChangeKindResource {
		t.Errorf("orders-cache kind = %q, want platform-resource — it is declared in a design, not a directory",
			rows["orders-cache"].Kind)
	}
	if rows["legacy-mailer"].Kind != VersionChangeKindExternal {
		t.Errorf("legacy-mailer kind = %q, want external", rows["legacy-mailer"].Kind)
	}
}

// The name carries no sequence, so the NEWEST version is the most recently
// created tag — even when an older tag's name sorts above it.
func TestBuildVersionFactsOrdersByCreationNotName(t *testing.T) {
	ctx := context.Background()
	r := newRig(t, map[string]string{"specs/requirements/prd.md": "# PRD\n\nfirst\n"})

	tagAt(t, r, "v9", specTagSubject+"v9", "2026-01-01T10:00:00+00:00")
	r.seed(map[string]string{"specs/requirements/prd.md": "# PRD\n\nsecond\n"}, "edit")
	tagAt(t, r, "payments-v2", specTagSubject+"payments-v2", "2026-02-01T10:00:00+00:00")

	facts, err := r.svc.BuildVersionFacts(ctx, r.org, r.proj)
	if err != nil {
		t.Fatalf("BuildVersionFacts: %v", err)
	}
	if facts.CurrentVersion != "payments-v2" {
		t.Errorf("current = %q, want payments-v2 — the newer tag, whatever it is called", facts.CurrentVersion)
	}
	// Two versions exist, so the third is suggested.
	if facts.SuggestedVersion != "v3" {
		t.Errorf("suggested = %q, want v3", facts.SuggestedVersion)
	}
	if !facts.SpecUnchanged {
		t.Errorf("the newest tag names HEAD's tree, so this is a rebuild: %+v", facts)
	}
}

// A tag this platform did not cut is not a version, however it is named.
func TestBuildVersionFactsIgnoresForeignTags(t *testing.T) {
	ctx := context.Background()
	r := newRig(t, map[string]string{"specs/requirements/prd.md": "# PRD\n\nfirst\n"})

	tagAt(t, r, "v1", specTagSubject+"v1", "2026-01-01T10:00:00+00:00")
	r.seed(map[string]string{"specs/requirements/prd.md": "# PRD\n\nsecond\n"}, "edit")
	// Two release tags somebody pushed, newer than the version — and one of them
	// is named exactly like a version, which is the case a name alone gets wrong.
	tagAt(t, r, "release-2026-02", "ship it", "2026-02-01T10:00:00+00:00")
	tagAt(t, r, "v2", "ship it", "2026-02-02T10:00:00+00:00")

	facts, err := r.svc.BuildVersionFacts(ctx, r.org, r.proj)
	if err != nil {
		t.Fatalf("BuildVersionFacts: %v", err)
	}
	if facts.CurrentVersion != "v1" {
		t.Errorf("current = %q, want v1 — a release tag is not a version", facts.CurrentVersion)
	}
	// One version exists, so counting offers v2 — which the release tag already
	// holds, so the suggestion steps past it.
	if facts.SuggestedVersion != "v3" {
		t.Errorf("suggested = %q, want v3 — a foreign tag is not counted, but its name is taken",
			facts.SuggestedVersion)
	}
	if facts.SpecUnchanged {
		t.Errorf("the spec moved after v1: %+v", facts)
	}
}

// The suggestion counts versions, and steps past a name already taken.
func TestBuildVersionFactsSuggestionStepsPastATakenName(t *testing.T) {
	ctx := context.Background()
	r := newRig(t, map[string]string{"specs/requirements/prd.md": "# PRD\n\nfirst\n"})

	tagAt(t, r, "v1", specTagSubject+"v1", "2026-01-01T10:00:00+00:00")
	r.seed(map[string]string{"specs/requirements/prd.md": "# PRD\n\nsecond\n"}, "edit")
	// The second version was named by hand, and it took the name counting would
	// have offered next.
	tagAt(t, r, "v3", specTagSubject+"v3", "2026-02-01T10:00:00+00:00")

	facts, err := r.svc.BuildVersionFacts(ctx, r.org, r.proj)
	if err != nil {
		t.Fatalf("BuildVersionFacts: %v", err)
	}
	if facts.SuggestedVersion != "v4" {
		t.Errorf("suggested = %q, want v4 — v3 is taken", facts.SuggestedVersion)
	}
	if !strings.HasPrefix(facts.CurrentVersion, "v3") {
		t.Errorf("current = %q, want v3", facts.CurrentVersion)
	}
}
