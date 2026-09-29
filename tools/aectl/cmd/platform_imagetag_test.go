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
	"regexp"
	"strings"
	"testing"
)

// TestImageTagOverridesEmpty is the case every released install takes: no
// --image-tag, so the chart's own released tags are left alone. A regression
// here would silently re-point production installs at whatever tag leaked in.
func TestImageTagOverridesEmpty(t *testing.T) {
	if got := imageTagOverrides(""); got != nil {
		t.Errorf("imageTagOverrides(%q) = %v, want nil", "", got)
	}
}

// TestImageTagOverridesSetsEveryService covers the flag's actual job: one
// --set per service, tag only. The repository is deliberately NOT set — the
// images are expected to exist under the chart's own repository names.
func TestImageTagOverridesSetsEveryService(t *testing.T) {
	got := imageTagOverrides("dev-local")

	if len(got) != len(platformServiceChartKeys)*2 {
		t.Fatalf("imageTagOverrides returned %d args, want %d", len(got), len(platformServiceChartKeys)*2)
	}
	for i := 0; i < len(got); i += 2 {
		if got[i] != "--set" {
			t.Errorf("arg %d = %q, want %q", i, got[i], "--set")
		}
	}

	joined := strings.Join(got, " ")
	for _, key := range platformServiceChartKeys {
		want := key + ".image.tag=dev-local"
		if !strings.Contains(joined, want) {
			t.Errorf("missing override %q in %q", want, joined)
		}
		if strings.Contains(joined, key+".image.repository=") {
			t.Errorf("unexpected repository override for %q: --image-tag moves the tag only", key)
		}
	}
}

// TestPlatformServiceChartKeysMatchChart is the coupling that actually breaks
// in practice: a service added to the chart with an image of its own, and not
// added here, keeps its released tag while every sibling moves to the locally
// built one — a half-local install that looks like a working one. Reading the
// chart's values.yaml rather than restating the list keeps the two in step.
func TestPlatformServiceChartKeysMatchChart(t *testing.T) {
	valuesPath := filepath.Join("..", "..", "..", "deployments", "helm-charts", "platform", "values.yaml")
	data, err := os.ReadFile(valuesPath)
	if err != nil {
		t.Fatalf("read chart values: %v", err)
	}

	// A top-level service block owning an image the chart tags:
	//
	//   console:
	//     image:
	//       repository: ...
	//       tag: ...
	//
	// Anchored at column 0 so only top-level keys match, and requiring the
	// repository/tag pair so blocks carrying a bare `image: "<ref>"` string
	// (remoteWorker, temporal) are correctly left out — those are not
	// repository/tag values and --image-tag cannot re-point them.
	re := regexp.MustCompile(`(?m)^([a-zA-Z][a-zA-Z0-9]*):\n(?:[ \t]+.*\n|\n)*?[ \t]+image:\n[ \t]+repository:.*\n[ \t]+tag:`)

	inChart := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(string(data), -1) {
		inChart[m[1]] = true
	}
	if len(inChart) == 0 {
		t.Fatal("no repository/tag image blocks found in values.yaml — the pattern needs updating, not the catalog")
	}

	known := map[string]bool{}
	for _, key := range platformServiceChartKeys {
		known[key] = true
		if !inChart[key] {
			t.Errorf("platformServiceChartKeys has %q, which the chart has no repository/tag image for", key)
		}
	}
	for key := range inChart {
		if !known[key] {
			t.Errorf("chart service %q has a repository/tag image but is missing from platformServiceChartKeys — "+
				"a --image-tag install would leave it on its released tag", key)
		}
	}
}
