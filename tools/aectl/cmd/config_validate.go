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

	"github.com/spf13/cobra"

	"github.com/wso2/aep/aectl/internal/config"
	k8s "github.com/wso2/aep/aectl/internal/kubernetes"
)

var configTestFile string

var configTestCmd = &cobra.Command{
	Use:   "test",
	Short: "Validate a local config file or the cluster ConfigMap",
	Long: `Checks that all required keys are present and that values have the
correct type and format.

Validate a local file before importing:
  aep platform config test --config ~/aectl-configs/defaults.yaml

Validate what is currently in the cluster:
  aep platform config test`,
	RunE: runConfigTest,
}

func init() {
	configCmd.AddCommand(configTestCmd)
	configTestCmd.Flags().StringVar(&configTestFile, "config", "", "path to local config YAML file to validate (omit to validate the cluster ConfigMap)")
}

func runConfigTest(cmd *cobra.Command, args []string) error {
	var errs []string

	if configTestFile != "" {
		errs = config.ValidateFile(configTestFile)
	} else {
		ctx := context.Background()
		client, err := k8s.NewClient(kubeconfig)
		if err != nil {
			return fmt.Errorf("connect to cluster: %w", err)
		}
		const aepNamespace = "wso2-aep"
		if _, err := config.LoadFromCluster(ctx, client, aepNamespace); err != nil {
			return fmt.Errorf("load cluster config: %w", err)
		}
		errs = config.ValidateLoaded()
	}

	if len(errs) > 0 {
		_, _ = fmt.Fprintln(os.Stderr, "Config validation failed:")
		for _, e := range errs {
			_, _ = fmt.Fprintf(os.Stderr, "  %s\n", e)
		}
		return fmt.Errorf("config is invalid — fix the errors above")
	}

	_, _ = fmt.Fprintln(os.Stdout, "Config is valid.")
	return nil
}
