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
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

// The stock observability-plane chart exposes no extraVolumes for the SRE
// agent, so aectl adds what the agent needs to the rendered Deployment with a
// Helm post-renderer (`aectl sre post-render`, stdin -> stdout):
//
//   - the sre-agent-extensions ConfigMap at EXTENSIONS_DIR, laid out as the
//     remediation extension the agent's loader expects; and
//   - a CA bundle for SSL_CERT_FILE: the image's system bundle plus
//     cluster-gateway-ca, concatenated in-pod by an initContainer into an
//     emptyDir, so the agent trusts aep-mcp-server's https endpoint.
const (
	sreExtensionsVolume    = sreExtensionsConfigMap
	sreExtensionsMountPath = "/opt/aep/sre-agent-extensions"

	sreClusterCAVolume    = "cluster-gateway-ca"
	sreClusterCAMountPath = "/opt/aep/cluster-ca"
	sreCABundleVolume     = "aep-ca-bundle"
	sreCABundleMountPath  = "/opt/aep/ca"
	sreCABundleInit       = "aep-ca-bundle"

	// sreSystemCABundle is where the agent image keeps its system CA bundle
	// (the image's own SSL_CERT_FILE default).
	sreSystemCABundle = "/etc/ssl/certs/ca-certificates.crt"
)

// sreCABundleScript runs under the agent image's python (the image has no
// shell) and writes the merged bundle SSL_CERT_FILE points at.
var sreCABundleScript = fmt.Sprintf(
	"import pathlib as p; p.Path(%q).write_text(p.Path(%q).read_text() + '\\n' + p.Path(%q).read_text())",
	sreCABundleMountPath+"/ca-bundle.crt", sreSystemCABundle, sreClusterCAMountPath+"/ca.crt")

var srePostRenderComponent string

var srePostRenderCmd = &cobra.Command{
	Use:          "post-render",
	Short:        "Helm post-renderer: mount the SRE extensions and CA bundle into the SRE agent",
	Hidden:       true,
	SilenceUsage: true,
	Args:         cobra.NoArgs,
	// A pure stdin -> stdout transform: skip the root command's cluster setup.
	PersistentPreRunE: func(*cobra.Command, []string) error { return nil },
	RunE: func(cmd *cobra.Command, _ []string) error {
		in, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return err
		}
		// The Helm plugin (sre_helm_plugin.go) invokes this with no args, so by
		// default any accepted component label matches: sreAgentComponents
		// covers both the 1.2.0+ "sre-agent" label and the pre-1.2.0
		// "ai-rca-agent" label an adopted plane may still carry. An explicit
		// --component narrows matching to that one value.
		accepted := sreAgentComponents
		if cmd.Flags().Changed("component") {
			accepted = []string{srePostRenderComponent}
		}
		out, err := addExtensionsMount(in, accepted)
		if err != nil {
			return err
		}
		_, err = cmd.OutOrStdout().Write(out)
		return err
	},
}

func init() {
	sreCmd.AddCommand(srePostRenderCmd)
	srePostRenderCmd.Flags().StringVar(&srePostRenderComponent, "component", "sre-agent",
		"app.kubernetes.io/component label (and container name) to restrict matching to; "+
			"unset, any of sreAgentComponents matches (the chart's rca.name, current or pre-1.2.0)")
}

var manifestSeparator = regexp.MustCompile(`(?m)^---[ \t]*\n?`)

// addExtensionsMount wires the extensions mount and the CA bundle into the
// Deployment whose app.kubernetes.io/component label is in accepted, on its
// container named after that matched label value. Other documents pass
// through verbatim. Idempotent. It fails when no such Deployment is in the
// stream, so a chart change that renames the agent beyond accepted cannot
// silently drop the mounts.
func addExtensionsMount(manifests []byte, accepted []string) ([]byte, error) {
	var docs []string
	for _, d := range manifestSeparator.Split(string(manifests), -1) {
		if strings.TrimSpace(d) != "" {
			docs = append(docs, d)
		}
	}
	found := false
	for i, doc := range docs {
		var obj map[string]interface{}
		if err := yaml.Unmarshal([]byte(doc), &obj); err != nil {
			return nil, fmt.Errorf("decode manifest %d: %w", i, err)
		}
		u := unstructured.Unstructured{Object: obj}
		component := u.GetLabels()["app.kubernetes.io/component"]
		if obj == nil || u.GetKind() != "Deployment" || !slices.Contains(accepted, component) {
			continue
		}
		if err := wireSREAgentDeployment(&u, component); err != nil {
			return nil, fmt.Errorf("deployment %s: %w", u.GetName(), err)
		}
		b, err := yaml.Marshal(u.Object)
		if err != nil {
			return nil, err
		}
		docs[i] = string(b)
		found = true
	}
	if !found {
		return nil, fmt.Errorf("no Deployment labelled app.kubernetes.io/component in (%s) in the rendered manifests",
			strings.Join(accepted, ", "))
	}
	var b strings.Builder
	for _, d := range docs {
		b.WriteString("---\n")
		b.WriteString(d)
		if !strings.HasSuffix(d, "\n") {
			b.WriteString("\n")
		}
	}
	return []byte(b.String()), nil
}

