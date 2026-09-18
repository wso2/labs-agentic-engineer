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

package envidp

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

func TestAepBootstrapNaming(t *testing.T) {
	if got, want := aepBootstrapConfigMapName("thunder-default-default"), "thunder-default-default-aep-bootstrap"; got != want {
		t.Errorf("aepBootstrapConfigMapName = %q, want %q", got, want)
	}
	if got, want := aepBootstrapImportJobName("thunder-default-default"), "thunder-default-default-aep-bootstrap-import"; got != want {
		t.Errorf("aepBootstrapImportJobName = %q, want %q", got, want)
	}
}

func TestAepBootstrapImportChartSpec(t *testing.T) {
	inst := &ThunderInstance{Release: "thunder-default-default", Namespace: "thunder-default-default"}
	spec, err := aepBootstrapImportChartSpec(inst, "1.0.0", "thunder-default-default-aep-bootstrap", []string{"80-aep-system-client.yaml"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.ReleaseName != inst.Release || spec.Chart != thunderChart || spec.Version != "1.0.0" || spec.Namespace != inst.Namespace {
		t.Errorf("spec = %+v, unexpected release/chart/version/namespace", spec)
	}
	// Deliberately narrow: unlike thunderChartSpec (CREATE), this must NOT
	// reassert unrelated settings (deployment.replicaCount, sqlite db types,
	// etc.) that would silently diverge from the release's own live values.
	if len(spec.Sets) != 0 {
		t.Errorf("Sets = %v, want none — repair must only override the bootstrap ConfigMap", spec.Sets)
	}
	if !containsString(spec.SetStrings, "bootstrap.configMap.name=thunder-default-default-aep-bootstrap") {
		t.Errorf("SetStrings %v missing bootstrap.configMap.name override", spec.SetStrings)
	}
	if len(spec.SetJSON) != 1 || !strings.HasPrefix(spec.SetJSON[0], "bootstrap.configMap.files=") {
		t.Errorf("SetJSON = %v, want exactly one bootstrap.configMap.files override", spec.SetJSON)
	}
}

// TestWaitForJobDeleted_DelayedDeletion covers the race this polling exists
// for: the Job is still present on the first Get (as it would be mid
// Foreground-propagation deletion) and only gone on a later one.
func TestWaitForJobDeleted_DelayedDeletion(t *testing.T) {
	client := fake.NewClientset(&batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "stale-job", Namespace: "ns"},
	})
	var calls int
	client.PrependReactor("get", "jobs", func(action clienttesting.Action) (bool, runtime.Object, error) {
		calls++
		if calls < 3 {
			return false, nil, nil // let the tracker answer with the still-present object
		}
		return true, nil, apierrors.NewNotFound(batchv1.Resource("jobs"), "stale-job")
	})
	c := clients{k8s: client}

	if err := pollUntilJobDeleted(context.Background(), c, "ns", "stale-job", time.Second, time.Millisecond); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls < 3 {
		t.Errorf("expected at least 3 Get calls (seeing it present before it clears), got %d", calls)
	}
}

func TestWaitForJobDeleted_PropagatesUnexpectedError(t *testing.T) {
	client := fake.NewClientset()
	client.PrependReactor("get", "jobs", func(action clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewInternalError(errors.New("boom"))
	})
	c := clients{k8s: client}

	if err := pollUntilJobDeleted(context.Background(), c, "ns", "stale-job", time.Second, time.Millisecond); err == nil {
		t.Fatal("expected the unexpected error to be propagated, got nil")
	}
}

func TestWaitForJobDeleted_TimesOutWhileStillPresent(t *testing.T) {
	client := fake.NewClientset(&batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "stuck-job", Namespace: "ns"},
	})
	c := clients{k8s: client}

	err := pollUntilJobDeleted(context.Background(), c, "ns", "stuck-job", 5*time.Millisecond, time.Millisecond)
	if err == nil {
		t.Fatal("expected a timeout error, got nil")
	}
}

