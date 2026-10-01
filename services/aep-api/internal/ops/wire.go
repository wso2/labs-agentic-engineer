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

package ops

import "github.com/wso2/aep/aep-api/internal/gen"

// The domain's ONE public-wire projection. (The internal sre-create-rca-report
// op projects onto its own igen type in internal/edge/internal_sre.go.)
//
// It sits in the root rather than in a slice because both slices return the
// report on the wire — the duplication is real and immediate, which is the
// domain-root's stated purpose (§5). It cannot sit in a sub-package: a slice
// importing a sibling is forbidden, and importing httpapi/ would be a cycle.
//
// Before P1 there was no mapping at all — the gorm model WAS the wire type, via
// the contract's `x-go-type: models.RcaAgentReport`. That is not available to a
// domain: internal/gen is imported by clients/ (bound for platform/clients), so
// pointing x-go-type at internal/ops would make the kernel import a domain and
// give every other domain a transitive dependency on this one. gen stays a leaf;
// the mapping is the price, and it buys a wire type with no OrgID on it.

// ToWire projects the domain entity onto the contract's shape.
func ToWire(r RcaAgentReport) gen.RcaAgentReport {
	return gen.RcaAgentReport{
		ID:             r.ID,
		Project:        r.Project,
		Component:      r.Component,
		Title:          r.Title,
		Summary:        r.Summary,
		Classification: r.Classification,
		Diagnosis:      r.Diagnosis,
		IssueNumber:    r.IssueNumber,
		IssueURL:       r.IssueURL,
		IssueTitle:     r.IssueTitle,
		IssueExcerpt:   r.IssueExcerpt,
		Dispatched:     r.Dispatched,
		Deployed:       r.Deployed,
		DeployedAt:     r.DeployedAt,
		CreatedAt:      r.CreatedAt,
	}
}

// ToWireList projects a page of reports, PRESERVING nil.
//
// A nil page must stay nil: gorm's Find leaves an empty result nil, so an empty
// page has always marshalled to `"items":null`, and the contract marks items
// `nullable: true` to say so. Coercing to `[]` here would be a silent wire
// change — indefensible in a phase whose whole claim is that behaviour is
// unchanged. If null-items is wrong for the console, that is a product decision
// with a contract edit, not a side effect of moving a package.
func ToWireList(reports []RcaAgentReport) []gen.RcaAgentReport {
	if reports == nil {
		return nil
	}
	out := make([]gen.RcaAgentReport, 0, len(reports))
	for _, r := range reports {
		out = append(out, ToWire(r))
	}
	return out
}
