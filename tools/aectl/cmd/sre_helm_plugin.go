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
	"os"
	"path/filepath"
)

// srePostRenderPluginName is the Helm 4 post-renderer plugin aectl writes
// into a throwaway HELM_PLUGINS dir for one `helm upgrade`. Helm 4.2.3 takes
// a plugin name for --post-renderer, not an executable path (per `helm
// upgrade --help`: "the name of a postrenderer type plugin"), so there is no
// executable form of --post-renderer to fall back to.
const srePostRenderPluginName = "aep-sre-postrender"

// writeSREPostRenderPlugin writes a subprocess post-renderer plugin under
// pluginsDir that runs `<aectlPath> sre post-render`, and returns its name
// for use as the --post-renderer value. pluginsDir is meant to be a
// throwaway directory pointed to by HELM_PLUGINS for the single `helm`
// invocation; nothing is installed into the user's real Helm plugin
// directory.
//
// The plugin.yaml shape follows the Helm 4 "postrenderer/v1" plugin type
// with a "subprocess" runtime, verified against the Helm plugin docs
// (helm.sh/docs/plugins/developer/tutorial-postrenderer-plugin/) and by
// loading it from a temp HELM_PLUGINS dir with `helm plugin list` and `helm
// template --post-renderer`.
func writeSREPostRenderPlugin(pluginsDir, aectlPath string) (string, error) {
	dir := filepath.Join(pluginsDir, srePostRenderPluginName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	pluginYAML := fmt.Sprintf(`apiVersion: v1
type: postrenderer/v1
name: %s
version: 0.1.0
runtime: subprocess
runtimeConfig:
  platformCommand:
    - command: %q
      args: ["sre", "post-render"]
`, srePostRenderPluginName, aectlPath)
	if err := os.WriteFile(filepath.Join(dir, "plugin.yaml"), []byte(pluginYAML), 0o600); err != nil {
		return "", err
	}
	return srePostRenderPluginName, nil
}

// sreHelmPostRenderer returns the helm flags and extra environment that route
// a render through `<aectlPath> sre post-render` (addExtensionsMount), for the
// installed helm's major version, plus a cleanup to run once helm exits.
// Helm 4 takes a post-renderer plugin by name, so the plugin is written into a
// throwaway HELM_PLUGINS dir; Helm 3 takes the executable and its args.
func sreHelmPostRenderer(helmMajor int, aectlPath string) (flags, env []string, cleanup func(), err error) {
	if helmMajor < 4 {
		return []string{"--post-renderer", aectlPath,
			"--post-renderer-args", "sre", "--post-renderer-args", "post-render"}, nil, func() {}, nil
	}
	dir, err := os.MkdirTemp("", "aectl-helm-plugins-*")
	if err != nil {
		return nil, nil, nil, err
	}
	cleanup = func() { _ = os.RemoveAll(dir) }
	name, err := writeSREPostRenderPlugin(dir, aectlPath)
	if err != nil {
		cleanup()
		return nil, nil, nil, err
	}
	return []string{"--post-renderer", name}, []string{"HELM_PLUGINS=" + dir}, cleanup, nil
}