// renderedChartFixture is a minimal but realistic stand-in for `helm
// template`'s output against ThunderID's chart: a Secret and a ConfigMap
// (checking extractBootstrapImportJob correctly skips non-Job documents),
// then the pre-install hook Job itself, carrying the two markers that make
// it inert outside `helm install`/`upgrade` — the hook annotations — and a
// backoffLimit/restartPolicy this function must override.
const renderedChartFixture = `
apiVersion: v1
kind: Secret
metadata:
  name: thunder-default-default-admin-credentials
  namespace: thunder-default-default
data:
  password: c2VjcmV0
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: thunder-default-default-config
  namespace: thunder-default-default
data:
  key: value
---
apiVersion: batch/v1
kind: Job
metadata:
  name: thunder-default-default-setup
  namespace: thunder-default-default
  annotations:
    helm.sh/hook: pre-install
    helm.sh/hook-weight: "5"
    helm.sh/hook-delete-policy: before-hook-creation
  labels:
    app.kubernetes.io/name: thunderid
spec:
  backoffLimit: 3
  template:
    spec:
      restartPolicy: OnFailure
      containers:
        - name: setup
          image: thunderid/setup:1.0.0
`

func TestExtractBootstrapImportJob(t *testing.T) {
	job, err := extractBootstrapImportJob([]byte(renderedChartFixture), "thunder-default-default-aep-bootstrap-import", "thunder-default-default")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if job.Name != "thunder-default-default-aep-bootstrap-import" {
		t.Errorf("Name = %q, want the repair job name (not the chart's own setup Job name)", job.Name)
	}
	if job.Namespace != "thunder-default-default" {
		t.Errorf("Namespace = %q, want thunder-default-default", job.Namespace)
	}
	if len(job.Annotations) != 0 {
		t.Errorf("Annotations = %v, want stripped (helm hook markers must not survive)", job.Annotations)
	}
	if job.Labels["app.kubernetes.io/managed-by"] != "aectl" {
		t.Errorf("managed-by label = %q, want aectl", job.Labels["app.kubernetes.io/managed-by"])
	}
	// The original chart label must survive — only managed-by is added.
	if job.Labels["app.kubernetes.io/name"] != "thunderid" {
		t.Errorf("original label app.kubernetes.io/name lost: %v", job.Labels)
	}
	if job.Spec.BackoffLimit == nil || *job.Spec.BackoffLimit != 0 {
		t.Errorf("BackoffLimit = %v, want 0", job.Spec.BackoffLimit)
	}
	if job.Spec.Template.Spec.RestartPolicy != corev1.RestartPolicyNever {
		t.Errorf("RestartPolicy = %q, want Never", job.Spec.Template.Spec.RestartPolicy)
	}
	if len(job.Spec.Template.Spec.Containers) != 1 || job.Spec.Template.Spec.Containers[0].Image != "thunderid/setup:1.0.0" {
		t.Errorf("container spec lost in conversion: %+v", job.Spec.Template.Spec.Containers)
	}
}

func TestExtractBootstrapImportJob_NoJobPresent(t *testing.T) {
	const noJob = `
apiVersion: v1
kind: ConfigMap
metadata:
  name: thunder-default-default-config
  namespace: thunder-default-default
data:
  key: value
`
	if _, err := extractBootstrapImportJob([]byte(noJob), "x", "ns"); err == nil {
		t.Fatal("expected an error when the rendered chart carries no Job")
	}
}

func TestExtractBootstrapImportJob_InvalidYAML(t *testing.T) {
	if _, err := extractBootstrapImportJob([]byte("not: [valid yaml"), "x", "ns"); err == nil {
		t.Fatal("expected an error on malformed YAML")
	}
}

// TestRenderedChartFixture_HasThreeDocuments guards the fixture's own shape
// (Secret, ConfigMap, Job) so a future edit that accidentally drops a
// document is caught here, rather than silently changing what
// TestExtractBootstrapImportJob is actually exercising.
func TestRenderedChartFixture_HasThreeDocuments(t *testing.T) {
	docs := strings.Split(strings.TrimSpace(renderedChartFixture), "\n---\n")
	if len(docs) != 3 {
		t.Fatalf("fixture has %d documents, want 3 (Secret, ConfigMap, Job)", len(docs))
	}
}
