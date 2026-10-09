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

package github

import (
	"net/http"
	"testing"
)

// TestIsAlreadyExists is the duplicate-title discriminator in isolation.
// Everything hinges on it: a false positive sends CreateMilestone hunting for a
// milestone that was never created, a false negative mints a case-twin.
func TestIsAlreadyExists(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{
			name:   "github's verbatim duplicate rejection",
			status: http.StatusUnprocessableEntity,
			body:   `{"message":"Validation Failed","errors":[{"resource":"Milestone","code":"already_exists","field":"title"}]}`,
			want:   true,
		},
		{
			name:   "already_exists among several errors",
			status: http.StatusUnprocessableEntity,
			body:   `{"errors":[{"code":"missing_field"},{"code":"already_exists"}]}`,
			want:   true,
		},
		{
			name:   "a different 422 is a real rejection, not a duplicate",
			status: http.StatusUnprocessableEntity,
			body:   `{"message":"Validation Failed","errors":[{"resource":"Milestone","code":"invalid","field":"due_on"}]}`,
			want:   false,
		},
		{
			name:   "the shared Validation Failed message alone proves nothing",
			status: http.StatusUnprocessableEntity,
			body:   `{"message":"Validation Failed"}`,
			want:   false,
		},
		{
			name:   "already_exists at another status is not this branch",
			status: http.StatusForbidden,
			body:   `{"errors":[{"code":"already_exists"}]}`,
			want:   false,
		},
		{
			name:   "an unparseable body is never a duplicate",
			status: http.StatusUnprocessableEntity,
			body:   `<html>502 Bad Gateway</html>`,
			want:   false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := isAlreadyExists(c.status, []byte(c.body)); got != c.want {
				t.Fatalf("isAlreadyExists(%d, %s) = %v, want %v", c.status, c.body, got, c.want)
			}
		})
	}
}
