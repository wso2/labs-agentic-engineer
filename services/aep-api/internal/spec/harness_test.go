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

// Shared harness for the artifact tests: the REAL ArtifactService over the
// in-memory AE Studio pod (aestudiotest.Fake), whose default branch tip IS
// the draft "working tree" (arranged with r.seed / r.tag). Only the
// repository row is faked besides: a single in-memory GitRepository naming a
// GitHub repository (RefForRow needs owner/repo). save→tag and the reads at
// HEAD / at a tag / at a sha therefore run end-to-end over git's object
// semantics (blob and commit shas, annotated tags), offline.
//
// The Fake's BeforeTag / BeforeCommit hooks are the race-injection seams: a
// test acts right before a Tag or Commit lands (the tag-collision and
// baseSha windows).

import (
	"context"
	"crypto/sha1" //nolint:gosec // git object names are SHA-1 by definition
	"encoding/hex"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools/aestudiotest"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// ----- faked edge: the RepoRepository row -----

// stubRepoRepo returns one fixed GitRepository row.
type stubRepoRepo struct{ rec *sourcecontrol.GitRepository }

var _ sourcecontrol.RepoRepository = (*stubRepoRepo)(nil)

func (s *stubRepoRepo) GetByOrgAndProjectID(context.Context, string, string) (*sourcecontrol.GitRepository, error) {
	return s.rec, nil
}
func (s *stubRepoRepo) FindInOrgByFullName(context.Context, string, string) (*sourcecontrol.GitRepository, error) {
	return nil, nil
}

func (s *stubRepoRepo) GetByOrgAndSlug(context.Context, string, string) (*sourcecontrol.GitRepository, error) {
	return nil, sourcecontrol.ErrRepoNotFound
}
func (s *stubRepoRepo) ListAllReady(context.Context) ([]sourcecontrol.GitRepository, error) {
	return nil, nil
}
func (s *stubRepoRepo) SetWebhookIDIfReady(context.Context, string, string, int64) (bool, error) {
	panic("not used")
}
func (s *stubRepoRepo) ClearWebhookIDs(context.Context, string) error { panic("not used") }
func (s *stubRepoRepo) SetStatusIf(context.Context, string, string, string, string) (bool, error) {
	panic("not used")
}
func (s *stubRepoRepo) ListByOrg(context.Context, string) ([]sourcecontrol.GitRepository, error) {
	panic("stubRepoRepo: ListByOrg not expected in artifacts tests")
}
func (s *stubRepoRepo) Create(context.Context, *sourcecontrol.GitRepository) error { return nil }
func (s *stubRepoRepo) Update(context.Context, *sourcecontrol.GitRepository) error { return nil }
func (s *stubRepoRepo) DeleteByOrgAndProjectID(context.Context, string, string) error {
	return nil
}

// ----- rig -----

type rig struct {
	t    *testing.T
	svc  ArtifactService
	pod  *aestudiotest.Fake
	rec  *sourcecontrol.GitRepository
	org  string
	proj string
}

var idSanitize = regexp.MustCompile(`[^A-Za-z0-9_-]`)

// idsFor derives a unique (orgID, projectID) from the test name.
func idsFor(t *testing.T) (string, string) {
	safe := idSanitize.ReplaceAllString(t.Name(), "-")
	return "org-" + safe, "proj-" + safe
}

// newRig seeds the repository's default branch with `seed` (repo-relative
// path → content) as the initial draft and wires the artifact service over
// the pod.
func newRig(t *testing.T, seed map[string]string) *rig {
	t.Helper()
	org, proj := idsFor(t)
	rec := &sourcecontrol.GitRepository{
		OrgID:         org,
		ProjectID:     proj,
		RepoURL:       "https://github.com/acme/widgets",
		RepoSlug:      "acme-widgets",
		DefaultBranch: "main",
		Status:        "ready",
	}
	pod := aestudiotest.New()
	r := &rig{t: t, pod: pod, rec: rec, org: org, proj: proj}
	pod.SeedRepo(r.repoRef(), seed)
	r.svc = NewArtifactService(&stubRepoRepo{rec: rec}, pod)
	return r
}

// ----- arrange / assert helpers (against the branch tip = the draft) -----

// commitAtTip commits writes and deletes on ref's tip in one commit, each
// under the path's current blob sha (an external writer's commit), and
// answers the new tip.
func commitAtTip(t *testing.T, pod *aestudiotest.Fake, ref sourcecontrol.RepoRef, writes map[string]string, deletes []string, msg string) string {
	t.Helper()
	ctx := context.Background()
	entries, _, err := pod.List(ctx, ref, "")
	if err != nil {
		t.Fatalf("commit %q: %v", msg, err)
	}
	current := map[string]string{}
	for _, e := range entries {
		current[e.Path] = e.SHA
	}
	req := sourcecontrol.CommitRequest{Message: msg}
	for _, p := range slices.Sorted(maps.Keys(writes)) {
		req.Writes = append(req.Writes, sourcecontrol.FileWrite{Path: p, Content: writes[p], BaseSHA: current[p]})
	}
	for _, p := range deletes {
		req.Deletes = append(req.Deletes, sourcecontrol.FileDelete{Path: p, BaseSHA: current[p]})
	}
	res, err := pod.Commit(ctx, ref, req)
	if err != nil {
		t.Fatalf("commit %q: %v", msg, err)
	}
	return res.CommitSHA
}

// seed advances the default branch with the given files (a new draft
// commit) and answers the new tip.
func (r *rig) seed(files map[string]string, msg string) string {
	r.t.Helper()
	return commitAtTip(r.t, r.pod, r.repoRef(), files, nil, msg)
}

// remove deletes paths from the default branch in one commit.
func (r *rig) remove(msg string, paths ...string) {
	r.t.Helper()
	commitAtTip(r.t, r.pod, r.repoRef(), nil, paths, msg)
}

// tag creates an annotated tag on the current tip.
func (r *rig) tag(name, msg string) {
	r.t.Helper()
	if err := r.pod.Tag(context.Background(), r.repoRef(), sourcecontrol.TagSpec{Name: name, Message: msg}); err != nil {
		r.t.Fatalf("tag %s: %v", name, err)
	}
}

// tags lists the repository's tag names, sorted.
func (r *rig) tags() []string {
	r.t.Helper()
	infos, err := r.pod.ListTags(context.Background(), r.repoRef(), "")
	if err != nil {
		r.t.Fatalf("tags: %v", err)
	}
	names := make([]string, 0, len(infos))
	for _, ti := range infos {
		names = append(names, ti.Name)
	}
	return names
}

// tagCommit is the commit tag name points at.
func (r *rig) tagCommit(name string) string {
	r.t.Helper()
	sha, err := r.pod.Head(context.Background(), r.repoRef(), "tags/"+name)
	if err != nil {
		r.t.Fatalf("tag %s: %v", name, err)
	}
	return sha
}

// headSHA is the default branch tip.
func (r *rig) headSHA() string {
	r.t.Helper()
	sha, err := r.pod.Head(context.Background(), r.repoRef(), "")
	if err != nil {
		r.t.Fatalf("head: %v", err)
	}
	return sha
}

// fileAt is path's content at the default branch tip.
func (r *rig) fileAt(path string) string {
	r.t.Helper()
	content, _, err := r.pod.ReadFile(context.Background(), r.repoRef(), "", path)
	if err != nil {
		r.t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}

// blobSHA is git's blob object name of content — what the pod reports as a
// file's sha.
func blobSHA(content []byte) string {
	h := sha1.New() //nolint:gosec // git object names are SHA-1 by definition
	fmt.Fprintf(h, "blob %d\x00", len(content))
	h.Write(content)
	return hex.EncodeToString(h.Sum(nil))
}

// validComponentDesignJSON is a component design.json that satisfies the design
// schema gate — the shared seed for every save/read test.
func validComponentDesignJSON(name string) string {
	return `{"name":"` + name + `","type":"service","version":"1.0.0","language":"go",` +
		`"buildpack":"go","appPath":".","entrypoint":"main.go","exposure":"internet",` +
		`"stories":[1],"dependencies":[],"description":"a service"}`
}

// memRepos is a project-repository table holding one ready row: org's
// project p, at repository url. Any other org or project has no row.
func memRepos(t *testing.T, org, project, url string) sourcecontrol.RepoRepository {
	t.Helper()
	return &orgScopedRepoRepo{stubRepoRepo: stubRepoRepo{rec: &sourcecontrol.GitRepository{
		OrgID: org, ProjectID: project, RepoURL: url, DefaultBranch: "main", Status: "ready",
	}}}
}

// orgScopedRepoRepo answers its row only for the row's own org and project.
type orgScopedRepoRepo struct{ stubRepoRepo }

func (s *orgScopedRepoRepo) GetByOrgAndProjectID(_ context.Context, org, project string) (*sourcecontrol.GitRepository, error) {
	if org != s.rec.OrgID || project != s.rec.ProjectID {
		return nil, nil
	}
	return s.rec, nil
}

// repoRef is the repository the service addresses for the row.
func (r *rig) repoRef() sourcecontrol.RepoRef {
	r.t.Helper()
	return rowRef(r.t, r.org, r.rec)
}
