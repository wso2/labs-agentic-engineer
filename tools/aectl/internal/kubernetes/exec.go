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

package kubernetes

import (
	"context"
	"fmt"
	"io"
	"os/exec"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// FindRunningPod returns the name of the first Running pod whose name contains
// the given substring, in the given namespace.
func FindRunningPod(ctx context.Context, client *kubernetes.Clientset, namespace, nameContains string) (string, error) {
	pods, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("list pods: %w", err)
	}
	for _, p := range pods.Items {
		if p.Status.Phase == "Running" {
			if nameContains == "" || contains(p.Name, nameContains) {
				return p.Name, nil
			}
		}
	}
	return "", fmt.Errorf("no running pod matching %q found in namespace %q", nameContains, namespace)
}

// ExecInPod runs a shell command inside a pod via kubectl exec, streaming
// stdout and stderr to out.
func ExecInPod(ctx context.Context, namespace, pod, kubeconfig string, shellCmd string, out io.Writer) error {
	args := []string{"exec", "-n", namespace, pod, "--", "sh", "-c", shellCmd}
	if kubeconfig != "" {
		args = append([]string{"--kubeconfig", kubeconfig}, args...)
	}
	cmd := exec.CommandContext(ctx, "kubectl", args...)
	cmd.Stdout = out
	cmd.Stderr = out
	return cmd.Run()
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
