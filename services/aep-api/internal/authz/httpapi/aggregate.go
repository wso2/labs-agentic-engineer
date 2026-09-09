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
	"github.com/wso2/aep/aep-api/internal/authz"
	"github.com/wso2/aep/aep-api/internal/authz/ensurerole"
	"github.com/wso2/aep/aep-api/internal/authz/rolepermissions"
)

type rolepermissionsHandler = rolepermissions.Handler
type ensureroleHandler = ensurerole.Handler

// Handlers is the authz domain's slice handlers, embedded so Go promotes each
// operation exactly once into the edge's composite. It declares nothing.
type Handlers struct {
	*rolepermissionsHandler
	*ensureroleHandler
}

// New assembles the domain: pure wiring, constructor injection only.
func New(d authz.Deps) (*Handlers, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	return &Handlers{
		rolepermissionsHandler: rolepermissions.New(d.AuthZ),
		ensureroleHandler:      ensurerole.New(d.AuthZ),
	}, nil
}

// NewEmpty assembles the domain unwired so component tests that do not wire
// authz get 503 from every op rather than a nil-embed panic.
func NewEmpty() *Handlers {
	return &Handlers{
		rolepermissionsHandler: rolepermissions.New(nil),
		ensureroleHandler:      ensurerole.New(nil),
	}
}
