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

package files

import (
	"context"

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// Handler serves the files feature's one remaining operation, the reference
// documents upload. The operation is org-scoped: the tenant gate bound the
// token org before it runs.
type Handler struct {
	refs    aestudiotools.References
	repos   sourcecontrol.ProjectRepoRows
	kickoff kickoffStarter
}

// kickoffStarter fires a project's opening `/start` turn (#562). The
// references upload is the SECOND of its two triggers: a create that declared
// documents were coming holds the kickoff, because they are the primary brief
// and an interview run before they land is conducted blind.
// *spec.KickoffService satisfies it. Nil is a documented no-op.
type kickoffStarter interface {
	Kickoff(ctx context.Context, orgID, projectID string)
}

// NewHandler returns the slice's handler: refs is the org pods' reference
// store, repos resolves the project's repository, kickoff fires the held
// kickoff (nil: none).
func NewHandler(refs aestudiotools.References, repos sourcecontrol.ProjectRepoRows, kickoff kickoffStarter) *Handler {
	return &Handler{refs: refs, repos: repos, kickoff: kickoff}
}
