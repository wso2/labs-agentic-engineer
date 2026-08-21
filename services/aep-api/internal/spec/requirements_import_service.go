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
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path"
	"sort"
	"strings"
)

// Requirements import budgets. Soft warning (not a refusal) above the soft
// cap; hard refuse above the hard cap. Every admitted .md is inlined into
// every design turn with no truncation, so size is a correctness concern.
const (
	requirementsImportHardBytes = 256 * 1024
	requirementsImportSoftBytes = 64 * 1024
)

// ErrRequirementsExist — create-only import refused because
// specs/requirements/prd.md is already at HEAD. Maps to 409.
var ErrRequirementsExist = errors.New("requirements already exist")

// RequirementsImportIssue is one gate failure in a refused import.
type RequirementsImportIssue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Path    string `json:"path,omitempty"`
}

// RequirementsImportError carries one or more structured gate failures.
// Nothing is written when this is returned.
type RequirementsImportError struct {
	Issues []RequirementsImportIssue
}

func (e *RequirementsImportError) Error() string {
	parts := make([]string, 0, len(e.Issues))
	for _, i := range e.Issues {
		parts = append(parts, i.Code+": "+i.Message)
	}
	return "requirements import failed: " + strings.Join(parts, "; ")
}

func reqImportErr(code, message, path string) *RequirementsImportError {
	return &RequirementsImportError{Issues: []RequirementsImportIssue{{Code: code, Message: message, Path: path}}}
}

// RequirementsImportResult is the service response for a successful import.
type RequirementsImportResult struct {
	Files    []string
	Tag      string
	Version  int
	Warnings []string
}

// RequirementsImportService unpacks a requirements-bundle tarball, gates it,
// commits create-only under specs/requirements/, and cuts a requirements vN
// tag via SaveRequirements.
type RequirementsImportService struct {
	files     FilesService
	artifacts ArtifactService
}

// NewRequirementsImportService wires the import path. Either dep may be nil
// in degraded boot; Import then fails closed.
func NewRequirementsImportService(files FilesService, artifacts ArtifactService) *RequirementsImportService {
	return &RequirementsImportService{files: files, artifacts: artifacts}
}

// Import reads a gzip+tar requirements bundle, validates it, commits the
// flat files under specs/requirements/ (create-only), and cuts a
// requirements version tag. Returns RequirementsImportError for gate
// failures, ErrRequirementsExist when prd.md already exists, or the result.
func (s *RequirementsImportService) Import(ctx context.Context, orgID, projectID, actor string, r io.Reader) (*RequirementsImportResult, error) {
	if s == nil || s.files == nil || s.artifacts == nil {
		return nil, fmt.Errorf("requirements import service: not configured")
	}

	// Create-only: refuse before unpacking so an existing project never sees
	// a partial apply from a race with a second upload.
	if err := s.requireAbsentPRD(ctx, orgID, projectID); err != nil {
		return nil, err
	}

	files, warnings, err := extractRequirementsTarball(r)
	if err != nil {
		return nil, err
	}
	if err := gateRequirementsBundle(files); err != nil {
		return nil, err
	}

	writes := make([]WriteOp, 0, len(files))
	paths := make([]string, 0, len(files))
	for name, content := range files {
		p := path.Join(RequirementsDir, name)
		writes = append(writes, WriteOp{Path: p, Content: content, BaseSHA: ""})
		paths = append(paths, p)
	}
	sort.Strings(paths)

	msg := fmt.Sprintf("feat(requirements): import bundle\n\nby %s", actor)
	applied, conflicts, err := s.files.Apply(ctx, orgID, projectID, ApplyRequest{
		Writes:  writes,
		Message: msg,
	})
	if err != nil {
		if errors.Is(err, ErrApplyConflict) {
			// Another writer landed prd.md between our check and apply.
			for _, c := range conflicts {
				if c.Path == path.Join(RequirementsDir, requirementsMainFile) {
					return nil, ErrRequirementsExist
				}
			}
			return nil, reqImportErr("APPLY_CONFLICT",
				"one or more requirements paths already exist at HEAD", "")
		}
		return nil, fmt.Errorf("apply requirements import: %w", err)
	}

	save, err := s.artifacts.SaveRequirements(ctx, orgID, projectID, SaveRequest{
		CommitSHA: applied.CommitSHA,
		Message:   "import requirements bundle",
	})
	if err != nil {
		return nil, fmt.Errorf("tag requirements import: %w", err)
	}

	slog.InfoContext(ctx, "requirements imported",
		"orgID", orgID, "projectID", projectID, "actor", actor,
		"files", len(paths), "tag", save.Tag, "warnings", len(warnings))

	return &RequirementsImportResult{
		Files:    paths,
		Tag:      save.Tag,
		Version:  save.Version,
		Warnings: warnings,
	}, nil
}

