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

package files_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/wso2/aep/ae-studio-tools/internal/files"
	"github.com/wso2/aep/ae-studio-tools/internal/github"
	"github.com/wso2/aep/ae-studio-tools/internal/projects"
	"github.com/wso2/aep/ae-studio-tools/internal/projects/projectstest"
	"github.com/wso2/aep/ae-studio-tools/internal/repo"
	"github.com/wso2/aep/ae-studio-tools/internal/repo/repotest"
)

// applyRig is an Applier over a real file:// origin, plus the Reader that
// shares its engine (the Room seeds from Bundle and applies with its shas).
type applyRig struct {
	applier files.Applier
	reader  files.Reader
}

func (a *applyRig) Apply(ctx context.Context, project string, req files.ApplyRequest) (*files.ApplyResult, []files.Conflict, error) {
	return a.applier.Apply(ctx, project, req)
}

type applyOpt func(t *testing.T, a *files.Applier, origin *repotest.Origin)

func withCompleter(c files.Completer) applyOpt {
	return func(_ *testing.T, a *files.Applier, _ *repotest.Origin) { a.Completer = c }
}

// withIdentity makes the gitpat user's GET /user answer login and email
// (name unset), through the real github.CommitAuthor and its fallbacks.
func withIdentity(login, email string) applyOpt {
	return func(_ *testing.T, a *files.Applier, _ *repotest.Origin) {
		a.Identity = github.NewCommitAuthor(fakeUsers{user: github.User{Login: login, ID: 1, Email: email}})
	}
}

// withFailingIdentity makes every GET /user fail.
func withFailingIdentity() applyOpt {
	return func(_ *testing.T, a *files.Applier, _ *repotest.Origin) {
		a.Identity = github.NewCommitAuthor(fakeUsers{err: &github.HTTPStatusError{StatusCode: 502}})
	}
}

// withENOSPCOnPush makes origin refuse every push with the strerror text git
// relays for a full disk, which the engine classifies as ENOSPC.
func withENOSPCOnPush() applyOpt {
	return func(t *testing.T, _ *files.Applier, origin *repotest.Origin) {
		hook := filepath.Join(origin.Dir(), "hooks", "pre-receive")
		if err := os.MkdirAll(filepath.Dir(hook), 0o755); err != nil {
			t.Fatal(err)
		}
		script := "#!/bin/sh\necho 'fatal: write error: No space left on device' >&2\nexit 1\n"
		if err := os.WriteFile(hook, []byte(script), 0o755); err != nil { //nolint:gosec // a test hook must be executable
			t.Fatal(err)
		}
	}
}

func newApplier(t *testing.T, seed map[string]string, opts ...applyOpt) (*applyRig, *repotest.Origin) {
	t.Helper()
	origin := repotest.NewOrigin(t, seed)
	engine, _, err := repo.New(filepath.Join(t.TempDir(), "studio-data"), nil)
	if err != nil {
		t.Fatal(err)
	}
	fake := projectstest.NewFake(map[string]projects.Repository{"greeter": {
		Owner: "Acme", Repo: "Greeter-App", DefaultBranch: repotest.Branch, CloneURL: origin.URL(),
	}})
	reader := files.Reader{Engine: engine, Projects: fake, Org: testOrg}
	a := files.Applier{Reader: reader, Completer: &fakeCompleter{}}
	withIdentity("aep-bot", "")(t, &a, origin)
	for _, o := range opts {
		o(t, &a, origin)
	}
	return &applyRig{applier: a, reader: reader}, origin
}

// fakeUsers is the gitpat user's GET /user answer.
type fakeUsers struct {
	user github.User
	err  error
}

func (f fakeUsers) User(context.Context) (github.User, error) { return f.user, f.err }

// fakeCompleter completes the stubs it knows and records every call's paths.
type fakeCompleter struct {
	mu      sync.Mutex
	answers map[string]files.Completed
	calls   [][]string
}

func newFakeCompleter(answers map[string]files.Completed) *fakeCompleter {
	return &fakeCompleter{answers: answers}
}

func (f *fakeCompleter) Complete(_ context.Context, project string, writes []files.WriteOp) (map[string]files.Completed, []files.Warning, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	paths := make([]string, 0, len(writes))
	out := map[string]files.Completed{}
	for _, w := range writes {
		paths = append(paths, w.Path)
		if c, ok := f.answers[w.Path]; ok {
			out[w.Path] = c
		}
	}
	f.calls = append(f.calls, paths)
	return out, nil, nil
}

