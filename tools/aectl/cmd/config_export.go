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
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/wso2/aep/aectl/internal/config"
	k8s "github.com/wso2/aep/aectl/internal/kubernetes"
	"github.com/wso2/aep/aectl/internal/ui"
)

var (
	configExportOutput   string
	configExportOverride bool
)

var configExportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export the in-cluster config to a local YAML file",
	Long: `Reads the aep-cli-config ConfigMap from the cluster and writes it as a
YAML file you can edit and re-apply with 'aectl platform config import'.

  aep platform config export --output aectl-config.yaml

Use - as the output path to print to stdout instead of writing a file.`,
	RunE: runConfigExport,
}

func init() {
	configCmd.AddCommand(configExportCmd)
	configExportCmd.Flags().StringVar(&configExportOutput, "output", "aectl-config.yaml", "file to write (use - for stdout)")
	configExportCmd.Flags().BoolVar(&configExportOverride, "override", false, "overwrite existing file without prompting")
}

func runConfigExport(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	client, err := k8s.NewClient(kubeconfig)
	if err != nil {
		return fmt.Errorf("connect to cluster: %w", err)
	}

	const aepNamespace = "wso2-aep"
	cm, err := client.CoreV1().ConfigMaps(aepNamespace).Get(ctx, config.ConfigMapName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("%s not found — run 'aectl platform install' first", config.ConfigMapName)
		}
		return fmt.Errorf("read %s: %w", config.ConfigMapName, err)
	}

	// Reconstruct a nested map from the dot-notation keys in the ConfigMap.
	// All keys are included even when the value is empty so that the exported
	// file is a complete template the operator can fill in.
	root := make(map[string]interface{})
	for _, k := range config.ConfigMapKeys {
		v := cm.Data[k] // empty string if key is absent — intentional
		setNestedKey(root, strings.Split(k, "."), v)
	}

	out, err := yaml.Marshal(root)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if configExportOutput == "-" {
		_, err = os.Stdout.Write(out)
		return err
	}

	// Warn and prompt before overwriting an existing file.
	if _, statErr := os.Stat(configExportOutput); statErr == nil && !configExportOverride {
		ui.Warn(fmt.Sprintf("%s already exists", configExportOutput))
		fmt.Printf("  Overwrite %s? Type \"yes\" to confirm: ", configExportOutput)
		var answer string
		if _, err := fmt.Scanln(&answer); err != nil || strings.TrimSpace(answer) != "yes" {
			ui.Print("Aborted.")
			return nil
		}
	}

	if err := os.WriteFile(configExportOutput, out, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", configExportOutput, err)
	}
	ui.Success(fmt.Sprintf("Config exported to %s", configExportOutput))
	return nil
}

// setNestedKey drills into m using parts as a key path and sets the leaf
// value, creating intermediate maps as needed.
func setNestedKey(m map[string]interface{}, parts []string, value string) {
	if len(parts) == 1 {
		m[parts[0]] = value
		return
	}
	child, ok := m[parts[0]].(map[string]interface{})
	if !ok {
		child = make(map[string]interface{})
		m[parts[0]] = child
	}
	setNestedKey(child, parts[1:], value)
}
