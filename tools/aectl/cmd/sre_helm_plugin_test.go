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
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestWriteSREPostRenderPlugin(t *testing.T) {
	dir := t.TempDir()
	name, err := writeSREPostRenderPlugin(dir, "/usr/local/bin/aectl")
	if err != nil {
		t.Fatal(err)
	}
	if name != srePostRenderPluginName {
		t.Fatalf("name = %q", name)
	}
	b, err := os.ReadFile(filepath.Join(dir, srePostRenderPluginName, "plugin.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"postrenderer/v1", "/usr/local/bin/aectl", "post-render"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("plugin.yaml missing %q:\n%s", want, b)
		}
	}
}

func TestSREHelmPostRenderer(t *testing.T) {
	t.Run("helm 4 takes the plugin from a throwaway HELM_PLUGINS", func(t *testing.T) {
		flags, env, cleanup, err := sreHelmPostRenderer(4, "/usr/local/bin/aectl")
		if err != nil {
			t.Fatal(err)
		}
		if want := []string{"--post-renderer", srePostRenderPluginName}; !slices.Equal(flags, want) {
			t.Errorf("flags = %q, want %q", flags, want)
		}
		if len(env) != 1 || !strings.HasPrefix(env[0], "HELM_PLUGINS=") {
			t.Fatalf("env = %q, want one HELM_PLUGINS entry", env)
		}
		dir := strings.TrimPrefix(env[0], "HELM_PLUGINS=")
		if _, err := os.Stat(filepath.Join(dir, srePostRenderPluginName, "plugin.yaml")); err != nil {
			t.Fatalf("plugin not written: %v", err)
		}
		cleanup()
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Errorf("cleanup must remove %s (stat err: %v)", dir, err)
		}
	})
	t.Run("helm 3 runs aectl as an executable post-renderer", func(t *testing.T) {
		flags, env, cleanup, err := sreHelmPostRenderer(3, "/usr/local/bin/aectl")
		if err != nil {
			t.Fatal(err)
		}
		defer cleanup()
		want := []string{"--post-renderer", "/usr/local/bin/aectl", "--post-renderer-args", "sre", "--post-renderer-args", "post-render"}
		if !slices.Equal(flags, want) || env != nil {
			t.Errorf("flags = %q env = %q, want %q and no env", flags, env, want)
		}
	})
}
