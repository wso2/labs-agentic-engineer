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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// testMCPURL is the in-cluster MCP endpoint aectl builds for a "wso2-aep"
// AEP namespace.
const testMCPURL = "http://aep-mcp-server.wso2-aep.svc.cluster.local:3400/mcp"

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
			URL string `json:"url"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal([]byte(cm.Data["mcp.json"]), &rendered); err != nil {
		t.Fatalf("rendered mcp.json is not JSON: %v\n%s", err, cm.Data["mcp.json"])
	}
	if got := rendered.MCPServers["ae"].URL; got != testMCPURL {
		t.Fatalf("mcp.json ae url = %q, want %q", got, testMCPURL)
	}
	if strings.Contains(cm.Data["mcp.json"], sreMCPURLPlaceholder) {
		t.Fatalf("mcp.json still carries the placeholder: %s", cm.Data["mcp.json"])
	}
}

func TestMountSREAgentRuntimePatchesExtensionAndCredentialFile(t *testing.T) {
	ctx := context.Background()

	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "ai-rca-agent", Namespace: "obs"},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					// An install from an earlier aectl carries an unused
					// AEP_MCP_TOKEN secret ref; the patch must remove it.
					Containers: []corev1.Container{{
						Name: "ai-rca-agent",
						Env: []corev1.EnvVar{{
							Name: "AEP_MCP_TOKEN",
							ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "aep-mcp-token"},
								Key:                  "AEP_MCP_TOKEN",
							}},
						}},
					}},
				},
			},
		},
	}
	client := fake.NewSimpleClientset(deploy)

	if err := mountSREAgentRuntime(ctx, client, "obs", "ai-rca-agent", testMCPURL); err != nil {
		t.Fatalf("mount runtime: %v", err)
	}

	got, err := client.AppsV1().Deployments("obs").Get(ctx, "ai-rca-agent", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get deployment: %v", err)
	}
	volumes := got.Spec.Template.Spec.Volumes
	if len(volumes) != 2 {
		t.Fatalf("volumes len = %d, want 2", len(volumes))
	}
	if volumes[0].Name != "sre-agent-extensions" || volumes[0].ConfigMap == nil {
		t.Fatalf("missing extension configmap volume: %#v", volumes[0])
	}
	items := volumes[0].ConfigMap.Items
	wantPaths := []string{
		"remediation/mcp.json",
		"remediation/CONTEXT.md",
		"remediation/skills/coding-agent-handoff/SKILL.md",
	}
	for i, want := range wantPaths {
		if got := items[i].Path; got != want {
			t.Fatalf("item[%d].Path = %q, want %q", i, got, want)
		}
	}
	if volumes[1].Name != "anthropic-key" || volumes[1].Secret == nil || volumes[1].Secret.SecretName != "rca-agent-anthropic-secret" {
		t.Fatalf("missing anthropic secret volume: %#v", volumes[1])
	}

	c := got.Spec.Template.Spec.Containers[0]
	assertEnv := func(name, value string) {
		t.Helper()
		for _, env := range c.Env {
			if env.Name == name && env.Value == value {
				return
			}
		}
		t.Fatalf("missing env %s=%s in %#v", name, value, c.Env)
	}
	assertEnv("EXTENSIONS_DIR", "/etc/openchoreo/sre-agent")
	assertEnv("RCA_LLM_API_KEY_FILE", "/etc/rca-agent/anthropic/RCA_LLM_API_KEY")
	assertEnv("AEP_MCP_URL", testMCPURL)
	for _, env := range c.Env {
		if env.Name == "AEP_MCP_TOKEN" {
			t.Fatalf("SRE pod must not carry the MCP credential; found %#v", env)
		}
	}
	if len(c.VolumeMounts) != 2 {
		t.Fatalf("volumeMounts len = %d, want 2", len(c.VolumeMounts))
	}
}
