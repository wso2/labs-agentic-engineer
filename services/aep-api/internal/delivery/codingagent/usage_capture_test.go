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

import "testing"

func TestUsageFromLogReadsTheResultLine(t *testing.T) {
	log := `2026-07-21T10:00:00.000000000Z {"schemaVersion":1,"ts":"t","seq":1,"kind":"phase","phase":"agent"}
2026-07-21T10:00:01.000000000Z [oneshot] plain bootstrap line
2026-07-21T10:00:02.000000000Z {"schemaVersion":1,"ts":"t","seq":9,"kind":"result","status":"success","usage":{"inputTokens":100,"outputTokens":20,"cacheReadTokens":3000,"cacheCreationTokens":40,"model":"claude-fable-5"}}
`
	u := usageFromLog(log)
	if u == nil {
		t.Fatal("expected usage, got nil")
	}
	if u.InputTokens != 100 || u.OutputTokens != 20 || u.CacheReadTokens != 3000 ||
		u.CacheCreationTokens != 40 || u.Model != "claude-fable-5" {
		t.Fatalf("unexpected usage: %+v", u)
	}
}

func TestUsageFromLogLastResultWins(t *testing.T) {
	log := `{"schemaVersion":1,"ts":"t","seq":1,"kind":"result","status":"failure","usage":{"inputTokens":1,"outputTokens":1,"cacheReadTokens":0,"cacheCreationTokens":0,"model":"claude-fable-5"}}
{"schemaVersion":1,"ts":"t","seq":2,"kind":"result","status":"success","usage":{"inputTokens":7,"outputTokens":3,"cacheReadTokens":0,"cacheCreationTokens":0,"model":"claude-fable-5"}}
`
	u := usageFromLog(log)
	if u == nil || u.InputTokens != 7 {
		t.Fatalf("expected the last result's usage, got %+v", u)
	}
}

func TestUsageFromLogAbsentForPreCaptureRunners(t *testing.T) {
	log := `{"schemaVersion":1,"ts":"t","seq":1,"kind":"result","status":"success"}
some stray text mentioning "result" and "usage" but not JSON
`
	if u := usageFromLog(log); u != nil {
		t.Fatalf("expected nil for a usage-less log, got %+v", u)
	}
}
