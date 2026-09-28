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
	"time"
)

// TurnError is a coded `error` part: the agents service ended the turn for a
// reason it can name (`provider_limit`, `output_truncated`) rather than
// forwarding the provider's raw error. Message is the part's `error` sentence;
// Host and ResetAt are set on a provider limit (ResetAt only when the
// provider said when it resets).
type TurnError struct {
	Code    string
	Message string
	Host    string
	ResetAt *time.Time
}

// TurnErrorOf extracts the coded error from a parsed StreamPart. ok is false
// for any other part, including an uncoded `{type: "error", error}` part. An
// unparseable resetAt is dropped rather than failing the read: the code is the
// fact that matters, the reset time only refines the sentence.
func TurnErrorOf(part StreamPart) (TurnError, bool) {
	if part.Type != "error" || part.Code == "" {
		return TurnError{}, false
	}
	te := TurnError{Code: part.Code, Host: part.Host}
	_ = json.Unmarshal(part.Error, &te.Message) // non-string error → no sentence
	if at, err := time.Parse(time.RFC3339, part.ResetAt); err == nil {
		at = at.UTC()
		te.ResetAt = &at
	}
	return te, true
}
