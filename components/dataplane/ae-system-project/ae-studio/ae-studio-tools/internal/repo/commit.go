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

package repo

import (
	"context"
	"crypto/sha1" //nolint:gosec // git object names are SHA-1 by definition
	"encoding/hex"
	"errors"
	"fmt"
)

// The baseSha write surface: one commit of writes
// and deletes, each pinned to the blob it expects. The precondition check is
// the one the Room's apply (files.Applier) also runs inside its own Mutate.

// CommitWrite is one file a Commit writes. BaseSHA "" means the path must not
// exist yet; otherwise it is the blob sha the path must have at the tip.
type CommitWrite struct {
	Path    string
	Content []byte
	BaseSHA string
}

// CommitDelete is one path a Commit deletes. BaseSHA "" means whatever is
// there, but the path must still exist.
type CommitDelete struct {
	Path    string
	BaseSHA string
}

// BlobSHA is the git blob object name of content: SHA-1 over
// "blob <len>\x00" + content, what `git hash-object` gives for the blobs a
// commit stages. A write's answer reports it so the caller's next baseSha
// matches what a later read returns.
func BlobSHA(content []byte) string {
	h := sha1.New() //nolint:gosec // git object names are SHA-1 by definition
	fmt.Fprintf(h, "blob %d\x00", len(content))
	h.Write(content)
	return hex.EncodeToString(h.Sum(nil))
}

// Conflict is one failed baseSha precondition. CurrentSHA is empty when the
// path does not exist at the tip.
type Conflict struct {
	Path, BaseSHA, CurrentSHA string
}

// CheckPreconditions compares each op's BaseSHA against current (path → blob
// sha of the tree a commit builds on) and returns every failure, writes
// first, in request order; nil when all hold. Content is not read.
func CheckPreconditions(current map[string]string, writes []CommitWrite, deletes []CommitDelete) []Conflict {
	var conflicts []Conflict
	for _, w := range writes {
		cur, exists := current[w.Path]
		if w.BaseSHA == "" {
			if exists {
				conflicts = append(conflicts, Conflict{Path: w.Path, BaseSHA: "", CurrentSHA: cur})
			}
			continue
		}
		if !exists || cur != w.BaseSHA {
			conflicts = append(conflicts, Conflict{Path: w.Path, BaseSHA: w.BaseSHA, CurrentSHA: cur})
		}
	}
	for _, d := range deletes {
		cur, exists := current[d.Path]
		if !exists {
			conflicts = append(conflicts, Conflict{Path: d.Path, BaseSHA: d.BaseSHA, CurrentSHA: ""})
			continue
		}
		if d.BaseSHA != "" && cur != d.BaseSHA {
			conflicts = append(conflicts, Conflict{Path: d.Path, BaseSHA: d.BaseSHA, CurrentSHA: cur})
		}
	}
	return conflicts
}

// BaseBlobs is snap's whole tree as path → blob sha: the input to
// CheckPreconditions.
func BaseBlobs(snap Snapshot) (map[string]string, error) {
	current := map[string]string{}
	err := snap.Walk("", func(rel, blobSHA string) error {
		current[rel] = blobSHA
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk base tree: %w", err)
	}
	return current, nil
}

// errCommitPreconditions aborts Commit's Mutate fn on a failed precondition:
// a fn error ends the CAS loop with no retry.
var errCommitPreconditions = errors.New("commit preconditions failed")

// Commit implements Workspace: Mutate (the one CAS retry loop, default
// policy) whose fn checks every baseSha against the tip that attempt builds
// on, then stages the writes and the deletes (writes first, so a path both
// written and deleted is deleted). A concurrent push re-runs the check
// against the new tip. committer nil is the author, as GitHub's API does
// when only an author is given; author nil is the AEP default identity.
//
// On a failed precondition it returns (CommitResult{}, conflicts,
// ErrCommitConflict) and nothing is applied. Content identical to the tip
// returns CommitResult{Changed: false} at the tip. Other errors are Mutate's
// (ErrRefNotFastForward after the retries, DiskFullError, a git failure).
func (e *Engine) Commit(ctx context.Context, ref RepoRef, writes []CommitWrite, deletes []CommitDelete, message string, author, committer *GitIdentity) (CommitResult, []Conflict, error) {
	if committer == nil {
		committer = author
	}
	var conflicts []Conflict
	res, err := e.Mutate(ctx, ref, func(tx Tx) error {
		conflicts = nil // fn re-runs against a fresh base on a CAS retry
		current, err := BaseBlobs(tx.Base())
		if err != nil {
			return err
		}
		if conflicts = CheckPreconditions(current, writes, deletes); len(conflicts) > 0 {
			return errCommitPreconditions
		}
		for _, w := range writes {
			tx.Write(w.Path, w.Content)
		}
		for _, d := range deletes {
			tx.Delete(d.Path)
		}
		return nil
	}, CommitOpts{Message: message, Author: author, Committer: committer})
	if errors.Is(err, errCommitPreconditions) {
		return CommitResult{}, conflicts, ErrCommitConflict
	}
	if err != nil {
		return CommitResult{}, nil, err
	}
	return res, nil, nil
}
