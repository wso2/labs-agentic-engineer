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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/yaml"

	"github.com/wso2/aep/aectl/internal/helm"
	k8s "github.com/wso2/aep/aectl/internal/kubernetes"
)

// aepBootstrapConfigMapName is kept apart from createThunder's install-time
// bootstrap ConfigMap name (<release>-bootstrap) so a later repair always has
// an AEP-owned source to mount, independent of whatever the release was
// originally installed with — mirrors setup-environment-thunder.sh's own
// AEP_BOOTSTRAP_CM vs. INSTALL_BOOTSTRAP_CM split.
func aepBootstrapConfigMapName(release string) string { return release + "-aep-bootstrap" }

func aepBootstrapImportJobName(release string) string { return release + "-aep-bootstrap-import" }

// repairAepSystemClient publishes AEP's bootstrap bundle (the aep-system-client
// application, its role, and the two platform documents every T2 needs) into
// an EXISTING Thunder release whose aep-system-client cannot currently mint —
// e.g. a release this process did not create (Agent Manager's own environment
// script, or a manual install), or one left behind by an earlier, interrupted
// run of this same package.
//
// ThunderID's bootstrap import runs only as a pre-install Helm hook Job — a
// release that never received AEP's documents at install time never gets
// them from a later `helm upgrade`, and upgrading a release this process did
// not create is off the table anyway (installThunder's own CREATE/BIND
// contract). What the chart DOES expose is the Job template itself:
// re-render the SAME chart at the release's OWN version and live values (so
// nothing else about the release changes), with only the bootstrap
// ConfigMap swapped for an AEP-owned one, extract the rendered Job, strip
// its Helm-hook markers so it runs as an ordinary Job instead of being
// silently ignored by a bare `kubectl apply`, and run it directly. Mirrors
// setup-environment-thunder.sh's run_aep_bootstrap_import.
func repairAepSystemClient(ctx context.Context, c clients, cfg Config, inst *ThunderInstance) error {
	docs := renderBootstrapDocuments(cfg.Org, cfg.Env, inst.SystemClientSecret, inst.SystemResourceIdentifier)
	bootstrapCM := aepBootstrapConfigMapName(inst.Release)
	data := make(map[string]string, len(docs))
	names := make([]string, 0, len(docs))
	for _, d := range docs {
		data[d.name] = d.content
		names = append(names, d.name)
	}
	sort.Strings(names) // ThunderID imports in lexical order; documented dependency order.
	if _, err := c.k8s.CoreV1().ConfigMaps(inst.Namespace).Apply(ctx,
		applyConfigMap(inst.Namespace, bootstrapCM, data, nil), metav1.ApplyOptions{FieldManager: "aectl", Force: true},
	); err != nil {
		return fmt.Errorf("apply AEP bootstrap ConfigMap %s/%s: %w", inst.Namespace, bootstrapCM, err)
	}

	version, err := helm.GetReleaseVersion(ctx, cfg.Kubeconfig, inst.Release, inst.Namespace)
	if err != nil {
		return fmt.Errorf("read live chart version: %w", err)
	}
	liveValues, err := helm.GetReleaseValuesYAML(ctx, cfg.Kubeconfig, inst.Release, inst.Namespace)
	if err != nil {
		return fmt.Errorf("read live release values: %w", err)
	}

	spec, err := aepBootstrapImportChartSpec(inst, version, bootstrapCM, names)
	if err != nil {
		return fmt.Errorf("build bootstrap-import chart spec: %w", err)
	}
	rendered, err := helm.TemplateChart(ctx, cfg.Kubeconfig, spec, liveValues)
	if err != nil {
		return fmt.Errorf("render bootstrap-import Job: %w", err)
	}

	job, err := extractBootstrapImportJob(rendered, aepBootstrapImportJobName(inst.Release), inst.Namespace)
	if err != nil {
		return fmt.Errorf("extract bootstrap-import Job: %w", err)
	}

	// A leftover Job from an earlier, interrupted repair attempt must be
	// cleared first — the Job's name (and its spec.selector, immutable once
	// set) cannot be reused by a fresh Create. Delete returning success does
	// not mean the object is already gone: RunJob's own cleanup deletes with
	// Foreground propagation, which keeps the Job present (deletionTimestamp
	// set) until its dependent Pod finishes finalizing, so the very next
	// Create below can otherwise race it and fail with AlreadyExists.
	if err := c.k8s.BatchV1().Jobs(inst.Namespace).Delete(ctx, job.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete stale bootstrap-import Job %s/%s: %w", inst.Namespace, job.Name, err)
	}
	if err := waitForJobDeleted(ctx, c, inst.Namespace, job.Name, 2*time.Minute); err != nil {
		return fmt.Errorf("wait for stale bootstrap-import Job %s/%s to clear: %w", inst.Namespace, job.Name, err)
	}

	var out bytes.Buffer
	if err := k8s.RunJob(ctx, c.k8s, job, &out); err != nil {
		return fmt.Errorf("bootstrap-import Job failed: %w\n%s", err, out.String())
	}
	return nil
}

