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

package agentfold

// manifest.go — the D14 integrity gate. The agents service appends ONE
// terminal manifest frame per successful turn (before [DONE]), built from its
// authoritative FileBundle: `files` maps every path mutated this turn and
// still present to the sha256 (lowercase hex, over the UTF-8 bytes) of its
// LF-canonical final content; `deleted` lists the mutated paths no longer
// present; both sorted. Verify recomputes the same view from the Go fold —
// any difference is fold drift and the turn must fail with no commit. An
// ABSENT manifest (severed stream) also means no commit; an EMPTY manifest is
// a valid chat/task-plan turn (commit nothing, complete normally).

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// Manifest is the terminal manifest part's payload.
type Manifest struct {
	// Files maps each mutated, still-present path to the sha256 hex of its
	// final content.
	Files map[string]string
	// Deleted lists the mutated paths absent at turn end.
	Deleted []string
	// Usage is the turn's token spend (#249); nil for pre-capture agents.
	Usage *TurnUsage
}

// ManifestOf extracts the manifest from a parsed StreamPart. ok is false for
// any other part type.
func ManifestOf(part StreamPart) (Manifest, bool) {
	if part.Type != "manifest" {
		return Manifest{}, false
	}
	m := Manifest{Files: part.Files, Deleted: part.Deleted, Usage: part.Usage}
	if m.Files == nil {
		m.Files = map[string]string{}
	}
	return m, true
}

// IsEmpty reports a no-file-ops turn (chat / task-plan): valid, commits nothing.
func (m Manifest) IsEmpty() bool {
	return len(m.Files) == 0 && len(m.Deleted) == 0
}

// FoldParityError carries the exact paths on which the Go fold and the
// agents-side FileBundle disagree. Any instance means the turn fails loudly
// and main stays untouched (D14).
type FoldParityError struct {
	// MissingInFold: the manifest lists the path but the fold has no
	// surviving mutation for it.
	MissingInFold []string
	// ExtraInFold: the fold mutated the path but the manifest does not list it.
	ExtraInFold []string
	// HashMismatch: both sides have the path but the content hashes differ.
	HashMismatch []string
	// DeletedMismatch: the two deleted sets differ on these paths.
	DeletedMismatch []string
}

func (e *FoldParityError) Error() string {
	var parts []string
	add := func(label string, paths []string) {
		if len(paths) > 0 {
			parts = append(parts, fmt.Sprintf("%s: %s", label, strings.Join(paths, ", ")))
		}
	}
	add("missing in fold", e.MissingInFold)
	add("extra in fold", e.ExtraInFold)
	add("hash mismatch", e.HashMismatch)
	add("deleted mismatch", e.DeletedMismatch)
	return "agentfold: fold-parity failure — " + strings.Join(parts, "; ")
}

// Verify checks the Go fold's touched state against the manifest. nil means
// the two folds agree byte-for-byte on every mutated path (including that
// nothing was mutated, for an empty manifest).
func Verify(f *Fold, m Manifest) error {
	foldFiles := map[string]string{}
	foldDeleted := map[string]bool{}
	for path, content := range f.Touched() {
		if content == nil {
			foldDeleted[path] = true
		} else {
			foldFiles[path] = sha256Hex(*content)
		}
	}

	perr := &FoldParityError{}
	for path, wantHash := range m.Files {
		gotHash, ok := foldFiles[path]
		switch {
		case !ok:
			perr.MissingInFold = append(perr.MissingInFold, path)
		case gotHash != wantHash:
			perr.HashMismatch = append(perr.HashMismatch, path)
		}
	}
	for path := range foldFiles {
		if _, ok := m.Files[path]; !ok {
			perr.ExtraInFold = append(perr.ExtraInFold, path)
		}
	}
	manifestDeleted := map[string]bool{}
	for _, path := range m.Deleted {
		manifestDeleted[path] = true
		if !foldDeleted[path] {
			perr.DeletedMismatch = append(perr.DeletedMismatch, path)
		}
	}
	for path := range foldDeleted {
		if !manifestDeleted[path] {
			perr.DeletedMismatch = append(perr.DeletedMismatch, path)
		}
	}

	sort.Strings(perr.MissingInFold)
	sort.Strings(perr.ExtraInFold)
	sort.Strings(perr.HashMismatch)
	sort.Strings(perr.DeletedMismatch)
	if len(perr.MissingInFold)+len(perr.ExtraInFold)+len(perr.HashMismatch)+len(perr.DeletedMismatch) > 0 {
		return perr
	}
	return nil
}

// sha256Hex matches services/agents src/shared/hash.ts sha256Hex: sha256 over
// the UTF-8 bytes, lowercase hex.
func sha256Hex(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}
