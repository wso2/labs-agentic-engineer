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

package design

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/wso2/aep/aep-api/internal/feature/artifacts"
	"github.com/wso2/aep/aep-api/internal/feature/dependencies/resources"
	"github.com/wso2/aep/aep-api/models"
)

// Generic conditional skill attachment (Task G4,
// learning/thunder-resource/PLAN-generalization.md): a `platform-resource`
// dependency whose ClusterResourceType carries the PE-authored
// `aep.wso2.com/skill` annotation (resources.TypeMarkers.Skill) means the
// design needs that skill's agent instructions to work with the dependency —
// so design save ensures the skill name is present in the design's
// skillsApplied. Membership keys ONLY on the CRT annotation, never on a
// resourceType name or component type: any dependency kind carrying the
// marker qualifies, exactly like deriveEndUserAuth keys on the role label
// rather than a hardcoded name.
//
// This is append-only by design: skillsApplied may carry entries from other
// sources (generation-time skill selection, manual edits), and this pass must
// never remove or reorder them — it only ever adds a missing name, once.
// An unresolvable/unknown skill name is deliberately NOT validated here: it is
// attached as-authored on the CRT, and the downstream resolve layer
// (skills.SkillService.ResolveMany, read at execution time via
// execution.SkillsService.SkillsForExecution) already tolerates a missing
// skill name by warning and omitting it rather than failing — see
// skills/repo_store.go's ResolveMany. A PE typo in the annotation must not
// brick every save of every design that happens to depend on that type.

// attachAnnotatedSkills ensures every skill named by a platform-resource
// dependency's CRT skill annotation is present in designFile.SkillsApplied.
// Mutates designFile.SkillsApplied in place (append-only, de-duplicated) and
// reports whether it appended at least one new entry. A nil/empty markers map
// (no platform-resource dependency in the design, or none of its types carry
// the annotation) appends nothing.
func attachAnnotatedSkills(designFile *artifacts.DesignFile, markers map[string]resources.TypeMarkers) bool {
	present := make(map[string]struct{}, len(designFile.SkillsApplied))
	for _, name := range designFile.SkillsApplied {
		present[name] = struct{}{}
	}
	changed := false
	for i := range designFile.Components {
		for _, dep := range designFile.Components[i].Dependencies {
			if dep.Kind != models.DependencyKindPlatformResource {
				continue
			}
			skill := markers[dep.ResourceType].Skill
			if skill == "" {
				continue
			}
			if _, ok := present[skill]; ok {
				continue
			}
			present[skill] = struct{}{}
			designFile.SkillsApplied = append(designFile.SkillsApplied, skill)
			changed = true
		}
	}
	return changed
}

// persistSkillsApplied runs attachAnnotatedSkills over designFile and, when it
// appended at least one skill, commits the updated root design.md (the
// skillsApplied frontmatter lives there — see artifacts.SplitDesign/
// AssembleDesign, not per-component design.json) to main via the
// committed-truth write surface, mirroring persistEndUserAuthDerivation's
// render-CAS-commit shape for the derive_auth seam. It runs from
// SaveAndProceed in the SAME pass and over the SAME marker map
// resourceMarkersForAuthDerivation already fetched — no second catalog call.
//
// Returns (true, nil) when a commit landed — the caller must re-resolve HEAD
// (its designFile + any pinned commitSHA are now stale), the same convention
// the auto-fetch-on-save and auth-derivation steps already follow. A nil
// fileCommitter (degraded boot) is a best-effort no-op after a successful
// in-memory attach: designFile.SkillsApplied still reflects the addition for
// THIS response, but nothing is persisted.
func (s *designService) persistSkillsApplied(ctx context.Context, orgID, projectID string, designFile *artifacts.DesignFile, markers map[string]resources.TypeMarkers) (bool, error) {
	if !attachAnnotatedSkills(designFile, markers) {
		return false, nil
	}
	if s.fileCommitter == nil {
		return false, nil
	}

	rendered, rerr := artifacts.SplitDesign(&artifacts.DesignFile{
		Overview:      designFile.Overview,
		SourceSpec:    designFile.SourceSpec,
		SkillsApplied: designFile.SkillsApplied,
	})
	if rerr != nil {
		return false, fmt.Errorf("render design.md: %w", rerr)
	}
	content, ok := rendered[artifacts.DesignRootFile]
	if !ok {
		return false, fmt.Errorf("render design.md: %q missing from split", artifacts.DesignRootFile)
	}

	designFull := artifacts.DesignDir + "/" + artifacts.DesignRootFile
	_, sha, exists, rerr := s.fileCommitter.ReadFile(ctx, orgID, projectID, designFull)
	if rerr != nil {
		return false, fmt.Errorf("read %q for CAS: %w", designFull, rerr)
	}
	if !exists {
		return false, fmt.Errorf("%q missing on disk", designFull)
	}

	if err := s.fileCommitter.Commit(ctx, orgID, projectID,
		[]DesignFileWrite{{Path: designFull, Content: content, BaseSHA: sha}},
		"Attach CRT-annotated skills to the design"); err != nil {
		return false, err
	}
	slog.InfoContext(ctx, "design save: skill auto-attach persisted",
		"org", orgID, "project", projectID, "skillsApplied", designFile.SkillsApplied)
	return true, nil
}
