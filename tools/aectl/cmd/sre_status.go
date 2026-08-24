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
	"strconv"

	"github.com/spf13/cobra"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	k8s "github.com/wso2/aep/aectl/internal/kubernetes"
	"github.com/wso2/aep/aectl/internal/ui"
)

var sreStatusObsNamespace string

var sreStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the health of the SRE observability plane",
	Long:  `Prints Helm release status, deployment readiness, ESO secret presence, and the pod table for the observability namespace.`,
	RunE:  runSreStatus,
}

func init() {
	sreCmd.AddCommand(sreStatusCmd)
	sreStatusCmd.Flags().StringVar(&sreStatusObsNamespace, "obs-namespace", "openchoreo-observability-plane", "Observability plane namespace")
}

func runSreStatus(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	client, err := k8s.NewClient(kubeconfig)
	if err != nil {
		return fmt.Errorf("connect to cluster: %w", err)
	}

	// Helm releases section
	ui.Section(fmt.Sprintf("Helm Releases (%s)", sreStatusObsNamespace))
	knownReleases := []string{"observability-plane", "observability-logs-opensearch"}
	allReleases, err := listHelmReleases(ctx, sreStatusObsNamespace, "observability.*")
	if err != nil {
		ui.Warn(fmt.Sprintf("could not query helm: %v", err))
	} else {
		byName := make(map[string]helmRelease, len(allReleases))
		for _, r := range allReleases {
			byName[r.Name] = r
		}
		t := ui.NewTable("NAME", "STATUS", "CHART")
		for _, name := range knownReleases {
			if r, ok := byName[name]; ok {
				t.AddRow(r.Name, colorHelmStatus(r.Status), r.Chart)
			} else {
				t.AddRow(name, ui.Yellow("not installed"), "")
			}
		}
		t.Print()
	}

	// Deployments section
	ui.Section("Deployments")
	deps, err := client.AppsV1().Deployments(sreStatusObsNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		ui.Warn(fmt.Sprintf("error: %v", err))
	} else if len(deps.Items) == 0 {
		ui.Detail("none found")
	} else {
		t := ui.NewTable("NAME", "AVAILABLE", "AGE")
		for _, d := range deps.Items {
			avail := fmt.Sprintf("%d/%d", d.Status.AvailableReplicas, *d.Spec.Replicas)
			if d.Status.AvailableReplicas < *d.Spec.Replicas {
				avail = ui.Yellow(avail)
			} else {
				avail = ui.Green(avail)
			}
			t.AddRow(d.Name, avail, statusFmtAge(d.CreationTimestamp.Time))
		}
		t.Print()
	}

	// ESO secrets section
	ui.Section("ESO Secrets")
	esoSecrets := []string{"opensearch-admin-credentials", "rca-agent-secret", "observer-secret"}
	for _, name := range esoSecrets {
		_, err := client.CoreV1().Secrets(sreStatusObsNamespace).Get(ctx, name, metav1.GetOptions{})
		switch {
		case err == nil:
			fmt.Printf("  %-36s %s\n", name, ui.Green("present"))
		case apierrors.IsNotFound(err):
			fmt.Printf("  %-36s %s\n", name, ui.Yellow("missing"))
		default:
			fmt.Printf("  %-36s %s\n", name, ui.Red(fmt.Sprintf("error: %v", err)))
		}
	}

	// Pods section
	pods, err := client.CoreV1().Pods(sreStatusObsNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		ui.Section(fmt.Sprintf("Pods (%s)", sreStatusObsNamespace))
		ui.Warn(fmt.Sprintf("error: %v", err))
		return nil
	}

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
	ui.Section(fmt.Sprintf("Pods (%s)  %s", sreStatusObsNamespace, readyStr))
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
	fmt.Println()
	return nil
}
