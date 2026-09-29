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
	// Cobra prints a returned error itself, and Execute below prints it too —
	// which is one error message too many. Execute keeps the job, because it
	// is what also sets the exit code, so the two cannot drift.
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		ui.Banner()
		return cmd.Help()
	},
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Past this line, nothing that fails is a usage problem: flags parsed
		// and args validated before PersistentPreRunE runs, so a bad flag or a
		// missing argument still gets the full usage block, while a cluster
		// that would not connect or an install that died halfway does not
		// answer with forty lines of flag documentation. Set before the work
		// below so that work's own failures are covered.
		cmd.SilenceUsage = true
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

// Execute runs the CLI and is the single place a failure is reported, in the
// same red-cross shape every other failure in a run already uses (ui.Fail) so
// the last line of a broken install does not look like it came from a
// different program than the twenty lines above it.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		ui.Fail(err.Error())
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(config.Init)
	rootCmd.PersistentFlags().StringVar(&kubeconfig, "kubeconfig", "", "path to kubeconfig file (default: $HOME/.kube/config)")
}
