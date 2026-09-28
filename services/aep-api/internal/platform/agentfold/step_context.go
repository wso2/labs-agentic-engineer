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

import "encoding/json"

// PartFinishStep is the part the agents service relays at the end of every
// model step (the AI SDK's own `finish-step` stream part, forwarded verbatim).
const PartFinishStep = "finish-step"

// finishStepPart is the slice of the AI SDK's `finish-step` part that
// measures the step: its `usage` is that ONE step's LanguageModelUsage (the
// manifest's usage is the whole turn's sum, and its inputTokens exclude the
// cache). The SDK's inputTokens is the step's whole prompt, cached or not;
// inputTokenDetails splits it, and is the fallback when a provider reports
// only the split.
type finishStepPart struct {
	Type  string `json:"type"`
	Usage *struct {
		InputTokens       *int64 `json:"inputTokens"`
		InputTokenDetails struct {
			NoCacheTokens    int64 `json:"noCacheTokens"`
			CacheReadTokens  int64 `json:"cacheReadTokens"`
			CacheWriteTokens int64 `json:"cacheWriteTokens"`
		} `json:"inputTokenDetails"`
		OutputTokens int64 `json:"outputTokens"`
	} `json:"usage"`
}

// StepContextOf reads, off one raw `finish-step` payload, how many tokens of
// context the conversation held when that step ended: the step's whole prompt
// plus what it generated, which joins the history the next step reads. The
// last step's figure is the turn's closing context size. ok is false for any
// other part, a step that reported no usage, or a payload that does not parse.
func StepContextOf(raw []byte) (int64, bool) {
	var p finishStepPart
	if err := json.Unmarshal(raw, &p); err != nil || p.Type != PartFinishStep || p.Usage == nil {
		return 0, false
	}
	u := p.Usage
	input := u.InputTokenDetails.NoCacheTokens + u.InputTokenDetails.CacheReadTokens + u.InputTokenDetails.CacheWriteTokens
	if u.InputTokens != nil {
		input = *u.InputTokens
	}
	total := input + u.OutputTokens
	if total <= 0 {
		return 0, false
	}
	return total, true
}
