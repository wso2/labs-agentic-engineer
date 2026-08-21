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
		"requirements/":             "",
		"requirements/prd.md":       validImportPRD,
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
		"requirements/":                    "",
		"requirements/prd.md":              validImportPRD,
		"requirements/features/foo.md":     "# nested",
	})
	_, _, err := extractRequirementsTarball(bytes.NewReader(tgz))
	if reqImportCode(err) != "NESTED_PATH" {
		t.Fatalf("err = %v (code %q), want NESTED_PATH", err, reqImportCode(err))
	}
}

func TestExtractRequirementsTarball_UnsupportedExt(t *testing.T) {
	t.Parallel()
	tgz := makeTarGz(t, map[string]string{
		"requirements/":        "",
		"requirements/prd.md":  validImportPRD,
		"requirements/notes.txt": "nope",
	})
	_, _, err := extractRequirementsTarball(bytes.NewReader(tgz))
	if reqImportCode(err) != "UNSUPPORTED_EXT" {
		t.Fatalf("err = %v (code %q), want UNSUPPORTED_EXT", err, reqImportCode(err))
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

type fakeReqFiles struct {
	existingPRD bool
	applied     *ApplyRequest
	applyErr    error
	commitSHA   string
}

func (f *fakeReqFiles) List(context.Context, string, string, string) ([]FileMeta, error) {
	return nil, nil
}
func (f *fakeReqFiles) Read(_ context.Context, _, _, path string) (*FileContent, error) {
	if strings.HasSuffix(path, "prd.md") {
		if f.existingPRD {
			return &FileContent{Path: path, Content: "x", SHA: "abc"}, nil
		}
		return nil, ErrFileNotFound
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
	out := &ApplyResult{CommitSHA: sha, Changed: true}
	for _, w := range req.Writes {
		out.Files = append(out.Files, FileMeta{Path: w.Path, SHA: "blob", Size: int64(len(w.Content))})
	}
	return out, nil, nil
}
func (f *fakeReqFiles) PutReferences(context.Context, string, string, []gitfs.ReferenceDoc) error {
	return nil
}

func TestRequirementsImport_Happy(t *testing.T) {
	t.Parallel()
	files := &fakeReqFiles{}
	var saved *SaveRequest
	arts := &fakeArtifactSvc{
		SaveRequirementsFunc: func(_ context.Context, _, _ string, req SaveRequest) (*RequirementsSaveResult, error) {
			saved = &req
			return &RequirementsSaveResult{Status: "approved", Tag: "v1", Version: 1, CommitHash: req.CommitSHA}, nil
		},
	}
	svc := NewRequirementsImportService(files, arts)
	tgz := makeTarGz(t, map[string]string{
		"bundle/":                "",
		"bundle/prd.md":          validImportPRD,
		"bundle/integrations.md": "# Integrations\n\n- Transactional email\n",
	})
	res, err := svc.Import(context.Background(), "org", "proj", "alice", bytes.NewReader(tgz))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if res.Tag != "v1" || res.Version != 1 {
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

func TestRequirementsImport_Exists(t *testing.T) {
	t.Parallel()
	files := &fakeReqFiles{existingPRD: true}
	arts := &fakeArtifactSvc{}
	svc := NewRequirementsImportService(files, arts)
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

func TestRequirementsImport_MissingStories(t *testing.T) {
	t.Parallel()
	files := &fakeReqFiles{}
	arts := &fakeArtifactSvc{}
	svc := NewRequirementsImportService(files, arts)
	tgz := makeTarGz(t, map[string]string{
		"bundle/":       "",
		"bundle/prd.md": "# X — PRD\n\n## User Stories\n\nnothing numbered\n",
	})
	_, err := svc.Import(context.Background(), "org", "proj", "alice", bytes.NewReader(tgz))
	if reqImportCode(err) != "MISSING_USER_STORIES" {
		t.Fatalf("err = %v", err)
	}
}
