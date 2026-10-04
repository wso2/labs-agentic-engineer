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

package edge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/wso2/aep/aep-api/internal/organization/aestudio"
	"github.com/wso2/aep/aep-api/internal/platform/auth"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// fakeProjectRepos answers the ae-studio/ lookup from rows keyed
// "org/project" and records the org it was asked for.
type fakeProjectRepos struct {
	rows   map[string]aestudio.ProjectRepository
	err    error
	gotOrg []string
}

func (f *fakeProjectRepos) Lookup(_ context.Context, org, project string) (aestudio.ProjectRepository, error) {
	f.gotOrg = append(f.gotOrg, org)
	if f.err != nil {
		return aestudio.ProjectRepository{}, f.err
	}
	row, ok := f.rows[org+"/"+project]
	if !ok {
		return aestudio.ProjectRepository{}, aestudio.ErrProjectNotFound
	}
	return row, nil
}

// The ae-studio/ route group admits only an org's ae-studio-<org> client
// token, and the org is the one that client is recorded for (04 §2, 03 §1,
// Q-1). aep-api never accepts the org's publisher token (a coding Job holds
// it) or the AE-only client's token (no org claim; scenario 4.9): the AE-only
// pin lives in the tools pod's /internal/v1 gate, not here.
func TestInternalGate_AEStudio(t *testing.T) {
	stack := newInternalStack(t)
	greeter := aestudio.ProjectRepository{
		Owner: "acme-gh", Repo: "greeter", DefaultBranch: "main",
		CloneURL: "https://github.com/acme-gh/greeter.git",
	}
	repos := &fakeProjectRepos{rows: map[string]aestudio.ProjectRepository{"acme/greeter": greeter}}
	build := func(mut func(*InternalDeps)) http.Handler {
		deps := stack.deps
		deps.AEStudioRepositories = repos
		deps.SREHandoff = auth.NewSREHandoffVerifier("s3cr3t", "acme")
		if mut != nil {
			mut(&deps)
		}
		return NewHandler(AppParams{InternalDeps: deps})
	}
	on := build(nil)
	noVerifier := build(func(d *InternalDeps) { d.StudioClients = nil })
	noLookup := build(func(d *InternalDeps) { d.AEStudioRepositories = nil })
	failing := build(func(d *InternalDeps) { d.AEStudioRepositories = &fakeProjectRepos{err: errors.New("db down")} })

	token := func(aud, ouHandle string) string {
		return "Bearer " + stack.sign(auth.PublisherClaims{
			RegisteredClaims: jwt.RegisteredClaims{
				Issuer:    pubIssuer,
				Audience:  jwt.ClaimStrings{aud},
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			},
			OuHandle: ouHandle,
		})
	}
	studio := "Bearer " + stack.mintStudio("acme")
	const path = "/internal/v1/ae-studio/projects/greeter/repository"

	cases := []struct {
		name, path, bearer string
		header             map[string]string
		h                  http.Handler
		want               int
	}{
		{name: "ae-studio client token of the owning org", bearer: studio, want: 200},
		{name: "ae-studio client token, unknown project", path: "/internal/v1/ae-studio/projects/nope/repository", bearer: studio, want: 404},
		{name: "ae-studio client token of another org", bearer: "Bearer " + stack.mintStudio("evil"), want: 404},
		{name: "ae-studio client token, name breaks the slug pattern", path: "/internal/v1/ae-studio/projects/Not_A_Slug/repository", bearer: studio, want: 400},
		{name: "no bearer", want: 401},
		{name: "no bearer, name breaks the slug pattern", path: "/internal/v1/ae-studio/projects/Not_A_Slug/repository", want: 401},
		// Scenario 4.7: a user's JWT is never an internal credential.
		{name: "user JWT", bearer: token("aep-console", "acme"), want: 401},
		// Scenario 4.9 / carry 5: aep-api's own AE-only client (no org claim)
		// is refused, with or without an impersonation header.
		{name: "AE-only client token", bearer: token("ae-studio-internal-client", ""), want: 401},
		{name: "AE-only client token impersonating the org", bearer: token("ae-studio-internal-client", ""), header: map[string]string{"X-Impersonate-Org": "acme"}, want: 401},
		// Q-1: the org's publisher token (what its coding Jobs hold) never
		// opens ae-studio/.
		{name: "publisher token of the owning org", bearer: "Bearer " + stack.mint("acme"), want: 401},
		{name: "ae-studio audience without an org claim", bearer: token("ae-studio-acme", ""), want: 401},
		{name: "ae-studio audience naming another org", bearer: token("ae-studio-acme", "evil"), want: 401},
		{name: "ae-studio client of an org with none recorded", bearer: "Bearer " + stack.mintStudio(orgWithoutStudioClient), want: 401},
		{name: "SRE handoff bearer", bearer: "Bearer s3cr3t", want: 401},
		{name: "no ae-studio client verifier configured", h: noVerifier, bearer: studio, want: 401},
		{name: "no lookup configured", h: noLookup, bearer: studio, want: 503},
		{name: "lookup fails", h: failing, bearer: studio, want: 500},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, h := tc.path, tc.h
			if p == "" {
				p = path
			}
			if h == nil {
				h = on
			}
			req := httptest.NewRequest(http.MethodGet, p, nil)
			if tc.bearer != "" {
				req.Header.Set("Authorization", tc.bearer)
			}
			for k, v := range tc.header {
				req.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.want, rec.Body)
			}
			if tc.want != 200 && strings.Contains(rec.Body.String(), "-gh") {
				t.Fatalf("a refused request leaked the repository: %s", rec.Body)
			}
		})
	}
	for _, org := range repos.gotOrg {
		if org != "acme" && org != "evil" {
			t.Errorf("lookup got org %q; the org must come from the verified token", org)
		}
	}
}

