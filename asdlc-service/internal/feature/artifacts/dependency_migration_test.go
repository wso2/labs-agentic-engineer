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

// Legacy frontmatter (dependsOn + dependentApis) folds into the unified
// dependencies model on read.
func TestAssemble_FoldsLegacyDependencies(t *testing.T) {
	files := map[string]string{
		DesignRootFile: "System overview.\n",
		"components/web/design.md": "---\n" +
			"type: web-app\n" +
			"dependsOn:\n  - api\n" +
			"dependentApis:\n" +
			"  - name: employee-api\n" + // name-only → org-service
			"  - name: weather\n    url: https://api.example.com\n    authentication: api-key\n" + // url+auth → external
			"---\nbody\n",
	}
	d, err := AssembleDesign(files)
	if err != nil {
		t.Fatalf("AssembleDesign: %v", err)
	}
	if len(d.Components) != 1 {
		t.Fatalf("want 1 component, got %d", len(d.Components))
	}
	deps := d.Components[0].Dependencies
	if len(deps) != 3 {
		t.Fatalf("want 3 folded deps, got %d: %+v", len(deps), deps)
	}
	byName := map[string]models.Dependency{}
	for _, dep := range deps {
		byName[dep.Name] = dep
	}
	if byName["api"].Kind != models.DependencyKindComponent {
		t.Errorf("api: want component, got %q", byName["api"].Kind)
	}
	if byName["employee-api"].Kind != models.DependencyKindOrgService {
		t.Errorf("employee-api: want org-service, got %q", byName["employee-api"].Kind)
	}
	w := byName["weather"]
	if w.Kind != models.DependencyKindExternal {
		t.Fatalf("weather: want external, got %q", w.Kind)
	}
	// external fold derives a base-URL config key + an api-key secret.
	var hasURL, hasKey bool
	for _, c := range w.Config {
		if c.Key == "WEATHER_BASE_URL" && !c.Secret {
			hasURL = true
		}
		if c.Key == "WEATHER_API_KEY" && c.Secret {
			hasKey = true
		}
	}
	if !hasURL || !hasKey {
		t.Errorf("weather config fold wrong: %+v", w.Config)
	}
}

// A unified design round-trips through Split → Assemble unchanged, and the
// emitted frontmatter uses `dependencies:` (not the legacy fields).
func TestSplitAssemble_UnifiedDependenciesRoundTrip(t *testing.T) {
	in := &DesignFile{
		Overview: "overview",
		Components: []models.DesignComponent{
			{
				Name:          "api",
				ComponentType: "service",
				Language:      "Go",
				Entrypoint:    "deployment/service",
				Buildpack:     "docker",
				AppPath:       "api",
				Dependencies: []models.Dependency{
					{Kind: models.DependencyKindComponent, Name: "notifier"},
					{
						Kind:        models.DependencyKindExternal,
						Name:        "openweather",
						Description: "OpenWeatherMap REST API.",
						Status:      "resolved",
						Config: []models.ConfigKey{
							{Key: "OPENWEATHER_BASE_URL", Secret: false},
							{Key: "OPENWEATHER_API_KEY", Secret: true},
						},
					},
				},
			},
		},
	}
	files, err := SplitDesign(in)
	if err != nil {
		t.Fatalf("SplitDesign: %v", err)
	}
	fm := files["components/api/design.md"]
	if !strings.Contains(fm, "dependencies:") {
		t.Fatalf("emitted frontmatter missing dependencies:\n%s", fm)
	}
	if strings.Contains(fm, "dependentApis:") || strings.Contains(fm, "dependsOn:") {
		t.Fatalf("emitted frontmatter still has legacy fields:\n%s", fm)
	}

	out, err := AssembleDesign(files)
	if err != nil {
		t.Fatalf("AssembleDesign: %v", err)
	}
	got := out.Components[0].Dependencies
	if len(got) != 2 {
		t.Fatalf("want 2 deps after round-trip, got %d: %+v", len(got), got)
	}
	if got[0].Kind != models.DependencyKindComponent || got[0].Name != "notifier" {
		t.Errorf("dep[0] wrong: %+v", got[0])
	}
	if got[1].Kind != models.DependencyKindExternal || len(got[1].Config) != 2 {
		t.Errorf("dep[1] wrong: %+v", got[1])
	}
}
