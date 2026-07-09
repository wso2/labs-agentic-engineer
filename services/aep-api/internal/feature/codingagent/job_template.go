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

// Package codingagent builds the per-task K8s Job manifest the BFF
// POSTs through cluster-gateway-proxy. The shape is plain `map[string]any`
// so it composes directly with clients/clustergatewayproxy.ApplyJob — no
// typed k8s.io/api/batch/v1 dependency leaks here.
//
// Inputs and naming:
//
//   - runName     — short DNS-1123 label, used as the Job name and as
//     the workflow-run identifier persisted on
//     ComponentTask. The watcher keys on this.
//   - orgNS       — per-org remote-worker namespace
//     (`wc-<orgUUID8>-<orgHash8>-remote-worker`).
//   - anthropicSecretName / githubSecretName / publisherSecretName
//     — K8s Secret names materialized by per-run
//     ExternalSecrets just before the Job is applied.
//     Mounted as envFrom so a leaked process listing
//     won't disclose secret values.
//   - runnerImage — the aep remote-worker image; pinned by the BFF
//     from cfg.AgentRunnerImage.
//   - prompt / repo URL / identity / callback URL — pre-rendered
//     dispatch payload the runner reads from
//     AEP_* env vars.
//
// Mounts:
//
//   - the runner's workspace lives on an `emptyDir` at
//     /home/aep/aep-workspace (matches remote-worker's config.ts
//     default).
//
// Restart policy:
//
//   - restartPolicy: Never + backoffLimit: 0. Failures are picked up by
//     the watcher polling Job.status, not by K8s replay. Argo-style
//     retry is intentionally absent — agent retries are policy, not
//     plumbing.
package codingagent

import (
	"fmt"
	"strings"
)

// JobInputs collects everything the template needs. All fields are
// required unless the doc-comment says otherwise; Build returns an
// error rather than producing a manifest that K8s will reject.
type JobInputs struct {
	RunName       string // Job name (DNS-1123 label, ≤ 63 chars)
	OrgNS         string // target namespace
	TaskID        string // ComponentTask UUID, stamped as a label for queries
	OrgID         string // OC org handle, stamped as a label
	ProjectID     string
	ComponentName string

	// RunnerImage is the docker image the runner pod uses. Always pinned —
	// `:latest` is OK in dev but the BFF should resolve to a digest for prod.
	RunnerImage string

	// ServiceAccountName is the SA the runner pod runs as. The dispatcher
	// ensures this SA exists in OrgNS before applying the Job.
	ServiceAccountName string

	// Per-run K8s Secret names that ExternalSecrets will materialize. The
	// Job applies before the secret arrives is possible in theory; in
	// practice the BFF applies the ExternalSecret first and the per-run
	// secret is in place by the time the kubelet pulls the image.
	AnthropicSecretName string
	GitHubSecretName    string
	// PublisherSecretName is the K8s Secret holding the per-org publisher
	// cc creds (client_id + client_secret) materialised by a per-run ES
	// from SM-API. Empty disables the runner-auth path; the runner then
	// falls back to AEP_BEARER for /credentials/refresh calls.
	PublisherSecretName string
	// ExternalResourceSecretNames are the per-run K8s Secrets holding the external
	// resources' secret values (one per external resource the task binds), each
	// mounted via envFrom so every secret key (e.g. OPENWEATHER_API_KEY) is a
	// runner env var. Empty when the task binds no secret-bearing external resource.
	ExternalResourceSecretNames []string
	// PublisherTokenURL is the Thunder /oauth2/token endpoint used by the
	// runner's cc helper. Non-secret; rides as a plain env. Set in lockstep
	// with PublisherSecretName.
	PublisherTokenURL string

	// Dispatch payload — passed verbatim into the runner via AEP_* env vars.
	RepoURL string
	// SkillsRepoURL is the org's `org-skills` repo clone URL (the `_skills`
	// sentinel row). The runner clones it to resolve the design's applied
	// skills locally (replacing the retired S2S skills-pull). Optional: empty
	// (unprovisioned org / lookup failure) is stamped as no env var, and the
	// runner degrades to the base `aep` plugin only.
	SkillsRepoURL string
	Prompt        string
	IdentityName  string
	IdentityEmail string
	IdentityLogin string // optional
	GitServiceURL string // AEP_GIT_SERVICE_URL (BFF URL reachable from the pod)
	CallbackURL   string // AEP_PLATFORM_URL (BFF callback URL)
	CorrelationID string // optional; runner synthesizes one if absent

	// Bearer is the bespoke AEP_BEARER param. When PublisherSecretName
	// is also set the runner prefers the Thunder client_credentials flow
	// and uses Bearer only as a fallback.
	Bearer string

	// ActiveDeadlineSeconds bounds the agent run. Zero falls back to 1h.
	ActiveDeadlineSeconds int64
}

