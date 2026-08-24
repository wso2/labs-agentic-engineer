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
	"sort"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/wso2/aep/aectl/internal/config"
	k8s "github.com/wso2/aep/aectl/internal/kubernetes"
	"github.com/wso2/aep/aectl/internal/ui"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage the in-cluster AEP configuration",
}

var configImportFile string

var configImportCmd = &cobra.Command{
	Use:   "import",
	Short: "Apply a local config file to the in-cluster aep-cli-config ConfigMap",
	Long: `Reads a local YAML config file, validates it, and writes all recognised
config keys to the aep-cli-config ConfigMap in the wso2-aep namespace.
Creates the namespace if it does not yet exist.

This command must be run before 'aectl platform install'. All config values
for the install must come from this ConfigMap — no hardcoded defaults are
used at install time.

All keys from the file are written; keys absent from the file are stored
as empty (their zero value). thunder.admin_client_secret is intentionally
ignored — it is managed by OpenBao/ESO and never stored in the ConfigMap.

Start from the defaults template:
  aep platform config import --config ~/aectl-configs/defaults.yaml`,
	RunE: runConfigImport,
}

func init() {
	platformCmd.AddCommand(configCmd)
	configCmd.AddCommand(configImportCmd)
	configImportCmd.Flags().StringVar(&configImportFile, "config", "", "path to local config YAML file (required)")
	_ = configImportCmd.MarkFlagRequired("config")
}

func runConfigImport(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Validate the file before touching the cluster. Fail fast on errors.
	if errs := config.ValidateFile(configImportFile); len(errs) > 0 {
		ui.Fail("Config file validation failed:")
		for _, e := range errs {
			ui.Detail(e)
		}
		return fmt.Errorf("fix the errors above and re-run import")
	}

	// Re-read the file into an isolated viper to collect values.
	fv := viper.New()
	fv.SetConfigFile(configImportFile)
	if err := fv.ReadInConfig(); err != nil {
		return fmt.Errorf("read config file %s: %w", configImportFile, err)
	}

	if fv.IsSet("thunder.admin_client_secret") {
		ui.Warn("thunder.admin_client_secret is managed by OpenBao/ESO — ignoring")
	}

	// Write ALL recognised keys. Keys absent from the file are stored as empty
	// string (their zero value). This ensures the ConfigMap is always complete
	// and ValidateLoaded can distinguish "never imported" from "explicitly empty".
	data := make(map[string]string, len(config.ConfigMapKeys))
	for _, k := range config.ConfigMapKeys {
		data[k] = fv.GetString(k)
	}

	client, err := k8s.NewClient(kubeconfig)
	if err != nil {
		return fmt.Errorf("connect to cluster: %w", err)
	}

	const aepNamespace = "wso2-aep"
	if err := ensureNamespace(ctx, client, aepNamespace); err != nil {
		return fmt.Errorf("ensure namespace %s: %w", aepNamespace, err)
	}


	existing, err := client.CoreV1().ConfigMaps(aepNamespace).Get(ctx, config.ConfigMapName, metav1.GetOptions{})
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("get %s: %w", config.ConfigMapName, err)
		}
		_, err = client.CoreV1().ConfigMaps(aepNamespace).Create(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      config.ConfigMapName,
				Namespace: aepNamespace,
				Labels:    map[string]string{"app.kubernetes.io/managed-by": "aectl"},
			},
			Data: data,
		}, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("create %s: %w", config.ConfigMapName, err)
		}
	} else {
		existing.Data = data
		_, err = client.CoreV1().ConfigMaps(aepNamespace).Update(ctx, existing, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("update %s: %w", config.ConfigMapName, err)
		}
	}

	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	ui.Success(fmt.Sprintf("Wrote %d key(s) to %s/%s", len(keys), aepNamespace, config.ConfigMapName))
	t := ui.NewTable("KEY", "VALUE")
	for _, k := range keys {
		t.AddRow(k, data[k])
	}
	t.Print()
	fmt.Println()
	ui.Detail("Run 'aectl platform install' to apply.")
	return nil
}

