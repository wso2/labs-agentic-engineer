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
	"testing"

	"github.com/spf13/viper"

	"github.com/wso2/aep/aectl/internal/config"
)

// clearURLKeys resets the public-URL keys, so one test's value cannot
// leak into the next through viper's global registry.
func clearURLKeys(t *testing.T) {
	t.Helper()
	for _, k := range []string{"console.public_url", "tryit.public_url"} {
		viper.Set(k, "")
	}
	t.Cleanup(func() {
		for _, k := range []string{"console.public_url", "tryit.public_url"} {
			viper.Set(k, "")
		}
	})
}

// TestPublicURLsFallBackToLocalDefaults is the install that configures
// nothing: the local k3d URLs, unchanged from before these became config
// keys.
func TestPublicURLsFallBackToLocalDefaults(t *testing.T) {
	clearURLKeys(t)

	if got := consolePublicURL(); got != defaultConsoleURL {
		t.Errorf("consolePublicURL() = %q, want %q", got, defaultConsoleURL)
	}
	if got := tryItPublicURL(); got != defaultTryItURL {
		t.Errorf("tryItPublicURL() = %q, want %q", got, defaultTryItURL)
	}
}

// TestPublicURLsReadConfig is the re-domained install: the config file names
// both origins and nothing else has to be passed on the command line.
func TestPublicURLsReadConfig(t *testing.T) {
	clearURLKeys(t)
	viper.Set("console.public_url", "http://console.ae.example.com:8080")
	viper.Set("tryit.public_url", "http://tryit.ae.example.com:8080")

	if got, want := consolePublicURL(), "http://console.ae.example.com:8080"; got != want {
		t.Errorf("consolePublicURL() = %q, want %q", got, want)
	}
	if got, want := tryItPublicURL(), "http://tryit.ae.example.com:8080"; got != want {
		t.Errorf("tryItPublicURL() = %q, want %q", got, want)
	}
}

// TestPublicURLKeysArePersisted guards the half that is easy to forget: a key
// absent from ConfigMapKeys is accepted by `platform config import` and then
// silently dropped, so the next command reads the default instead of what the
// file said.
func TestPublicURLKeysArePersisted(t *testing.T) {
	for _, want := range []string{"console.public_url", "tryit.public_url"} {
		found := false
		for _, k := range config.ConfigMapKeys {
			if k == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s must be present in config.ConfigMapKeys", want)
		}
	}
}
