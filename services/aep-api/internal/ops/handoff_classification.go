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

const (
	ClassificationCodeLevel   = "code-level"
	ClassificationConfigLevel = "config-level"
	ClassificationMixed       = "mixed"
	ClassificationNone        = "none"
)

// ClassifyActions derives the remediation classification from ordered action
// statuses. Revised, applied, and dismissed actions are settled configuration
// work; every other status is pending code work.
func ClassifyActions(statuses []*string) string {
	var hasConfig, hasCode bool
	for _, status := range statuses {
		if status != nil && isSettledConfiguration(*status) {
			hasConfig = true
		} else {
			hasCode = true
		}
	}

	switch {
	case hasConfig && hasCode:
		return ClassificationMixed
	case hasConfig:
		return ClassificationConfigLevel
	case hasCode:
		return ClassificationCodeLevel
	default:
		return ClassificationNone
	}
}

// AdoptableClassification reports whether a handoff classification may be
// adopted for code work. Only settled configuration-only handoffs are excluded.
func AdoptableClassification(classification string) bool {
	return classification != ClassificationConfigLevel
}

func isSettledConfiguration(status string) bool {
	switch status {
	case "revised", "applied", "dismissed":
		return true
	default:
		return false
	}
}
