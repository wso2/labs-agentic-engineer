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

package requirements

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/wso2/aep/aep-api/internal/feature/artifacts"
	"github.com/wso2/aep/aep-api/models"
)

// RequirementsService is the read + version surface for the multi-file
// requirements bundle stored at `specs/requirements/*.md`. The bundle is
// versioned together as `v<N>` tags (one bump per save). Reads and saves are
// GitHub-direct (docs/design/agents-generation-migration.md §5): there is no
// local working tree. Drafts live on the frontend and are committed via the
// generic Files API (`internal/feature/files`); this service only reads HEAD,
// cuts version tags, and reverts to them. Generation moved to the unified
// genai turn endpoint (`internal/feature/genai`).
type RequirementsService interface {
	GetRequirements(ctx context.Context, orgID, projectID string) (*models.RequirementsBundle, error)
	GetRequirementsAtTag(ctx context.Context, orgID, projectID, tag string) (*models.RequirementsBundle, error)
	// SaveAndProceed cuts the next `v<N>` tag. commitSHA, when non-empty, is the
	// commit the caller's files-apply just created — the save gates and tags that
	// exact commit instead of re-reading `heads/main` (whose reads lag writes).
	SaveAndProceed(ctx context.Context, orgID, projectID, commitSHA string) (*models.RequirementsBundle, error)
	DiscardChanges(ctx context.Context, orgID, projectID string) (*models.RequirementsBundle, error)
	ListVersions(ctx context.Context, orgID, projectID string) ([]models.ArtifactVersion, error)
}

// cycleHook is the optional development-cycle coupling: when requirements are
// approved (a v<N> tag cut) it starts the cycle's Temporal workflow. Wired
// post-construction via SetCycleHook; nil in tests and when orchestration is
// disabled (dev without Temporal), so the call site is always nil-guarded.
type cycleHook interface {
	OnRequirementsApproved(ctx context.Context, orgHandle, projectName, reqTag string) error
}

type requirementsService struct {
	store       *artifacts.ArtifactStore
	artifactSvc artifacts.ArtifactService
	cycle       cycleHook // optional; may be nil
}

func NewRequirementsService(
	store *artifacts.ArtifactStore,
	artifactSvc artifacts.ArtifactService,
) *requirementsService {
	return &requirementsService{
		store:       store,
		artifactSvc: artifactSvc,
	}
}

// SetCycleHook wires the optional development-cycle starter (see cycleHook).
func (s *requirementsService) SetCycleHook(h cycleHook) { s.cycle = h }

// GetRequirements returns the requirements file map at HEAD plus version
// metadata. An empty tree yields a draft with no files (the "no requirements
// yet" state). Has-unsaved-changes is computed by comparing the HEAD file map
// to the snapshot at the latest tag.
func (s *requirementsService) GetRequirements(ctx context.Context, orgID, projectID string) (*models.RequirementsBundle, error) {
	files, err := s.store.ListRequirements(ctx, orgID, projectID)
	if err != nil {
		return nil, fmt.Errorf("list requirements: %w", err)
	}

	out := &models.RequirementsBundle{
		ProjectID: projectID,
		Files:     files,
		Status:    "draft",
	}
	if len(files) == 0 {
		return out, nil
	}

	if s.artifactSvc == nil {
		return out, nil
	}

	versions, err := s.artifactSvc.ListRequirementsVersions(ctx, orgID, projectID)
	if err != nil {
		slog.WarnContext(ctx, "list requirements versions failed", "error", err)
		return out, nil
	}
	out.Versions = mapRequirementsVersions(versions)
	if len(versions) > 0 {
		out.Status = "approved"
		out.Version = versions[0].Version

		// Has-unsaved-changes: compare HEAD to the snapshot at the latest tag.
		tagged, err := s.artifactSvc.GetRequirementsAtTag(ctx, orgID, projectID, versions[0].Tag)
		if err == nil && !fileMapsEqual(tagged, files) {
			out.HasUnsavedChanges = true
		}
	}
	return out, nil
}

func (s *requirementsService) GetRequirementsAtTag(ctx context.Context, orgID, projectID, tag string) (*models.RequirementsBundle, error) {
	if s.artifactSvc == nil {
		return nil, fmt.Errorf("git client not configured")
	}
	files, err := s.artifactSvc.GetRequirementsAtTag(ctx, orgID, projectID, tag)
	if err != nil {
		if errors.Is(err, artifacts.ErrArtifactNotFound) {
			return nil, artifacts.ErrSpecNotFound
		}
		return nil, fmt.Errorf("get requirements at %s: %w", tag, err)
	}
	return &models.RequirementsBundle{
		ProjectID: projectID,
		Files:     files,
		Status:    "approved",
	}, nil
}

// SaveAndProceed cuts the next `v<N>` tag after the hard save gate passes
// (requirements.md must exist). No commit is created — the accepted draft is
// already committed to `main` via the Files API. With commitSHA set the gate +
// tag operate on that exact commit (the publish flow's just-applied commit);
// otherwise on freshly-resolved HEAD.
func (s *requirementsService) SaveAndProceed(ctx context.Context, orgID, projectID, commitSHA string) (*models.RequirementsBundle, error) {
	if s.artifactSvc == nil {
		return nil, fmt.Errorf("git client not configured")
	}
	res, err := s.artifactSvc.SaveRequirements(ctx, orgID, projectID, artifacts.SaveRequest{
		Message:   "Update requirements",
		CommitSHA: commitSHA,
	})
	if err != nil {
		return nil, fmt.Errorf("save requirements: %w", err)
	}
	slog.InfoContext(ctx, "requirements save completed",
		"project", projectID, "tag", res.Tag, "status", res.Status)

	// Start (idempotently) the development cycle for this requirement version.
	// Best-effort: a workflow-start failure must not fail the save.
	if s.cycle != nil && res.Tag != "" {
		if herr := s.cycle.OnRequirementsApproved(ctx, orgID, projectID, res.Tag); herr != nil {
			slog.WarnContext(ctx, "start development cycle after requirements save failed (non-fatal)",
				"project", projectID, "tag", res.Tag, "error", herr)
		}
	}
	return s.GetRequirements(ctx, orgID, projectID)
}

// DiscardChanges reverts the requirements subtree on `main` back to its content
// at the latest `v<N>` tag (a revert-commit; there is no working tree to
// reset). With no tag yet the subtree is reverted to empty (nothing approved).
func (s *requirementsService) DiscardChanges(ctx context.Context, orgID, projectID string) (*models.RequirementsBundle, error) {
	if s.artifactSvc == nil {
		return nil, fmt.Errorf("git client not configured")
	}
	if _, err := s.artifactSvc.DiscardRequirements(ctx, orgID, projectID); err != nil {
		return nil, fmt.Errorf("discard requirements: %w", err)
	}
	return s.GetRequirements(ctx, orgID, projectID)
}

func (s *requirementsService) ListVersions(ctx context.Context, orgID, projectID string) ([]models.ArtifactVersion, error) {
	if s.artifactSvc == nil {
		return nil, nil
	}
	versions, err := s.artifactSvc.ListRequirementsVersions(ctx, orgID, projectID)
	if err != nil {
		return nil, fmt.Errorf("list requirements versions: %w", err)
	}
	return mapRequirementsVersions(versions), nil
}

// fileMapsEqual compares two filename→content maps for byte-equality (after
// trimming surrounding whitespace, matching the save flow's unchanged
// detection).
func fileMapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, va := range a {
		vb, ok := b[k]
		if !ok {
			return false
		}
		if strings.TrimSpace(va) != strings.TrimSpace(vb) {
			return false
		}
	}
	return true
}
