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
	"github.com/wso2/aep/aep-api/internal/projects"
	"github.com/wso2/aep/aep-api/internal/projects/componentbuild"
	"github.com/wso2/aep/aep-api/internal/projects/componentconfig"
	"github.com/wso2/aep/aep-api/internal/projects/componentread"
	"github.com/wso2/aep/aep-api/internal/projects/projectcrud"
	"github.com/wso2/aep/aep-api/internal/projects/projectusage"
)

// Every slice names its type Handler, so embedding them directly would be
// "Handler redeclared". Local aliases give distinct field names (§6).
type (
	projectcrudHandler     = projectcrud.Handler
	componentreadHandler   = componentread.Handler
	componentbuildHandler  = componentbuild.Handler
	componentconfigHandler = componentconfig.Handler
	projectusageHandler    = projectusage.Handler
)

// Handlers is the projects domain's slice handlers, embedded so Go promotes each
// operation exactly once into the edge's composite. It declares nothing.
type Handlers struct {
	*projectcrudHandler
	*componentreadHandler
	*componentbuildHandler
	*componentconfigHandler
	*projectusageHandler
}

// New assembles the domain: pure wiring, constructor injection only.
//
// projects is fail-LOUD: the pre-migration handlers had no nil guard, so an
// unwired collaborator panics exactly as it did before (the edge assigns each
// dep directly, no OrEmpty helper). The one exception is projectusage, whose
// ports are nil-tolerant by design (zero usage IS the truth for an unwired
// capture store — the component-test harness contract).
func New(d projects.Deps) (*Handlers, error) {
	return &Handlers{
		projectcrudHandler:     projectcrud.New(d.ProjectSvc),
		componentreadHandler:   componentread.New(d.ComponentSvc),
		componentbuildHandler:  componentbuild.New(d.ComponentSvc),
		componentconfigHandler: componentconfig.New(d.ConfigSvc),
		projectusageHandler:    projectusage.New(d.TurnUsage, d.ExecUsage, d.Pricer),
	}, nil
}
