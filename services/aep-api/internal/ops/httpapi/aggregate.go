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
	"fmt"

	"github.com/wso2/aep/aep-api/internal/ops"
	"github.com/wso2/aep/aep-api/internal/ops/getreport"
	"github.com/wso2/aep/aep-api/internal/ops/listreports"
)

// An embedded field is named by its UNQUALIFIED type name, so embedding
// *getreport.Handler beside *listreports.Handler is "Handler redeclared" —
// every slice calls its type Handler. Local aliases give distinct field names
// while each slice keeps the unstuttering name. (The same trick the edge needs
// for the domains themselves; §6.)
type (
	getReportHandler   = getreport.Handler
	listReportsHandler = listreports.Handler
)

// Handlers is the ops domain's slice handlers, embedded so Go promotes each
// operation exactly once into the edge's composite. It declares nothing itself.
type Handlers struct {
	*getReportHandler
	*listReportsHandler
}

// New assembles the domain: pure wiring, constructor injection only, no setters.
// Being pure is what lets the assembly test build the real graph in
// microseconds with a faked repository.
func New(d ops.Deps) (*Handlers, error) {
	if err := d.Validate(); err != nil {
		return nil, fmt.Errorf("ops httpapi: %w", err)
	}
	return &Handlers{
		getReportHandler:   getreport.New(d.Reports, d.Execs),
		listReportsHandler: listreports.New(d.Reports),
	}, nil
}
