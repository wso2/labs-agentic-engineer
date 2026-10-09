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

// FullName is the ref's owner/repo for log lines, case kept. It never reads
// CloneURL, so userinfo in a URL can never reach a log line through it.
func TestRepoRef_FullName(t *testing.T) {
	ref := repo.RepoRef{Owner: "Acme", Repo: "Greeter-App", CloneURL: "https://user:not-a-real-token@github.com/acme/greeter-app.git"}
	if got := ref.FullName(); got != "Acme/Greeter-App" {
		t.Fatalf("FullName = %q, want Acme/Greeter-App", got)
	}
}