// waitForJobDeleted polls Get until name is gone from namespace (NotFound) or
// timeout elapses, propagating any other error immediately. See its caller
// for why Delete's own success is not sufficient here.
func waitForJobDeleted(ctx context.Context, c clients, namespace, name string, timeout time.Duration) error {
	return pollUntilJobDeleted(ctx, c, namespace, name, timeout, 2*time.Second)
}

func pollUntilJobDeleted(ctx context.Context, c clients, namespace, name string, timeout, interval time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		_, err := c.k8s.BatchV1().Jobs(namespace).Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("get job %s/%s: %w", namespace, name, err)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out after %s waiting for job %s/%s to be deleted", timeout, namespace, name)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
}

// aepBootstrapImportChartSpec builds the minimal `helm template` override for
// a repair run: ONLY the bootstrap ConfigMap name/file-list changes — every
// other setting comes from the release's own live values (baseValuesYAML,
// passed separately to helm.TemplateChart), so re-rendering the Job cannot
// silently revert an unrelated setting to the chart's default. Deliberately
// NOT thunderChartSpec, which is CREATE's full fresh-install spec and would
// reassert every value that liveValues might disagree with.
func aepBootstrapImportChartSpec(inst *ThunderInstance, version, bootstrapCM string, fileNames []string) (helm.ChartSpec, error) {
	filesJSON, err := json.Marshal(fileNames)
	if err != nil {
		return helm.ChartSpec{}, fmt.Errorf("marshal bootstrap file list: %w", err)
	}
	return helm.ChartSpec{
		ReleaseName: inst.Release,
		Chart:       thunderChart,
		Version:     version,
		Namespace:   inst.Namespace,
		SetStrings: []string{
			fmt.Sprintf("bootstrap.configMap.name=%s", bootstrapCM),
		},
		SetJSON: []string{
			fmt.Sprintf("bootstrap.configMap.files=%s", filesJSON),
		},
	}, nil
}

// extractBootstrapImportJob finds the single Job document in a chart's
// rendered manifests (ThunderID's own install-hook Job) and prepares it to
// run standalone:
//   - Helm hook annotations are stripped — `helm template`'s output is inert
//     as far as hook semantics go (hooks only run via `helm install`/
//     `upgrade` itself), so a bare `kubectl apply`/Create of the rendered Job
//     as-is would leave it exactly as harmless AND useless; the annotations
//     have to go for this to be an ordinary Job Kubernetes just runs.
//   - The name is replaced with jobName so repeated repairs always target
//     the same, predictable object (and so Delete-before-Create — see the
//     caller — knows what to clear).
//   - backoffLimit is forced to 0 so a bad document fails once with its logs
//     readable, rather than retrying into a BackOff.
//   - restartPolicy is forced to Never (required for backoffLimit: 0 to mean
//     what it says — Kubernetes rejects Never/OnFailure ambiguity otherwise).
func extractBootstrapImportJob(rendered []byte, jobName, namespace string) (*batchv1.Job, error) {
	dec := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(rendered), 4096)
	for {
		obj := &unstructured.Unstructured{}
		if err := dec.Decode(obj); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("decode rendered manifest: %w", err)
		}
		if len(obj.Object) == 0 || obj.GetKind() != "Job" {
			continue
		}
		var job batchv1.Job
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, &job); err != nil {
			return nil, fmt.Errorf("convert rendered Job: %w", err)
		}
		job.Name = jobName
		job.Namespace = namespace
		job.Annotations = nil
		if job.Labels == nil {
			job.Labels = map[string]string{}
		}
		job.Labels["app.kubernetes.io/managed-by"] = "aectl"
		backoff := int32(0)
		job.Spec.BackoffLimit = &backoff
		job.Spec.Template.Spec.RestartPolicy = corev1.RestartPolicyNever
		return &job, nil
	}
	return nil, fmt.Errorf("no Job found in rendered chart — the chart layout may have changed")
}
