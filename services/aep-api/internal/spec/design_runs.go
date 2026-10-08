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

package spec

import (
	"regexp"
	"slices"
	"strings"
)

// A design run's feature scope in the finished-turn ledger (#878 E1). The pod
// reports the IDs a `/design F1 F2` turn named (record-turn-usage
// designFeatures); the ledger keeps them, and nothing else, space-joined in a
// design turn's agent_turns.Summary. The build gate's staleness check and
// GET /spec/state read them back as DesignRun.Features.

// FlowDesign is the flow of a design turn (AgentTurn.Flow): the only flow
// whose Summary holds feature IDs.
const FlowDesign = "design"

// designFeatureID matches one feature ID inside a stored Summary.
var designFeatureID = regexp.MustCompile(`\bF[0-9]+\b`)

// designSummary is what a turn's Summary stores: a design turn's feature IDs,
// space-joined and deduplicated in the order named; "" for any other flow, or
// a design turn that named none (every feature).
func designSummary(flow string, ids []string) string {
	if flow != FlowDesign {
		return ""
	}
	var kept []string
	for _, id := range ids {
		if !slices.Contains(kept, id) {
			kept = append(kept, id)
		}
	}
	return strings.Join(kept, " ")
}

// DesignedFeatures reads which features a design run designed from its ledger
// Summary: "F1 F2" names them. Rows the in-process engine wrote hold the
// `/design F1 F2` line instead, which reads the same. None (an empty Summary,
// a bare `/design`) is nil, which means every feature designable at the run's
// commit.
func DesignedFeatures(summary string) []string {
	var ids []string
	for _, id := range designFeatureID.FindAllString(summary, -1) {
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	return ids
}
