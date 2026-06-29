package artifacts

import (
	"strings"
	"testing"

	"github.com/wso2/asdlc/asdlc-service/models"
)

func TestExternalNeedsSpecUnresolvedAtRead(t *testing.T) {
	files, err := SplitDesign(&DesignFile{
		Overview: "x",
		Components: []models.DesignComponent{{
			Name: "weather-api", ComponentType: "service", Language: "go",
			Dependencies: []models.Dependency{{
				Kind: models.DependencyKindExternal, Name: "openweather",
				Description: "weather", NeedsSpec: true, // no SpecPath
				Config: []models.ConfigKey{{Key: "OPENWEATHER_API_KEY", Secret: true}},
			}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := AssembleDesign(files)
	if err != nil {
		t.Fatal(err)
	}
	if got := out.Components[0].Dependencies[0].Status; got != "unresolved" {
		t.Fatalf("want unresolved, got %q", got)
	}
}

func TestExternalNeedsSpecWithSpecPathResolved(t *testing.T) {
	files, err := SplitDesign(&DesignFile{
		Overview: "x",
		Components: []models.DesignComponent{{
			Name: "weather-api", ComponentType: "service", Language: "go",
			Dependencies: []models.Dependency{{
				Kind: models.DependencyKindExternal, Name: "openweather",
				Description: "weather", NeedsSpec: true,
				SpecPath: "dependencies/openweather.openapi.yaml",
				Config:   []models.ConfigKey{{Key: "OPENWEATHER_API_KEY", Secret: true}},
			}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := AssembleDesign(files)
	if err != nil {
		t.Fatal(err)
	}
	if got := out.Components[0].Dependencies[0].Status; got == "unresolved" {
		t.Fatalf("want not-unresolved (specPath set), got %q", got)
	}
}

// TestExternalNeedsSpecReasonNeedsSpec asserts the 4-state model (task A2b):
// an external dep with needsSpec=true and no specPath must produce
// status="unresolved" AND reason="needs-spec".
func TestExternalNeedsSpecReasonNeedsSpec(t *testing.T) {
	files, err := SplitDesign(&DesignFile{
		Overview: "x",
		Components: []models.DesignComponent{{
			Name: "weather-api", ComponentType: "service", Language: "go",
			Dependencies: []models.Dependency{{
				Kind: models.DependencyKindExternal, Name: "openweather",
				Description: "weather", NeedsSpec: true, // no SpecPath
			}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := AssembleDesign(files)
	if err != nil {
		t.Fatal(err)
	}
	dep := out.Components[0].Dependencies[0]
	if dep.Status != "unresolved" {
		t.Errorf("needsSpec/no-specPath: status = %q, want %q", dep.Status, "unresolved")
	}
	if dep.Reason != "needs-spec" {
		t.Errorf("needsSpec/no-specPath: reason = %q, want %q", dep.Reason, "needs-spec")
	}
}

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
