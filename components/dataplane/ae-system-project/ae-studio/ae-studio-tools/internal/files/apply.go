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

// Moved from services/aep-api/internal/spec/files_service.go (Apply);
// the aep-api copy is deleted in phase 4.

package files

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/wso2/aep/ae-studio-tools/internal/repo"
)

const (
	// maxFileBytes caps a single written file. specs/ artifacts (markdown,
	// DSL, component design.json, rendered excalidraw scenes) stay well under
	// this.
	maxFileBytes = 5 << 20 // 5 MiB
	// casAttempts bounds Mutate's fast-forward retry on a concurrent writer.
	casAttempts = 4
)

var (
	// ErrApplyConflict means one or more baseSha preconditions failed. The
	// caller answers 409 with the returned conflicts; nothing was applied.
	ErrApplyConflict = errors.New("apply conflict: stale baseSha")
	// errConflictSentinel short-circuits Mutate's CAS retry loop on a
	// precondition failure (distinct from a non-fast-forward, which retries).
	errConflictSentinel = errors.New("precondition conflict")
)

// PathRefusedError is a write-rule refusal of one path of an apply. It wraps
// ErrPathInvalid; Path is the refused path, which the Files socket names in
// its 400 so the Room can set that path aside and save the rest.
type PathRefusedError struct {
	Path string
	Err  error
}

func (e *PathRefusedError) Error() string { return e.Err.Error() }

func (e *PathRefusedError) Unwrap() error { return e.Err }

// refusePath wraps a write-rule error with the path it refuses.
func refusePath(p string, err error) error {
	return &PathRefusedError{Path: p, Err: err}
}

// WriteOp is one file write. BaseSHA empty means "must not exist yet".
type WriteOp struct {
	Path, Content, BaseSHA string
}

// DeleteOp is one file delete. BaseSHA empty means "whatever is there", but
// the path must still exist.
type DeleteOp struct {
	Path, BaseSHA string
}

// ApplyRequest is one atomic save: every write and delete lands as one
// commit, or none does. Message is appended to the commit subject; the Room
// puts its Co-authored-by trailers there.
type ApplyRequest struct {
	Writes  []WriteOp
	Deletes []DeleteOp
	Message string
}

// Warning is a non-blocking note on a file of the save: soft validation, a
// scaffold, a security-design notice or a dependency completion.
type Warning struct {
	Path, Code, Message string
}

// ApplyResult is a successful apply. Changed is false when the save was
// byte-identical to the tip: no commit was made and CommitSHA is the
// unchanged tip. Files carries the blob sha of every file written, the
// caller's next baseSha for it.
type ApplyResult struct {
	CommitSHA string
	Changed   bool
	Files     []Meta
	Warnings  []Warning
}

// Conflict is one failed baseSha precondition (repo.CheckPreconditions).
// CurrentSHA is empty when the path does not exist at the tip.
type Conflict = repo.Conflict

// Applier is the Files write path: the one atomic apply, over the same
// project resolution and engine as the reads. Completer and Identity are
// required.
type Applier struct {
	Reader Reader
	// Completer completes dependency stubs on aep-api's side of the CP/DP
	// seam.
	Completer Completer
	// Identity names the gitpat user every commit is authored by.
	Identity IdentitySource
}

