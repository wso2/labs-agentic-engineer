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
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wso2/aep/aectl/internal/ui"
)

var (
	rollbackNamespace       string
	rollbackPlatformRelease string
	rollbackRevision        int
	rollbackYes             bool
)

var rollbackCmd = &cobra.Command{
	Use:   "rollback",
	Short: "Roll back the platform chart to a previous revision",
	Long: `Shows the release history then rolls back to a previous revision.

Rolls back to the immediately preceding revision by default. Supply
--revision to target a specific one (as shown by the history table).

  aep platform rollback                  # roll back to previous
  aep platform rollback --revision 3     # roll back to revision 3`,
	RunE: runRollback,
}

func init() {
	platformCmd.AddCommand(rollbackCmd)
	rollbackCmd.Flags().StringVar(&rollbackNamespace, "namespace", "wso2-aep", "Namespace where the platform chart is installed")
	rollbackCmd.Flags().StringVar(&rollbackPlatformRelease, "platform-release", "aep-platform", "Helm release name")
	rollbackCmd.Flags().IntVar(&rollbackRevision, "revision", 0, "Revision to roll back to (default 0 = previous revision)")
	rollbackCmd.Flags().BoolVarP(&rollbackYes, "yes", "y", false, "Skip confirmation prompt")
}

func runRollback(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	if _, err := exec.LookPath("helm"); err != nil {
		return fmt.Errorf("helm is required but was not found in PATH")
	}

	// Show history so the operator can see what they are rolling back to.
	fmt.Printf("\n%s\n\n", ui.Bold(fmt.Sprintf("Release history for %s", rollbackPlatformRelease)))
	histOut, _ := exec.CommandContext(ctx, "helm", "history",
		rollbackPlatformRelease, "-n", rollbackNamespace,
		"--max", "10", "--output", "table").CombinedOutput()
	fmt.Println(strings.TrimSpace(string(histOut)))
	fmt.Println()

	if !rollbackYes {
		target := "previous revision"
		if rollbackRevision > 0 {
			target = fmt.Sprintf("revision %d", rollbackRevision)
		}
		fmt.Printf("  Roll back %q to %s? Type \"yes\" to confirm: ", rollbackPlatformRelease, target)
		scanner := bufio.NewScanner(os.Stdin)
		scanner.Scan()
		if strings.TrimSpace(scanner.Text()) != "yes" {
			ui.Print("Aborted.")
			return nil
		}
	}

	ui.Step(fmt.Sprintf("Rolling back %q", rollbackPlatformRelease))
	helmArgs := []string{"rollback", rollbackPlatformRelease, "-n", rollbackNamespace}
	if rollbackRevision > 0 {
		helmArgs = append(helmArgs, strconv.Itoa(rollbackRevision))
	}

	var out bytes.Buffer
	c := exec.CommandContext(ctx, "helm", helmArgs...)
	c.Stdout = &out
	c.Stderr = &out
	if err := c.Run(); err != nil {
		return fmt.Errorf("helm rollback: %w\n%s", err, out.String())
	}
	ui.Success("Rollback complete")
	return nil
}
