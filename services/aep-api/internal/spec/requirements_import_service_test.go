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

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/platform/gitfs"
)

const validImportPRD = `# Expense — PRD

## Problem Statement
Employees submit expenses on paper and finance loses track.

## Solution
A web app for submitting and approving employee expenses.

## Actors
- Employee: submits and tracks expenses
- Manager: approves team expenses

## User Stories
1. As a Employee, I want to submit an expense, so that finance can reimburse me.
2. As a Manager, I want to approve expenses, so that policy is enforced.

## Product Decisions
- Sign-in via the company identity provider

## Out of Scope
- Payroll integration

## Open Questions
1. deferred — does not block design: multi-currency rates source
`

func reqImportCode(err error) string {
	var verr *RequirementsImportError
	if !errors.As(err, &verr) || len(verr.Issues) == 0 {
		return ""
	}
	return verr.Issues[0].Code
}

func TestExtractRequirementsTarball_Happy(t *testing.T) {
	t.Parallel()
	tgz := makeTarGz(t, map[string]string{
		"requirements/":                "",
		"requirements/prd.md":          validImportPRD,
		"requirements/domain-model.md": "# Domain\n",
	})
	files, warnings, err := extractRequirementsTarball(bytes.NewReader(tgz))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if files["prd.md"] == "" || files["domain-model.md"] == "" {
		t.Fatalf("files = %#v", files)
	}
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
}

func TestExtractRequirementsTarball_NestedPath(t *testing.T) {
	t.Parallel()
	tgz := makeTarGz(t, map[string]string{
		"requirements/":                "",
		"requirements/prd.md":          validImportPRD,
		"requirements/features/foo.md": "# nested",
	})
	_, _, err := extractRequirementsTarball(bytes.NewReader(tgz))
	if reqImportCode(err) != "NESTED_PATH" {
		t.Fatalf("err = %v (code %q), want NESTED_PATH", err, reqImportCode(err))
	}
}

func TestExtractRequirementsTarball_UnsupportedExt(t *testing.T) {
	t.Parallel()
	tgz := makeTarGz(t, map[string]string{
		"requirements/":          "",
		"requirements/prd.md":    validImportPRD,
		"requirements/notes.txt": "nope",
	})
	_, _, err := extractRequirementsTarball(bytes.NewReader(tgz))
	if reqImportCode(err) != "UNSUPPORTED_EXT" {
		t.Fatalf("err = %v (code %q), want UNSUPPORTED_EXT", err, reqImportCode(err))
	}
}

func TestExtractRequirementsTarball_UnsafeFilename(t *testing.T) {
	t.Parallel()
	tgz := makeTarGz(t, map[string]string{
		"requirements/":             "",
		"requirements/prd.md":       validImportPRD,
		"requirements/notes\x01.md": "control character in the name",
	})
	_, _, err := extractRequirementsTarball(bytes.NewReader(tgz))
	if reqImportCode(err) != "UNSAFE_ENTRY" {
		t.Fatalf("err = %v (code %q), want UNSAFE_ENTRY", err, reqImportCode(err))
	}
}

func TestExtractRequirementsTarball_SoftWarning(t *testing.T) {
	t.Parallel()
	// Build a payload just over the soft limit without hitting the hard cap.
	pad := strings.Repeat("x", requirementsImportSoftBytes+1024)
	tgz := makeTarGz(t, map[string]string{
		"requirements/":       "",
		"requirements/prd.md": validImportPRD + "\n\n## Further Notes\n" + pad + "\n",
	})
	files, warnings, err := extractRequirementsTarball(bytes.NewReader(tgz))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if files["prd.md"] == "" {
		t.Fatal("missing prd.md")
	}
	if len(warnings) == 0 || !strings.Contains(warnings[0], "soft limit") {
		t.Fatalf("warnings = %v, want soft-limit advisory", warnings)
	}
}

func TestGateRequirementsBundle(t *testing.T) {
	t.Parallel()
	if err := gateRequirementsBundle(map[string]string{}); reqImportCode(err) != "PRD_MISSING" {
		t.Fatalf("empty: %v", err)
	}
	if err := gateRequirementsBundle(map[string]string{"prd.md": "   "}); reqImportCode(err) != "PRD_MISSING" {
		t.Fatalf("blank: %v", err)
	}
	if err := gateRequirementsBundle(map[string]string{
		"prd.md": "# X — PRD\n\n## User Stories\n\nno numbers here\n",
	}); reqImportCode(err) != "MISSING_USER_STORIES" {
		t.Fatalf("no stories: %v", err)
	}
	if err := gateRequirementsBundle(map[string]string{"prd.md": validImportPRD}); err != nil {
		t.Fatalf("valid: %v", err)
	}
}

// fakeReqFiles is a small in-memory HEAD: `committed` is what Read/List see,
// and a successful Apply lands its writes there — real enough for
// resumeIfExactMatch's byte-comparison to mean something, unlike a
// hardcoded Read response that could never actually match an upload.
type fakeReqFiles struct {
	committed map[string]string // path -> content, simulates the tree at HEAD
	applied   *ApplyRequest
	applyErr  error
	commitSHA string
}

