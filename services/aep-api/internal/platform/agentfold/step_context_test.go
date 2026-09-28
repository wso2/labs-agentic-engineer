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

import "testing"

func TestStepContextOf(t *testing.T) {
	cases := []struct {
		name   string
		raw    string
		want   int64
		wantOK bool
	}{
		{
			// The SDK's inputTokens already includes the cache: the details
			// split it, and must not be added on top.
			name: "whole prompt plus output",
			raw: `{"type":"finish-step","finishReason":"tool-calls","usage":{"inputTokens":90000,` +
				`"inputTokenDetails":{"noCacheTokens":1000,"cacheReadTokens":85000,"cacheWriteTokens":4000},` +
				`"outputTokens":2500,"outputTokenDetails":{"textTokens":2500},"totalTokens":92500}}`,
			want: 92500, wantOK: true,
		},
		{
			name: "only the split reported",
			raw: `{"type":"finish-step","usage":{"inputTokenDetails":{"noCacheTokens":100,` +
				`"cacheReadTokens":200,"cacheWriteTokens":300},"outputTokens":50}}`,
			want: 650, wantOK: true,
		},
		{name: "no usage", raw: `{"type":"finish-step","finishReason":"stop"}`},
		{name: "zero usage", raw: `{"type":"finish-step","usage":{"inputTokens":0,"outputTokens":0}}`},
		{
			// The manifest carries a usage too — the turn's sum, never a step.
			name: "manifest usage is not a step",
			raw:  `{"type":"manifest","files":{},"usage":{"inputTokens":10,"outputTokens":5,"model":""}}`,
		},
		{name: "unparseable", raw: `{"type":"finish-step","usage":`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := StepContextOf([]byte(c.raw))
			if ok != c.wantOK || got != c.want {
				t.Fatalf("StepContextOf = (%d, %v), want (%d, %v)", got, ok, c.want, c.wantOK)
			}
		})
	}
}