func (f *fakeCompleter) Calls() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

type failingCompleter struct{}

func (failingCompleter) Complete(context.Context, string, []files.WriteOp) (map[string]files.Completed, []files.Warning, error) {
	return nil, nil, projects.ErrUnavailable
}

func shaOf(b *files.Bundle, path string) string {
	for _, f := range b.Files {
		if f.Path == path {
			return f.SHA
		}
	}
	return ""
}

func conflictPaths(cs []files.Conflict) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.Path)
	}
	sort.Strings(out)
	return out
}

func warningCodes(ws []files.Warning) []string {
	out := make([]string, 0, len(ws))
	for _, w := range ws {
		out = append(out, w.Code)
	}
	return out
}

func changedPathsIn(t *testing.T, origin *repotest.Origin, commit string) []string {
	t.Helper()
	out := origin.Git(t, "diff-tree", "--no-commit-id", "--name-only", "-r", commit)
	paths := strings.Fields(out)
	sort.Strings(paths)
	return paths
}

func authorOf(t *testing.T, origin *repotest.Origin, commit string) string {
	t.Helper()
	return origin.Git(t, "log", "-1", "--format=%an <%ae>", commit)
}

func sameStrings(got, want []string) bool {
	g, w := slices.Clone(got), slices.Clone(want)
	sort.Strings(g)
	sort.Strings(w)
	return slices.Equal(g, w)
}

const paymentsStub = `{"name":"payments","resource":{"ref":"payments","name":"payments"}}`

// ---- Review Focus 1 + the brief's cases ---------------------------------