func (f *fakeReqFiles) List(_ context.Context, _, _, prefix string) ([]FileMeta, error) {
	metas := make([]FileMeta, 0, len(f.committed))
	for p, c := range f.committed {
		if strings.HasPrefix(p, prefix) {
			metas = append(metas, FileMeta{Path: p, SHA: "sha-" + p, Size: int64(len(c))})
		}
	}
	return metas, nil
}
func (f *fakeReqFiles) Read(_ context.Context, _, _, path string) (*FileContent, error) {
	if c, ok := f.committed[path]; ok {
		return &FileContent{Path: path, Content: c, SHA: "sha-" + path}, nil
	}
	return nil, ErrFileNotFound
}
func (f *fakeReqFiles) ReadAt(ctx context.Context, org, proj, path, _ string) (*FileContent, error) {
	return f.Read(ctx, org, proj, path)
}
func (f *fakeReqFiles) Bundle(context.Context, string, string, string, string) (*FileBundle, error) {
	return &FileBundle{}, nil
}
func (f *fakeReqFiles) Apply(_ context.Context, _, _ string, req ApplyRequest) (*ApplyResult, []Conflict, error) {
	f.applied = &req
	if f.applyErr != nil {
		return nil, nil, f.applyErr
	}
	sha := f.commitSHA
	if sha == "" {
		sha = "deadbeef"
	}
	if f.committed == nil {
		f.committed = map[string]string{}
	}
	out := &ApplyResult{CommitSHA: sha, Changed: true}
	for _, w := range req.Writes {
		f.committed[w.Path] = w.Content
		out.Files = append(out.Files, FileMeta{Path: w.Path, SHA: "blob", Size: int64(len(w.Content))})
	}
	return out, nil, nil
}
func (f *fakeReqFiles) PutReferences(context.Context, string, string, []gitfs.ReferenceDoc) error {
	return nil
}
func (f *fakeReqFiles) SetRegisteredResourceReader(RegisteredResourceReader) {}

func TestRequirementsImport_Happy(t *testing.T) {
	t.Parallel()
	files := &fakeReqFiles{}
	var saved *SaveRequest
	arts := &fakeArtifactSvc{
		SaveSpecFunc: func(_ context.Context, _, _ string, req SaveRequest) (*SpecSaveResult, error) {
			saved = &req
			return &SpecSaveResult{Status: SpecSaveApproved, Tag: "v1", CommitHash: req.CommitSHA}, nil
		},
	}
	svc := NewRequirementsImportService(files, arts, nil)
	tgz := makeTarGz(t, map[string]string{
		"bundle/":                "",
		"bundle/prd.md":          validImportPRD,
		"bundle/integrations.md": "# Integrations\n\n- Transactional email\n",
	})
	res, err := svc.Import(context.Background(), "org", "proj", "alice", bytes.NewReader(tgz))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if res.Tag != "v1" {
		t.Fatalf("result = %+v", res)
	}
	if len(res.Files) != 2 {
		t.Fatalf("files = %v", res.Files)
	}
	if files.applied == nil || len(files.applied.Writes) != 2 {
		t.Fatalf("apply writes = %+v", files.applied)
	}
	for _, w := range files.applied.Writes {
		if w.BaseSHA != "" {
			t.Fatalf("expected create-only baseSha \"\", got %q on %s", w.BaseSHA, w.Path)
		}
		if !strings.HasPrefix(w.Path, "specs/requirements/") {
			t.Fatalf("path %q not under specs/requirements/", w.Path)
		}
	}
	if saved == nil || saved.CommitSHA != "deadbeef" {
		t.Fatalf("save = %+v", saved)
	}
	if res.Warnings == nil {
		t.Fatal("Warnings must be non-nil")
	}
}

// A PRD with a version already cut is the genuine conflict — something has
// completed since, distinct from resumeIfExactMatch's "committed but never
// tagged" shape below.
func TestRequirementsImport_Exists(t *testing.T) {
	t.Parallel()
	files := &fakeReqFiles{committed: map[string]string{"specs/requirements/prd.md": "unrelated existing PRD"}}
	arts := &fakeArtifactSvc{
		ListSpecVersionTagsFunc: func(context.Context, string, string) (*TagList, error) {
			return &TagList{Tags: []string{"v1"}, Latest: "v1"}, nil
		},
	}
	svc := NewRequirementsImportService(files, arts, nil)
	tgz := makeTarGz(t, map[string]string{
		"bundle/":       "",
		"bundle/prd.md": validImportPRD,
	})
	_, err := svc.Import(context.Background(), "org", "proj", "alice", bytes.NewReader(tgz))
	if !errors.Is(err, ErrRequirementsExist) {
		t.Fatalf("err = %v, want ErrRequirementsExist", err)
	}
	if files.applied != nil {
		t.Fatal("Apply must not run when requirements already exist")
	}
}