// Apply commits req to project's default branch as one commit, pushed under
// `--force-with-lease`. Every precondition is checked against the tree the
// commit builds on, inside the engine's CAS loop: a concurrent push re-runs
// the checks against the new tip, and a failed check aborts with
// (nil, conflicts, ErrApplyConflict), nothing applied.
//
// Before the commit, dependency stubs are completed through Completer (never
// inside the CAS-retried function) and the completions land in the same
// commit; a design.cell in the batch scaffolds the missing component
// design.json skeletons; soft validation and the security design's coverage
// notices become Warnings. Commits are not gated by any of them.
//
// Errors: ErrPathInvalid for a request the write rules refuse (a
// *PathRefusedError when one path is the cause), the
// projects lookup's errors (projects.ErrUnknown also when the completions
// call answers 404 for the project), ErrApplyConflict, or a *RepoError from the engine
// (wrapping repo.ErrDiskFull, repo.ErrRefNotFastForward after the retries, or
// a git failure).
func (a Applier) Apply(ctx context.Context, project string, req ApplyRequest) (*ApplyResult, []Conflict, error) {
	// Path and size validation happens once, before any git operation: a bad
	// path is a 400, never a partial commit.
	seen, err := validateApply(req)
	if err != nil {
		return nil, nil, err
	}
	ref, err := a.Reader.repoRef(ctx, project)
	if err != nil {
		return nil, nil, err
	}
	author, committer := a.saveIdentities(ctx)

	completions, completionWarnings, err := a.completeDependencies(ctx, project, req.Writes)
	if err != nil {
		return nil, nil, err
	}
	// A document the platform lands beside a definition is platform-authored:
	// the request must not also write or delete it. Deletes are applied after
	// writes, so a request that deleted one would commit a definition pointing
	// at a document the same commit removed.
	for _, c := range completions {
		for _, p := range sortedPaths(c.Files) {
			if seen[p] {
				return nil, nil, refusePath(p, fmt.Errorf("%w: %s is written by the platform for this dependency and cannot be written or deleted in the same request", ErrPathInvalid, p))
			}
		}
	}

	preWrites, preDeletes := preconditionsOf(req)
	var conflicts []Conflict
	var files []Meta
	var warnings []Warning
	res, err := a.Reader.Engine.Mutate(ctx, ref, func(tx repo.Tx) error {
		// fn re-runs against a fresh base on a CAS retry: start clean.
		conflicts, files, warnings = nil, nil, append([]Warning(nil), completionWarnings...)

		// The committed base tree this attempt builds on: path → blob sha,
		// the input to every per-file baseSha precondition.
		current, werr := repo.BaseBlobs(tx.Base())
		if werr != nil {
			return werr
		}

		conflicts = repo.CheckPreconditions(current, preWrites, preDeletes)
		if len(conflicts) > 0 {
			return errConflictSentinel // fn error aborts Mutate: no retry, the 409 path
		}

		batch := map[string]bool{}
		for _, w := range req.Writes {
			content := w.Content
			if c, completed := completions[w.Path]; completed {
				// The completion replaces the stub; its documents land in the
				// same commit, platform-authored, beside the file.
				content = c.Definition
				for _, p := range sortedPaths(c.Files) {
					tx.Write(p, []byte(c.Files[p]))
					batch[p] = true
					files = append(files, Meta{Path: p, SHA: repo.BlobSHA([]byte(c.Files[p]))})
				}
			}
			tx.Write(w.Path, []byte(content))
			batch[w.Path] = true
			// The staged blob's object name is a pure function of its content
			// (what `git hash-object` produces), so the result carries the
			// exact sha a later read returns.
			files = append(files, Meta{Path: w.Path, SHA: repo.BlobSHA([]byte(content))})
			warnings = append(warnings, softValidate(w.Path, content)...)
		}
		for _, d := range req.Deletes {
			tx.Delete(d.Path)
		}
		// Scaffold engine: a batch that lands design.cell also lands a
		// design.json skeleton for every deployable component the cell
		// declares that has none yet, in the same commit. It rides the
		// warnings channel so the caller sees what was generated.
		batchContent := map[string]string{}
		for _, w := range req.Writes {
			batchContent[w.Path] = w.Content
		}
		if cellSource, cellInBatch := batchContent[DesignCellPath]; cellInBatch {
			scaffolds := scaffoldFromCell(cellSource, func(path string) bool {
				_, inTree := current[path]
				return inTree || batch[path]
			})
			for _, path := range sortedPaths(scaffolds) {
				content := scaffolds[path]
				tx.Write(path, []byte(content))
				batchContent[path] = content
				files = append(files, Meta{Path: path, SHA: repo.BlobSHA([]byte(content))})
				warnings = append(warnings, Warning{Path: path, Message: "scaffolded from design.cell — enrich, don't author, the mechanical fields"})
			}
		}
		// The security design's coverage notices need the whole design bundle
		// (the committed tree plus what this batch lands), which this is the
		// first place to hold.
		deleted := map[string]bool{}
		for _, d := range req.Deletes {
			deleted[d.Path] = true
		}
		warnings = append(warnings, securityDesignNotices(tx.Base(), current, batchContent, deleted)...)
		return nil
	}, repo.CommitOpts{
		Message:   applyMessage(req.Message),
		Author:    author,
		Committer: committer,
		Retry:     repo.RetryPolicy{Attempts: casAttempts},
	})
	if errors.Is(err, errConflictSentinel) {
		return nil, conflicts, ErrApplyConflict
	}
	if err != nil {
		return nil, nil, repoError(ref, fmt.Errorf("apply: %w", err))
	}
	return &ApplyResult{CommitSHA: res.CommitSHA, Changed: res.Changed, Files: files, Warnings: warnings}, nil, nil
}

// preconditionsOf is req's baseSha expectations in the engine's terms (the
// check reads paths and shas only, never content).
func preconditionsOf(req ApplyRequest) ([]repo.CommitWrite, []repo.CommitDelete) {
	writes := make([]repo.CommitWrite, len(req.Writes))
	for i, w := range req.Writes {
		writes[i] = repo.CommitWrite{Path: w.Path, BaseSHA: w.BaseSHA}
	}
	deletes := make([]repo.CommitDelete, len(req.Deletes))
	for i, d := range req.Deletes {
		deletes[i] = repo.CommitDelete{Path: d.Path, BaseSHA: d.BaseSHA}
	}
	return writes, deletes
}

// validateApply applies the write rules to the whole request and returns
// every path it names: specs/-only, canonical, at most maxFileBytes per
// write, no path twice, never both written and deleted, and not empty.
func validateApply(req ApplyRequest) (map[string]bool, error) {
	if len(req.Writes) == 0 && len(req.Deletes) == 0 {
		return nil, fmt.Errorf("%w: empty apply (no writes or deletes)", ErrPathInvalid)
	}
	seen := map[string]bool{}
	for _, w := range req.Writes {
		if err := validatePath(w.Path); err != nil {
			return nil, refusePath(w.Path, err)
		}
		if len(w.Content) > maxFileBytes {
			return nil, refusePath(w.Path, fmt.Errorf("%w: %s exceeds %d bytes", ErrPathInvalid, w.Path, maxFileBytes))
		}
		if seen[w.Path] {
			return nil, refusePath(w.Path, fmt.Errorf("%w: %s appears more than once", ErrPathInvalid, w.Path))
		}
		seen[w.Path] = true
	}
	for _, d := range req.Deletes {
		if err := validatePath(d.Path); err != nil {
			return nil, refusePath(d.Path, err)
		}
		if seen[d.Path] {
			return nil, refusePath(d.Path, fmt.Errorf("%w: %s is both written and deleted", ErrPathInvalid, d.Path))
		}
		seen[d.Path] = true
	}
	return seen, nil
}

// applyMessage is the commit message: a fixed subject, then the caller's
// suffix (which may carry trailers).
func applyMessage(suffix string) string {
	const base = "aep: apply file changes"
	if strings.TrimSpace(suffix) == "" {
		return base
	}
	return base + ": " + suffix
}
