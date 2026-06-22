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

package api

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestOpenAPISpecFresh is the drift guard: the checked-in api/openapi.yaml MUST
// match what the code generates. Run `make openapi` and commit when this fails.
func TestOpenAPISpecFresh(t *testing.T) {
	got, err := GenerateOpenAPIYAML()
	if err != nil {
		t.Fatalf("generate spec: %v", err)
	}
	want, err := os.ReadFile("openapi.yaml")
	if err != nil {
		t.Fatalf("read api/openapi.yaml (run `make openapi`): %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("api/openapi.yaml is stale — run `make openapi` and commit the result")
	}
}

// TestOrgScopedInputIsOnlyOrgHandleBinding is the IDOR arch-lock for the
// code-first surface: every org-scoped operation must bind {orgHandle} via
// humakit.OrgScopedInput (whose Resolve method is the tenant gate). No feature
// *_huma.go may hand-roll a `path:"orgHandle"` field — that would be an ungated
// org-scoped route. Combined with TestHumaRegistration (which proves every
// {orgHandle} path has a binding, since Huma panics otherwise), this means
// every org-scoped operation is gated by construction.
func TestOrgScopedInputIsOnlyOrgHandleBinding(t *testing.T) {
	root := filepath.Join("..", "internal", "feature")
	var offenders []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, "_huma.go") {
			return nil
		}
		b, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if strings.Contains(string(b), `path:"orgHandle"`) {
			offenders = append(offenders, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk feature packages: %v", err)
	}
	if len(offenders) > 0 {
		t.Fatalf("these *_huma.go hand-roll a path:%q binding instead of embedding humakit.OrgScopedInput "+
			"(ungated org-scoped route risk): %v", "orgHandle", offenders)
	}
}
