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

package files

import (
	"errors"
	"testing"
)

// TestReadPaths pins today's read rules (moved from aep-api's
// TestValidateReadPath_WorkloadEscapeHatch): specs/, the exact allow-list and
// a component's workload.yaml one segment deep, all behind the canonical and
// traversal checks.
func TestReadPaths(t *testing.T) {
	for _, c := range []struct {
		path string
		ok   bool
	}{
		{"todo-webapp/workload.yaml", true},
		{"workload.yaml", true}, // a component building from the repo root
		{"tests/acceptance/report.json", true},
		{"specs/design/domain-model.md", true},
		{"specs/requirements/仕様-résumé ノート.md", true},

		{"a/b/workload.yaml", false},
		{"../workload.yaml", false},
		{"svc/../../workload.yaml", false},
		{"specs/../src/main.go", false},
		{"specs//a.md", false},
		{"/etc/workload.yaml", false},
		{"./workload.yaml", false},
		{"svc/workload.yaml.bak", false},
		{"svc/secrets.yaml", false},
		{"src/main.go", false},
		{"README.md", false},
		{"tests/acceptance/other.json", false},
		{"", false},
	} {
		err := validateReadPath(c.path)
		if c.ok && err != nil {
			t.Errorf("validateReadPath(%q) = %v, want allowed", c.path, err)
		}
		if !c.ok && !errors.Is(err, ErrPathInvalid) {
			t.Errorf("validateReadPath(%q) = %v, want ErrPathInvalid", c.path, err)
		}
	}
}

// TestReadPaths_Commit pins the ref rule: empty or a hex object name, never a
// revision expression.
func TestReadPaths_Commit(t *testing.T) {
	for _, ok := range []string{"", "abc1234", "0123456789abcdef0123456789abcdef01234567", "ABCDEF0"} {
		if err := validateCommit(ok); err != nil {
			t.Errorf("validateCommit(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"main", "HEAD~1", "refs/heads/main", "abc123", "tags/v1", "abc1234^"} {
		if err := validateCommit(bad); !errors.Is(err, ErrPathInvalid) {
			t.Errorf("validateCommit(%q) = %v, want ErrPathInvalid", bad, err)
		}
	}
}

// TestReadPaths_BlobSHA pins blobSHA to git's blob object name.
func TestReadPaths_BlobSHA(t *testing.T) {
	// `git hash-object --stdin </dev/null`: the empty blob.
	if got := blobSHA(nil); got != "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391" {
		t.Fatalf("blobSHA(empty) = %s", got)
	}
}
