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

package organization

import "github.com/wso2/aep/aep-api/internal/platform/modelconn"

// SRESource is the origin of the effective SRE connection.
type SRESource string

const (
	// SRESourceOverride indicates the connection comes from an SRE model
	// connection override.
	SRESourceOverride SRESource = "override"
	// SRESourceOrganization indicates the connection comes from the
	// organization's model connection.
	SRESourceOrganization SRESource = "organization"
	// SRESourceNone indicates no SRE connection is configured.
	SRESourceNone SRESource = "none"
)

// EffectiveSRE is the resolved SRE connection: which source provides it, the
// connection itself, and its key. Key is never logged.
type EffectiveSRE struct {
	Source SRESource
	Conn   modelconn.Connection
	Key    string // never logged
}

// ResolveEffectiveSRE is the one statement of which connection the SRE agent
// runs on: an SRE model connection when one is set, else the org model
// connection when it has the SREAgent capability, else none.
func ResolveEffectiveSRE(override *OrgSreModelConnection, overrideKey string,
	org *modelconn.Connection, orgKey string) EffectiveSRE {
	if override != nil && overrideKey != "" {
		return EffectiveSRE{Source: SRESourceOverride, Conn: override.Connection(), Key: overrideKey}
	}
	if org != nil && orgKey != "" && modelconn.CapabilitiesOf(*org).SREAgent {
		return EffectiveSRE{Source: SRESourceOrganization, Conn: *org, Key: orgKey}
	}
	return EffectiveSRE{Source: SRESourceNone}
}
