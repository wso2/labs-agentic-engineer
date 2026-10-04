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

package skills

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/wso2/aep/ae-studio-tools/internal/repo"
	"github.com/wso2/aep/ae-studio-tools/internal/repo/repotest"
)

// testOrigin is a file:// origin addressed as one GitHub repository.
type testOrigin struct {
	*repotest.Origin
	owner, name string
}

// Ref addresses the origin as its GitHub repository.
func (o testOrigin) Ref() repo.RepoRef {
	return repo.RepoRef{Owner: o.owner, Repo: o.name, CloneURL: o.URL(), DefaultBranch: repotest.Branch}
}

// Tree is every file at the origin's tip, path → content.
func (o testOrigin) Tree(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, p := range strings.Split(o.Git(t, "ls-tree", "-r", "--name-only", repotest.Branch), "\n") {
		if p != "" {
			out[p] = o.FileAt(t, repotest.Branch, p)
		}
	}
	return out
}

// originSeq names each origin its own GitHub repository (the engine keys a
// mirror by owner/repo).
var originSeq atomic.Int64

// newOrigin seeds an origin addressed as acme/repo-<n>.
func newOrigin(t *testing.T, files map[string]string) testOrigin {
	t.Helper()
	return testOrigin{Origin: repotest.NewOrigin(t, files), owner: "acme", name: fmt.Sprintf("repo-%d", originSeq.Add(1))}
}

// newEngine is a real engine over a fresh studio-data root.
func newEngine(t *testing.T, _ ...testOrigin) *repo.Engine {
	t.Helper()
	e, _, err := repo.New(filepath.Join(t.TempDir(), "studio-data"), nil)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	return e
}

// skillMD is a minimal valid SKILL.md (today's frontmatter: name,
// description, metadata.aep.audience).
func skillMD(name string, audience []string, body string) string {
	meta := ""
	if len(audience) > 0 {
		meta = "metadata:\n  aep:\n    audience: [" + strings.Join(audience, ", ") + "]\n"
	}
	return fmt.Sprintf("---\nname: %s\ndescription: d.\n%s---\n\n%s\n", name, meta, body)
}

