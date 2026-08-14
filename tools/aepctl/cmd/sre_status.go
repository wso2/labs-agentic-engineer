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
	"text/tabwriter"

	"github.com/spf13/cobra"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	k8s "github.com/wso2/aep/aepctl/internal/kubernetes"
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

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)

	// ── Helm releases ────────────────────────────────────────────────────────
	fmt.Printf("Helm releases (%s)\n", sreStatusObsNamespace)
	knownReleases := []string{"observability-plane", "observability-logs-opensearch"}
	allReleases, err := listHelmReleases(ctx, sreStatusObsNamespace, "observability.*")
	if err != nil {
		fmt.Printf("  (could not query helm: %v)\n", err)
	} else {
		byName := make(map[string]helmRelease, len(allReleases))
		for _, r := range allReleases {
			byName[r.Name] = r
		}
		_, _ = fmt.Fprintf(w, "  NAME\tSTATUS\tCHART\n")
		for _, name := range knownReleases {
			if r, ok := byName[name]; ok {
				_, _ = fmt.Fprintf(w, "  %s\t%s\t%s\n", r.Name, r.Status, r.Chart)
			} else {
				_, _ = fmt.Fprintf(w, "  %s\tnot installed\t\n", name)
			}
		}
		_ = w.Flush()
	}
	fmt.Println()

	// ── Deployments ──────────────────────────────────────────────────────────
	fmt.Println("Deployments")
	deps, err := client.AppsV1().Deployments(sreStatusObsNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		fmt.Printf("  (error: %v)\n", err)
	} else if len(deps.Items) == 0 {
		fmt.Println("  (none found)")
	} else {
		_, _ = fmt.Fprintf(w, "  NAME\tAVAILABLE\tAGE\n")
		for _, d := range deps.Items {
			avail := fmt.Sprintf("%d/%d", d.Status.AvailableReplicas, *d.Spec.Replicas)
			_, _ = fmt.Fprintf(w, "  %s\t%s\t%s\n", d.Name, avail, statusFmtAge(d.CreationTimestamp.Time))
		}
		_ = w.Flush()
	}
	fmt.Println()

	// ── ESO secrets ──────────────────────────────────────────────────────────
	fmt.Println("ESO secrets")
	esoSecrets := []string{
		"opensearch-admin-credentials",
		"rca-agent-secret",
		"rca-agent-anthropic-secret",
		"observer-secret",
	}
	for _, name := range esoSecrets {
		_, err := client.CoreV1().Secrets(sreStatusObsNamespace).Get(ctx, name, metav1.GetOptions{})
		switch {
		case err == nil:
			fmt.Printf("  %-36s present\n", name)
		case apierrors.IsNotFound(err):
			fmt.Printf("  %-36s missing\n", name)
		default:
			fmt.Printf("  %-36s (error: %v)\n", name, err)
		}
	}
	fmt.Println()

	// ── RCA LLM key ──────────────────────────────────────────────────────────
	// Secret presence above says nothing about whether the RCA agent can reach an
	// LLM. Both sources may hold an EMPTY RCA_LLM_API_KEY — an empty string is a
	// legal Secret value, and rca-agent-secret is created unconditionally even
	// when deployments/.env carries no key — so "present" reads identically for a
	// working install and a broken one. Report the source the agent will actually
	// resolve instead: src/clients/llm.py:resolve_api_key prefers the mounted file
	// (rca-agent-anthropic-secret) and falls back to the envFrom variable
	// (rca-agent-secret).
	fmt.Println("RCA LLM key")
	switch {
	case secretKeyNonEmpty(ctx, client, sreStatusObsNamespace, "rca-agent-anthropic-secret", "RCA_LLM_API_KEY"):
		fmt.Printf("  %-36s org's console-connected key\n", "rca-agent-anthropic-secret")
	case secretKeyNonEmpty(ctx, client, sreStatusObsNamespace, "rca-agent-secret", "RCA_LLM_API_KEY"):
		fmt.Printf("  %-36s static key from deployments/.env\n", "rca-agent-secret")
	default:
		fmt.Printf("  %-36s NONE — analyses will fail\n", "(unresolved)")
		fmt.Println("  Connect a key at Settings → Anthropic Integration, or set")
		fmt.Println("  ANTHROPIC_API_KEY in deployments/.env and re-run setup-observability.sh.")
	}
	fmt.Println()

	// ── Pods ─────────────────────────────────────────────────────────────────
	pods, err := client.CoreV1().Pods(sreStatusObsNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		fmt.Printf("Pods (%s): (error: %v)\n", sreStatusObsNamespace, err)
	} else {
		readyPods, total := 0, len(pods.Items)
		for _, p := range pods.Items {
			if statusPodReady(p) {
				readyPods++
			}
		}
		fmt.Printf("Pods (%s)   %d/%d ready\n", sreStatusObsNamespace, readyPods, total)
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

	return nil
}

// secretKeyNonEmpty reports whether ns/name carries a non-empty value at key.
//
// A missing Secret, a missing key, and a present-but-empty value all collapse to
// false on purpose: for credential wiring they are the same outcome, and it is
// the empty value specifically that a plain existence check reports as healthy.
// Any API error is likewise false — an unreadable Secret is not evidence of a
// usable key.
func secretKeyNonEmpty(ctx context.Context, client *kubernetes.Clientset, ns, name, key string) bool {
	s, err := client.CoreV1().Secrets(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return false
	}
	return len(s.Data[key]) > 0
}