// wireSREAgentDeployment mutates only spec.template.spec.volumes,
// spec.template.spec.initContainers, and the agent container's
// volumeMounts, leaving every other field of the rendered pod spec exactly
// as the chart produced it. It never round-trips the pod spec through a
// typed corev1.PodSpec: doing so would silently drop any chart-authored
// field the vendored k8s.io/api types don't model, and would add
// zero-value fields (e.g. `resources: {}`) that were never there.
func wireSREAgentDeployment(u *unstructured.Unstructured, component string) error {
	containers, _, err := unstructured.NestedSlice(u.Object, "spec", "template", "spec", "containers")
	if err != nil {
		return fmt.Errorf("read containers: %w", err)
	}

	var agent map[string]interface{}
	for _, c := range containers {
		cm, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		if name, _, _ := unstructured.NestedString(cm, "name"); name == component {
			agent = cm
			break
		}
	}
	if agent == nil {
		return fmt.Errorf("no container named %q", component)
	}

	image, _, _ := unstructured.NestedString(agent, "image")
	imagePullPolicy, _, _ := unstructured.NestedString(agent, "imagePullPolicy")
	// NestedMap deep-copies, so the initContainer below gets its own copy.
	securityContext, _, err := unstructured.NestedMap(agent, "securityContext")
	if err != nil {
		return fmt.Errorf("read agent securityContext: %w", err)
	}

	// Optional: Helm renders the agent before `sre install` applies the
	// ConfigMap (then restarts the agent), and --ae-handoff=false never
	// applies it; neither may keep the pod from starting.
	volumes, _, err := unstructured.NestedSlice(u.Object, "spec", "template", "spec", "volumes")
	if err != nil {
		return fmt.Errorf("read volumes: %w", err)
	}
	volumes = appendUnlessNamed(volumes, sreExtensionsVolumeManifest())
	volumes = appendUnlessNamed(volumes, sreClusterCAVolumeManifest())
	volumes = appendUnlessNamed(volumes, sreCABundleVolumeManifest())
	if err := unstructured.SetNestedSlice(u.Object, volumes, "spec", "template", "spec", "volumes"); err != nil {
		return fmt.Errorf("set volumes: %w", err)
	}

	mounts, _, err := unstructured.NestedSlice(agent, "volumeMounts")
	if err != nil {
		return fmt.Errorf("read agent volumeMounts: %w", err)
	}
	mounts = appendUnlessNamed(mounts, volumeMountManifest(sreExtensionsVolume, sreExtensionsMountPath, true))
	mounts = appendUnlessNamed(mounts, volumeMountManifest(sreCABundleVolume, sreCABundleMountPath, true))
	if err := unstructured.SetNestedSlice(agent, mounts, "volumeMounts"); err != nil {
		return fmt.Errorf("set agent volumeMounts: %w", err)
	}
	if err := unstructured.SetNestedSlice(u.Object, containers, "spec", "template", "spec", "containers"); err != nil {
		return fmt.Errorf("set containers: %w", err)
	}

	initContainers, _, err := unstructured.NestedSlice(u.Object, "spec", "template", "spec", "initContainers")
	if err != nil {
		return fmt.Errorf("read initContainers: %w", err)
	}
	for _, c := range initContainers {
		cm, ok := c.(map[string]interface{})
		if ok {
			if name, _, _ := unstructured.NestedString(cm, "name"); name == sreCABundleInit {
				return nil // already wired
			}
		}
	}
	initContainer := map[string]interface{}{
		"name":    sreCABundleInit,
		"image":   image,
		"command": []interface{}{"python", "-c", sreCABundleScript},
		"volumeMounts": []interface{}{
			volumeMountManifest(sreClusterCAVolume, sreClusterCAMountPath, true),
			volumeMountManifest(sreCABundleVolume, sreCABundleMountPath, false),
		},
	}
	if imagePullPolicy != "" {
		initContainer["imagePullPolicy"] = imagePullPolicy
	}
	if securityContext != nil {
		initContainer["securityContext"] = securityContext
	}
	initContainers = append(initContainers, initContainer)
	if err := unstructured.SetNestedSlice(u.Object, initContainers, "spec", "template", "spec", "initContainers"); err != nil {
		return fmt.Errorf("set initContainers: %w", err)
	}
	return nil
}

// appendUnlessNamed appends item unless items already has a map with
// the same "name" key, so wiring stays idempotent across repeated renders.
func appendUnlessNamed(items []interface{}, item map[string]interface{}) []interface{} {
	name, _, _ := unstructured.NestedString(item, "name")
	for _, existing := range items {
		m, ok := existing.(map[string]interface{})
		if ok {
			if n, _, _ := unstructured.NestedString(m, "name"); n == name {
				return items
			}
		}
	}
	return append(items, item)
}

func sreExtensionsVolumeManifest() map[string]interface{} {
	return map[string]interface{}{
		"name": sreExtensionsVolume,
		"configMap": map[string]interface{}{
			"name":     sreExtensionsConfigMap,
			"optional": true,
			"items": []interface{}{
				map[string]interface{}{"key": sreExtensionsKeyMCPJSON, "path": "remediation/mcp.json"},
				map[string]interface{}{"key": sreExtensionsKeyContext, "path": "remediation/CONTEXT.md"},
				map[string]interface{}{"key": sreExtensionsKeySkillMD, "path": "remediation/skills/coding-agent-handoff/SKILL.md"},
			},
		},
	}
}

func sreClusterCAVolumeManifest() map[string]interface{} {
	return map[string]interface{}{
		"name": sreClusterCAVolume,
		"configMap": map[string]interface{}{
			"name": sreClusterCAVolume,
			"items": []interface{}{
				map[string]interface{}{"key": "ca.crt", "path": "ca.crt"},
			},
		},
	}
}

func sreCABundleVolumeManifest() map[string]interface{} {
	return map[string]interface{}{
		"name":     sreCABundleVolume,
		"emptyDir": map[string]interface{}{},
	}
}

func volumeMountManifest(name, mountPath string, readOnly bool) map[string]interface{} {
	m := map[string]interface{}{"name": name, "mountPath": mountPath}
	if readOnly {
		m["readOnly"] = true
	}
	return m
}