func TestMirror_WritesEnabledCodingSkillsAndPrunes(t *testing.T) {
	skills, project := newOrigin(t, map[string]string{
		"skills/review/SKILL.md": skillMD("review", []string{"coding"}, "body"),
		"skills/chat/SKILL.md":   skillMD("chat", []string{"design"}, "body"),
		"skills-manifest.json":   `{"review":{"origin":"platform","baseHash":"x"},"chat":{"origin":"platform","baseHash":"y"}}`,
	}), newOrigin(t, map[string]string{".claude/skills/stale/SKILL.md": "old"})
	m := NewMirror(newEngine(t, skills, project))

	res, err := m.Mirror(context.Background(), project.Ref(), skills.Ref(), []string{"chat"})
	if err != nil || !res.Changed {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	tree := project.Tree(t)
	if _, ok := tree[".claude/skills/review/SKILL.md"]; !ok {
		t.Error("enabled coding skill missing")
	}
	if _, ok := tree[".claude/skills/chat/SKILL.md"]; !ok {
		t.Error("pinned design skill missing")
	}
	if _, ok := tree[".claude/skills/stale/SKILL.md"]; ok {
		t.Error("stale skill not pruned")
	}
	if res.CommitSHA != project.HeadSHA(t) {
		t.Errorf("commitSha %s, want the new tip %s", res.CommitSHA, project.HeadSHA(t))
	}
	again, err := m.Mirror(context.Background(), project.Ref(), skills.Ref(), []string{"chat"})
	if err != nil || again.Changed {
		t.Errorf("second mirror must be a no-op: %+v %v", again, err)
	}
	if again.CommitSHA != res.CommitSHA {
		t.Errorf("no-op commitSha %s, want the unchanged tip %s", again.CommitSHA, res.CommitSHA)
	}
}

func TestMirror_DisabledSkillIsPrunedUnlessPinned(t *testing.T) {
	skills, project := newOrigin(t, map[string]string{
		"skills/go/SKILL.md":            skillMD("go", []string{"coding"}, "go body"),
		"skills/go/references/style.md": "STYLE",
		"skills/py/SKILL.md":            skillMD("py", nil, "py body"),
		"skills-manifest.json":          `{"go":{"origin":"platform","baseHash":"x","disabled":true},"py":{"origin":"platform","baseHash":"y","disabled":true}}`,
	}), newOrigin(t, map[string]string{
		".claude/skills/go/SKILL.md": "an earlier copy",
		".claude/skills/py/SKILL.md": "an earlier copy",
	})
	m := NewMirror(newEngine(t))

	if _, err := m.Mirror(context.Background(), project.Ref(), skills.Ref(), []string{"go"}); err != nil {
		t.Fatal(err)
	}
	tree := project.Tree(t)
	if !strings.Contains(tree[".claude/skills/go/SKILL.md"], "go body") || tree[".claude/skills/go/references/style.md"] != "STYLE" {
		t.Errorf("a pinned disabled skill must land with its references (drift guard): %v", keys(tree))
	}
	if _, ok := tree[".claude/skills/py/SKILL.md"]; ok {
		t.Error("an unpinned disabled skill must be pruned")
	}
}

func TestMirror_HandEditRevertsAndOtherPathsStay(t *testing.T) {
	skills, project := newOrigin(t, map[string]string{
		"skills/review/SKILL.md": skillMD("review", []string{"coding"}, "guidance body"),
	}), newOrigin(t, map[string]string{
		".claude/skills/review/SKILL.md": "HAND EDITED",
		".claude/settings.json":          "{}",
		".claude/skillsets/x.md":         "not under .claude/skills/",
		"specs/requirements.md":          "reqs",
	})
	m := NewMirror(newEngine(t))

	res, err := m.Mirror(context.Background(), project.Ref(), skills.Ref(), nil)
	if err != nil || !res.Changed {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	tree := project.Tree(t)
	if !strings.Contains(tree[".claude/skills/review/SKILL.md"], "guidance body") {
		t.Errorf("hand edit survived: %q", tree[".claude/skills/review/SKILL.md"])
	}
	for _, p := range []string{".claude/settings.json", ".claude/skillsets/x.md", "specs/requirements.md"} {
		if _, ok := tree[p]; !ok {
			t.Errorf("%s outside .claude/skills/ was touched", p)
		}
	}
	if msg := project.Git(t, "log", "-1", "--format=%s", repotest.Branch); msg != syncMessage {
		t.Errorf("commit message %q", msg)
	}
}

func TestMirror_EmptyLibraryPrunesEverything(t *testing.T) {
	skills, project := newOrigin(t, map[string]string{"README.md": "no skills yet"}),
		newOrigin(t, map[string]string{".claude/skills/old/SKILL.md": "old", "README.md": "p"})
	m := NewMirror(newEngine(t))

	if _, err := m.Mirror(context.Background(), project.Ref(), skills.Ref(), nil); err != nil {
		t.Fatal(err)
	}
	if tree := project.Tree(t); len(tree) != 1 || tree["README.md"] != "p" {
		t.Errorf("tree %v, want only README.md", keys(tree))
	}
}

// A failing library read aborts with no writes and no deletes: pruning on a
// degraded read would delete a build's guidance because GitHub blipped.
func TestMirror_FailingSkillsReadWritesNothing(t *testing.T) {
	_, project := newOrigin(t, nil), newOrigin(t, map[string]string{".claude/skills/keep/SKILL.md": "keep"})
	before := project.HeadSHA(t)
	m := NewMirror(newEngine(t))
	gone := repo.RepoRef{Owner: "acme", Repo: "org-skills", CloneURL: "file://" + filepath.Join(t.TempDir(), "missing.git"), DefaultBranch: "main"}

	if _, err := m.Mirror(context.Background(), project.Ref(), gone, nil); err == nil {
		t.Fatal("want the skills read's error")
	}
	if project.HeadSHA(t) != before {
		t.Fatal("the project repo moved after a failed library read")
	}
}

// pushingWorkspace pushes a hand edit to the project's origin right before
// the first Commit, so that commit's baseSha no longer holds.
type pushingWorkspace struct {
	*repo.Engine
	project testOrigin
	t       *testing.T
	commits int
}

func (w *pushingWorkspace) Commit(ctx context.Context, ref repo.RepoRef, writes []repo.CommitWrite, deletes []repo.CommitDelete, message string, author, committer *repo.GitIdentity) (repo.CommitResult, []repo.Conflict, error) {
	w.commits++
	if w.commits == 1 {
		w.project.Commit(w.t, map[string]string{".claude/skills/review/SKILL.md": "RACING EDIT"}, "race")
	}
	return w.Engine.Commit(ctx, ref, writes, deletes, message, author, committer)
}

func TestMirror_RecomputesOnAConflict(t *testing.T) {
	skills, project := newOrigin(t, map[string]string{
		"skills/review/SKILL.md": skillMD("review", []string{"coding"}, "guidance body"),
	}), newOrigin(t, map[string]string{".claude/skills/review/SKILL.md": "old"})
	ws := &pushingWorkspace{Engine: newEngine(t), project: project, t: t}

	res, err := NewMirror(ws).Mirror(context.Background(), project.Ref(), skills.Ref(), nil)
	if err != nil || !res.Changed {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if ws.commits != 2 {
		t.Errorf("commits = %d, want a recompute after the conflict", ws.commits)
	}
	if got := project.Tree(t)[".claude/skills/review/SKILL.md"]; !strings.Contains(got, "guidance body") {
		t.Errorf("SKILL.md = %q", got)
	}
}

// alwaysConflicting answers every Commit with a conflict.
type alwaysConflicting struct{ *repo.Engine }

func (alwaysConflicting) Commit(context.Context, repo.RepoRef, []repo.CommitWrite, []repo.CommitDelete, string, *repo.GitIdentity, *repo.GitIdentity) (repo.CommitResult, []repo.Conflict, error) {
	return repo.CommitResult{}, []repo.Conflict{{Path: ".claude/skills/x/SKILL.md"}}, repo.ErrCommitConflict
}

func TestMirror_GivesUpAfterRepeatedConflicts(t *testing.T) {
	skills, project := newOrigin(t, map[string]string{
		"skills/review/SKILL.md": skillMD("review", []string{"coding"}, "b"),
	}), newOrigin(t, nil)
	_, err := NewMirror(alwaysConflicting{newEngine(t)}).Mirror(context.Background(), project.Ref(), skills.Ref(), nil)
	if !errors.Is(err, repo.ErrCommitConflict) {
		t.Fatalf("err = %v, want ErrCommitConflict", err)
	}
}

type fixedIdentity struct{}

func (fixedIdentity) Identity(context.Context) (string, string, error) {
	return "Gitpat User", "gitpat@example.com", nil
}

func TestMirror_AuthorsAsTheGitpatUser(t *testing.T) {
	skills, project := newOrigin(t, map[string]string{
		"skills/review/SKILL.md": skillMD("review", []string{"coding"}, "b"),
	}), newOrigin(t, nil)
	if _, err := NewMirror(newEngine(t), WithAuthor(fixedIdentity{})).Mirror(context.Background(), project.Ref(), skills.Ref(), nil); err != nil {
		t.Fatal(err)
	}
	if got := project.Git(t, "log", "-1", "--format=%an <%ae>|%cn <%ce>", repotest.Branch); got != "Gitpat User <gitpat@example.com>|Gitpat User <gitpat@example.com>" {
		t.Errorf("author|committer = %q", got)
	}
}

// ---- desiredMirror (ported from aep-api's skill_mirror_test.go) ----------

func TestDesiredMirror(t *testing.T) {
	cases := []struct {
		name   string
		lib    []Skill
		pinned map[string]bool
		wantMD map[string]string
		absent []string
	}{
		{name: "coding audience + enabled is included",
			lib:    []Skill{{Name: "a", Enabled: true, Audience: []string{AudienceCoding}, SkillMD: "MD-A"}},
			wantMD: map[string]string{"a": "MD-A"}},
		{name: "design-only is excluded",
			lib:    []Skill{{Name: "b", Enabled: true, Audience: []string{AudienceDesign}, SkillMD: "MD-B"}},
			absent: []string{"b"}},
		{name: "disabled is excluded",
			lib:    []Skill{{Name: "c", Enabled: false, Audience: []string{AudienceCoding}, SkillMD: "MD-C"}},
			absent: []string{"c"}},
		{name: "disabled BUT pinned is included (drift guard)",
			lib:    []Skill{{Name: "d", Enabled: false, Audience: []string{AudienceCoding}, SkillMD: "MD-D"}},
			pinned: map[string]bool{"d": true}, wantMD: map[string]string{"d": "MD-D"}},
		{name: "design-only AND disabled BUT pinned is still included",
			lib:    []Skill{{Name: "d2", Enabled: false, Audience: []string{AudienceDesign}, SkillMD: "MD-D2"}},
			pinned: map[string]bool{"d2": true}, wantMD: map[string]string{"d2": "MD-D2"}},
		{name: "unmarked audience (permissive default) is included",
			lib:    []Skill{{Name: "e", Enabled: true, Audience: nil, SkillMD: "MD-E"}},
			wantMD: map[string]string{"e": "MD-E"}},
		{name: "not pinned and not coding: absent",
			lib:    []Skill{{Name: "f", Enabled: true, Audience: []string{AudienceDesign}, SkillMD: "MD-F"}},
			pinned: map[string]bool{"unrelated": true}, absent: []string{"f"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := desiredMirror(tc.lib, tc.pinned)
			for name, want := range tc.wantMD {
				if string(got[".claude/skills/"+name+"/SKILL.md"]) != want {
					t.Fatalf("SKILL.md of %s = %q, want %q (keys %v)", name, got[".claude/skills/"+name+"/SKILL.md"], want, keys(got))
				}
			}
			for _, name := range tc.absent {
				if _, ok := got[".claude/skills/"+name+"/SKILL.md"]; ok {
					t.Fatalf("%s must be absent (keys %v)", name, keys(got))
				}
			}
		})
	}
}

func TestDesiredMirror_ReferencesCarriedAtRightPaths(t *testing.T) {
	got := desiredMirror([]Skill{{
		Name: "with-refs", Enabled: true, Audience: []string{AudienceCoding}, SkillMD: "MD-BODY",
		References: map[string]string{"references/guide.md": "GUIDE", "scripts/setup.sh": "SETUP"},
	}, {
		Name: "excluded", Enabled: false, Audience: []string{AudienceCoding}, SkillMD: "MD",
		References: map[string]string{"references/x.md": "X"},
	}}, nil)
	want := map[string]string{
		".claude/skills/with-refs/SKILL.md":            "MD-BODY",
		".claude/skills/with-refs/references/guide.md": "GUIDE",
		".claude/skills/with-refs/scripts/setup.sh":    "SETUP",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want exactly %v", keys(got), keys(want))
	}
	for k, v := range want {
		if string(got[k]) != v {
			t.Fatalf("%s = %q, want %q", k, got[k], v)
		}
	}
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
