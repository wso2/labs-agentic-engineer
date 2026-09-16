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
	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/organization/connectgithub"
	"github.com/wso2/aep/aep-api/internal/organization/disconnectgithub"
	"github.com/wso2/aep/aep-api/internal/organization/getconfig"
	"github.com/wso2/aep/aep-api/internal/organization/getconfigstatus"
	"github.com/wso2/aep/aep-api/internal/organization/listorgs"
	"github.com/wso2/aep/aep-api/internal/organization/patchconfig"
)

// Every slice names its type Handler, so embedding them directly would be
// "Handler redeclared". Local aliases give distinct field names (§6).
type (
	getconfigHandler        = getconfig.Handler
	getconfigstatusHandler  = getconfigstatus.Handler
	patchconfigHandler      = patchconfig.Handler
	connectgithubHandler    = connectgithub.Handler
	disconnectgithubHandler = disconnectgithub.Handler
	listorgsHandler         = listorgs.Handler
)

// Handlers is the organization domain's slice handlers, embedded so Go promotes
// each operation exactly once into the edge's composite. It declares nothing.
//
// The org's IDP profile is read-only over HTTP: it rides the /config projection
// (getconfig) and is written only by the platform itself. Publisher-secret
// rotation and OIDC issuer discovery are Service methods with no route — see
// organization.Service.RotateIDPClientSecret / DiscoverIDP.
type Handlers struct {
	*getconfigHandler
	*getconfigstatusHandler
	*patchconfigHandler
	*connectgithubHandler
	*disconnectgithubHandler
	*listorgsHandler
}

// New assembles the domain: pure wiring, constructor injection only.
//
// The /config ops share the one *organization.Service orchestrator;
// list-organizations reads the OrganizationService. Both are fail-LOUD: the
// pre-migration handlers had no nil guard, so an unwired collaborator panics
// exactly as it did before (the edge assigns deps.Organization directly, no
// OrEmpty helper).
func New(d organization.Deps) (*Handlers, error) {
	return &Handlers{
		getconfigHandler:        getconfig.New(d.Config),
		getconfigstatusHandler:  getconfigstatus.New(d.Config),
		patchconfigHandler:      patchconfig.New(d.Config),
		connectgithubHandler:    connectgithub.New(d.Config),
		disconnectgithubHandler: disconnectgithub.New(d.Config),
		listorgsHandler:         listorgs.New(d.OrgSvc),
	}, nil
}