// The Room seeds from a bundle and applies with its blob shas. A commit made
// outside the Room after the seed must surface as a conflict naming exactly
// the externally changed path, and fresh shas must then succeed.
func TestApply_ConflictAfterExternalCommit(t *testing.T) {
	a, origin := newApplier(t, map[string]string{"specs/requirements/prd.md": "v1", "specs/design/x.md": "x"})
	b, err := a.reader.Bundle(ctx, "greeter", "specs/", "")
	if err != nil {
		t.Fatal(err)
	}
	origin.Commit(t, map[string]string{"specs/design/x.md": "changed outside the Room"}, "external")

	_, conflicts, err := a.Apply(ctx, "greeter", files.ApplyRequest{Writes: []files.WriteOp{
		{Path: "specs/requirements/prd.md", Content: "v2", BaseSHA: shaOf(b, "specs/requirements/prd.md")},
		{Path: "specs/design/x.md", Content: "mine", BaseSHA: shaOf(b, "specs/design/x.md")},
	}})
	if !errors.Is(err, files.ErrApplyConflict) {
		t.Fatalf("err = %v, want ErrApplyConflict", err)
	}
	if got := conflictPaths(conflicts); !slices.Equal(got, []string{"specs/design/x.md"}) {
		t.Fatalf("conflicts = %v, want exactly the externally changed path", got)
	}
	if got := origin.FileAt(t, repotest.Branch, "specs/design/x.md"); got != "changed outside the Room" {
		t.Fatalf("the external commit was overwritten: %q", got)
	}

	res, _, err := a.Apply(ctx, "greeter", files.ApplyRequest{Writes: []files.WriteOp{
		{Path: "specs/requirements/prd.md", Content: "v2", BaseSHA: shaOf(b, "specs/requirements/prd.md")},
	}})
	if err != nil || !res.Changed {
		t.Fatalf("apply with fresh shas = %+v, %v", res, err)
	}

	fresh, err := a.reader.Bundle(ctx, "greeter", "specs/", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.Apply(ctx, "greeter", files.ApplyRequest{Writes: []files.WriteOp{
		{Path: "specs/design/x.md", Content: "mine", BaseSHA: shaOf(fresh, "specs/design/x.md")},
	}}); err != nil {
		t.Fatalf("apply after a re-seed: %v", err)
	}
}

func TestApply_DependencyStubCompletedInSameCommit(t *testing.T) {
	a, origin := newApplier(t, nil, withCompleter(newFakeCompleter(map[string]files.Completed{
		"specs/design/dependencies/payments/dependency.json": {
			Definition: `{"name":"payments","resource":{"ref":"payments"}}`,
			Files:      map[string]string{"specs/design/dependencies/payments/openapi.yaml": "openapi: 3.0.3\n"},
		},
	})))
	res, _, err := a.Apply(ctx, "greeter", files.ApplyRequest{Writes: []files.WriteOp{
		{Path: "specs/design/dependencies/payments/dependency.json", Content: paymentsStub},
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"specs/design/dependencies/payments/dependency.json", "specs/design/dependencies/payments/openapi.yaml"}
	if got := changedPathsIn(t, origin, res.CommitSHA); !sameStrings(got, want) {
		t.Fatalf("commit changed %v, want %v", got, want)
	}
	if got := origin.FileAt(t, res.CommitSHA, "specs/design/dependencies/payments/dependency.json"); got != `{"name":"payments","resource":{"ref":"payments"}}` {
		t.Fatalf("the completed definition did not replace the stub: %q", got)
	}
	if len(res.Files) != 2 {
		t.Fatalf("files = %+v, want the definition and its document", res.Files)
	}
}

func TestApply_CompleterDownLandsStubWithWarning(t *testing.T) {
	a, origin := newApplier(t, nil, withCompleter(failingCompleter{}))
	res, _, err := a.Apply(ctx, "greeter", files.ApplyRequest{Writes: []files.WriteOp{
		{Path: "specs/design/dependencies/payments/dependency.json", Content: paymentsStub},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(warningCodes(res.Warnings), "registry-unreachable") {
		t.Fatalf("warnings = %+v, want registry-unreachable", res.Warnings)
	}
	if got := origin.FileAt(t, res.CommitSHA, "specs/design/dependencies/payments/dependency.json"); got != paymentsStub {
		t.Fatalf("the stub did not land as written: %q", got)
	}
}

func TestApply_AuthorIsGitpatIdentity(t *testing.T) {
	a, origin := newApplier(t, nil, withIdentity("octo-bot", ""))
	res, _, err := a.Apply(ctx, "greeter", files.ApplyRequest{
		Writes:  []files.WriteOp{{Path: "specs/a.md", Content: "a"}},
		Message: "collab session\n\nCo-authored-by: Ann <ann@x>",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := authorOf(t, origin, res.CommitSHA); got != "octo-bot <octo-bot@users.noreply.github.com>" {
		t.Fatalf("author = %q", got)
	}
	if got := origin.Git(t, "log", "-1", "--format=%cn <%ce>", res.CommitSHA); got != "octo-bot <octo-bot@users.noreply.github.com>" {
		t.Fatalf("committer = %q", got)
	}
	msg := origin.Git(t, "log", "-1", "--format=%B", res.CommitSHA)
	if !strings.HasPrefix(msg, "aep: apply file changes: collab session") || !strings.Contains(msg, "Co-authored-by: Ann <ann@x>") {
		t.Fatalf("message = %q", msg)
	}
}

func TestApply_ENOSPCIsDiskFull(t *testing.T) {
	a, _ := newApplier(t, nil, withENOSPCOnPush())
	_, _, err := a.Apply(ctx, "greeter", files.ApplyRequest{Writes: []files.WriteOp{{Path: "specs/a.md", Content: "a"}}})
	if !errors.Is(err, repo.ErrDiskFull) {
		t.Fatalf("err = %v, want repo.ErrDiskFull", err)
	}
	// The engine failure carries git's text (which may name the clone URL),
	// so it is a *RepoError the edge logs by class only.
	var re *files.RepoError
	if !errors.As(err, &re) {
		t.Fatalf("err = %#v, want a *RepoError", err)
	}
}

// ---- completions (Q-9, carries 2-4) --------------------------------------

// Only dependency stubs reach the completer: a finished definition, a plain
// spec file and a provider stub whose document the same save writes are not
// sent, and a save with no stubs makes no call.
func TestApply_CompletesOnlyStubs(t *testing.T) {
	finished := `{"name":"billing","resource":{"name":"billing","provider":"stripe"}}`
	providerOwed := `{"name":"maps","resource":{"name":"maps","contract":{"type":"openapi","path":"openapi.yaml","origin":"provider"}},"provenance":{"sourceUrl":"https://maps.example/openapi.yaml"}}`
	providerSelf := `{"name":"geo","resource":{"name":"geo","contract":{"type":"openapi","path":"openapi.yaml","origin":"provider"}},"provenance":{"sourceUrl":"https://geo.example/openapi.yaml"}}`
	legacyOrgStub := `{"name":"ledger","source":"org"}`
	c := newFakeCompleter(nil)
	a, _ := newApplier(t, nil, withCompleter(c))

	if _, _, err := a.Apply(ctx, "greeter", files.ApplyRequest{Writes: []files.WriteOp{
		{Path: "specs/requirements/prd.md", Content: "# PRD"},
		{Path: "specs/design/dependencies/billing/dependency.json", Content: finished},
	}}); err != nil {
		t.Fatal(err)
	}
	if calls := c.Calls(); len(calls) != 0 {
		t.Fatalf("a save without stubs called the completer: %v", calls)
	}

	if _, _, err := a.Apply(ctx, "greeter", files.ApplyRequest{Writes: []files.WriteOp{
		{Path: "specs/design/dependencies/payments/dependency.json", Content: paymentsStub},
		{Path: "specs/design/dependencies/maps/dependency.json", Content: providerOwed},
		{Path: "specs/design/dependencies/geo/dependency.json", Content: providerSelf},
		{Path: "specs/design/dependencies/geo/openapi.yaml", Content: "openapi: 3.0.3\n"},
		{Path: "specs/design/dependencies/ledger/dependency.json", Content: legacyOrgStub},
	}}); err != nil {
		t.Fatal(err)
	}
	calls := c.Calls()
	want := []string{
		"specs/design/dependencies/ledger/dependency.json",
		"specs/design/dependencies/maps/dependency.json",
		"specs/design/dependencies/payments/dependency.json",
	}
	if len(calls) != 1 || !sameStrings(calls[0], want) {
		t.Fatalf("completer calls = %v, want one call with %v", calls, want)
	}
}

// While aep-api is down each stub gets its own kind's warning, and a non-stub
// dependency write gets none.
func TestApply_CompleterDownWarnsEachStubByKind(t *testing.T) {
	finished := `{"name":"billing","resource":{"name":"billing","provider":"stripe"}}`
	providerOwed := `{"name":"maps","resource":{"name":"maps","contract":{"type":"openapi","path":"openapi.yaml","origin":"provider"}},"provenance":{"sourceUrl":"https://maps.example/openapi.yaml"}}`
	a, _ := newApplier(t, nil, withCompleter(failingCompleter{}))
	res, _, err := a.Apply(ctx, "greeter", files.ApplyRequest{Writes: []files.WriteOp{
		{Path: "specs/design/dependencies/payments/dependency.json", Content: paymentsStub},
		{Path: "specs/design/dependencies/maps/dependency.json", Content: providerOwed},
		{Path: "specs/design/dependencies/billing/dependency.json", Content: finished},
	}})
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string][]string{}
	for _, w := range res.Warnings {
		byPath[w.Path] = append(byPath[w.Path], w.Code)
	}
	if got := byPath["specs/design/dependencies/payments/dependency.json"]; !slices.Equal(got, []string{"registry-unreachable"}) {
		t.Errorf("registry stub warnings = %v", got)
	}
	if got := byPath["specs/design/dependencies/maps/dependency.json"]; !slices.Equal(got, []string{"provider-document-unavailable"}) {
		t.Errorf("provider stub warnings = %v", got)
	}
	if got := byPath["specs/design/dependencies/billing/dependency.json"]; len(got) != 0 {
		t.Errorf("a finished definition got completion warnings: %v", got)
	}
}

// One save makes one completions call with at most 64 stubs: aep-api's
// per-call bound on outbound fetches is then a bound per save. Stubs past the
// first 64 (request order) are not sent and land as written with a warning.
func TestApply_AtMost64StubsPerSave(t *testing.T) {
	c := newFakeCompleter(nil)
	a, origin := newApplier(t, nil, withCompleter(c))
	var writes []files.WriteOp
	for i := range 65 {
		name := fmt.Sprintf("dep-%02d", i)
		writes = append(writes, files.WriteOp{
			Path:    "specs/design/dependencies/" + name + "/dependency.json",
			Content: fmt.Sprintf(`{"name":%q,"resource":{"ref":%q,"name":%q}}`, name, name, name),
		})
	}
	res, _, err := a.Apply(ctx, "greeter", files.ApplyRequest{Writes: writes})
	if err != nil {
		t.Fatal(err)
	}
	calls := c.Calls()
	if len(calls) != 1 || len(calls[0]) != 64 {
		t.Fatalf("completer calls = %d (first carries %d), want one call with 64", len(calls), len(calls[0]))
	}
	for i, p := range calls[0] {
		if p != writes[i].Path {
			t.Fatalf("call[%d] = %s, want the first 64 in request order", i, p)
		}
	}
	last := writes[64]
	if len(res.Warnings) != 1 || res.Warnings[0].Path != last.Path || res.Warnings[0].Code != "registry-unreachable" {
		t.Fatalf("warnings = %+v, want one for the 65th stub only", res.Warnings)
	}
	if got := origin.FileAt(t, res.CommitSHA, last.Path); got != last.Content {
		t.Fatalf("the 65th stub did not land as written: %q", got)
	}
}

// aep-api not knowing the project is not degraded: the save is refused
// (project_unknown at the edge) and nothing commits.
func TestApply_CompletionsUnknownProjectRefusesTheSave(t *testing.T) {
	a, origin := newApplier(t, nil, withCompleter(unknownProjectCompleter{}))
	head := origin.HeadSHA(t)
	_, _, err := a.Apply(ctx, "greeter", files.ApplyRequest{Writes: []files.WriteOp{
		{Path: "specs/design/dependencies/payments/dependency.json", Content: paymentsStub},
	}})
	if !errors.Is(err, projects.ErrUnknown) {
		t.Fatalf("err = %v, want projects.ErrUnknown", err)
	}
	if origin.HeadSHA(t) != head {
		t.Fatal("a save for a project aep-api does not know committed")
	}
}

type unknownProjectCompleter struct{}

func (unknownProjectCompleter) Complete(_ context.Context, project string, _ []files.WriteOp) (map[string]files.Completed, []files.Warning, error) {
	return nil, nil, fmt.Errorf("%w: %q", projects.ErrUnknown, project)
}

// The completer's warnings ride the apply's warnings.
func TestApply_CompleterWarningsPassThrough(t *testing.T) {
	a, _ := newApplier(t, nil, withCompleter(warningCompleter{w: files.Warning{
		Path: "specs/design/dependencies/payments/dependency.json", Code: "registry-miss", Message: "no such resource",
	}}))
	res, _, err := a.Apply(ctx, "greeter", files.ApplyRequest{Writes: []files.WriteOp{
		{Path: "specs/design/dependencies/payments/dependency.json", Content: paymentsStub},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(warningCodes(res.Warnings), "registry-miss") {
		t.Fatalf("warnings = %+v", res.Warnings)
	}
}

type warningCompleter struct{ w files.Warning }

func (c warningCompleter) Complete(context.Context, string, []files.WriteOp) (map[string]files.Completed, []files.Warning, error) {
	return nil, []files.Warning{c.w}, nil
}

// A save that itself writes or deletes a document the platform lands for one
// of its stubs is refused before any git operation.
func TestApply_RefusesWritingAPlatformLandedDocument(t *testing.T) {
	completed := map[string]files.Completed{"specs/design/dependencies/payments/dependency.json": {
		Definition: `{"name":"payments","resource":{"ref":"payments"}}`,
		Files:      map[string]string{"specs/design/dependencies/payments/openapi.yaml": "openapi: 3.0.3\n"},
	}}
	a, origin := newApplier(t, map[string]string{"specs/design/dependencies/payments/openapi.yaml": "old"}, withCompleter(newFakeCompleter(completed)))
	head := origin.HeadSHA(t)
	b, err := a.reader.Bundle(ctx, "greeter", "specs/", "")
	if err != nil {
		t.Fatal(err)
	}
	docSHA := shaOf(b, "specs/design/dependencies/payments/openapi.yaml")
	for name, req := range map[string]files.ApplyRequest{
		"write": {Writes: []files.WriteOp{
			{Path: "specs/design/dependencies/payments/dependency.json", Content: paymentsStub},
			{Path: "specs/design/dependencies/payments/openapi.yaml", Content: "mine", BaseSHA: docSHA},
		}},
		"delete": {
			Writes:  []files.WriteOp{{Path: "specs/design/dependencies/payments/dependency.json", Content: paymentsStub}},
			Deletes: []files.DeleteOp{{Path: "specs/design/dependencies/payments/openapi.yaml", BaseSHA: docSHA}},
		},
	} {
		if _, _, err := a.Apply(ctx, "greeter", req); !errors.Is(err, files.ErrPathInvalid) {
			t.Errorf("%s: err = %v, want ErrPathInvalid", name, err)
		}
	}
	if origin.HeadSHA(t) != head {
		t.Fatal("a refused save committed")
	}
}

// A completion that would land a file outside its stub's dependency
// directory, or complete a path that was never sent, is not trusted: the
// whole answer is dropped and every stub lands as written with a warning.
func TestApply_UntrustedCompletionIsDropped(t *testing.T) {
	cases := map[string]map[string]files.Completed{
		"traversal": {"specs/design/dependencies/payments/dependency.json": {
			Definition: `{"name":"payments"}`,
			Files:      map[string]string{"specs/design/dependencies/payments/../billing/openapi.yaml": "x"},
		}},
		"sibling dependency": {"specs/design/dependencies/payments/dependency.json": {
			Definition: `{"name":"payments"}`,
			Files:      map[string]string{"specs/design/dependencies/billing/openapi.yaml": "x"},
		}},
		"subdirectory": {"specs/design/dependencies/payments/dependency.json": {
			Definition: `{"name":"payments"}`,
			Files:      map[string]string{"specs/design/dependencies/payments/docs/openapi.yaml": "x"},
		}},
		"outside specs": {"specs/design/dependencies/payments/dependency.json": {
			Definition: `{"name":"payments"}`,
			Files:      map[string]string{".github/workflows/x.yaml": "x"},
		}},
		"the definition itself": {"specs/design/dependencies/payments/dependency.json": {
			Definition: `{"name":"payments"}`,
			Files:      map[string]string{"specs/design/dependencies/payments/dependency.json": "x"},
		}},
		"unsent path": {"specs/design/dependencies/billing/dependency.json": {
			Definition: `{"name":"billing"}`,
		}},
	}
	for name, answer := range cases {
		t.Run(name, func(t *testing.T) {
			a, origin := newApplier(t, nil, withCompleter(newFakeCompleterAnswering(answer)))
			res, _, err := a.Apply(ctx, "greeter", files.ApplyRequest{Writes: []files.WriteOp{
				{Path: "specs/design/dependencies/payments/dependency.json", Content: paymentsStub},
			}})
			if err != nil {
				t.Fatal(err)
			}
			if got := changedPathsIn(t, origin, res.CommitSHA); !slices.Equal(got, []string{"specs/design/dependencies/payments/dependency.json"}) {
				t.Fatalf("commit changed %v, want only the stub", got)
			}
			if got := origin.FileAt(t, res.CommitSHA, "specs/design/dependencies/payments/dependency.json"); got != paymentsStub {
				t.Fatalf("stub = %q, want it as written", got)
			}
			if !slices.Contains(warningCodes(res.Warnings), "registry-unreachable") {
				t.Fatalf("warnings = %+v", res.Warnings)
			}
		})
	}
}

// fakeCompleterAnswering returns answer verbatim, whatever was sent.
type fakeCompleterAnswering map[string]files.Completed

func newFakeCompleterAnswering(answer map[string]files.Completed) fakeCompleterAnswering {
	return fakeCompleterAnswering(answer)
}

func (f fakeCompleterAnswering) Complete(context.Context, string, []files.WriteOp) (map[string]files.Completed, []files.Warning, error) {
	return f, nil, nil
}

// ---- identity ------------------------------------------------------------

// GitHub unreachable for the identity lookup does not gate the save: it
// commits under the platform's default identity.
func TestApply_IdentityLookupFailureFallsBackToAEP(t *testing.T) {
	a, origin := newApplier(t, nil, withFailingIdentity())
	res, _, err := a.Apply(ctx, "greeter", files.ApplyRequest{Writes: []files.WriteOp{{Path: "specs/a.md", Content: "a"}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := authorOf(t, origin, res.CommitSHA); got != "AEP <noreply@aep.dev>" {
		t.Fatalf("author = %q", got)
	}
}
