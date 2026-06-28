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

package artifacts

import (
	"strings"
	"testing"

	"github.com/wso2/asdlc/asdlc-service/models"
)

// Round-trip frontmatter ± exposesAPI block: components without
// `exposesAPI` produce no `exposesAPI:` line in the YAML, and components
// with `exposesAPI.auth: end-user-required` survive Split → Assemble
// cleanly with managed/userContext preserved.
func TestComponentFrontmatterAPIRoundTrip(t *testing.T) {
	cases := []struct {
		name                string
		comp                models.DesignComponent
		wantContainsExposes bool
		wantAuthAfter       string
	}{
		{
			name: "without exposesAPI block",
			comp: models.DesignComponent{
				Name:                       "svc",
				ComponentType:              "service",
				Language:                   "Go",
				Dependencies:               []models.Dependency{},
				Entrypoint:                 "deployment/service",
				Buildpack:                  "docker",
				AppPath:                    "svc",
				ComponentAgentInstructions: "build it",
			},
			wantContainsExposes: false,
		},
		{
			name: "with exposesAPI.auth=end-user-required",
			comp: models.DesignComponent{
				Name:                       "svc",
				ComponentType:              "service",
				Language:                   "Go",
				Dependencies:               []models.Dependency{},
				Entrypoint:                 "deployment/service",
				Buildpack:                  "docker",
				AppPath:                    "svc",
				ComponentAgentInstructions: "build it",
				ExposesAPI: &models.ExposesAPI{
					Auth:        "end-user-required",
					UserContext: "X-User-Id",
				},
			},
			wantContainsExposes: true,
			wantAuthAfter:       "end-user-required",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := &DesignFile{
				Overview:   "the system",
				Components: []models.DesignComponent{c.comp},
			}
			files, err := SplitDesign(d)
			if err != nil {
				t.Fatalf("SplitDesign: %v", err)
			}
			path := componentDirPrefix + c.comp.Name + "/design.md"
			content, ok := files[path]
			if !ok {
				t.Fatalf("expected %s in files; got keys: %v", path, keysOf(files))
			}
			if c.wantContainsExposes {
				if !strings.Contains(content, "exposesAPI:") || !strings.Contains(content, "auth: "+c.wantAuthAfter) {
					t.Fatalf("expected exposesAPI block with auth=%q in:\n%s", c.wantAuthAfter, content)
				}
			} else {
				if strings.Contains(content, "exposesAPI:") {
					t.Fatalf("did NOT expect any exposesAPI block in:\n%s", content)
				}
			}

			// Assemble the file map back into a DesignFile and check the
			// component round-trips.
			out, err := AssembleDesign(files)
			if err != nil {
				t.Fatalf("AssembleDesign: %v", err)
			}
			if len(out.Components) != 1 {
				t.Fatalf("expected 1 component, got %d", len(out.Components))
			}
			got := out.Components[0]
			if c.wantContainsExposes {
				if got.ExposesAPI == nil || got.ExposesAPI.Auth != c.wantAuthAfter {
					t.Fatalf("after round-trip want exposesAPI.auth=%q, got %+v", c.wantAuthAfter, got.ExposesAPI)
				}
			} else if got.ExposesAPI != nil {
				t.Fatalf("after round-trip want nil ExposesAPI, got %+v", got.ExposesAPI)
			}
		})
	}
}

// TestWriteComponentDesignRendersOrgPublished verifies the rendering path
// WriteComponentDesign relies on: a single-component DesignFile splits to a
// `components/<name>/design.md` whose frontmatter carries `orgPublished: true`,
// and that file round-trips back to a component with OrgPublished set. (The
// store's PutFile is exercised via integration; this guards the encoding.)
func TestWriteComponentDesignRendersOrgPublished(t *testing.T) {
	comp := models.DesignComponent{
		Name:                       "employee-api",
		ComponentType:              "service",
		Language:                   "Go",
		AppPath:                    "employee-api",
		ComponentAgentInstructions: "serve employees",
		ExposesAPI:                 &models.ExposesAPI{OrgPublished: true},
	}
	files, err := SplitDesign(&DesignFile{Components: []models.DesignComponent{comp}})
	if err != nil {
		t.Fatalf("SplitDesign: %v", err)
	}
	subPath := componentDirPrefix + comp.Name + "/design.md"
	content, ok := files[subPath]
	if !ok {
		t.Fatalf("expected %s in files; got %v", subPath, keysOf(files))
	}
	if !strings.Contains(content, "orgPublished: true") {
		t.Fatalf("rendered design.md missing orgPublished:\n%s", content)
	}
	out, err := AssembleDesign(files)
	if err != nil {
		t.Fatalf("AssembleDesign: %v", err)
	}
	if len(out.Components) != 1 || out.Components[0].ExposesAPI == nil || !out.Components[0].ExposesAPI.OrgPublished {
		t.Fatalf("orgPublished did not round-trip: %+v", out.Components)
	}
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
