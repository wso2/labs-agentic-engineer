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
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wso2/aep/ae-studio-tools/internal/gen"
	"github.com/wso2/aep/ae-studio-tools/internal/repo"
)

// handlerFixture serves the handler over a real engine whose owner/repo
// clone the matching test origin (any other clones a missing path).
type handlerFixture struct {
	h       Handler
	skills  testOrigin
	project testOrigin
	root    string
}

func newHandlerFixture(t *testing.T, ws func(*repo.Engine) Workspace) handlerFixture {
	t.Helper()
	skills := newOrigin(t, map[string]string{"skills/review/SKILL.md": skillMD("review", []string{"coding"}, "guidance body")})
	project := newOrigin(t, map[string]string{"README.md": "p"})
	root := filepath.Join(t.TempDir(), "studio-data")
	engine, _, err := repo.New(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	missing := "file://" + filepath.Join(t.TempDir(), "missing.git")
	cloneURL := func(owner, name string) string {
		for _, o := range []testOrigin{skills, project} {
			if strings.EqualFold(owner, o.owner) && strings.EqualFold(name, o.name) {
				return o.URL()
			}
		}
		return missing
	}
	var w Workspace = engine
	if ws != nil {
		w = ws(engine)
	}
	return handlerFixture{h: NewHandler(NewMirror(w), cloneURL, "ACME"), skills: skills, project: project, root: root}
}

func (f handlerFixture) request(skillsOwner, skillsRepo string) gen.MirrorSkillsRequestObject {
	return gen.MirrorSkillsRequestObject{
		Owner: f.project.owner, Repo: f.project.name,
		Body: &gen.MirrorSkillsJSONRequestBody{SkillsRepo: gen.SkillsRepo{Owner: skillsOwner, Repo: skillsRepo}, Pinned: []string{}},
	}
}

// visit writes resp through its generated Visit method.
func visit(t *testing.T, resp gen.MirrorSkillsResponseObject) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	if err := resp.VisitMirrorSkillsResponse(rec); err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body %q: %v", rec.Body.String(), err)
	}
	return rec.Code, body
}

func TestMirrorSkills_MirrorsThenIsANoop(t *testing.T) {
	f := newHandlerFixture(t, nil)
	ctx := context.Background()

	resp, err := f.h.MirrorSkills(ctx, f.request("acme", f.skills.name))
	if err != nil {
		t.Fatal(err)
	}
	ok, isOK := resp.(gen.MirrorSkills200JSONResponse)
	if !isOK || !ok.Changed || ok.CommitSha != f.project.HeadSHA(t) {
		t.Fatalf("first = %#v, tip %s", resp, f.project.HeadSHA(t))
	}
	if !strings.Contains(f.project.Tree(t)[".claude/skills/review/SKILL.md"], "guidance body") {
		t.Fatal("skill not mirrored")
	}
	resp, err = f.h.MirrorSkills(ctx, f.request("Acme", f.skills.name))
	if again, isOK := resp.(gen.MirrorSkills200JSONResponse); err != nil || !isOK || again.Changed || again.CommitSha != ok.CommitSha {
		t.Fatalf("second = %#v %v, want changed false at %s", resp, err, ok.CommitSha)
	}
}

func TestMirrorSkills_RefusesAForeignSkillsOwner(t *testing.T) {
	f := newHandlerFixture(t, nil)
	before := f.project.HeadSHA(t)
	for _, owner := range []string{"someone-else", "acme-2"} {
		resp, err := f.h.MirrorSkills(context.Background(), f.request(owner, f.skills.name))
		if err != nil {
			t.Fatal(err)
		}
		if code, body := visit(t, resp); code != http.StatusForbidden || body["code"] != "owner_not_allowed" {
			t.Fatalf("%s: %d %v", owner, code, body)
		}
	}
	if f.project.HeadSHA(t) != before {
		t.Fatal("the project moved")
	}
	if _, err := os.Stat(filepath.Join(f.root, "repos")); err == nil {
		entries, _ := os.ReadDir(filepath.Join(f.root, "repos"))
		if len(entries) > 0 {
			t.Fatalf("a refused request cloned: %v", entries)
		}
	}

	t.Run("no connected owner refuses every request", func(t *testing.T) {
		g := newHandlerFixture(t, nil)
		g.h = NewHandler(g.h.mirror, g.h.cloneURL, "")
		resp, _ := g.h.MirrorSkills(context.Background(), g.request("acme", g.skills.name))
		if code, body := visit(t, resp); code != http.StatusForbidden || body["code"] != "owner_not_allowed" {
			t.Fatalf("%d %v", code, body)
		}
	})
}

