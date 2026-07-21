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

package codingagent

import (
	"bufio"
	"strings"

	"github.com/wso2/aep/aep-api/internal/contracts"
)

// usageFromLog extracts the run's token usage (#249) from the captured pod
// log: the last runner NDJSON `result` event carrying a usage object wins
// (there is one per run; "last" guards against a retried in-container agent).
// nil when the log carries none — pre-capture runners, or a run that died
// before its terminal message.
func usageFromLog(text string) *contracts.TokenUsage {
	var found *contracts.TokenUsage
	scanner := bufio.NewScanner(strings.NewReader(text))
	scanner.Buffer(make([]byte, 0, 4096), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		// Cheap pre-filter before the JSON parse: result lines are rare.
		if !strings.Contains(line, `"result"`) || !strings.Contains(line, `"usage"`) {
			continue
		}
		_, msg := splitTimestampPrefix(line)
		ev := parseProgressLine(msg)
		if ev.Kind == "result" && ev.Usage != nil {
			u := *ev.Usage
			found = &u
		}
	}
	return found
}
