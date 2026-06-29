package artifacts

import (
	"strings"
	"testing"

	"github.com/wso2/asdlc/asdlc-service/models"
)

func TestDependencyNeedsSpecRoundTrip(t *testing.T) {
	in := &DesignFile{
		Overview: "x",
		Components: []models.DesignComponent{{
			Name:          "weather-api",
			ComponentType: "service",
			Language:      "go",
			Dependencies: []models.Dependency{{
				Kind:        models.DependencyKindExternal,
				Name:        "openweather",
				Description: "Current-weather REST API.",
				NeedsSpec:   true,
				SpecPath:    "dependencies/openweather.openapi.yaml",
				Config:      []models.ConfigKey{{Key: "OPENWEATHER_API_KEY", Secret: true}},
			}},
		}},
	}
	files, err := SplitDesign(in)
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	md := files["components/weather-api/design.md"]
	if !strings.Contains(md, "needsSpec: true") || !strings.Contains(md, "specPath: dependencies/openweather.openapi.yaml") {
		t.Fatalf("frontmatter missing needsSpec/specPath:\n%s", md)
	}
	out, err := AssembleDesign(files)
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	dep := out.Components[0].Dependencies[0]
	if !dep.NeedsSpec || dep.SpecPath != "dependencies/openweather.openapi.yaml" {
		t.Fatalf("round-trip lost fields: %+v", dep)
	}
}