func TestMirrorSkills_ErrorMap(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name   string
		ws     func(*repo.Engine) Workspace
		req    func(f handlerFixture) gen.MirrorSkillsRequestObject
		status int
		code   string
	}{
		{name: "unreachable skills repo", req: func(f handlerFixture) gen.MirrorSkillsRequestObject {
			return f.request("acme", "no-such-repo")
		}, status: http.StatusBadGateway, code: "github_error"},
		{name: "skills default branch git refuses", req: func(f handlerFixture) gen.MirrorSkillsRequestObject {
			r := f.request("acme", f.skills.name)
			r.Body.SkillsRepo.DefaultBranch = "bad..name"
			return r
		}, status: http.StatusBadRequest, code: "validation_failed"},
		{name: "project default branch git refuses", req: func(f handlerFixture) gen.MirrorSkillsRequestObject {
			r := f.request("acme", f.skills.name)
			r.Params.DefaultBranch = "-x"
			return r
		}, status: http.StatusBadRequest, code: "validation_failed"},
		{name: "skills default branch missing", req: func(f handlerFixture) gen.MirrorSkillsRequestObject {
			r := f.request("acme", f.skills.name)
			r.Body.SkillsRepo.DefaultBranch = "release"
			return r
		}, status: http.StatusNotFound, code: "ref_not_found"},
		{name: "conflict after every recompute", ws: func(e *repo.Engine) Workspace { return alwaysConflicting{e} },
			req:    func(f handlerFixture) gen.MirrorSkillsRequestObject { return f.request("acme", f.skills.name) },
			status: http.StatusConflict, code: "conflict"},
		{name: "no body", req: func(f handlerFixture) gen.MirrorSkillsRequestObject {
			r := f.request("acme", f.skills.name)
			r.Body = nil
			return r
		}, status: http.StatusBadRequest, code: "validation_failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newHandlerFixture(t, tc.ws)
			before := f.project.HeadSHA(t)
			resp, err := f.h.MirrorSkills(ctx, tc.req(f))
			if err != nil {
				t.Fatal(err)
			}
			if code, body := visit(t, resp); code != tc.status || body["code"] != tc.code {
				t.Fatalf("%d %v, want %d %s", code, body, tc.status, tc.code)
			}
			if f.project.HeadSHA(t) != before {
				t.Fatal("the project moved")
			}
		})
	}

	t.Run("caller gone is the ctx error", func(t *testing.T) {
		f := newHandlerFixture(t, nil)
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		if _, err := f.h.MirrorSkills(cctx, f.request("acme", f.skills.name)); err == nil {
			t.Fatal("want the ctx error")
		}
	})
}

// A library read failure is logged under the skills repository, value-free.
func TestMirrorSkills_LibraryFailureNamesTheSkillsRepo(t *testing.T) {
	var buf strings.Builder
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	f := newHandlerFixture(t, nil)
	resp, err := f.h.MirrorSkills(context.Background(), f.request("acme", "No-Such-Repo"))
	if err != nil {
		t.Fatal(err)
	}
	if code, _ := visit(t, resp); code != http.StatusBadGateway {
		t.Fatalf("%d", code)
	}
	logs := buf.String()
	if !strings.Contains(logs, `"msg":"repo.git_failed"`) || !strings.Contains(logs, `"op":"mirror-skills"`) || !strings.Contains(logs, `"repo":"acme/no-such-repo"`) {
		t.Fatalf("logs: %s", logs)
	}
	if strings.Contains(logs, "missing.git") || strings.Contains(logs, "file://") {
		t.Fatalf("a log line carries the clone URL: %s", logs)
	}
}