// The 200 body is exactly the contract's AEStudioProjectRepository, and the
// org the lookup is asked for is the token's, never request input.
func TestInternalRoutes_AEStudioProjectRepository(t *testing.T) {
	stack := newInternalStack(t)
	repos := &fakeProjectRepos{rows: map[string]aestudio.ProjectRepository{"acme/greeter": {
		Owner: "acme-gh", Repo: "greeter", DefaultBranch: "main",
		CloneURL: "https://github.com/acme-gh/greeter.git",
	}}}
	deps := stack.deps
	deps.AEStudioRepositories = repos
	h := NewHandler(AppParams{InternalDeps: deps})

	req := httptest.NewRequest(http.MethodGet, "/internal/v1/ae-studio/projects/greeter/repository", nil)
	req.Header.Set("Authorization", "Bearer "+stack.mintStudio("acme"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body: %v\n%s", err, rec.Body)
	}
	want := map[string]string{
		"owner": "acme-gh", "repo": "greeter", "defaultBranch": "main",
		"cloneUrl": "https://github.com/acme-gh/greeter.git",
	}
	if len(got) != len(want) {
		t.Fatalf("body = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("body = %v, want %v", got, want)
		}
	}
	if len(repos.gotOrg) != 1 || repos.gotOrg[0] != "acme" {
		t.Fatalf("lookup orgs = %v, want [acme]", repos.gotOrg)
	}
}

// fakeCompleter records each call and answers one completion and one warning
// per call.
type fakeCompleter struct {
	orgs   []string
	writes [][]spec.WriteOp
}

func (f *fakeCompleter) complete(_ context.Context, org string, writes []spec.WriteOp) (map[string]spec.CompletedFile, []spec.Warning) {
	f.orgs = append(f.orgs, org)
	f.writes = append(f.writes, writes)
	return map[string]spec.CompletedFile{
			depStubPath: {Definition: `{"name":"payments"}`, Files: map[string]string{
				"specs/design/dependencies/payments/z.yaml":       "z",
				"specs/design/dependencies/payments/openapi.yaml": "openapi: 3.0.3\n",
			}},
		}, []spec.Warning{
			{Path: depStubPath, Code: spec.WarningRegistryCopied, Message: "copied"},
		}
}

const (
	depStubPath     = "specs/design/dependencies/payments/dependency.json"
	completionsPath = "/internal/v1/ae-studio/dependency-completions"
)

// completionsBody is a request for project greeter with one stub per path.
func completionsBody(paths ...string) string {
	return completionsBodyFor("greeter", paths...)
}

func completionsBodyFor(project string, paths ...string) string {
	writes := make([]map[string]string, 0, len(paths))
	for _, p := range paths {
		writes = append(writes, map[string]string{"path": p, "content": `{"name":"payments","resource":{"ref":"payments","name":"payments"}}`})
	}
	b, _ := json.Marshal(map[string]any{"project": project, "writes": writes})
	return string(b)
}

// aeStudioProjects owns greeter for acme and ledger for evil.
func aeStudioProjects() *fakeProjectRepos {
	return &fakeProjectRepos{rows: map[string]aestudio.ProjectRepository{
		"acme/greeter": {Owner: "acme-gh", Repo: "greeter", DefaultBranch: "main", CloneURL: "https://github.com/acme-gh/greeter.git"},
		"evil/ledger":  {Owner: "evil-gh", Repo: "ledger", DefaultBranch: "main", CloneURL: "https://github.com/evil-gh/ledger.git"},
	}}
}

// complete-ae-studio-dependencies rides the same gate as the repository
// lookup: only the org's ae-studio client token, the org is its recorded one.
// Every write must be a dependency definition path (400 path_invalid).
func TestInternalGate_AEStudioDependencyCompletions(t *testing.T) {
	stack := newInternalStack(t)
	completer := &fakeCompleter{}
	build := func(mut func(*InternalDeps)) http.Handler {
		deps := stack.deps
		deps.DependencyCompleter = completer.complete
		deps.AEStudioRepositories = aeStudioProjects()
		deps.SREHandoff = auth.NewSREHandoffVerifier("s3cr3t", "acme")
		if mut != nil {
			mut(&deps)
		}
		return NewHandler(AppParams{InternalDeps: deps})
	}
	on := build(nil)
	noCompleter := build(func(d *InternalDeps) { d.DependencyCompleter = nil })
	noLookup := build(func(d *InternalDeps) { d.AEStudioRepositories = nil })
	sixtyFive := make([]string, 65)
	for i := range sixtyFive {
		sixtyFive[i] = "specs/design/dependencies/d" + strconv.Itoa(i) + "/dependency.json"
	}
	token := func(aud, ouHandle string) string {
		return "Bearer " + stack.sign(auth.PublisherClaims{
			RegisteredClaims: jwt.RegisteredClaims{
				Issuer:    pubIssuer,
				Audience:  jwt.ClaimStrings{aud},
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			},
			OuHandle: ouHandle,
		})
	}
	studio := "Bearer " + stack.mintStudio("acme")

	cases := []struct {
		name, body, bearer, wantCode string
		h                            http.Handler
		want                         int
	}{
		{name: "ae-studio client token of the org", bearer: studio, want: 200},
		{name: "no bearer", want: 401},
		{name: "user JWT", bearer: token("aep-console", "acme"), want: 401},
		{name: "AE-only client token", bearer: token("ae-studio-internal-client", ""), want: 401},
		{name: "publisher token of the org", bearer: "Bearer " + stack.mint("acme"), want: 401},
		{name: "SRE handoff bearer", bearer: "Bearer s3cr3t", want: 401},
		{name: "write outside dependencies/", body: completionsBody("specs/design/components/api/design.json"), bearer: studio, want: 400, wantCode: codePathInvalid},
		{name: "a dependency's document, not its definition", body: completionsBody("specs/design/dependencies/payments/openapi.yaml"), bearer: studio, want: 400, wantCode: codePathInvalid},
		{name: "traversal", body: completionsBody("specs/design/dependencies/../dependency.json"), bearer: studio, want: 400, wantCode: codePathInvalid},
		{name: "same path twice", body: completionsBody(depStubPath, depStubPath), bearer: studio, want: 400, wantCode: codePathInvalid},
		{name: "no writes", body: `{"writes":[]}`, bearer: studio, want: 400},
		{name: "unknown field", body: `{"writes":[{"path":"` + depStubPath + `","content":"{}","baseSha":"x"}]}`, bearer: studio, want: 400},
		{name: "body over the 1 MiB cap", body: completionsBody(depStubPath)[:10] + strings.Repeat(" ", 1<<20), bearer: studio, want: 413},
		{name: "no completer configured", h: noCompleter, bearer: studio, want: 503},
		// The project gates the call (the same answer as the repository lookup).
		{name: "another org's project", body: completionsBodyFor("ledger", depStubPath), bearer: studio, want: 404},
		{name: "unknown project", body: completionsBodyFor("nope", depStubPath), bearer: studio, want: 404},
		{name: "project breaks the slug pattern", body: completionsBodyFor("Not_A_Slug", depStubPath), bearer: studio, want: 400},
		{name: "no project", body: `{"writes":[{"path":"` + depStubPath + `","content":"{}"}]}`, bearer: studio, want: 400},
		{name: "no lookup configured", h: noLookup, bearer: studio, want: 503},
		{name: "64 writes", body: completionsBody(sixtyFive[:64]...), bearer: studio, want: 200},
		{name: "65 writes", body: completionsBody(sixtyFive...), bearer: studio, want: 400},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, h := tc.body, tc.h
			if body == "" {
				body = completionsBody(depStubPath)
			}
			if h == nil {
				h = on
			}
			calls := len(completer.orgs)
			req := httptest.NewRequest(http.MethodPost, completionsPath, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			if tc.bearer != "" {
				req.Header.Set("Authorization", tc.bearer)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.want, rec.Body)
			}
			if tc.wantCode != "" {
				var e struct{ Code string }
				if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil || e.Code != tc.wantCode {
					t.Fatalf("error code = %q (%v), want %q: %s", e.Code, err, tc.wantCode, rec.Body)
				}
			}
			if tc.want != 200 && len(completer.orgs) != calls {
				t.Fatalf("a refused request reached the completer")
			}
			if tc.want != 200 && strings.Contains(rec.Body.String(), "-gh") {
				t.Fatalf("a refused request leaked a repository: %s", rec.Body)
			}
		})
	}
	for _, org := range completer.orgs {
		if org != "acme" {
			t.Errorf("completer got org %q; the org must come from the verified token", org)
		}
	}
}

