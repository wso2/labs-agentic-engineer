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

// UNIT tier: the REAL designService with every
// out-of-process seam faked — the artifact service (wrapped by the REAL
// NewArtifactStore decorator, so the store's split/assemble logic runs
// for real). No HTTP, no DB — design has no SQL-shaped behavior (persistence
// delegates to artifacts/git), so there is no dbtest tier for this feature.
//
// This file holds the shared UNIT-tier fixtures (validDesignFiles, newService)
// the design feature's write-path tests build on: CollectSpec (collect_spec_test),
// the end-user-auth derivation (derive_auth_test), and the conditional skill
// attach (attach_skills_test). The old hard save gate (SaveAndProceed) and the
// read+version HTTP surface it fronted were retired — tagging is the single-tag
// POST /build flow now, and reads are the Files API.
package spec

// --- fixtures ----------------------------------------------------------------

// validDesignFiles is a well-formed working-tree map that AssembleDesign
// accepts: a root design.cell (frontmatter carrying sourceSpec) plus one service
// component with a design.json + openapi.yaml. Mirrors the harvested golden shape.
func validDesignFiles() map[string]string {
	return map[string]string{
		DesignRootFile: "---\nsourceSpec: v1\n---\ntitle Hello\n\ncomponent hello-api as \"Hello API\" service\n",
		"components/hello-api/design.json": "{\n" +
			"  \"name\": \"hello-api\",\n" +
			"  \"type\": \"service\",\n" +
			"  \"language\": \"Go\",\n" +
			"  \"description\": \"Build it.\",\n" +
			"  \"dependencies\": []\n" +
			"}\n",
		"components/hello-api/openapi.yaml": "openapi: 3.0.3\n",
	}
}

// newService builds the REAL designService over the given fake artifact service,
// wrapping it in the REAL ArtifactStore decorator.
func newService(fake *fakeArtifactSvc) *designService {
	return &designService{
		store:       NewArtifactStore(fake),
		artifactSvc: fake,
	}
}
