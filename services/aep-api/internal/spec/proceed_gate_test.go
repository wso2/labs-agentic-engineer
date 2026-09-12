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

package spec

import (
	"context"
	"testing"
)

// --- proceed-gate (dependency-management Phase 5) ----------------------------
//
// SaveAndProceed (the tag-cut = approve) blocks on exactly two dependency
// conditions and no others: an `org-service` that is not namespace-visible
// (unresolved/blocked against the live catalog) and an `external`
// that declares needsSpec but has no collected spec yet. external-values
// (config-only external) and platform-resource deps are NOT proceed-gated —
// they are dispatch-gated in Phase 6.

// designFilesWithDeps is a well-formed design tree whose single `consumer`
// component carries the given dependencies JSON array.
func designFilesWithDeps(depsJSON string) map[string]string {
	return map[string]string{
		DesignRootFile: "---\nsourceSpec: v1\n---\n\nOverview.\n",
		"components/consumer/design.json": `{
  "name": "consumer",
  "type": "service",
  "language": "Go",
  "description": "Consumes deps.",
  "dependencies": ` + depsJSON + `
}
`,
	}
}

// readsFor wires a fake artifact service for the SaveAndProceed pre-gate read
// (HEAD resolution) with the given design tree.
func readsFor(t *testing.T, files map[string]string) *fakeArtifactSvc {
	t.Helper()
	return &fakeArtifactSvc{
		ListDesignFilesFunc: func(context.Context, string, string) (map[string]string, error) {
			return files, nil
		},
	}
}

// happySave wires a fake that lets the tag-cut through: a resolved read.
func happySave(files map[string]string) *fakeArtifactSvc {
	return &fakeArtifactSvc{
		ListDesignFilesFunc: func(context.Context, string, string) (map[string]string, error) {
			return files, nil
		},
	}
}
