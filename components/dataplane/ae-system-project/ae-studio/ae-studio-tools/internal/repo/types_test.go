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

package repo_test

import (
	"testing"

	"github.com/wso2/aep/ae-studio-tools/internal/repo"
)

// FullName is the owner/repo of a GitHub URL for log lines: suffix and
// trailing slash dropped, case kept, userinfo never included.
func TestRepoRef_FullName(t *testing.T) {
	for _, c := range []struct{ url, want string }{
		{"https://github.com/Acme/Greeter-App.git", "Acme/Greeter-App"},
		{"https://github.com/acme/greeter", "acme/greeter"},
		{"https://github.com/acme/greeter/", "acme/greeter"},
		{"https://x-access-token:not-a-real-token@github.com/acme/greeter.git", "acme/greeter"},
		{"https://user@github.com/acme/greeter", "acme/greeter"},
		{"https://gitlab.com/acme/greeter.git", ""},
		{"file:///tmp/origin.git", ""},
	} {
		if got := (repo.RepoRef{CloneURL: c.url}).FullName(); got != c.want {
			t.Errorf("FullName(%q) = %q, want %q", c.url, got, c.want)
		}
	}
}
