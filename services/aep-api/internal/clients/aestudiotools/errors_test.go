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
	"testing"
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
		{ErrAEStudioAbsent, false},
		{ErrTurnInProgress, false},
		{context.Canceled, false},
		{errors.New("other"), false},
	} {
		if got := IsPermanent(tc.err); got != tc.want {
			t.Errorf("IsPermanent(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}
