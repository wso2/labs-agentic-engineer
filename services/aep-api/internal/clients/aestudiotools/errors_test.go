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

package aestudiotools

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

func TestIsPermanent(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want bool
	}{
		{nil, false},
		{ErrAEStudioMisconfigured, true},
		{fmt.Errorf("wrapped: %w", ErrAEStudioMisconfigured), true},
		{ErrReferenceRejected, true},
		{&StatusError{Status: 400, Code: "validation_failed"}, true},
		{&StatusError{Status: 404, Code: "project_unknown"}, true},
		{&StatusError{Status: 409, Code: "no_default_key"}, true},
		{&StatusError{Status: 413, Code: "payload_too_large"}, true},
		{&StatusError{Status: 408}, false},
		{&StatusError{Status: 429, Code: "github_rate_limited"}, false},
		{&StatusError{Status: 502, Code: "agent_error"}, false},
		{ErrAEStudioUnavailable, false},
		{ErrAEStudioAbsent, true},
		{sourcecontrol.ErrOwnerNotAllowed, true},
		{&sourcecontrol.HTTPStatusError{StatusCode: 404}, true},
		{ErrTurnInProgress, false},
		{context.Canceled, false},
		{errors.New("other"), false},
	} {
		if got := IsPermanent(tc.err); got != tc.want {
			t.Errorf("IsPermanent(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}

// A failed turn's error names its code and nothing else: the pod's free-text
// message (possibly a provider's error body) never reaches an error string,
// so it never reaches Temporal history or a run record (R1-M4).
func TestTurnFailedError_ClassifiesByCodeAndCarriesNoMessage(t *testing.T) {
	for _, tc := range []struct {
		code                         string
		interrupted, providerLimited bool
	}{
		{TurnCodeShutdown, true, false},
		{TurnCodeStreamDied, true, false},
		{TurnCodeProviderLimit, false, true},
		{"agent-error", false, false},
		{"output_truncated", false, false},
		{"internal", false, false},
		{"", false, false},
	} {
		e := &TurnFailedError{Code: tc.code}
		if e.Interrupted() != tc.interrupted || e.ProviderLimited() != tc.providerLimited {
			t.Errorf("%q: interrupted=%v providerLimited=%v, want %v %v", tc.code, e.Interrupted(), e.ProviderLimited(), tc.interrupted, tc.providerLimited)
		}
		if tc.code != "" && !strings.Contains(e.Error(), tc.code) {
			t.Errorf("%q: Error() = %q, want the code in it", tc.code, e.Error())
		}
		if IsPermanent(e) {
			t.Errorf("%q: IsPermanent must not classify a turn failure; the caller's retry policy does", tc.code)
		}
	}
}
