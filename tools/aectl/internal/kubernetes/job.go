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
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
)

// RunJob creates the Job, waits for it to finish, prints all logs, then
// deletes the Job. Returns nil on success, non-nil on failure.
func RunJob(ctx context.Context, client *kubernetes.Clientset, job *batchv1.Job, out io.Writer) error {
	ns := job.Namespace

	created, err := client.BatchV1().Jobs(ns).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("create job: %w", err)
	}

	defer func() {
		_ = client.BatchV1().Jobs(ns).Delete(context.Background(), created.Name, metav1.DeleteOptions{
			PropagationPolicy: propagationForeground(),
		})
	}()

	_, _ = fmt.Fprintln(out, "Waiting for job to complete...")
	jobErr := waitForJobDone(ctx, client, ns, created.Name)

	// Always print logs regardless of success or failure.
	podName, err := getPodName(ctx, client, ns, created.Name)
	if err == nil {
		_ = dumpLogs(ctx, client, ns, podName, out)
	}

	return jobErr
}

func getPodName(ctx context.Context, client *kubernetes.Clientset, ns, jobName string) (string, error) {
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		pods, err := client.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{
			LabelSelector: "job-name=" + jobName,
		})
		if err != nil {
			return "", err
		}
		for _, p := range pods.Items {
			if p.Name != "" {
				return p.Name, nil
			}
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return "", fmt.Errorf("pod for job %q not found", jobName)
}

func dumpLogs(ctx context.Context, client *kubernetes.Clientset, ns, podName string, out io.Writer) error {
	req := client.CoreV1().Pods(ns).GetLogs(podName, &corev1.PodLogOptions{})
	stream, err := req.Stream(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = stream.Close() }()
	_, err = io.Copy(out, stream)
	return err
}

func waitForJobDone(ctx context.Context, client *kubernetes.Clientset, ns, jobName string) error {
	watcher, err := client.BatchV1().Jobs(ns).Watch(ctx, metav1.ListOptions{
		FieldSelector: "metadata.name=" + jobName,
	})
	if err != nil {
		return fmt.Errorf("watch job: %w", err)
	}
	defer watcher.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event, ok := <-watcher.ResultChan():
			if !ok {
				return fmt.Errorf("job watch channel closed unexpectedly")
			}
			if event.Type != watch.Modified && event.Type != watch.Added {
				continue
			}
			j, ok := event.Object.(*batchv1.Job)
			if !ok {
				continue
			}
			for _, cond := range j.Status.Conditions {
				if cond.Type == batchv1.JobComplete && cond.Status == corev1.ConditionTrue {
					return nil
				}
				if cond.Type == batchv1.JobFailed && cond.Status == corev1.ConditionTrue {
					return fmt.Errorf("job failed: %s", cond.Message)
				}
			}
		}
	}
}

func propagationForeground() *metav1.DeletionPropagation {
	p := metav1.DeletePropagationForeground
	return &p
}
