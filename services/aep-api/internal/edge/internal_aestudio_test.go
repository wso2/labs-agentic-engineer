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

// The ae-studio/ route group admits only an org's publisher client token, and
// the org is that token's ouHandle (04 §2, 03 §1). aep-api never accepts the
// AE-only client's token (no org claim; scenario 4.9) or the org's ae-studio-<org>
// client: the AE-only pin lives in the tools pod's /internal/v1 gate, not here.
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
	noVerifier := build(func(d *InternalDeps) { d.PublisherTokens = nil })
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
	publisher := "Bearer " + stack.mint("acme")
	const path = "/internal/v1/ae-studio/projects/greeter/repository"

	cases := []struct {
		name, path, bearer string
		header             map[string]string
		h                  http.Handler
		want               int
	}{
		{name: "publisher token of the owning org", bearer: publisher, want: 200},
		{name: "publisher token, unknown project", path: "/internal/v1/ae-studio/projects/nope/repository", bearer: publisher, want: 404},
		{name: "publisher token of another org", bearer: "Bearer " + stack.mint("evil"), want: 404},
		{name: "publisher token, name breaks the slug pattern", path: "/internal/v1/ae-studio/projects/Not_A_Slug/repository", bearer: publisher, want: 400},
		{name: "no bearer", want: 401},
		{name: "no bearer, name breaks the slug pattern", path: "/internal/v1/ae-studio/projects/Not_A_Slug/repository", want: 401},
		// Scenario 4.7: a user's JWT is never an internal credential.
		{name: "user JWT", bearer: token("aep-console", "acme"), want: 401},
		// Scenario 4.9 / carry 5: aep-api's own AE-only client (no org claim)
		// is refused, with or without an impersonation header.
		{name: "AE-only client token", bearer: token("ae-studio-internal-client", ""), want: 401},
		{name: "AE-only client token impersonating the org", bearer: token("ae-studio-internal-client", ""), header: map[string]string{"X-Impersonate-Org": "acme"}, want: 401},
		{name: "ae-studio-<org> client token", bearer: token("ae-studio-acme", "acme"), want: 401},
		{name: "publisher audience without an org claim", bearer: token(pubAudPrefix+"acme", ""), want: 401},
		{name: "publisher audience naming another org", bearer: token(pubAudPrefix+"acme", "evil"), want: 401},
		{name: "SRE handoff bearer", bearer: "Bearer s3cr3t", want: 401},
		{name: "no publisher verifier configured", h: noVerifier, bearer: publisher, want: 401},
		{name: "no lookup configured", h: noLookup, bearer: publisher, want: 503},
		{name: "lookup fails", h: failing, bearer: publisher, want: 500},
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
	req.Header.Set("Authorization", "Bearer "+stack.mint("acme"))
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
// lookup: only the org's publisher client token, the org is its ouHandle.
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
	publisher := "Bearer " + stack.mint("acme")

	cases := []struct {
		name, body, bearer, wantCode string
		h                            http.Handler
		want                         int
	}{
		{name: "publisher token of the org", bearer: publisher, want: 200},
		{name: "no bearer", want: 401},
		{name: "user JWT", bearer: token("aep-console", "acme"), want: 401},
		{name: "AE-only client token", bearer: token("ae-studio-internal-client", ""), want: 401},
		{name: "ae-studio-<org> client token", bearer: token("ae-studio-acme", "acme"), want: 401},
		{name: "SRE handoff bearer", bearer: "Bearer s3cr3t", want: 401},
		{name: "write outside dependencies/", body: completionsBody("specs/design/components/api/design.json"), bearer: publisher, want: 400, wantCode: codePathInvalid},
		{name: "a dependency's document, not its definition", body: completionsBody("specs/design/dependencies/payments/openapi.yaml"), bearer: publisher, want: 400, wantCode: codePathInvalid},
		{name: "traversal", body: completionsBody("specs/design/dependencies/../dependency.json"), bearer: publisher, want: 400, wantCode: codePathInvalid},
		{name: "same path twice", body: completionsBody(depStubPath, depStubPath), bearer: publisher, want: 400, wantCode: codePathInvalid},
		{name: "no writes", body: `{"writes":[]}`, bearer: publisher, want: 400},
		{name: "unknown field", body: `{"writes":[{"path":"` + depStubPath + `","content":"{}","baseSha":"x"}]}`, bearer: publisher, want: 400},
		{name: "body over the 1 MiB cap", body: completionsBody(depStubPath)[:10] + strings.Repeat(" ", 1<<20), bearer: publisher, want: 413},
		{name: "no completer configured", h: noCompleter, bearer: publisher, want: 503},
		// The project gates the call (the same answer as the repository lookup).
		{name: "another org's project", body: completionsBodyFor("ledger", depStubPath), bearer: publisher, want: 404},
		{name: "unknown project", body: completionsBodyFor("nope", depStubPath), bearer: publisher, want: 404},
		{name: "project breaks the slug pattern", body: completionsBodyFor("Not_A_Slug", depStubPath), bearer: publisher, want: 400},
		{name: "no project", body: `{"writes":[{"path":"` + depStubPath + `","content":"{}"}]}`, bearer: publisher, want: 400},
		{name: "no lookup configured", h: noLookup, bearer: publisher, want: 503},
		{name: "64 writes", body: completionsBody(sixtyFive[:64]...), bearer: publisher, want: 200},
		{name: "65 writes", body: completionsBody(sixtyFive...), bearer: publisher, want: 400},
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
	req.Header.Set("Authorization", "Bearer "+stack.mint("acme"))
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
