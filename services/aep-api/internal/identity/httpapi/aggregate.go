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
	"github.com/wso2/aep/aep-api/internal/identity"
	"github.com/wso2/aep/aep-api/internal/identity/rolespanel"
)

// The slice names its handler type Handler; a local alias keeps a distinct field
// name here and leaves room for a second slice without a rename (§6).
type rolespanelHandler = rolespanel.Handler

// Handlers is the identity domain's slice handlers, embedded so Go promotes each
// operation exactly once into the edge's composite. It declares nothing.
type Handlers struct {
	*rolespanelHandler
}

// New assembles the domain: pure wiring, constructor injection only. Deps lives
// in the domain root (identity/module.go), matching every other flat-root
// domain, and is validated here rather than trusted — a nil service builds green
// and 503s on every request, which is a wiring defect worth refusing at
// construction.
func New(d identity.Deps) (*Handlers, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	return &Handlers{rolespanelHandler: rolespanel.New(d.Panel)}, nil
}

// NewEmpty assembles the domain UNWIRED, for the edge's harness contract: a
// component test that does not wire identity must get 503 from every
// Security-panel op rather than a nil-embed panic.
//
// It is separate from New on purpose. The slice is nil-tolerant, but a nil panel
// reaching PRODUCTION wiring is a defect, so New refuses it — and the one caller
// that legitimately wants the unwired shape says so by name instead of being
// waved through by a validator too permissive to catch anything.
func NewEmpty() *Handlers {
	return &Handlers{rolespanelHandler: rolespanel.New(nil)}
}
