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
	"fmt"
	"os"
	"path/filepath"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type sreExtensionAssets struct {
	MCPJSON  string
	Context  string
	SkillMD  string
	RootHint string
}

// Asset paths relative to an AE repository checkout. The skill is read from
// aep-mcp-server's tree (its canonical source) so the MCP tools and the SRE
// agent's instructions cannot drift apart.
const (
	sreAssetMCPJSON   = "deployments/sre-agent-extensions/remediation/mcp.json"
	sreAssetContext   = "deployments/sre-agent-extensions/remediation/CONTEXT.md"
	sreAssetSkillMD   = "services/aep-mcp-server/skills/coding-agent-handoff/SKILL.md"
	sreAssetsRootFlag = "--assets-root"
)

// The sre-agent-extensions ConfigMap and its keys, shared by the ConfigMap
// aectl applies and the volume the post-renderer mounts it through.
const (
	sreExtensionsConfigMap  = "sre-agent-extensions"
	sreExtensionsKeyMCPJSON = "mcp.json"
	sreExtensionsKeyContext = "CONTEXT.md"
	sreExtensionsKeySkillMD = "SKILL.md"
)

// loadSreExtensionAssets reads the remediation extension from explicitRoot
// when it is set (the --assets-root flag), otherwise from the AE checkout
// found by walking up from the working directory.
func loadSreExtensionAssets(explicitRoot string) (sreExtensionAssets, error) {
	root, err := resolveSREAssetsRoot(explicitRoot)
	if err != nil {
		return sreExtensionAssets{}, err
	}
	read := func(rel string) (string, error) {
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	mcpJSON, err := read(sreAssetMCPJSON)
	if err != nil {
		return sreExtensionAssets{}, fmt.Errorf("read remediation mcp.json: %w", err)
	}
	contextMD, err := read(sreAssetContext)
	if err != nil {
		return sreExtensionAssets{}, fmt.Errorf("read remediation CONTEXT.md: %w", err)
	}
	skillMD, err := read(sreAssetSkillMD)
	if err != nil {
		return sreExtensionAssets{}, fmt.Errorf("read coding-agent-handoff skill: %w", err)
	}
	return sreExtensionAssets{MCPJSON: mcpJSON, Context: contextMD, SkillMD: skillMD, RootHint: root}, nil
}

func resolveSREAssetsRoot(explicitRoot string) (string, error) {
	if explicitRoot != "" {
		if !hasSREAssets(explicitRoot) {
			return "", fmt.Errorf("%s %q does not contain %s, %s and %s", sreAssetsRootFlag, explicitRoot, sreAssetMCPJSON, sreAssetContext, sreAssetSkillMD)
		}
		return explicitRoot, nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return findRepoRootForSREAssets(wd)
}

func findRepoRootForSREAssets(start string) (string, error) {
	for dir := start; ; dir = filepath.Dir(dir) {
		if hasSREAssets(dir) {
			return dir, nil
		}
		if filepath.Dir(dir) == dir {
			break
		}
	}
	return "", fmt.Errorf("could not locate an AE repository checkout containing the SRE extension assets from %s; run from inside a checkout or pass %s <checkout>", start, sreAssetsRootFlag)
}

func hasSREAssets(dir string) bool {
	for _, rel := range []string{sreAssetMCPJSON, sreAssetContext, sreAssetSkillMD} {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			return false
		}
	}
	return true
}

// sreMCPURLPlaceholder is the token remediation/mcp.json carries for the MCP
// URL. The SRE extension loader validates the URL before it expands env vars,
// so the placeholder is rendered here rather than left to AEP_MCP_URL.
const sreMCPURLPlaceholder = "${AEP_MCP_URL}"

// renderMCPJSON substitutes the concrete MCP URL for sreMCPURLPlaceholder.
func renderMCPJSON(mcpJSON, mcpURL string) string {
	return strings.ReplaceAll(mcpJSON, sreMCPURLPlaceholder, mcpURL)
}

func applyExtensionsConfigMap(ctx context.Context, client kubernetes.Interface, ns string, assets sreExtensionAssets, mcpURL string) error {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: sreExtensionsConfigMap, Namespace: ns},
		Data: map[string]string{
			sreExtensionsKeyMCPJSON: renderMCPJSON(assets.MCPJSON, mcpURL),
			sreExtensionsKeyContext: assets.Context,
			sreExtensionsKeySkillMD: assets.SkillMD,
		},
	}
	if _, err := client.CoreV1().ConfigMaps(ns).Create(ctx, cm, metav1.CreateOptions{}); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return err
		}
		if _, err := client.CoreV1().ConfigMaps(ns).Update(ctx, cm, metav1.UpdateOptions{}); err != nil {
			return err
		}
	}
	return nil
}
