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
	"os"
	"os/exec"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/wso2/aep/aectl/internal/config"
	k8s "github.com/wso2/aep/aectl/internal/kubernetes"
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

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)

	// ── Helm release ─────────────────────────────────────────────────────────
	fmt.Println("Helm release")
	releases, err := listHelmReleases(ctx, platformStatusNamespace, platformStatusRelease)
	if err != nil {
		fmt.Printf("  (could not query helm: %v)\n", err)
	} else if len(releases) == 0 {
		fmt.Printf("  %s: not installed\n", platformStatusRelease)
	} else {
		_, _ = fmt.Fprintf(w, "  NAME\tSTATUS\tCHART\tUPDATED\n")
		for _, r := range releases {
			_, _ = fmt.Fprintf(w, "  %s\t%s\t%s\t%s\n", r.Name, r.Status, r.Chart, r.Updated)
		}
		_ = w.Flush()
	}
	fmt.Println()

	// ── Pods ─────────────────────────────────────────────────────────────────
	pods, err := client.CoreV1().Pods(platformStatusNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		fmt.Printf("Pods (%s): (error: %v)\n\n", platformStatusNamespace, err)
	} else {
		readyPods, total := 0, len(pods.Items)
		for _, p := range pods.Items {
			if statusPodReady(p) {
				readyPods++
			}
		}
		fmt.Printf("Pods (%s)   %d/%d ready\n", platformStatusNamespace, readyPods, total)
		if total > 0 {
			_, _ = fmt.Fprintf(w, "  NAME\tREADY\tSTATUS\tRESTARTS\tAGE\n")
			for _, p := range pods.Items {
				rc, tc := statusReadyContainers(p.Status.ContainerStatuses)
				_, _ = fmt.Fprintf(w, "  %s\t%d/%d\t%s\t%d\t%s\n",
					p.Name, rc, tc, string(p.Status.Phase),
					statusSumRestarts(p.Status.ContainerStatuses),
					statusFmtAge(p.CreationTimestamp.Time))
			}
			_ = w.Flush()
		}
	}
	fmt.Println()

	// ── Config ───────────────────────────────────────────────────────────────
	fmt.Println("Config")
	_, err = client.CoreV1().ConfigMaps(platformStatusNamespace).Get(ctx, config.ConfigMapName, metav1.GetOptions{})
	switch {
	case err == nil:
		fmt.Printf("  %s: present\n", config.ConfigMapName)
	case apierrors.IsNotFound(err):
		fmt.Printf("  %s: missing\n", config.ConfigMapName)
	default:
		fmt.Printf("  %s: (error: %v)\n", config.ConfigMapName, err)
	}

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