// Build returns the Job manifest as a map[string]any ready to hand to
// clustergatewayproxy.ApplyJob. Validates required fields up-front so
// the proxy doesn't surface a 422 from k8s.
func Build(in JobInputs) (map[string]any, error) {
	if err := validate(in); err != nil {
		return nil, err
	}

	activeDeadline := in.ActiveDeadlineSeconds
	if activeDeadline <= 0 {
		activeDeadline = 3600 // 1h default
	}

	envVars := []map[string]any{
		{"name": "AEP_TASK_ID", "value": in.TaskID},
		{"name": "AEP_ORG_ID", "value": in.OrgID},
		{"name": "AEP_PROJECT_ID", "value": in.ProjectID},
		{"name": "AEP_COMPONENT_NAME", "value": in.ComponentName},
		{"name": "AEP_REPO_URL", "value": in.RepoURL},
		{"name": "AEP_PROMPT", "value": in.Prompt},
		{"name": "AEP_GIT_SERVICE_URL", "value": in.GitServiceURL},
		{"name": "AEP_PLATFORM_URL", "value": in.CallbackURL},
		{"name": "AEP_IDENTITY_NAME", "value": in.IdentityName},
		{"name": "AEP_IDENTITY_EMAIL", "value": in.IdentityEmail},
		{"name": "AEP_IDENTITY_LOGIN", "value": in.IdentityLogin},
		{"name": "AEP_CORRELATION_ID", "value": in.CorrelationID},
		{"name": "WORKSPACE_BASE_PATH", "value": "/home/aep/aep-workspace"},
	}
	if in.Bearer != "" {
		envVars = append(envVars, map[string]any{
			"name": "AEP_BEARER", "value": in.Bearer,
		})
	}
	// Optional: only stamped when the org's skills repo resolved. Absent → the
	// runner skips the per-task skills clone and degrades to the base plugin.
	if in.SkillsRepoURL != "" {
		envVars = append(envVars, map[string]any{
			"name": "AEP_SKILLS_REPO_URL", "value": in.SkillsRepoURL,
		})
	}

	envFrom := []map[string]any{
		{"secretRef": map[string]any{"name": in.AnthropicSecretName}},
		{"secretRef": map[string]any{"name": in.GitHubSecretName}},
	}
	for _, n := range in.ExternalResourceSecretNames {
		if n != "" {
			envFrom = append(envFrom, map[string]any{"secretRef": map[string]any{"name": n}})
		}
	}
	if in.PublisherSecretName != "" {
		envFrom = append(envFrom, map[string]any{
			"secretRef": map[string]any{"name": in.PublisherSecretName},
		})
		if in.PublisherTokenURL != "" {
			envVars = append(envVars, map[string]any{
				"name": "PUBLISHER_TOKEN_URL", "value": in.PublisherTokenURL,
			})
		}
	}

	labels := map[string]string{
		"app.kubernetes.io/name":    "remote-worker",
		"app.kubernetes.io/part-of": "aep",
		"aep.io/task":               in.TaskID,
		"aep.io/org":                in.OrgID,
		"aep.io/project":            in.ProjectID,
		"aep.io/component":          in.ComponentName,
	}

	return map[string]any{
		"apiVersion": "batch/v1",
		"kind":       "Job",
		"metadata": map[string]any{
			"name":      in.RunName,
			"namespace": in.OrgNS,
			"labels":    labels,
		},
		"spec": map[string]any{
			"backoffLimit":            int64(0),
			"activeDeadlineSeconds":   activeDeadline,
			"ttlSecondsAfterFinished": int64(86400), // 24h cleanup grace
			"template": map[string]any{
				"metadata": map[string]any{
					"labels": labels,
				},
				"spec": map[string]any{
					"restartPolicy":      "Never",
					"serviceAccountName": in.ServiceAccountName,
					"containers": []map[string]any{
						{
							"name":    "remote-worker",
							"image":   in.RunnerImage,
							"env":     envVars,
							"envFrom": envFrom,
							"volumeMounts": []map[string]any{
								{"name": "workspace", "mountPath": "/home/aep/aep-workspace"},
								{"name": "tmp", "mountPath": "/tmp"},
							},
						},
					},
					"volumes": []map[string]any{
						{"name": "workspace", "emptyDir": map[string]any{}},
						{"name": "tmp", "emptyDir": map[string]any{}},
					},
				},
			},
		},
	}, nil
}

func validate(in JobInputs) error {
	var missing []string
	check := func(name, v string) {
		if strings.TrimSpace(v) == "" {
			missing = append(missing, name)
		}
	}
	check("RunName", in.RunName)
	check("OrgNS", in.OrgNS)
	check("TaskID", in.TaskID)
	check("OrgID", in.OrgID)
	check("ProjectID", in.ProjectID)
	check("ComponentName", in.ComponentName)
	check("RunnerImage", in.RunnerImage)
	check("ServiceAccountName", in.ServiceAccountName)
	check("AnthropicSecretName", in.AnthropicSecretName)
	check("GitHubSecretName", in.GitHubSecretName)
	check("RepoURL", in.RepoURL)
	check("Prompt", in.Prompt)
	check("IdentityName", in.IdentityName)
	check("IdentityEmail", in.IdentityEmail)
	check("GitServiceURL", in.GitServiceURL)
	check("CallbackURL", in.CallbackURL)

	if len(in.RunName) > 63 {
		return fmt.Errorf("codingagent: RunName %q exceeds K8s 63-char DNS label limit", in.RunName)
	}
	if len(missing) > 0 {
		return fmt.Errorf("codingagent: missing required field(s): %s", strings.Join(missing, ", "))
	}
	return nil
}
