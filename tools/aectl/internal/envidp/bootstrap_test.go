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

package envidp

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestRenderBootstrapDocuments_Substitution(t *testing.T) {
	docs := renderBootstrapDocuments("acme", "prod", "shh-secret", "https://prod-idp.example.com/mcp")

	byName := make(map[string]string, len(docs))
	for _, d := range docs {
		byName[d.name] = d.content
		if strings.Contains(d.content, "${") {
			t.Errorf("document %s still has an unsubstituted placeholder:\n%s", d.name, d.content)
		}
	}

	for _, want := range []string{"13-fix-thunder-system-rs-identifier.yaml", "80-aep-system-client.yaml", "84-aep-system-role.yaml"} {
		if _, ok := byName[want]; !ok {
			t.Errorf("expected document %q not present; got %v", want, docNames(docs))
		}
	}

	client := byName["80-aep-system-client.yaml"]
	if !strings.Contains(client, "shh-secret") {
		t.Errorf("client document missing substituted secret:\n%s", client)
	}
	if !strings.Contains(client, "prod") {
		t.Errorf("client document missing substituted env name:\n%s", client)
	}

	rsFix := byName["13-fix-thunder-system-rs-identifier.yaml"]
	if !strings.Contains(rsFix, "https://prod-idp.example.com/mcp") {
		t.Errorf("resource-server fix missing substituted identifier:\n%s", rsFix)
	}
}

// TestRenderBootstrapDocuments_ValidYAML guards against a template edit that
// breaks YAML structure — every rendered document must parse.
func TestRenderBootstrapDocuments_ValidYAML(t *testing.T) {
	docs := renderBootstrapDocuments("default", "default", "secret", "http://x/mcp")
	for _, d := range docs {
		var out map[string]any
		if err := yaml.Unmarshal([]byte(d.content), &out); err != nil {
			t.Errorf("document %s is not valid YAML: %v\n%s", d.name, err, d.content)
		}
	}
}

// TestAepSystemClientDoc_MatchesSourceShape pins the embedded document's
// resource_type/id/scopes fields against the values
// thunder-env-resources/80-aep-system-client.yaml declares, so a drift
// between the two is a test failure rather than a silent divergence the next
// time either is edited (see addons_test.go's own
// TestEmbeddedResourceTypes_MatchTheAuthoredSource for the same pattern
// against ClusterResourceTypes).
func TestAepSystemClientDoc_MatchesSourceShape(t *testing.T) {
	var doc struct {
		ResourceType      string `yaml:"resource_type"`
		ID                string `yaml:"id"`
		Type              string `yaml:"type"`
		InboundAuthConfig []struct {
			Config struct {
				ClientID   string   `yaml:"clientId"`
				Scopes     []string `yaml:"scopes"`
				GrantTypes []string `yaml:"grantTypes"`
			} `yaml:"config"`
		} `yaml:"inboundAuthConfig"`
	}
	if err := yaml.Unmarshal([]byte(aepSystemClientDoc), &doc); err != nil {
		t.Fatalf("parse aepSystemClientDoc: %v", err)
	}
	if doc.ResourceType != "application" {
		t.Errorf("resource_type = %q, want application", doc.ResourceType)
	}
	if doc.ID != "aep-system-client" {
		t.Errorf("id = %q, want aep-system-client", doc.ID)
	}
	if doc.Type != "m2m" {
		t.Errorf("type = %q, want m2m", doc.Type)
	}
	if len(doc.InboundAuthConfig) != 1 {
		t.Fatalf("expected exactly one inboundAuthConfig entry, got %d", len(doc.InboundAuthConfig))
	}
	cfg := doc.InboundAuthConfig[0].Config
	if cfg.ClientID != "aep-system-client" {
		t.Errorf("clientId = %q, want aep-system-client", cfg.ClientID)
	}
	if len(cfg.Scopes) != 1 || cfg.Scopes[0] != "system" {
		t.Errorf("scopes = %v, want [system] only", cfg.Scopes)
	}
	if len(cfg.GrantTypes) != 1 || cfg.GrantTypes[0] != "client_credentials" {
		t.Errorf("grantTypes = %v, want [client_credentials] only", cfg.GrantTypes)
	}
}

// TestAepSystemRoleDoc_MatchesSourceShape pins the embedded role document's
// shape against thunder-env-resources/84-aep-system-role.yaml.
func TestAepSystemRoleDoc_MatchesSourceShape(t *testing.T) {
	var doc struct {
		ResourceType string `yaml:"resource_type"`
		ID           string `yaml:"id"`
		Permissions  []struct {
			ResourceServerID string   `yaml:"resourceServerId"`
			Permissions      []string `yaml:"permissions"`
		} `yaml:"permissions"`
		Assignments []struct {
			ID   string `yaml:"id"`
			Type string `yaml:"type"`
		} `yaml:"assignments"`
	}
	if err := yaml.Unmarshal([]byte(aepSystemRoleDoc), &doc); err != nil {
		t.Fatalf("parse aepSystemRoleDoc: %v", err)
	}
	if doc.ResourceType != "role" {
		t.Errorf("resource_type = %q, want role", doc.ResourceType)
	}
	if len(doc.Permissions) != 1 || len(doc.Permissions[0].Permissions) != 1 || doc.Permissions[0].Permissions[0] != "system" {
		t.Errorf("permissions = %+v, want exactly one entry granting only 'system'", doc.Permissions)
	}
	// The built-in System resource server's fixed id — never the
	// Administrator role's id (see the doc comment on why this role is
	// AEP-owned, not the built-in Administrator).
	if doc.Permissions[0].ResourceServerID != "01900000-0000-7000-8000-000000000020" {
		t.Errorf("resourceServerId = %q, want the built-in System resource server id", doc.Permissions[0].ResourceServerID)
	}
	if len(doc.Assignments) != 1 || doc.Assignments[0].ID != "aep-system-client" || doc.Assignments[0].Type != "app" {
		t.Errorf("assignments = %+v, want exactly aep-system-client/app", doc.Assignments)
	}
}

func docNames(docs []bootstrapDocument) []string {
	names := make([]string, len(docs))
	for i, d := range docs {
		names[i] = d.name
	}
	return names
}
