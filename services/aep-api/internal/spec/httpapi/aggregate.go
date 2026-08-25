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

package httpapi

import (
	"github.com/wso2/aep/aep-api/internal/spec"
	"github.com/wso2/aep/aep-api/internal/spec/collab"
	"github.com/wso2/aep/aep-api/internal/spec/files"
	"github.com/wso2/aep/aep-api/internal/spec/genaiturns"
	"github.com/wso2/aep/aep-api/internal/spec/skills"
	"github.com/wso2/aep/aep-api/internal/spec/tags"
)

// Every slice names its type Handler, so embedding them directly would be
// "Handler redeclared". Local aliases give distinct field names (§6).
type (
	genaiturnsHandler = genaiturns.Handler
	filesHandler      = files.Handler
	tagsHandler       = tags.Handler
	skillsHandler     = skills.Handler
	collabHandler     = collab.Handler
)

// Handlers is the spec domain's slice handlers, embedded so Go promotes each
// operation exactly once into the edge's composite. It declares nothing.
type Handlers struct {
	*genaiturnsHandler
	*filesHandler
	*tagsHandler
	*skillsHandler
	*collabHandler
}

// New assembles the domain: pure wiring, constructor injection only.
//
// spec is fail-LOUD: the pre-migration handlers had no nil guard, so an
// unwired collaborator panics exactly as it did before (the edge assigns each
// dep directly, no OrEmpty helper).
func New(d spec.Deps) (*Handlers, error) {
	return &Handlers{
		genaiturnsHandler: genaiturns.New(d.GenAI),
		// The references upload releases a kickoff that project creation held
		// (#562) — the turn engine is what fires it, so the slice gets it as
		// a narrow port rather than growing a genai import.
		filesHandler:  files.New(d.Files, d.FilesActivity).WithKickoffStarter(d.GenAI),
		tagsHandler:   tags.New(d.Artifacts),
		skillsHandler: skills.New(d.Skills, d.SkillMut, d.SkillImport),
		collabHandler: collab.New(d.CollabRepo),
	}, nil
}
