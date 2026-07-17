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
	"github.com/wso2/aep/aepctl/internal/config"
	k8s "github.com/wso2/aep/aepctl/internal/kubernetes"
)

var kubeconfig string

var rootCmd = &cobra.Command{
	Use:   "aep",
	Short: "AEP CLI — deployment and operations tooling for the AEP platform",
	Long:  `aep manages the lifecycle of an AEP platform installation on a Kubernetes cluster.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// aep platform install bootstraps the cluster from scratch — the
		// ConfigMap doesn't exist yet, so skip the cluster config load.
		if cmd.Annotations["skipClusterConfig"] == "true" {
			return nil
		}
		ctx := context.Background()
		client, err := k8s.NewClient(kubeconfig)
		if err != nil {
			return fmt.Errorf("connect to cluster: %w", err)
		}
		const aepNamespace = "wso2-aep"
		if err := config.LoadFromCluster(ctx, client, aepNamespace); err != nil {
			return err
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