// The absence of a tag is not, by itself, evidence of an abandoned import: a
// project mid-/start can carry a committed PRD with no version ever cut. Its
// content will not byte-match an unrelated upload, so it must be refused like
// any other existing PRD rather than "resumed" into an unwanted tag.
func TestRequirementsImport_ExistsTaglessButUnrelated(t *testing.T) {
	t.Parallel()
	files := &fakeReqFiles{committed: map[string]string{
		"specs/requirements/prd.md": "# Some other project's PRD, mid-/start, never built",
	}}
	arts := &fakeArtifactSvc{
		ListSpecVersionTagsFunc: func(context.Context, string, string) (*TagList, error) {
			return &TagList{}, nil // nothing has ever been tagged
		},
	}
	svc := NewRequirementsImportService(files, arts, nil)
	tgz := makeTarGz(t, map[string]string{
		"bundle/":       "",
		"bundle/prd.md": validImportPRD,
	})
	_, err := svc.Import(context.Background(), "org", "proj", "alice", bytes.NewReader(tgz))
	if !errors.Is(err, ErrRequirementsExist) {
		t.Fatalf("err = %v, want ErrRequirementsExist", err)
	}
	if files.applied != nil {
		t.Fatal("Apply must not run over an unrelated existing PRD")
	}
}

// The one recoverable shape of "requirements already exist": a prior Import
// committed the files (files.Apply succeeded) but SaveSpec's tag cut then
// failed. The retry must not be refused forever by the create-only gate — it
// resumes and completes the missing tag instead.
func TestRequirementsImport_ResumesAfterTagCutFailure(t *testing.T) {
	t.Parallel()
	files := &fakeReqFiles{}
	tagAttempts := 0
	arts := &fakeArtifactSvc{
		ListSpecVersionTagsFunc: func(context.Context, string, string) (*TagList, error) {
			return &TagList{}, nil // nothing has ever been tagged
		},
		SaveSpecFunc: func(_ context.Context, _, _ string, req SaveRequest) (*SpecSaveResult, error) {
			tagAttempts++
			if tagAttempts == 1 {
				return nil, errors.New("boom: transient tag-cut failure")
			}
			return &SpecSaveResult{Status: SpecSaveApproved, Tag: "v1", CommitHash: req.CommitSHA}, nil
		},
	}
	svc := NewRequirementsImportService(files, arts, nil)

	// First attempt: the commit lands, but cutting the tag fails.
	tgz := makeTarGz(t, map[string]string{
		"bundle/":       "",
		"bundle/prd.md": validImportPRD,
	})
	_, err := svc.Import(context.Background(), "org", "proj", "alice", bytes.NewReader(tgz))
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("first Import err = %v, want the tag-cut failure", err)
	}
	if files.applied == nil {
		t.Fatal("files.Apply should have committed before the tag cut failed")
	}

	// The retry re-uploads the SAME bundle: it byte-matches what Apply just
	// committed, and no tag is on record — it must resume, not refuse.
	tgz2 := makeTarGz(t, map[string]string{
		"bundle/":       "",
		"bundle/prd.md": validImportPRD,
	})
	res, err := svc.Import(context.Background(), "org", "proj", "alice", bytes.NewReader(tgz2))
	if err != nil {
		t.Fatalf("retry Import: %v", err)
	}
	if res.Tag != "v1" {
		t.Fatalf("result = %+v", res)
	}
	if len(res.Files) != 1 || res.Files[0] != "specs/requirements/prd.md" {
		t.Fatalf("resumed result files = %v", res.Files)
	}
	if tagAttempts != 2 {
		t.Fatalf("tagAttempts = %d, want 2", tagAttempts)
	}
}

func TestRequirementsImport_MissingStories(t *testing.T) {
	t.Parallel()
	files := &fakeReqFiles{}
	arts := &fakeArtifactSvc{}
	svc := NewRequirementsImportService(files, arts, nil)
	tgz := makeTarGz(t, map[string]string{
		"bundle/":       "",
		"bundle/prd.md": "# X — PRD\n\n## User Stories\n\nnothing numbered\n",
	})
	_, err := svc.Import(context.Background(), "org", "proj", "alice", bytes.NewReader(tgz))
	if reqImportCode(err) != "MISSING_USER_STORIES" {
		t.Fatalf("err = %v", err)
	}
}

type fakeImportTurns struct {
	newest *AgentTurn
	err    error
}

func (f *fakeImportTurns) Newest(context.Context, string, string) (*AgentTurn, error) {
	return f.newest, f.err
}

func TestRequirementsImport_RefusesWhileTurnRunning(t *testing.T) {
	t.Parallel()
	files := &fakeReqFiles{}
	arts := &fakeArtifactSvc{}
	turns := &fakeImportTurns{newest: &AgentTurn{Status: turnStatusRunning}}
	svc := NewRequirementsImportService(files, arts, turns)
	tgz := makeTarGz(t, map[string]string{
		"bundle/":       "",
		"bundle/prd.md": validImportPRD,
	})
	_, err := svc.Import(context.Background(), "org", "proj", "alice", bytes.NewReader(tgz))
	if !errors.Is(err, ErrRequirementsTurnActive) {
		t.Fatalf("err = %v, want ErrRequirementsTurnActive", err)
	}
	if files.applied != nil {
		t.Fatal("Apply must not run while a turn is active")
	}
}
