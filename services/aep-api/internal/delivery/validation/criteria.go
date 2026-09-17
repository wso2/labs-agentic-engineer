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

package validation

import (
	"fmt"
	"strings"
)

// AcceptanceFile is one `specs/acceptance/<slug>.feature` as committed.
type AcceptanceFile struct {
	Path    string
	Content string
}

// acceptanceSummary is the tally rendered in the issue's one-line rationale.
type acceptanceSummary struct {
	Files     int
	Rules     int
	Scenarios int
}

// summarize counts what the oracle contains, for DISPLAY only.
//
// Deliberately a line scan and not a parse: Go has no vendored Gherkin parser,
// the number exists to put a size in a sentence, and a miscount costs a reader a
// slightly wrong figure. Judging the scenarios is the agent's job, and the agent
// reads the files themselves. `Scenario:` and `Example:` are synonyms in the
// Gherkin grammar, so both count.
func summarize(files []AcceptanceFile) acceptanceSummary {
	sum := acceptanceSummary{Files: len(files)}
	for _, f := range files {
		for _, line := range strings.Split(f.Content, "\n") {
			switch t := strings.TrimSpace(line); {
			case strings.HasPrefix(t, "Rule:"):
				sum.Rules++
			case strings.HasPrefix(t, "Scenario:"), strings.HasPrefix(t, "Example:"):
				sum.Scenarios++
			}
		}
	}
	return sum
}

// validateAcceptance minimally checks the oracle is usable: at least one feature
// file carrying at least one scenario. An oracle with none is the agent's bug,
// not a reason to fail the save — the caller treats the error as "skip minting"
// and a corrected pass re-mints.
func validateAcceptance(files []AcceptanceFile) (acceptanceSummary, error) {
	sum := summarize(files)
	if sum.Files == 0 {
		return sum, fmt.Errorf("specs/acceptance/ holds no .feature files")
	}
	if sum.Scenarios == 0 {
		return sum, fmt.Errorf("specs/acceptance/ holds no scenarios")
	}
	return sum, nil
}
