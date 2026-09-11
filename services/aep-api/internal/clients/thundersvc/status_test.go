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

package thundersvc

import (
	"errors"
	"fmt"
	"testing"
)

// IsAuthError decides whether a cached per-environment admin client is thrown
// away. Get it wrong in one direction and a rotated secret leaves the client
// broken until the process restarts; wrong in the other and every ordinary 404
// costs two extra network reads to rebuild a client that was fine.
func TestIsAuthError(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		want bool
	}{
		"401 unauthorized":  {&statusError{code: 401, msg: "thunder GET /users returned 401"}, true},
		"403 forbidden":     {&statusError{code: 403, msg: "thunder GET /users returned 403"}, true},
		"403 wrapped":       {fmt.Errorf("create role %q: %w", "Viewer", &statusError{code: 403}), true},
		"404 not found":     {&statusError{code: 404}, false},
		"409 conflict":      {&statusError{code: 409}, false},
		"500 server error":  {&statusError{code: 500}, false},
		"transport failure": {errors.New("dial tcp: connection refused"), false},
		"no error":          {nil, false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := IsAuthError(tc.err); got != tc.want {
				t.Fatalf("IsAuthError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
