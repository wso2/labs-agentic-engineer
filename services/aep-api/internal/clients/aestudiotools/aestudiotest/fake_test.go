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

package aestudiotest_test

import (
	"bytes"
	"context"
	"crypto/sha1" //nolint:gosec // git object ids are sha1 by definition
	"encoding/hex"
	"errors"
	"io"
	"maps"
	"mime/multipart"
	"slices"
	"strconv"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools"
	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools/aestudiotest"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

var ref = sourcecontrol.RepoRef{Org: "default", Owner: "acme", Repo: "greeter"}

// The sub-port method sets as Task 4.13 writes them into ports.go: the GitHub
// ports keep their method names and take a RepoRef instead of owner, repo and
// a credential. Until then they live here so the Fake is held to them.
type (
	repoAdmin interface {
		CreateOrgRepo(ctx context.Context, ref sourcecontrol.RepoRef, req sourcecontrol.CreateOrgRepoRequest) (cloneURL string, err error)
	}
	issueOps interface {
		CreateIssue(ctx context.Context, ref sourcecontrol.RepoRef, req sourcecontrol.CreateIssueRequest) (*sourcecontrol.IssueResult, error)
		ListIssues(ctx context.Context, ref sourcecontrol.RepoRef, labels []string) ([]sourcecontrol.IssueInfo, error)
		GetIssue(ctx context.Context, ref sourcecontrol.RepoRef, number int) (*sourcecontrol.IssueInfo, error)
		ListIssueComments(ctx context.Context, ref sourcecontrol.RepoRef, number, limit int) ([]sourcecontrol.IssueComment, error)
		EnsureLabel(ctx context.Context, ref sourcecontrol.RepoRef, name, color string) error
		CloseIssue(ctx context.Context, ref sourcecontrol.RepoRef, number int) error
		ReopenIssue(ctx context.Context, ref sourcecontrol.RepoRef, number int) error
		CommentIssue(ctx context.Context, ref sourcecontrol.RepoRef, number int, body string) error
		EditIssueBody(ctx context.Context, ref sourcecontrol.RepoRef, number int, body string) error
		EditIssueTitle(ctx context.Context, ref sourcecontrol.RepoRef, number int, title string) error
		AddIssueLabels(ctx context.Context, ref sourcecontrol.RepoRef, number int, labels []string) error
		RemoveIssueLabel(ctx context.Context, ref sourcecontrol.RepoRef, number int, label string) error
		SetIssueLabels(ctx context.Context, ref sourcecontrol.RepoRef, number int, labels []string) error
		SetIssueMilestone(ctx context.Context, ref sourcecontrol.RepoRef, number, milestoneNumber int) error
		GetPullRequest(ctx context.Context, ref sourcecontrol.RepoRef, number int) (*sourcecontrol.PullRequestState, error)
		MergePullRequest(ctx context.Context, ref sourcecontrol.RepoRef, number int) error
		ListPullRequestFiles(ctx context.Context, ref sourcecontrol.RepoRef, number int) ([]string, error)
		CreateMilestone(ctx context.Context, ref sourcecontrol.RepoRef, req sourcecontrol.CreateMilestoneRequest) (*sourcecontrol.MilestoneResult, error)
		CloseMilestone(ctx context.Context, ref sourcecontrol.RepoRef, number int) error
		ReopenMilestone(ctx context.Context, ref sourcecontrol.RepoRef, number int) error
		ListMilestones(ctx context.Context, ref sourcecontrol.RepoRef, state string) ([]sourcecontrol.Milestone, error)
		ListMilestoneIssues(ctx context.Context, ref sourcecontrol.RepoRef, filter sourcecontrol.MilestoneIssuesFilter) ([]sourcecontrol.IssueInfo, error)
		MilestoneIssueCounts(ctx context.Context, ref sourcecontrol.RepoRef, number int) (*sourcecontrol.MilestoneIssueCounts, error)
		ListMilestoneIssueComments(ctx context.Context, ref sourcecontrol.RepoRef, number, perIssue int) (map[int][]sourcecontrol.IssueComment, error)
	}
	webhookOps interface {
		RegisterWebhook(ctx context.Context, ref sourcecontrol.RepoRef, events []string) (hookID int64, err error)
		UpdateWebhookEvents(ctx context.Context, ref sourcecontrol.RepoRef, hookID int64, events []string) error
		DeleteWebhook(ctx context.Context, ref sourcecontrol.RepoRef, hookID int64) error
	}
	host interface {
		repoAdmin
		issueOps
		webhookOps
	}
)

var (
	_ host                          = (*aestudiotest.Fake)(nil)
	_ sourcecontrol.Git             = (*aestudiotest.Fake)(nil)
	_ sourcecontrol.TrashOps        = (*aestudiotest.Fake)(nil)
	_ sourcecontrol.SkillsMirrorOps = (*aestudiotest.Fake)(nil)
	_ sourcecontrol.ReferencesOps   = (*aestudiotest.Fake)(nil)
	_ sourcecontrol.IdentityOps     = (*aestudiotest.Fake)(nil)
	_ aestudiotools.Turns           = (*aestudiotest.Fake)(nil)
)

func blobSHA(content string) string {
	h := sha1.New() //nolint:gosec // git object ids are sha1 by definition
	_, _ = io.WriteString(h, "blob "+strconv.Itoa(len(content))+"\x00"+content)
	return hex.EncodeToString(h.Sum(nil))
}

func TestFake_CommitConflictAndShas(t *testing.T) {
	f := aestudiotest.New()
	ref := sourcecontrol.RepoRef{Org: "default", Owner: "acme", Repo: "greeter"}
	f.SeedRepo(ref, map[string]string{"specs/a.md": "1"})
	_, sha, _ := f.ReadFile(context.Background(), ref, "", "specs/a.md")
	res, err := f.Commit(context.Background(), ref, sourcecontrol.CommitRequest{Writes: []sourcecontrol.FileWrite{{Path: "specs/a.md", Content: "2", BaseSHA: sha}}})
	if err != nil || !res.Changed || len(res.CommitSHA) != 40 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	_, err = f.Commit(context.Background(), ref, sourcecontrol.CommitRequest{Writes: []sourcecontrol.FileWrite{{Path: "specs/a.md", Content: "3", BaseSHA: sha}}})
	var cc *sourcecontrol.CommitConflictError
	if !errors.As(err, &cc) || !errors.Is(err, sourcecontrol.ErrCommitConflict) || cc.Conflicts[0].Path != "specs/a.md" {
		t.Fatalf("err = %v, want a CommitConflictError", err)
	}
}

func TestFake_InjectsAbsentAndPermanence(t *testing.T) {
	f := aestudiotest.New()
	f.FailOrg("default", sourcecontrol.ErrAEStudioAbsent)
	_, err := f.Head(context.Background(), sourcecontrol.RepoRef{Org: "default", Owner: "acme", Repo: "g"}, "")
	if !errors.Is(err, sourcecontrol.ErrAEStudioAbsent) || !sourcecontrol.IsPermanent(err) {
		t.Fatalf("err = %v", err)
	}
	f.FailOrg("default", sourcecontrol.ErrAEStudioUnavailable)
	_, err = f.Head(context.Background(), sourcecontrol.RepoRef{Org: "default", Owner: "acme", Repo: "g"}, "")
	if sourcecontrol.IsPermanent(err) {
		t.Fatal("Unavailable must stay retryable")
	}
	f.FailOrg("default", sourcecontrol.ErrAEStudioMisconfigured)
	_, err = f.Head(context.Background(), sourcecontrol.RepoRef{Org: "default", Owner: "acme", Repo: "g"}, "")
	if !sourcecontrol.IsPermanent(err) {
		t.Fatal("Misconfigured is a permanent config error (C3)")
	}
}

// A commit answers the written files' blob shas (git's own), a no-op commit
// keeps the tip, and the commit sha changes with the tree.
func TestFake_CommitResultIsGitShaped(t *testing.T) {
	f := aestudiotest.New()
	ctx := context.Background()
	f.SeedRepo(ref, map[string]string{"a.md": "1", "b.md": "x"})
	tip, err := f.Head(ctx, ref, "")
	if err != nil || len(tip) != 40 {
		t.Fatalf("head=%q err=%v", tip, err)
	}
	_, aSHA, _ := f.ReadFile(ctx, ref, "", "a.md")
	if aSHA != blobSHA("1") {
		t.Fatalf("blob sha = %s, want git's %s", aSHA, blobSHA("1"))
	}
	_, bSHA, _ := f.ReadFile(ctx, ref, "", "b.md")

	same, err := f.Commit(ctx, ref, sourcecontrol.CommitRequest{Writes: []sourcecontrol.FileWrite{{Path: "a.md", Content: "1", BaseSHA: aSHA}}})
	if err != nil || same.Changed || same.CommitSHA != tip {
		t.Fatalf("a no-op commit = %+v err=%v, want unchanged at %s", same, err, tip)
	}

	res, err := f.Commit(ctx, ref, sourcecontrol.CommitRequest{
		Writes:  []sourcecontrol.FileWrite{{Path: "c.md", Content: "new"}},
		Deletes: []sourcecontrol.FileDelete{{Path: "b.md", BaseSHA: bSHA}},
		Message: "m",
	})
	if err != nil || !res.Changed || res.CommitSHA == tip {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if !slices.Equal(res.Files, []sourcecontrol.CommittedFile{{Path: "c.md", SHA: blobSHA("new")}}) {
		t.Fatalf("files = %+v", res.Files)
	}
	if _, _, err := f.ReadFile(ctx, ref, "", "b.md"); !errors.Is(err, sourcecontrol.ErrPathNotFound) {
		t.Fatalf("deleted path: err = %v", err)
	}
	// The old tip still reads the old tree.
	if got, _, err := f.ReadFile(ctx, ref, tip, "b.md"); err != nil || string(got) != "x" {
		t.Fatalf("read at the old tip = %q err=%v", got, err)
	}

	// "" BaseSHA means the path must not exist; a delete must name the
	// current blob. Every conflicting path is reported.
	_, err = f.Commit(ctx, ref, sourcecontrol.CommitRequest{
		Writes:  []sourcecontrol.FileWrite{{Path: "a.md", Content: "2"}},
		Deletes: []sourcecontrol.FileDelete{{Path: "c.md", BaseSHA: blobSHA("old")}},
	})
	var cc *sourcecontrol.CommitConflictError
	if !errors.As(err, &cc) || len(cc.Conflicts) != 2 ||
		cc.Conflicts[0] != (sourcecontrol.Conflict{Path: "a.md", CurrentSHA: aSHA}) ||
		cc.Conflicts[1] != (sourcecontrol.Conflict{Path: "c.md", BaseSHA: blobSHA("old"), CurrentSHA: blobSHA("new")}) {
		t.Fatalf("err = %v (%+v)", err, cc)
	}
	if after, _ := f.Head(ctx, ref, ""); after != res.CommitSHA {
		t.Fatal("a refused commit moved the tip")
	}
}

func TestFake_ReadsListBundleAndTags(t *testing.T) {
	f := aestudiotest.New()
	ctx := context.Background()
	f.SeedRepo(ref, map[string]string{
		"specs/requirements.md":            "r",
		"specs/design/components/a.json":   "{}",
		"specs/design/components/b.yaml":   "b: 1",
		"specs/design/components/x/c.json": "[]",
	})
	entries, sha, err := f.List(ctx, ref, "", sourcecontrol.Local())
	if err != nil || len(sha) != 40 || len(entries) != 4 || entries[0].Path != "specs/design/components/a.json" || entries[0].Size != 2 {
		t.Fatalf("entries=%+v sha=%s err=%v", entries, sha, err)
	}

	files, _, err := f.ReadBundle(ctx, ref, "", sourcecontrol.BundleFilter{Prefix: "specs/design/components/", Exts: []string{".json"}})
	if err != nil || !maps.Equal(files, map[string]string{"specs/design/components/a.json": "{}", "specs/design/components/x/c.json": "[]"}) {
		t.Fatalf("bundle=%v err=%v", files, err)
	}
	files, _, _ = f.ReadBundle(ctx, ref, "", sourcecontrol.BundleFilter{Prefix: "nope/", Paths: []string{"specs/requirements.md", "missing.md"}})
	if !maps.Equal(files, map[string]string{"specs/requirements.md": "r"}) {
		t.Fatalf("exact-path bundle = %v", files)
	}

	if err := f.Tag(ctx, ref, sourcecontrol.TagSpec{Name: "v1", Message: "first"}); err != nil {
		t.Fatal(err)
	}
	if err := f.Tag(ctx, ref, sourcecontrol.TagSpec{Name: "v1"}); !errors.Is(err, sourcecontrol.ErrTagAlreadyExists) {
		t.Fatalf("retag: err = %v", err)
	}
	_ = f.Tag(ctx, ref, sourcecontrol.TagSpec{Name: "w1", Target: sha})
	if err := f.Tag(ctx, ref, sourcecontrol.TagSpec{Name: "z", Target: "tags/missing"}); !errors.Is(err, sourcecontrol.ErrRefNotFound) {
		t.Fatalf("tag at an unknown ref: err = %v", err)
	}
	tags, err := f.ListTags(ctx, ref, "v")
	if err != nil || len(tags) != 1 || tags[0].Name != "v1" || tags[0].CommitHash != sha || tags[0].Message != "first" || tags[0].CreatedAt.IsZero() {
		t.Fatalf("tags=%+v err=%v", tags, err)
	}
	if _, err := f.Commit(ctx, ref, sourcecontrol.CommitRequest{Writes: []sourcecontrol.FileWrite{{Path: "n.md", Content: "n"}}}); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.Head(ctx, ref, "tags/v1"); got != sha {
		t.Fatalf("head at tags/v1 = %s, want %s", got, sha)
	}
	if _, _, err := f.ReadFile(ctx, ref, "tags/v1", "n.md"); !errors.Is(err, sourcecontrol.ErrPathNotFound) {
		t.Fatalf("a file added after the tag: err = %v", err)
	}
	if _, err := f.Head(ctx, ref, "0000000000000000000000000000000000000000"); !errors.Is(err, sourcecontrol.ErrRefNotFound) {
		t.Fatalf("unknown sha: err = %v", err)
	}
	if _, err := f.Head(ctx, sourcecontrol.RepoRef{Org: "default", Owner: "acme", Repo: "other"}, ""); !errors.Is(err, sourcecontrol.ErrRepoNotFound) {
		t.Fatalf("unknown repo: err = %v", err)
	}
}

// Calls records each port call in order with its at and Local; BeforeCommit
// runs outside the Fake's lock, so the hook can read and commit itself.
func TestFake_CallsAndBeforeCommit(t *testing.T) {
	f := aestudiotest.New()
	ctx := context.Background()
	f.SeedRepo(ref, map[string]string{"a.md": "1"})
	sha, _ := f.Head(ctx, ref, "", sourcecontrol.Local())
	_, _ = f.ListTags(ctx, ref, "", sourcecontrol.Local())
	_, _, _ = f.ReadBundle(ctx, ref, sha, sourcecontrol.BundleFilter{Exts: []string{".md"}})

	raced := false
	f.BeforeCommit(func() {
		if raced {
			return
		}
		raced = true
		_, cur, _ := f.ReadFile(ctx, ref, "", "a.md")
		if _, err := f.Commit(ctx, ref, sourcecontrol.CommitRequest{Writes: []sourcecontrol.FileWrite{{Path: "a.md", Content: "other", BaseSHA: cur}}}); err != nil {
			t.Errorf("the racing commit: %v", err)
		}
	})
	_, err := f.Commit(ctx, ref, sourcecontrol.CommitRequest{Writes: []sourcecontrol.FileWrite{{Path: "a.md", Content: "mine", BaseSHA: blobSHA("1")}}})
	if !errors.Is(err, sourcecontrol.ErrCommitConflict) {
		t.Fatalf("err = %v, want the race to conflict", err)
	}

	calls := f.Calls()
	ops := make([]string, len(calls))
	for i, c := range calls {
		ops[i] = c.Op
	}
	want := []string{aestudiotest.OpHead, aestudiotest.OpListTags, aestudiotest.OpReadBundle, aestudiotest.OpCommit, aestudiotest.OpReadFile, aestudiotest.OpCommit}
	if !slices.Equal(ops, want) {
		t.Fatalf("ops = %v, want %v", ops, want)
	}
	if !calls[0].Local || !calls[1].Local || calls[2].Local || calls[2].At != sha || calls[2].Filter.Exts[0] != ".md" || calls[0].Ref != ref {
		t.Fatalf("calls = %+v", calls)
	}
	// A ref's DefaultBranch never splits state, so Calls is where it is asserted.
	branched := sourcecontrol.RepoRef{Org: ref.Org, Owner: ref.Owner, Repo: ref.Repo, DefaultBranch: "trunk"}
	if got, err := f.Head(ctx, branched, ""); err != nil || got == "" {
		t.Fatalf("head via a branch-carrying ref: %q %v", got, err)
	}
	if last := f.Calls()[len(f.Calls())-1]; last.Ref.DefaultBranch != "trunk" {
		t.Fatalf("the call lost the ref's DefaultBranch: %+v", last.Ref)
	}
}

func TestFake_FailOpAndRateLimit(t *testing.T) {
	f := aestudiotest.New()
	ctx := context.Background()
	f.SeedRepo(ref, nil)
	f.FailOp(aestudiotest.OpCreateIssue, &sourcecontrol.RateLimitedError{RetryAfter: 30})
	_, err := f.CreateIssue(ctx, ref, sourcecontrol.CreateIssueRequest{Title: "t"})
	var rl *sourcecontrol.RateLimitedError
	if !errors.As(err, &rl) || sourcecontrol.IsPermanent(err) {
		t.Fatalf("err = %v, want a retryable rate limit", err)
	}
	f.FailOp(aestudiotest.OpCreateIssue, nil)
	if _, err := f.CreateIssue(ctx, ref, sourcecontrol.CreateIssueRequest{Title: "t"}); err != nil {
		t.Fatalf("cleared: err = %v", err)
	}
	f.FailOrg("default", sourcecontrol.ErrAEStudioUnavailable)
	f.FailOrg("default", nil)
	if _, err := f.Head(ctx, ref, ""); err != nil {
		t.Fatalf("org cleared: err = %v", err)
	}
}

func TestFake_IssuesMilestonesAndPulls(t *testing.T) {
	f := aestudiotest.New()
	ctx := context.Background()
	f.SeedRepo(ref, nil)

	ms, err := f.CreateMilestone(ctx, ref, sourcecontrol.CreateMilestoneRequest{Title: "v1"})
	if err != nil || !ms.Created {
		t.Fatalf("ms=%+v err=%v", ms, err)
	}
	again, _ := f.CreateMilestone(ctx, ref, sourcecontrol.CreateMilestoneRequest{Title: "V1"})
	if again.Created || again.Number != ms.Number {
		t.Fatalf("case-twin milestone = %+v, want the existing one", again)
	}

	gate, _ := f.CreateIssue(ctx, ref, sourcecontrol.CreateIssueRequest{Title: "gate", Labels: []string{"provision"}, Milestone: &ms.Number})
	work, _ := f.CreateIssue(ctx, ref, sourcecontrol.CreateIssueRequest{Title: "work", Labels: []string{"aep", "development"}})
	if err := f.SetIssueMilestone(ctx, ref, work.Number, ms.Number); err != nil {
		t.Fatal(err)
	}
	if err := f.AddIssueLabels(ctx, ref, work.Number, []string{"aep", "aep:status/ready"}); err != nil {
		t.Fatal(err)
	}
	if err := f.RemoveIssueLabel(ctx, ref, work.Number, "absent"); err != nil {
		t.Fatalf("removing an absent label: %v", err)
	}
	if err := f.CommentIssue(ctx, ref, work.Number, "hello"); err != nil {
		t.Fatal(err)
	}
	if err := f.EditIssueBody(ctx, ref, work.Number, "body"); err != nil {
		t.Fatal(err)
	}

	got, _ := f.GetIssue(ctx, ref, work.Number)
	if got.Body != "body" || !slices.Equal(got.Labels, []string{"aep", "development", "aep:status/ready"}) || got.State != "open" {
		t.Fatalf("issue = %+v", got)
	}
	if _, err := f.GetIssue(ctx, ref, 999); !errors.Is(err, sourcecontrol.ErrIssueNotFound) {
		t.Fatalf("err = %v", err)
	}
	listed, _ := f.ListIssues(ctx, ref, []string{"aep"})
	if len(listed) != 1 || listed[0].Number != work.Number {
		t.Fatalf("label filter = %+v", listed)
	}
	if all := f.Issues(ref); len(all) != 2 || all[0].Number != gate.Number {
		t.Fatalf("Issues = %+v", all)
	}

	counts, err := f.MilestoneIssueCounts(ctx, ref, ms.Number)
	if err != nil || *counts != (sourcecontrol.MilestoneIssueCounts{OpenProvision: 1, OpenTotal: 2, OpenAgentWork: 1, OpenDevelopment: 1}) {
		t.Fatalf("counts=%+v err=%v", counts, err)
	}
	if _, err := f.MilestoneIssueCounts(ctx, ref, 99); !errors.Is(err, sourcecontrol.ErrMilestoneNotFound) {
		t.Fatalf("err = %v", err)
	}
	comments, _ := f.ListMilestoneIssueComments(ctx, ref, ms.Number, 5)
	if len(comments) != 1 || comments[work.Number][0].Body != "hello" {
		t.Fatalf("comments = %+v", comments)
	}
	if none, _ := f.ListMilestoneIssueComments(ctx, ref, ms.Number, 0); len(none) != 0 {
		t.Fatalf("perIssue 0 answers none, got %+v", none)
	}
	if none, _ := f.ListIssueComments(ctx, ref, work.Number, 0); none != nil {
		t.Fatalf("limit 0 answers none, got %+v", none)
	}
	if one, _ := f.ListIssueComments(ctx, ref, work.Number, 1); len(one) != 1 {
		t.Fatalf("limit 1 = %+v", one)
	}
	if err := f.CloseIssue(ctx, ref, gate.Number); err != nil {
		t.Fatal(err)
	}
	open, _ := f.ListMilestoneIssues(ctx, ref, sourcecontrol.MilestoneIssuesFilter{Number: ms.Number})
	if len(open) != 1 || open[0].Number != work.Number {
		t.Fatalf("open milestone issues = %+v", open)
	}

	pr := f.SeedPullRequest(ref, []string{"src/a.go"})
	if pr == work.Number || pr == gate.Number {
		t.Fatal("pull requests share the issue numbering")
	}
	if err := f.MergePullRequest(ctx, ref, pr); err != nil {
		t.Fatal(err)
	}
	st, _ := f.GetPullRequest(ctx, ref, pr)
	if st.State != "closed" || !st.Merged || len(st.MergeCommitSHA) != 40 {
		t.Fatalf("pr = %+v", st)
	}
	if files, _ := f.ListPullRequestFiles(ctx, ref, pr); !slices.Equal(files, []string{"src/a.go"}) {
		t.Fatalf("pr files = %v", files)
	}
	if len(f.Issues(ref)) != 2 {
		t.Fatal("a pull request is not an issue")
	}
}

func TestFake_ReposHooksAndIdentity(t *testing.T) {
	f := aestudiotest.New()
	ctx := context.Background()
	newRef := sourcecontrol.RepoRef{Org: "default", Owner: "acme", Repo: "fresh"}
	url, err := f.CreateOrgRepo(ctx, newRef, sourcecontrol.CreateOrgRepoRequest{Private: true})
	if err != nil || url != "https://github.com/acme/fresh.git" {
		t.Fatalf("url=%q err=%v", url, err)
	}
	if _, err := f.Head(ctx, newRef, ""); err != nil {
		t.Fatalf("a created repo has an initial commit: %v", err)
	}
	if _, err := f.CreateOrgRepo(ctx, newRef, sourcecontrol.CreateOrgRepoRequest{}); !sourcecontrol.IsRepoNameConflict(err) {
		t.Fatalf("err = %v, want a name conflict", err)
	}
	if _, err := f.CreateOrgRepo(ctx, newRef, sourcecontrol.CreateOrgRepoRequest{AdoptExisting: true}); err != nil {
		t.Fatalf("adopt: %v", err)
	}

	id, err := f.RegisterWebhook(ctx, newRef, []string{"push"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.UpdateWebhookEvents(ctx, newRef, id, []string{"push", "issues"}); err != nil {
		t.Fatal(err)
	}
	if got := f.HookEvents(newRef); !slices.Equal(got[id], []string{"push", "issues"}) {
		t.Fatalf("hooks = %v", got)
	}
	if err := f.DeleteWebhook(ctx, newRef, id); err != nil || len(f.HookEvents(newRef)) != 0 {
		t.Fatalf("delete: err=%v hooks=%v", err, f.HookEvents(newRef))
	}
	if err := f.DeleteWebhook(ctx, newRef, id); err != nil {
		t.Fatalf("a gone hook is success: %v", err)
	}

	if err := f.TrashRepo(ctx, newRef); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Head(ctx, newRef, ""); !errors.Is(err, sourcecontrol.ErrRepoNotFound) {
		t.Fatalf("trashed repo: err = %v", err)
	}

	if _, err := f.GitHubIdentity(ctx, "default"); !sourcecontrol.IsHTTPStatus(err, 502) {
		t.Fatalf("no identity: err = %v, want a github_error", err)
	}
	f.SetIdentity("default", &sourcecontrol.GitHubUser{Login: "acme-bot", ID: 1})
	if u, err := f.GitHubIdentity(ctx, "default"); err != nil || u.Login != "acme-bot" {
		t.Fatalf("user=%+v err=%v", u, err)
	}
}

// MirrorSkills is recorded, not simulated: the pod owns the catalog rule.
func TestFake_MirrorSkillsRecordsTheCall(t *testing.T) {
	f := aestudiotest.New()
	ctx := context.Background()
	project := sourcecontrol.RepoRef{Org: "default", Owner: "acme", Repo: "greeter", DefaultBranch: "trunk"}
	skills := sourcecontrol.RepoRef{Org: "default", Owner: "acme", Repo: "org-skills", DefaultBranch: "main"}
	f.SeedRepo(skills, map[string]string{"lint/SKILL.md": "lint"})
	f.SeedRepo(project, map[string]string{"README.md": "r"})
	tip, _ := f.Head(ctx, project, "")

	res, err := f.MirrorSkills(ctx, project, skills, []string{"lint"})
	if err != nil || res.Changed || res.CommitSHA != tip {
		t.Fatalf("res=%+v err=%v, want the tip unchanged", res, err)
	}
	if files, _, _ := f.ReadBundle(ctx, project, "", sourcecontrol.BundleFilter{}); !maps.Equal(files, map[string]string{"README.md": "r"}) {
		t.Fatalf("the Fake copied skill content: %v", files)
	}
	calls := f.Calls()
	c := calls[1]
	if c.Op != aestudiotest.OpMirrorSkills || c.Ref != project || c.Skills != skills || !slices.Equal(c.Pinned, []string{"lint"}) {
		t.Fatalf("call = %+v", c)
	}
	f.FailOp(aestudiotest.OpMirrorSkills, &sourcecontrol.CommitConflictError{})
	if _, err := f.MirrorSkills(ctx, project, skills, nil); !errors.Is(err, sourcecontrol.ErrCommitConflict) {
		t.Fatalf("injected: err = %v", err)
	}
}

// Commit records the author and the committer; an omitted committer is the
// author, as the pod defaults it.
func TestFake_CommitRecordsAuthorAndCommitter(t *testing.T) {
	f := aestudiotest.New()
	ctx := context.Background()
	f.SeedRepo(ref, nil)
	author := &sourcecontrol.GitIdentity{Name: "Ada", Email: "ada@example.com"}
	bot := &sourcecontrol.GitIdentity{Name: "AEP", Email: "aep@example.com"}
	if _, err := f.Commit(ctx, ref, sourcecontrol.CommitRequest{Writes: []sourcecontrol.FileWrite{{Path: "a.md", Content: "1"}}, Author: author, Committer: bot}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Commit(ctx, ref, sourcecontrol.CommitRequest{Writes: []sourcecontrol.FileWrite{{Path: "b.md", Content: "1"}}, Author: author}); err != nil {
		t.Fatal(err)
	}
	calls := f.Calls()
	if calls[0].Author != author || calls[0].Committer != bot {
		t.Fatalf("explicit committer: %+v", calls[0])
	}
	if calls[1].Author != author || calls[1].Committer != author {
		t.Fatalf("omitted committer must default to the author: %+v", calls[1])
	}
}

func TestFake_Turns(t *testing.T) {
	f := aestudiotest.New()
	f.ScriptTurn(
		aestudiotools.TurnEvent{Type: aestudiotools.EventTaskOp, Op: "plan"},
		aestudiotools.TurnEvent{Type: aestudiotools.EventKeepAlive},
	)
	seq, err := f.StartTurn(context.Background(), ref, aestudiotools.TurnRequest{TurnID: "t-1", Kind: aestudiotools.TurnKindPlan})
	if err != nil {
		t.Fatal(err)
	}
	var types []string
	for ev, err := range seq {
		if err != nil {
			t.Fatal(err)
		}
		types = append(types, ev.Type)
	}
	if !slices.Equal(types, []string{"task-op", "keep-alive", "result"}) {
		t.Fatalf("events = %v, want the script then a completed result", types)
	}
	if calls := f.TurnCalls(); len(calls) != 1 || calls[0].Ref != ref || calls[0].Request.TurnID != "t-1" {
		t.Fatalf("calls = %+v", calls)
	}
	f.FailOp(aestudiotest.OpStartTurn, aestudiotools.ErrTurnInProgress)
	if _, err := f.StartTurn(context.Background(), ref, aestudiotools.TurnRequest{}); !errors.Is(err, aestudiotools.ErrTurnInProgress) {
		t.Fatalf("err = %v", err)
	}
	if len(f.TurnCalls()) != 1 {
		t.Fatal("a failed start is not recorded")
	}
}

func TestFake_References(t *testing.T) {
	f := aestudiotest.New()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for _, n := range []string{"sketch.png", "brief.pdf"} {
		w, _ := mw.CreateFormFile("files", n)
		_, _ = io.WriteString(w, n)
	}
	_ = mw.Close()
	if err := f.PutReferences(context.Background(), ref, mw.FormDataContentType(), &buf); err != nil {
		t.Fatal(err)
	}
	// Stored per repository, whatever DefaultBranch the caller's ref carries.
	if got := f.References(sourcecontrol.RepoRef{Org: "default", Owner: "acme", Repo: "greeter", DefaultBranch: "main"}); !slices.Equal(got, []string{"sketch.png", "brief.pdf"}) {
		t.Fatalf("references = %v", got)
	}
	f.FailOp(aestudiotest.OpPutReferences, sourcecontrol.ErrReferenceRejected)
	if err := f.PutReferences(context.Background(), ref, mw.FormDataContentType(), &bytes.Buffer{}); !errors.Is(err, sourcecontrol.ErrReferenceRejected) {
		t.Fatalf("err = %v", err)
	}
	if got := f.References(ref); len(got) != 2 {
		t.Fatalf("a refused upload changed the set: %v", got)
	}
}
