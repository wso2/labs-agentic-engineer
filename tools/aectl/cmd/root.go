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
	"github.com/wso2/aep/aectl/internal/ui"
)

var kubeconfig string

var rootCmd = &cobra.Command{
	Use:   "aectl",
	Short: "AEP CLI — deployment and operations tooling for the AEP platform",
	Long:  `aectl manages the lifecycle of an AEP platform installation on a Kubernetes cluster.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ui.Banner()
		return cmd.Help()
	},
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Parent() == nil {
			// Bare `aectl` just shows help — skip cluster initialisation.
			return nil
		}
		ctx := context.Background()
		client, err := k8s.NewClient(kubeconfig)
		if err != nil {
			return fmt.Errorf("connect to cluster: %w", err)
		}
		const aepNamespace = "wso2-aep"
		n, err := config.LoadFromCluster(ctx, client, aepNamespace)
		if err != nil {
			return err
		}
		if n > 0 && cmd.CommandPath() == "aectl platform install" {
			ui.Warn(fmt.Sprintf("applying %d pre-seeded config key(s) from %s — run 'aectl platform config export' to review", n, config.ConfigMapName))
		}
		return config.LoadThunderSecretFromCluster(ctx, client, aepNamespace)
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(config.Init)
	rootCmd.PersistentFlags().StringVar(&kubeconfig, "kubeconfig", "", "path to kubeconfig file (default: $HOME/.kube/config)")
}
