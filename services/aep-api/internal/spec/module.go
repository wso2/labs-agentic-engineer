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

import "github.com/wso2/aep/aep-api/internal/sourcecontrol"

// Deps is what this domain must be handed to exist: typed ports / services,
// never concrete collaborators (§8). Constructor injection only.
//
// It lives in the domain ROOT, but the thing that CONSUMES it (the aggregator
// that builds the slice handlers) lives in httpapi/ — see httpapi/doc.go for why
// the domain's composition cannot sit here.
type Deps struct {
	// GenAI is the committed-truth turn orchestrator behind the five turn ops
	// (create / get / active / stream / rehydrate).
	GenAI *Service
	// Files is the spec-workspace read+apply service (list / read / apply).
	Files FilesService
	// FilesActivity records the spec_updated feed line when an apply commits
	// (issue #239). Optional — nil simply records nothing.
	FilesActivity SpecUpdatedRecorder
	// Artifacts is the spec-version tag reader (list-project-tags).
	Artifacts ArtifactService
	// Skills is the org-scoped skills catalogue reader (list / get / updates / sync).
	Skills *SkillService
	// SkillMut is the skills mutation service (create / update / delete).
	SkillMut *SkillMutationService
	// SkillImport is the AgentSkills-tarball import service.
	SkillImport *SkillImportService
	// RequirementsImport is the requirements-bundle tarball import service.
	RequirementsImport *RequirementsImportService
	// CollabRepo is the project-ownership oracle behind the two collab ops.
	CollabRepo sourcecontrol.RepoService
}
