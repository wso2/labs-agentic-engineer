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

package cmd

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// testMCPURL is the MCP endpoint aectl builds for its default --mcp-hostname
// and --mcp-port.
const testMCPURL = "https://aep-mcp.openchoreo.localhost:8443/internal/v1/sre-handoff/mcp"

func TestLoadSREExtensionAssetsFromRepo(t *testing.T) {
	assets, err := loadSreExtensionAssets("")
	if err != nil {
		t.Fatalf("load assets: %v", err)
	}
	if !strings.Contains(assets.MCPJSON, "${AEP_MCP_URL}") {
		t.Fatalf("mcp.json does not use AEP_MCP_URL placeholder: %s", assets.MCPJSON)
	}
	if !strings.Contains(assets.Context, "load_skill('coding-agent-handoff')") {
		t.Fatalf("CONTEXT.md does not load coding-agent-handoff")
	}
	if !strings.Contains(assets.Context, "actionStatuses") {
		t.Fatalf("CONTEXT.md does not require actionStatuses")
	}
	if !strings.Contains(assets.SkillMD, "ae_search_related_issues") {
		t.Fatalf("SKILL.md does not name ae_search_related_issues")
	}
	if !strings.Contains(assets.SkillMD, "ae_create_issue") {
		t.Fatalf("SKILL.md does not name ae_create_issue")
	}
}

// writeSREAssets lays out a minimal AE checkout under root.
func writeSREAssets(t *testing.T, root string) {
	t.Helper()
	for rel, body := range map[string]string{
		sreAssetMCPJSON: "mcp",
		sreAssetContext: "context",
		sreAssetSkillMD: "skill",
	} {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLoadSREExtensionAssetsFromExplicitRoot(t *testing.T) {
	root := t.TempDir()
	writeSREAssets(t, root)

	assets, err := loadSreExtensionAssets(root)
	if err != nil {
		t.Fatalf("load assets: %v", err)
	}
	if assets.MCPJSON != "mcp" || assets.Context != "context" || assets.SkillMD != "skill" || assets.RootHint != root {
		t.Fatalf("unexpected assets: %#v", assets)
	}
}

func TestLoadSREExtensionAssetsRejectsExplicitRootWithoutAssets(t *testing.T) {
	_, err := loadSreExtensionAssets(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), sreAssetsRootFlag) {
		t.Fatalf("err = %v, want an error naming %s", err, sreAssetsRootFlag)
	}
}

func TestFindRepoRootForSREAssetsWalksUp(t *testing.T) {
	root := t.TempDir()
	writeSREAssets(t, root)
	nested := filepath.Join(root, "tools", "aectl", "cmd")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := findRepoRootForSREAssets(nested)
	if err != nil {
		t.Fatalf("find root: %v", err)
	}
	if got != root {
		t.Fatalf("root = %q, want %q", got, root)
	}
}

func TestFindRepoRootForSREAssetsOutsideCheckoutNamesFlag(t *testing.T) {
	_, err := findRepoRootForSREAssets(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), sreAssetsRootFlag) {
		t.Fatalf("err = %v, want an error naming %s", err, sreAssetsRootFlag)
	}
}

func TestApplyExtensionsConfigMapIsIdempotent(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	assets := sreExtensionAssets{MCPJSON: "mcp", Context: "context", SkillMD: "skill"}

	if err := applyExtensionsConfigMap(ctx, client, "obs", assets, testMCPURL); err != nil {
		t.Fatalf("apply first: %v", err)
	}
	assets.SkillMD = "skill-v2"
	if err := applyExtensionsConfigMap(ctx, client, "obs", assets, testMCPURL); err != nil {
		t.Fatalf("apply second: %v", err)
	}

	cm, err := client.CoreV1().ConfigMaps("obs").Get(ctx, "sre-agent-extensions", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get configmap: %v", err)
	}
	if got := cm.Data["SKILL.md"]; got != "skill-v2" {
		t.Fatalf("SKILL.md = %q, want updated value", got)
	}
}

// The loader validates the MCP URL before env expansion, so the ConfigMap must
// carry the concrete URL, not the ${AEP_MCP_URL} placeholder from the repo.
// The Authorization header stays a ${AEP_MCP_TOKEN} reference the agent
// expands from its own env, so the token never lands in the ConfigMap.
func TestApplyExtensionsConfigMapRendersMCPURL(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	assets, err := loadSreExtensionAssets("")
	if err != nil {
		t.Fatalf("load assets: %v", err)
	}

	if err := applyExtensionsConfigMap(ctx, client, "obs", assets, testMCPURL); err != nil {
		t.Fatalf("apply: %v", err)
	}

	cm, err := client.CoreV1().ConfigMaps("obs").Get(ctx, "sre-agent-extensions", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get configmap: %v", err)
	}
	var rendered struct {
		MCPServers map[string]struct {
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal([]byte(cm.Data["mcp.json"]), &rendered); err != nil {
		t.Fatalf("rendered mcp.json is not JSON: %v\n%s", err, cm.Data["mcp.json"])
	}
	if got := rendered.MCPServers["ae"].URL; got != testMCPURL {
		t.Fatalf("mcp.json ae url = %q, want %q", got, testMCPURL)
	}
	if got, want := rendered.MCPServers["ae"].Headers["Authorization"], "Bearer ${AEP_MCP_TOKEN}"; got != want {
		t.Fatalf("mcp.json ae Authorization header = %q, want %q", got, want)
	}
	if strings.Contains(cm.Data["mcp.json"], sreMCPURLPlaceholder) {
		t.Fatalf("mcp.json still carries the placeholder: %s", cm.Data["mcp.json"])
	}
}
