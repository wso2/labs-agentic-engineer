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

package agentfold

import (
	"encoding/json"
	"testing"
	"time"
)

func parsePart(t *testing.T, raw string) StreamPart {
	t.Helper()
	var p StreamPart
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatalf("unmarshal %s: %v", raw, err)
	}
	return p
}

func TestTurnErrorOf_ProviderLimit(t *testing.T) {
	p := parsePart(t, `{"type":"error","code":"provider_limit","error":"ollama.com's usage limit is reached.","host":"ollama.com","resetAt":"2026-09-26T14:05:00.000Z"}`)
	te, ok := TurnErrorOf(p)
	if !ok {
		t.Fatal("coded frame not recognised")
	}
	want := time.Date(2026, 9, 26, 14, 5, 0, 0, time.UTC)
	if te.Code != "provider_limit" || te.Host != "ollama.com" || te.Message != "ollama.com's usage limit is reached." ||
		te.ResetAt == nil || !te.ResetAt.Equal(want) {
		t.Fatalf("got %+v", te)
	}
}

func TestTurnErrorOf_TruncatedWithoutReset(t *testing.T) {
	te, ok := TurnErrorOf(parsePart(t, `{"type":"error","code":"output_truncated","error":"cut off","toolName":"addFile"}`))
	if !ok || te.Code != "output_truncated" || te.ResetAt != nil || te.Host != "" {
		t.Fatalf("got %+v ok=%v", te, ok)
	}
}

func TestTurnErrorOf_IgnoresOtherParts(t *testing.T) {
	for _, raw := range []string{
		`{"type":"error","error":"boom"}`,   // uncoded error
		`{"type":"text-delta","delta":"x"}`, // not an error
		`{"type":"manifest","files":{},"deleted":[]}`,
	} {
		if te, ok := TurnErrorOf(parsePart(t, raw)); ok {
			t.Fatalf("%s: recognised as coded: %+v", raw, te)
		}
	}
}

func TestTurnErrorOf_BadResetAtKeepsTheCode(t *testing.T) {
	te, ok := TurnErrorOf(parsePart(t, `{"type":"error","code":"provider_limit","error":"x","host":"h","resetAt":"soon"}`))
	if !ok || te.ResetAt != nil || te.Code != "provider_limit" {
		t.Fatalf("got %+v ok=%v", te, ok)
	}
}
