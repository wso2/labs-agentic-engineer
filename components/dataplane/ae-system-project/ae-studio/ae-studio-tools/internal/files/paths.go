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

// Moved from services/aep-api/internal/spec/internals.go; the aep-api copy is
// deleted in phase 4.

package files

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

// specsPrefix scopes the Files API: only paths under specs/ are readable
// through it, plus the read escape hatches below.
const specsPrefix = "specs/"

// commitSHAPattern is the accepted shape of a caller-provided commit
// (abbreviated or full hex object name).
var commitSHAPattern = regexp.MustCompile(`^[0-9a-fA-F]{7,64}$`)

// validatePath enforces the specs/ scope: repo-relative, canonical, no
// traversal, under specs/. Mirrors the artifacts working-tree validator so the
// two agree on what "a spec file" is.
func validatePath(p string) error {
	if p == "" {
		return fmt.Errorf("%w: empty path", ErrPathInvalid)
	}
	clean := path.Clean(p)
	if clean != p {
		return fmt.Errorf("%w: non-canonical path %q", ErrPathInvalid, p)
	}
	if strings.HasPrefix(clean, "/") || strings.HasPrefix(clean, "..") {
		return fmt.Errorf("%w: must be repo-relative under specs/", ErrPathInvalid)
	}
	if !strings.HasPrefix(clean, specsPrefix) {
		return fmt.Errorf("%w: only specs/ paths are accessible via this API", ErrPathInvalid)
	}
	for _, seg := range strings.Split(clean, "/") {
		if seg == ".." {
			return fmt.Errorf("%w: traversal in path", ErrPathInvalid)
		}
	}
	return nil
}

// readAllowList holds explicit non-specs/ paths the Files API may READ at HEAD.
// It is a read-only escape hatch: the validation report is a runner-authored
// artifact outside specs/, surfaced by the console's Validation page. The write
// path (Apply) is never widened — validatePath stays specs/-only.
var readAllowList = map[string]bool{
	"tests/acceptance/report.json": true,
}

// workloadReadRE is the second read escape hatch, and unlike the list above it
// has to be a SHAPE: a component's `workload.yaml` sits at its App Path, which is
// per-component design data rather than a literal this package could enumerate.
//
// It exists because the wiring-conformance check reads what a component actually
// shipped — "does the workload consume the resources the design declares?" — and
// that file is repo content outside specs/ by construction. Fenced to a single
// path segment, so it reaches a component directory and nothing deeper, and the
// canonical/traversal checks in validateReadPath run BEFORE it is consulted.
var workloadReadRE = regexp.MustCompile(`^(?:[A-Za-z0-9._-]+/)?workload\.yaml$`)

// validateReadPath gates the read side: a canonical, traversal-free path passes
// when it is an exact readAllowList entry or a component workload.yaml;
// otherwise it defers to the specs/-only validatePath.
//
// The shared checks come FIRST and are not optional. Exact-match was
// traversal-safe on its own — a path bearing ".." can never equal a literal key —
// but a PATTERN is not, so the escape hatches are only ever consulted for a path
// already known to be repo-relative and canonical.
func validateReadPath(p string) error {
	if p == "" {
		return fmt.Errorf("%w: empty path", ErrPathInvalid)
	}
	if clean := path.Clean(p); clean != p {
		return fmt.Errorf("%w: non-canonical path %q", ErrPathInvalid, p)
	}
	if strings.HasPrefix(p, "/") {
		return fmt.Errorf("%w: must be repo-relative", ErrPathInvalid)
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return fmt.Errorf("%w: traversal in path", ErrPathInvalid)
		}
	}
	if readAllowList[p] || workloadReadRE.MatchString(p) {
		return nil
	}
	return validatePath(p)
}

// validateCommit gates the commit a read may be pinned to. Empty means the branch
// tip. Anything else must be a hex object name (commitSHAPattern).
//
// Deliberately NOT an arbitrary revision expression: a read that accepted branch
// names, tags or `HEAD~3` would turn an allow-listed path into a browser over the
// repo's whole history. An object name can only be supplied by something that
// already knows it — in practice the platform's own cycle records.
func validateCommit(at string) error {
	if at == "" || commitSHAPattern.MatchString(at) {
		return nil
	}
	return fmt.Errorf("%w: commit must be a hex object name", ErrPathInvalid)
}