// The 200 body is the contract's AEStudioDependencyCompletions, files in path
// order; the completer gets the writes as sent and the token's org.
func TestInternalRoutes_AEStudioDependencyCompletions(t *testing.T) {
	stack := newInternalStack(t)
	completer := &fakeCompleter{}
	deps := stack.deps
	deps.DependencyCompleter = completer.complete
	deps.AEStudioRepositories = aeStudioProjects()
	h := NewHandler(AppParams{InternalDeps: deps})

	req := httptest.NewRequest(http.MethodPost, completionsPath, strings.NewReader(completionsBody(depStubPath)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+stack.mintStudio("acme"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	want := `{"completed":[{"definition":"{\"name\":\"payments\"}","files":[` +
		`{"content":"openapi: 3.0.3\n","path":"specs/design/dependencies/payments/openapi.yaml"},` +
		`{"content":"z","path":"specs/design/dependencies/payments/z.yaml"}],` +
		`"path":"specs/design/dependencies/payments/dependency.json"}],` +
		`"warnings":[{"code":"registry-copied","message":"copied","path":"specs/design/dependencies/payments/dependency.json"}]}`
	var got, exp any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body: %v\n%s", err, rec.Body)
	}
	_ = json.Unmarshal([]byte(want), &exp)
	gotB, _ := json.Marshal(got)
	expB, _ := json.Marshal(exp)
	if string(gotB) != string(expB) {
		t.Fatalf("body =\n%s\nwant\n%s", gotB, expB)
	}
	if len(completer.orgs) != 1 || completer.orgs[0] != "acme" {
		t.Fatalf("completer orgs = %v, want [acme]", completer.orgs)
	}
	if w := completer.writes[0]; len(w) != 1 || w[0].Path != depStubPath || !strings.Contains(w[0].Content, `"ref":"payments"`) {
		t.Fatalf("completer writes = %+v", w)
	}
}

// The completions answer is bounded: a completion that would take the answer
// past its budget is left out (path order decides), and its success warning
// becomes the kind's "not completed" warning, so the pod lands that stub as
// written and still decodes every completion that fit.
func TestToIgenCompletions_BoundsTheAnswer(t *testing.T) {
	doc := strings.Repeat("x", 1000)
	stub := func(name string) string { return "specs/design/dependencies/" + name + "/dependency.json" }
	completed := map[string]spec.CompletedFile{
		stub("a"): {Definition: `{"name":"a"}`, Files: map[string]string{"specs/design/dependencies/a/openapi.yaml": doc}},
		stub("b"): {Definition: `{"name":"b"}`, Files: map[string]string{"specs/design/dependencies/b/openapi.yaml": doc}},
		stub("c"): {Definition: `{"name":"c"}`, Files: map[string]string{"specs/design/dependencies/c/openapi.yaml": doc}},
	}
	warnings := []spec.Warning{
		{Path: stub("a"), Code: spec.WarningRegistryCopied, Message: "copied"},
		{Path: stub("b"), Code: spec.WarningRegistryCopied, Message: "copied"},
		{Path: stub("c"), Code: spec.WarningProviderDocumentFetched, Message: "fetched"},
		{Path: stub("d"), Code: spec.WarningRegistryMiss, Message: "miss"},
	}

	out := toIgenCompletions(completed, warnings, 2500)

	var kept []string
	for _, c := range out.Completed {
		kept = append(kept, c.Path)
	}
	if strings.Join(kept, ",") != stub("a")+","+stub("b") {
		t.Fatalf("kept %v, want a and b", kept)
	}
	if b, _ := json.Marshal(out.Completed); len(b) > 2500 {
		t.Fatalf("completions encode to %d bytes, over the 2500 budget", len(b))
	}
	got := map[string]string{}
	for _, w := range out.Warnings {
		got[w.Path] = w.Code
	}
	want := map[string]string{
		stub("a"): spec.WarningRegistryCopied,
		stub("b"): spec.WarningRegistryCopied,
		stub("c"): spec.WarningProviderDocumentUnavailable,
		stub("d"): spec.WarningRegistryMiss,
	}
	if len(out.Warnings) != len(want) {
		t.Fatalf("warnings = %+v", out.Warnings)
	}
	for p, code := range want {
		if got[p] != code {
			t.Fatalf("warning on %s = %q, want %q (all: %+v)", p, got[p], code, out.Warnings)
		}
	}

	// A registry copy left out reads needs-input.
	out = toIgenCompletions(map[string]spec.CompletedFile{stub("a"): completed[stub("a")]}, warnings[:1], 100)
	if len(out.Completed) != 0 || len(out.Warnings) != 1 || out.Warnings[0].Code != spec.WarningRegistryUnreachable ||
		!strings.Contains(out.Warnings[0].Message, "needs-input") {
		t.Fatalf("over-budget registry copy = %+v", out)
	}
}

// The production budget stays inside what the pod reads of the answer
// (maxCompletionsBody, 32 MiB, ae-studio-tools/internal/files/completions.go).
func TestCompletionsAnswerBudget_InsideThePodsReadCap(t *testing.T) {
	if maxCompletionsAnswerBytes >= 32<<20 {
		t.Fatalf("budget %d is not inside the pod's 32 MiB read cap", maxCompletionsAnswerBytes)
	}
}

// fakeSkillsRepos answers the org skills lookup from rows keyed by org and
// records the org it was asked for.
type fakeSkillsRepos struct {
	rows   map[string]aestudio.ProjectRepository
	err    error
	gotOrg []string
}

func (f *fakeSkillsRepos) Lookup(_ context.Context, org string) (aestudio.ProjectRepository, error) {
	f.gotOrg = append(f.gotOrg, org)
	if f.err != nil {
		return aestudio.ProjectRepository{}, f.err
	}
	row, ok := f.rows[org]
	if !ok {
		return aestudio.ProjectRepository{}, aestudio.ErrSkillsRepositoryNotFound
	}
	return row, nil
}

// get-ae-studio-skills-repository rides the ae-studio/ gate: only the org's
// ae-studio client token, and the org is its recorded one. The 200 body is
// exactly AEStudioProjectRepository; an org without a skills repository is
// 404; a library that could not be reconciled is 503.
func TestInternalRoutes_AEStudioSkillsRepository(t *testing.T) {
	stack := newInternalStack(t)
	const path = "/internal/v1/ae-studio/skills/repository"
	skills := &fakeSkillsRepos{rows: map[string]aestudio.ProjectRepository{"acme": {
		Owner: "acme-gh", Repo: "org-skills", DefaultBranch: "main",
		CloneURL: "https://github.com/acme-gh/org-skills.git",
	}}}
	build := func(mut func(*InternalDeps)) http.Handler {
		deps := stack.deps
		deps.AEStudioSkills = skills
		if mut != nil {
			mut(&deps)
		}
		return NewHandler(AppParams{InternalDeps: deps})
	}
	on := build(nil)
	userJWT := "Bearer " + stack.sign(auth.PublisherClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    pubIssuer,
			Audience:  jwt.ClaimStrings{"aep-console"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
		OuHandle: "acme",
	})
	cases := []struct {
		name, bearer string
		h            http.Handler
		want         int
	}{
		{name: "ae-studio client token of the org", bearer: "Bearer " + stack.mintStudio("acme"), want: 200},
		{name: "org with no skills repository", bearer: "Bearer " + stack.mintStudio("evil"), want: 404},
		{name: "no bearer", want: 401},
		{name: "user JWT", bearer: userJWT, want: 401},
		{name: "publisher token of the org", bearer: "Bearer " + stack.mint("acme"), want: 401},
		{name: "no lookup configured", h: build(func(d *InternalDeps) { d.AEStudioSkills = nil }), bearer: "Bearer " + stack.mintStudio("acme"), want: 503},
		{name: "library not reconciled", h: build(func(d *InternalDeps) {
			d.AEStudioSkills = &fakeSkillsRepos{err: fmt.Errorf("%w: github down", aestudio.ErrSkillsUnavailable)}
		}), bearer: "Bearer " + stack.mintStudio("acme"), want: 503},
		{name: "lookup fails", h: build(func(d *InternalDeps) {
			d.AEStudioSkills = &fakeSkillsRepos{err: errors.New("db down")}
		}), bearer: "Bearer " + stack.mintStudio("acme"), want: 500},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := tc.h
			if h == nil {
				h = on
			}
			req := httptest.NewRequest(http.MethodGet, path, nil)
			if tc.bearer != "" {
				req.Header.Set("Authorization", tc.bearer)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.want, rec.Body)
			}
			if tc.want != 200 && strings.Contains(rec.Body.String(), "-gh") {
				t.Fatalf("a refused request leaked the repository: %s", rec.Body)
			}
			if tc.want != 200 {
				return
			}
			var got map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("body: %v\n%s", err, rec.Body)
			}
			want := map[string]string{
				"owner": "acme-gh", "repo": "org-skills", "defaultBranch": "main",
				"cloneUrl": "https://github.com/acme-gh/org-skills.git",
			}
			if fmt.Sprint(got) != fmt.Sprint(want) {
				t.Fatalf("body = %v, want %v", got, want)
			}
		})
	}
	for _, org := range skills.gotOrg {
		if org != "acme" && org != "evil" {
			t.Errorf("lookup got org %q; the org must come from the verified token", org)
		}
	}
}
