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

package identity

import "errors"

// Deps is what this domain must be handed to exist: typed ports, never concrete
// collaborators. Constructor injection only — no setters, no framework.
//
// It lives in the domain ROOT, matching every other flat-root domain, while the
// thing that CONSUMES it (the aggregator that builds the slice handlers) lives
// in httpapi/ — see httpapi/doc.go for why the domain's composition cannot sit
// here.
type Deps struct {
	// Panel serves the console's Security panel. Required: it is the domain's
	// only HTTP slice, so a nil one is a domain with no surface at all.
	Panel *PanelService
}

// Validate reports a Deps that cannot produce a working domain. It exists
// because `var _ Iface = (*T)(nil)` proves a method SET and never the wiring: a
// nil Panel builds green and 503s on every request, which is a wiring defect the
// assembly should refuse at construction rather than serve.
func (d Deps) Validate() error {
	if d.Panel == nil {
		return errors.New("identity: Deps.Panel is required")
	}
	return nil
}
