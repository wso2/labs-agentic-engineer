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
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"time"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/wso2/aep/aectl/internal/config"
	k8s "github.com/wso2/aep/aectl/internal/kubernetes"
	"github.com/wso2/aep/aectl/internal/ui"
)

var (
	platformStatusNamespace string
	platformStatusRelease   string
)

var platformStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the health of the AEP platform installation",
	Long:  `Prints the Helm release status, pod table, and key resource checks for the AEP platform namespace.`,
	RunE:  runPlatformStatus,
}

func init() {
	platformCmd.AddCommand(platformStatusCmd)
	platformStatusCmd.Flags().StringVar(&platformStatusNamespace, "namespace", "wso2-aep", "Platform namespace")
	platformStatusCmd.Flags().StringVar(&platformStatusRelease, "platform-release", "aep-platform", "Helm release name")
}

// helmRelease is the shape of one entry in `helm list --output json`.
type helmRelease struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Chart   string `json:"chart"`
	Updated string `json:"updated"`
}

func runPlatformStatus(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	client, err := k8s.NewClient(kubeconfig)
	if err != nil {
		return fmt.Errorf("connect to cluster: %w", err)
	}

	// Helm release section
	ui.Section("Helm Release")
	releases, err := listHelmReleases(ctx, platformStatusNamespace, platformStatusRelease)
	if err != nil {
		ui.Warn(fmt.Sprintf("could not query helm: %v", err))
	} else if len(releases) == 0 {
		ui.Detail(platformStatusRelease + ": not installed")
	} else {
		t := ui.NewTable("NAME", "STATUS", "CHART", "UPDATED")
		for _, r := range releases {
			t.AddRow(r.Name, colorHelmStatus(r.Status), r.Chart, r.Updated)
		}
		t.Print()
	}

	// Pods section
	pods, err := client.CoreV1().Pods(platformStatusNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		ui.Section(fmt.Sprintf("Pods (%s)", platformStatusNamespace))
		ui.Warn(fmt.Sprintf("error listing pods: %v", err))
	} else {
		readyPods, total := 0, len(pods.Items)
		for _, p := range pods.Items {
			if statusPodReady(p) {
				readyPods++
			}
		}
		readyStr := fmt.Sprintf("%d/%d ready", readyPods, total)
		if readyPods == total && total > 0 {
			readyStr = ui.Green(readyStr)
		} else if readyPods < total {
			readyStr = ui.Yellow(readyStr)
		}
		ui.Section(fmt.Sprintf("Pods (%s)  %s", platformStatusNamespace, readyStr))
		if total > 0 {
			t := ui.NewTable("NAME", "READY", "STATUS", "RESTARTS", "AGE")
			for _, p := range pods.Items {
				rc, tc := statusReadyContainers(p.Status.ContainerStatuses)
				restarts := statusSumRestarts(p.Status.ContainerStatuses)
				readyCells := fmt.Sprintf("%d/%d", rc, tc)
				if rc < tc {
					readyCells = ui.Yellow(readyCells)
				}
				restartStr := strconv.Itoa(int(restarts))
				if restarts > 5 {
					restartStr = ui.Red(restartStr)
				} else if restarts > 0 {
					restartStr = ui.Yellow(restartStr)
				} else {
					restartStr = ui.Gray(restartStr)
				}
				t.AddRow(
					p.Name,
					readyCells,
					colorPodPhase(string(p.Status.Phase)),
					restartStr,
					statusFmtAge(p.CreationTimestamp.Time),
				)
			}
			t.Print()
		}
	}

	// Config section
	ui.Section("Config")
	_, err = client.CoreV1().ConfigMaps(platformStatusNamespace).Get(ctx, config.ConfigMapName, metav1.GetOptions{})
	switch {
	case err == nil:
		fmt.Printf("  %-36s %s\n", config.ConfigMapName, ui.Green("present"))
	case apierrors.IsNotFound(err):
		fmt.Printf("  %-36s %s\n", config.ConfigMapName, ui.Yellow("missing"))
	default:
		fmt.Printf("  %-36s %s\n", config.ConfigMapName, ui.Red(fmt.Sprintf("error: %v", err)))
	}
	fmt.Println()

	return nil
}

// listHelmReleases runs `helm list --filter <name> --output json` and returns matching releases.
func listHelmReleases(ctx context.Context, namespace, filter string) ([]helmRelease, error) {
	out, err := exec.CommandContext(ctx, "helm", "list",
		"-n", namespace,
		"--filter", "^"+filter+"$",
		"--output", "json",
	).Output()
	if err != nil {
		return nil, err
	}
	var releases []helmRelease
	if err := json.Unmarshal(out, &releases); err != nil {
		return nil, err
	}
	return releases, nil
}

func colorHelmStatus(s string) string {
	switch s {
	case "deployed":
		return ui.Green(s)
	case "failed":
		return ui.Red(s)
	case "pending-install", "pending-upgrade", "pending-rollback":
		return ui.Yellow(s)
	default:
		return s
	}
}

func colorPodPhase(phase string) string {
	switch phase {
	case "Running", "Succeeded":
		return ui.Green(phase)
	case "Pending":
		return ui.Yellow(phase)
	case "Failed":
		return ui.Red(phase)
	default:
		return phase
	}
}

func statusPodReady(p corev1.Pod) bool {
	switch p.Status.Phase {
	case corev1.PodSucceeded:
		return true
	case corev1.PodRunning:
		for _, cs := range p.Status.ContainerStatuses {
			if !cs.Ready {
				return false
			}
		}
		return len(p.Status.ContainerStatuses) > 0
	default:
		return false
	}
}

func statusReadyContainers(statuses []corev1.ContainerStatus) (ready, total int) {
	total = len(statuses)
	for _, cs := range statuses {
		if cs.Ready {
			ready++
		}
	}
	return
}

func statusSumRestarts(statuses []corev1.ContainerStatus) int32 {
	var n int32
	for _, cs := range statuses {
		n += cs.RestartCount
	}
	return n
}

func statusFmtAge(t time.Time) string {
	d := time.Since(t)
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d >= time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d >= time.Minute:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
}