func (s *RequirementsImportService) requireAbsentPRD(ctx context.Context, orgID, projectID string) error {
	_, err := s.files.Read(ctx, orgID, projectID, path.Join(RequirementsDir, requirementsMainFile))
	if err == nil {
		return ErrRequirementsExist
	}
	if errors.Is(err, ErrFileNotFound) {
		return nil
	}
	return fmt.Errorf("check existing requirements: %w", err)
}

// extractRequirementsTarball decodes a gzip+tar into a flat name→content map
// (paths relative to the single top-level directory). Enforces: one top-level
// dir, no symlinks/hardlinks/.., no nested paths, no dotfile segments, hard
// size budget. Soft size warning is returned alongside the files.
func extractRequirementsTarball(r io.Reader) (files map[string]string, warnings []string, err error) {
	gz, gerr := gzip.NewReader(r)
	if gerr != nil {
		return nil, nil, reqImportErr("TARBALL_INVALID", "not a valid gzip stream: "+gerr.Error(), "")
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	files = map[string]string{}
	warnings = []string{}
	limited := &byteBudget{remaining: requirementsImportHardBytes}
	var topDir string
	var total int

	for {
		hdr, terr := tr.Next()
		if terr == io.EOF {
			break
		}
		if terr != nil {
			return nil, nil, reqImportErr("TARBALL_INVALID", "read tar: "+terr.Error(), "")
		}

		switch hdr.Typeflag {
		case tar.TypeReg, tar.TypeDir:
			// ok
		case tar.TypeSymlink, tar.TypeLink:
			return nil, nil, reqImportErr("UNSAFE_ENTRY",
				fmt.Sprintf("symlinks/hardlinks are not allowed (%q)", hdr.Name), "")
		default:
			return nil, nil, reqImportErr("UNSAFE_ENTRY",
				fmt.Sprintf("unsupported tar entry type for %q", hdr.Name), "")
		}

		clean := path.Clean(hdr.Name)
		if clean == "." || clean == "/" {
			continue
		}
		if strings.HasPrefix(clean, "..") || strings.Contains(clean, "../") || path.IsAbs(clean) {
			return nil, nil, reqImportErr("UNSAFE_ENTRY",
				fmt.Sprintf("path traversal in %q", hdr.Name), "")
		}

		segments := strings.Split(clean, "/")
		if hasDotSegment(segments) {
			continue
		}

		if topDir == "" {
			topDir = segments[0]
		} else if segments[0] != topDir {
			return nil, nil, reqImportErr("MULTIPLE_TOP_DIRS",
				"tarball must contain exactly one top-level directory", "")
		}

		if hdr.Typeflag == tar.TypeDir {
			continue
		}

		rel := strings.Join(segments[1:], "/")
		if rel == "" {
			return nil, nil, reqImportErr("NESTED_PATH",
				fmt.Sprintf("file %q is not under the top-level directory", hdr.Name), hdr.Name)
		}
		if strings.Contains(rel, "/") {
			return nil, nil, reqImportErr("NESTED_PATH",
				fmt.Sprintf("requirements files must be flat (no subdirectories); got %q", rel), rel)
		}
		if !hasAllowedRequirementExt(rel) {
			return nil, nil, reqImportErr("UNSUPPORTED_EXT",
				fmt.Sprintf("unsupported requirements extension for %q (allowed: .md, .excalidraw, .dsl)", rel), rel)
		}

		b, rerr := limited.readAll(tr)
		if rerr != nil {
			var sve *SkillValidationError
			if errors.As(rerr, &sve) && len(sve.Issues) > 0 {
				i := sve.Issues[0]
				msg := i.Message
				if i.Code == "SIZE_EXCEEDED" {
					msg = fmt.Sprintf("decompressed payload exceeds the %d-byte limit", requirementsImportHardBytes)
				}
				return nil, nil, reqImportErr(i.Code, msg, i.Path)
			}
			return nil, nil, rerr
		}
		files[rel] = string(b)
		total += len(b)
	}

	if topDir == "" || len(files) == 0 {
		return nil, nil, reqImportErr("TARBALL_EMPTY", "tarball has no requirement files", "")
	}
	if total > requirementsImportSoftBytes {
		warnings = append(warnings, fmt.Sprintf(
			"decompressed payload is %d bytes (soft limit %d) — every requirements .md is inlined into every design turn",
			total, requirementsImportSoftBytes))
	}
	return files, warnings, nil
}

// gateRequirementsBundle enforces the PRD spine the build gate will demand.
func gateRequirementsBundle(files map[string]string) error {
	prd, ok := files[requirementsMainFile]
	if !ok || strings.TrimSpace(prd) == "" {
		return reqImportErr("PRD_MISSING",
			fmt.Sprintf("%s is required and must be non-empty", requirementsMainFile), requirementsMainFile)
	}
	if len(parsePRDStories(prd)) == 0 {
		return reqImportErr("MISSING_USER_STORIES",
			"the PRD yields no stories to cover — its `## User Stories` section must hold a numbered `N. As a …` list",
			requirementsMainFile)
	}
	return nil
}
