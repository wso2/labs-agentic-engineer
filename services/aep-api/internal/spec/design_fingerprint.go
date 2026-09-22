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

// "Has the design moved since the prototypes were generated from it?" (#818)
//
// The same comparison requirements staleness makes (requirements_fingerprint.go),
// one stage further down: the design as it stands against the design as the
// newest successful `/prototype` turn read it. Nothing is stamped, for the same
// reason — an agent turn never commits, so there is no moment to stamp at.

package spec

import (
	"strings"

	"github.com/wso2/aep/aep-api/internal/platform/prototypespec"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// DesignFingerprint reduces a tree listing to one comparable value: every file
// under specs/design/ and its blob SHA, EXCEPT each component's prototype.json.
//
// The exclusion is the whole point of the value. A prototype is derived FROM
// the design, and a feedback turn rewrites a prototype alone — if prototypes
// counted, iterating on one would mark every prototype behind the design it
// never moved. Only the component slot is excluded (prototypespec's own path
// rule); any other file of that name is ordinary design.
func DesignFingerprint(entries []sourcecontrol.Entry) string {
	lines := make([]string, 0, len(entries))
	for _, e := range entries {
		rel, ok := strings.CutPrefix(e.Path, designPrefix)
		if !ok || rel == "" {
			continue
		}
		if _, isPrototype := prototypespec.BundleComponent(rel); isPrototype {
			continue
		}
		lines = append(lines, rel+"\x00"+e.SHA)
	}
	return fingerprintLines(lines)
}

// hasPrototype reports whether any component's prototype.json is in the
// listing — nothing can be behind the design before one exists.
func hasPrototype(entries []sourcecontrol.Entry) bool {
	for _, e := range entries {
		if rel, ok := strings.CutPrefix(e.Path, designPrefix); ok {
			if _, isPrototype := prototypespec.BundleComponent(rel); isPrototype {
				return true
			}
		}
	}
	return false
}
