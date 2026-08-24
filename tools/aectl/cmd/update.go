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
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wso2/aep/aectl/internal/ui"
)

var (
	updateNamespace       string
	updatePlatformRelease string
	updatePlatformVersion string
	updatePlatformChart   string
	updateResetValues     bool
	updatePullPolicy      string
	updateHelmSets        []string
	// Per-service image overrides. Each accepts "repo:tag".
	// For local images: load into the node runtime first (k3d image import /
	// kind load docker-image), then set --pull-policy Never.
	updateAepApiImage    string
	updateAgentsImage    string
	updateCollabImage    string
	updateMcpServerImage string
	updateConsoleImage   string
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Upgrade the AEP platform chart",
	Long: `Runs helm upgrade on the AEP platform release.

By default values from the previous release are reused (--reuse-values),
so only flags you explicitly pass are changed. Use --reset-values to
start from chart defaults instead.

Per-service image flags accept "repository:tag". To pin a locally built
image, load it into the node runtime first, then set --pull-policy:

  k3d:  k3d image import myimage:mytag
  kind: kind load docker-image myimage:mytag

  aep platform update --aep-api-image localhost/aep-api:hotfix --pull-policy Never

For arbitrary chart values not covered by a flag use --set (repeatable):

  aep platform update --set aepApi.resources.limits.cpu=500m`,
	RunE: runUpdate,
}

func init() {
	platformCmd.AddCommand(updateCmd)

	f := updateCmd.Flags()
	f.StringVar(&updateNamespace, "namespace", "wso2-aep", "Namespace where the platform chart is installed")
	f.StringVar(&updatePlatformRelease, "platform-release", "aep-platform", "Helm release name")
	f.StringVar(&updatePlatformVersion, "version", "", "Chart version to upgrade to (default: reuse current version)")
	f.StringVar(&updatePlatformChart, "platform-chart", "", "Local path to a platform chart (overrides --version)")
	f.BoolVar(&updateResetValues, "reset-values", false, "Reset all values to chart defaults before applying overrides (default: reuse previous values)")
	f.StringVar(&updatePullPolicy, "pull-policy", "", "imagePullPolicy applied to every service whose image is overridden (Never|IfNotPresent|Always)")
	f.StringArrayVar(&updateHelmSets, "set", nil, "Additional helm --set overrides (repeatable)")

	f.StringVar(&updateAepApiImage, "aep-api-image", "", "aep-api image as repo:tag  (e.g. ghcr.io/wso2/aep/aep-api:v1.2)")
	f.StringVar(&updateAgentsImage, "agents-image", "", "agents image as repo:tag")
	f.StringVar(&updateCollabImage, "collab-image", "", "collab image as repo:tag")
	f.StringVar(&updateMcpServerImage, "mcp-server-image", "", "aep-mcp-server image as repo:tag")
	f.StringVar(&updateConsoleImage, "console-image", "", "console image as repo:tag")
}

// serviceImageOverride maps a service's chart key prefix to the image flag value.
type serviceImageOverride struct {
	chartKey string // e.g. "aepApi" → aepApi.image.repository / aepApi.image.tag
	image    string // "repo:tag" from the flag
}

func runUpdate(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	if _, err := exec.LookPath("helm"); err != nil {
		return fmt.Errorf("helm is required but was not found in PATH")
	}

	overrides := []serviceImageOverride{
		{"aepApi", updateAepApiImage},
		{"aepAgents", updateAgentsImage},
		{"collab", updateCollabImage},
		{"aepMcpServer", updateMcpServerImage},
		{"console", updateConsoleImage},
	}

	// Build helm upgrade arguments.
	helmArgs := []string{
		"upgrade", updatePlatformRelease,
		"-n", updateNamespace,
	}

	// Chart source: local path > GHCR with version > GHCR without version.
	if updatePlatformChart != "" {
		helmArgs = append(helmArgs, updatePlatformChart)
	} else {
		// OCI artifact is named after the chart's `name:` (aep-platform).
		helmArgs = append(helmArgs, "oci://ghcr.io/wso2/aep/charts/aep-platform")
		if updatePlatformVersion != "" {
			helmArgs = append(helmArgs, "--version", updatePlatformVersion)
		}
	}

	// Value strategy.
	if updateResetValues {
		helmArgs = append(helmArgs, "--reset-values")
	} else {
		helmArgs = append(helmArgs, "--reuse-values")
	}

	// Per-service image overrides.
	for _, o := range overrides {
		if o.image == "" {
			continue
		}
		repo, tag, err := splitImage(o.image)
		if err != nil {
			return fmt.Errorf("--%s-image: %w", strings.ToLower(o.chartKey), err)
		}
		helmArgs = append(helmArgs,
			"--set", fmt.Sprintf("%s.image.repository=%s", o.chartKey, repo),
			"--set", fmt.Sprintf("%s.image.tag=%s", o.chartKey, tag),
		)
		if updatePullPolicy != "" {
			helmArgs = append(helmArgs,
				"--set", fmt.Sprintf("%s.image.pullPolicy=%s", o.chartKey, updatePullPolicy),
			)
		}
	}

	// Arbitrary --set overrides.
	for _, s := range updateHelmSets {
		helmArgs = append(helmArgs, "--set", s)
	}

	ui.Step(fmt.Sprintf("Upgrading platform chart %q", updatePlatformRelease))
	var out bytes.Buffer
	c := exec.CommandContext(ctx, "helm", helmArgs...)
	c.Stdout = &out
	c.Stderr = &out
	if err := c.Run(); err != nil {
		return fmt.Errorf("helm upgrade: %w\n%s", err, out.String())
	}
	ui.Success("Platform updated")
	return nil
}

// splitImage splits "repo:tag" on the last colon. Returns an error if the
// string has no colon or the tag is empty (bare repository names are
// ambiguous — always require an explicit tag).
func splitImage(image string) (repo, tag string, err error) {
	i := strings.LastIndex(image, ":")
	if i <= 0 || i == len(image)-1 {
		return "", "", fmt.Errorf("%q must be in repo:tag form", image)
	}
	return image[:i], image[i+1:], nil
}
