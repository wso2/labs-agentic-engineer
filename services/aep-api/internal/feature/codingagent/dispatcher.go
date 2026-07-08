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

// Package codingagent's Dispatcher takes a single per-task dispatch
// request and walks the cluster-gateway-proxy to apply (in order):
//
//  1. Namespace             — per-org `wc-…-remote-worker`
//  2. ServiceAccount        — `remote-worker-runner`
//  3. ExternalSecret×2      — anthropic + github-pat
//  4. Job                   — the coding-agent runner pod
//
// When the BFF is wired with a cluster-gateway-proxy client AND the
// per-org credential rows carry the SM-API triplet, the dispatch caller
// picks this orchestrator; otherwise the legacy ClusterWorkflow path runs.
package codingagent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/wso2/aep/aep-api/internal/clients/clustergatewayproxy"
	"github.com/wso2/aep/aep-api/internal/platform/tenant"
)

// Inputs gathers everything one dispatch call needs. All fields are
// required (verified by validate); the orchestrator returns the final
// runName the caller persists on the ComponentTask row.
type Inputs struct {
	// OrgUUID is the OC organization UUID (NOT the slug). Used to
	// derive the deterministic remote-worker NS via
	// RemoteWorkerNamespace.
	OrgUUID string

	// Job is the fully-resolved Job manifest inputs. The orchestrator
	// passes it straight to Build; callers fill in everything except
	// `OrgNS`, `AnthropicSecretName`, `GitHubSecretName`, and
	// `PublisherSecretName`, which the orchestrator computes from
	// the run name and overwrites.
	Job JobInputs

	// AnthropicSR / GitHubSR are the per-org SM-API triplets fetched
	// from `org_anthropic_credentials` / `org_credentials`. Both must
	// be populated — the orchestrator refuses to dispatch if the
	// Connect flow hasn't completed the SM-API mirror.
	AnthropicSR SecretRef
	GitHubSR    SecretRef

	// PublisherSR is the per-org publisher cc credentials triplet
	// (`organization_idp_profiles`). When present, the orchestrator
	// emits a third per-run ExternalSecret materialising both
	// client_id + client_secret into a K8s Secret that the runner
	// mounts via envFrom (PUBLISHER_CLIENT_ID + PUBLISHER_CLIENT_SECRET).
	// Optional — when absent the runner falls back to AEP_BEARER.
	PublisherSR *SecretRef

	// ExternalResourceSRs are the per-env secret bundles for the external
	// resources the task's component binds (the SM-API vault path + the secret
	// keys). The orchestrator emits one per-run ExternalSecret each, materialising
	// every key into a K8s Secret the runner mounts via envFrom — so the agent
	// integration-tests against the live service. Empty when the task binds no
	// secret-bearing external resource.
	ExternalResourceSRs []ExternalResourceSecretInputs

	// ClusterSecretStoreName is the ESO CSS that backs reads. On
	// cloud-dp-oc-dp this MUST be `application-secrets-read` (AppRole
	// `approle-creds-application-read-permission` — the only one
	// scoped to `user-app-secrets/*` on the DP). `secretstore-read`
	// will silently no-op. Local k3d reuses `default`.
	ClusterSecretStoreName string
}

// SecretRef is the lookup triplet persisted on the per-org credential
// row by the Connect flow.
type SecretRef struct {
	SecretRefName string
	KVPath        string
	Property      string
}

// ExternalResourceSecretInputs is one external resource's per-env secret bundle
// for the runner: the SM-API vault KV path + the secret keys (each key == its
// SM-API property == the env var name the runner reads via envFrom).
type ExternalResourceSecretInputs struct {
	KVPath string
	Keys   []string
}

// defaultMaxConcurrentJobs bounds concurrent coding-agent Jobs per org
// namespace (§R3.4) — the ResourceQuota concurrency cap that replaces the DB
// admission mutex. A conservative default; WithConcurrencyLimit overrides it.
const defaultMaxConcurrentJobs = 5

// Dispatcher wraps the proxy client + defaults. Construct once at boot.
type Dispatcher struct {
	proxy *clustergatewayproxy.Client

	// serviceAccount is the SA name applied to the per-org NS. Defaults
	// to "remote-worker-runner" but configurable for tests.
	serviceAccount string

	// maxConcurrentJobs is the ResourceQuota's count/jobs.batch cap per org
	// namespace (§R3.4). 0 disables quota ensure entirely (concurrency stays
	// gated by the DB admission mutex alone).
	maxConcurrentJobs int
}

// New constructs a Dispatcher. proxy must be non-nil — the orchestrator
// has no fallback path; the caller decides whether to construct it.
func New(proxy *clustergatewayproxy.Client) *Dispatcher {
	if proxy == nil {
		panic("codingagent.Dispatcher: proxy is required")
	}
	return &Dispatcher{
		proxy:             proxy,
		serviceAccount:    "remote-worker-runner",
		maxConcurrentJobs: defaultMaxConcurrentJobs,
	}
}

// WithServiceAccount overrides the runner SA name. Returns the receiver
// for chained construction.
func (d *Dispatcher) WithServiceAccount(name string) *Dispatcher {
	if name != "" {
		d.serviceAccount = name
	}
	return d
}

// WithConcurrencyLimit overrides the per-org ResourceQuota's concurrent-Jobs
// cap (§R3.4). limit <= 0 disables the quota ensure step entirely — dispatch
// falls back to the DB admission mutex alone. Returns the receiver for
// chained construction.
func (d *Dispatcher) WithConcurrencyLimit(limit int) *Dispatcher {
	d.maxConcurrentJobs = limit
	return d
}

// Dispatch walks the four-step apply chain. Returns the runName (echo
// of `in.Job.RunName`) so callers don't have to thread it back.
//
// Idempotency: namespace + SA + ExternalSecrets use Ensure* / Apply*
// semantics (POST → 409 tolerated). Jobs are immutable: ApplyJob does
// a DELETE+POST on 409 so a re-dispatch with the same runName starts a
// fresh Job (use case: operator-triggered retry that intentionally
// reuses the run name).
func (d *Dispatcher) Dispatch(ctx context.Context, in Inputs) (string, error) {
	ns := tenant.RemoteWorkerNamespace(in.OrgUUID)
	if ns == "" {
		return "", errors.New("codingagent dispatcher: failed to derive NS — empty OrgUUID")
	}
	if err := d.validate(in); err != nil {
		return "", err
	}

	slog.InfoContext(ctx, "coding-agent proxy dispatch: start",
		"orgUUID", in.OrgUUID, "namespace", ns, "runName", in.Job.RunName)

	// 1) Namespace.
	if err := d.proxy.EnsureNamespace(ctx, clustergatewayproxy.NamespaceMeta{
		Name: ns,
		Labels: map[string]string{
			"app.kubernetes.io/managed-by": "aep",
			"aep.io/purpose":               "remote-worker",
			"aep.io/org-uuid":              in.OrgUUID,
		},
	}); err != nil {
		return "", fmt.Errorf("dispatcher: ensure namespace %s: %w", ns, err)
	}

	// 1.5) ResourceQuota (§R3.4 concurrency cap). Best-effort: the proxy's
	// CR/verb allow-list may not include "resourcequotas" yet (a cross-service
	// dependency) — degrade gracefully rather than block dispatch on it; the
	// DB admission mutex (TryAdmit) remains the active gate either way.
	if d.maxConcurrentJobs > 0 {
		if err := d.proxy.ApplyResourceQuota(ctx, ns, resourceQuotaManifest(ns, d.maxConcurrentJobs)); err != nil {
			slog.WarnContext(ctx, "dispatcher: ensure resource quota failed — falling back to the DB admission mutex alone",
				"namespace", ns, "error", err)
		}
	}

	// 2) ServiceAccount.
	if err := d.proxy.EnsureServiceAccount(ctx, ns, d.serviceAccount); err != nil {
		return "", fmt.Errorf("dispatcher: ensure SA %s/%s: %w", ns, d.serviceAccount, err)
	}

	// Pre-compute per-run secret names so the Job and the ExternalSecrets
	// agree on a single source of truth.
	runName := in.Job.RunName
	anthropicSecret := runName + "-anthropic"
	githubSecret := runName + "-github"
	publisherSecret := ""
	if in.PublisherSR != nil {
		publisherSecret = runName + "-publisher"
	}

	// 3) ExternalSecrets.
	if err := d.applyExternalSecret(ctx, in, ns, runName+"-anthropic-es", anthropicSecret, "ANTHROPIC_API_KEY", in.AnthropicSR); err != nil {
		return "", fmt.Errorf("dispatcher: apply anthropic ExternalSecret: %w", err)
	}
	if err := d.applyExternalSecret(ctx, in, ns, runName+"-github-es", githubSecret, "GITHUB_TOKEN", in.GitHubSR); err != nil {
		return "", fmt.Errorf("dispatcher: apply github-pat ExternalSecret: %w", err)
	}
	if in.PublisherSR != nil {
		if err := d.applyPublisherExternalSecret(ctx, in, ns, runName+"-publisher-es", publisherSecret, *in.PublisherSR); err != nil {
			return "", fmt.Errorf("dispatcher: apply publisher ExternalSecret: %w", err)
		}
	}
	// Per-external-resource ExternalSecrets — each key materialises into a K8s
	// Secret the runner mounts via envFrom (the agent integration-tests against
	// the live resource).
	extResSecretNames := make([]string, 0, len(in.ExternalResourceSRs))
	for i, er := range in.ExternalResourceSRs {
		if er.KVPath == "" || len(er.Keys) == 0 {
			continue
		}
		esName := fmt.Sprintf("%s-extres%d-es", runName, i)
		secretName := fmt.Sprintf("%s-extres%d", runName, i)
		if err := d.applyExternalResourceExternalSecret(ctx, in, ns, esName, secretName, er); err != nil {
			return "", fmt.Errorf("dispatcher: apply external-resource ExternalSecret: %w", err)
		}
		extResSecretNames = append(extResSecretNames, secretName)
	}

	// 4) Job — fill the secret names + NS + SA into the job inputs the
	// caller pre-populated, then build + apply.
	job := in.Job
	job.OrgNS = ns
	job.ServiceAccountName = d.serviceAccount
	job.AnthropicSecretName = anthropicSecret
	job.GitHubSecretName = githubSecret
	job.PublisherSecretName = publisherSecret
	job.ExternalResourceSecretNames = extResSecretNames
	manifest, err := Build(job)
	if err != nil {
		return "", fmt.Errorf("dispatcher: build Job manifest: %w", err)
	}
	if err := d.proxy.ApplyJob(ctx, ns, manifest); err != nil {
		return "", fmt.Errorf("dispatcher: apply Job: %w", err)
	}
	slog.InfoContext(ctx, "coding-agent proxy dispatch: complete",
		"namespace", ns, "runName", runName)
	return runName, nil
}

func (d *Dispatcher) applyExternalSecret(ctx context.Context, in Inputs, ns, esName, secretName, localKey string, ref SecretRef) error {
	manifest, err := BuildExternalSecret(ExternalSecretInputs{
		Name:                   esName,
		Namespace:              ns,
		TargetSecretName:       secretName,
		ClusterSecretStoreName: in.ClusterSecretStoreName,
		RemoteRefKey:           ref.KVPath,
		RemoteRefProperty:      ref.Property,
		LocalKey:               localKey,
	})
	if err != nil {
		return err
	}
	return d.proxy.ApplyExternalSecret(ctx, ns, manifest)
}

// applyPublisherExternalSecret emits one ExternalSecret with two data
// entries — client_id and client_secret share the SM-API secret at
// ref.KVPath but materialise into distinct K8s Secret keys (PUBLISHER_
// CLIENT_ID + PUBLISHER_CLIENT_SECRET) for envFrom in the Job pod.
// Token URL is non-secret and rides as a plain env from the Job template.
func (d *Dispatcher) applyPublisherExternalSecret(ctx context.Context, in Inputs, ns, esName, secretName string, ref SecretRef) error {
	manifest, err := BuildExternalSecret(ExternalSecretInputs{
		Name:                   esName,
		Namespace:              ns,
		TargetSecretName:       secretName,
		ClusterSecretStoreName: in.ClusterSecretStoreName,
		RemoteRefKey:           ref.KVPath,
		DataEntries: []ExternalSecretDataEntry{
			{LocalKey: "PUBLISHER_CLIENT_ID", RemoteRefProperty: "client_id"},
			{LocalKey: "PUBLISHER_CLIENT_SECRET", RemoteRefProperty: "client_secret"},
		},
	})
	if err != nil {
		return err
	}
	return d.proxy.ApplyExternalSecret(ctx, ns, manifest)
}

// applyExternalResourceExternalSecret emits one ExternalSecret reading all of an
// external resource's secret keys from its single SM-API path (one data entry per
// key, localKey == property == key) into a K8s Secret the runner mounts via
// envFrom.
func (d *Dispatcher) applyExternalResourceExternalSecret(ctx context.Context, in Inputs, ns, esName, secretName string, er ExternalResourceSecretInputs) error {
	entries := make([]ExternalSecretDataEntry, 0, len(er.Keys))
	for _, k := range er.Keys {
		entries = append(entries, ExternalSecretDataEntry{LocalKey: k, RemoteRefProperty: k})
	}
	manifest, err := BuildExternalSecret(ExternalSecretInputs{
		Name:                   esName,
		Namespace:              ns,
		TargetSecretName:       secretName,
		ClusterSecretStoreName: in.ClusterSecretStoreName,
		RemoteRefKey:           er.KVPath,
		DataEntries:            entries,
	})
	if err != nil {
		return err
	}
	return d.proxy.ApplyExternalSecret(ctx, ns, manifest)
}

func (d *Dispatcher) validate(in Inputs) error {
	if in.OrgUUID == "" {
		return errors.New("codingagent dispatcher: OrgUUID required")
	}
	if in.ClusterSecretStoreName == "" {
		return errors.New("codingagent dispatcher: ClusterSecretStoreName required")
	}
	if in.AnthropicSR.KVPath == "" || in.AnthropicSR.Property == "" {
		return errors.New("codingagent dispatcher: AnthropicSR not populated — Connect flow must complete the SM-API mirror first")
	}
	if in.GitHubSR.KVPath == "" || in.GitHubSR.Property == "" {
		return errors.New("codingagent dispatcher: GitHubSR not populated — Connect flow must complete the SM-API mirror first")
	}
	return nil
}

// resourceQuotaManifest builds the per-org namespace's ResourceQuota (§R3.4):
// caps concurrent coding-agent Jobs at limit. One quota object per namespace,
// named deterministically so ApplyResourceQuota's upsert always targets the
// same object.
func resourceQuotaManifest(namespace string, limit int) map[string]any {
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "ResourceQuota",
		"metadata": map[string]any{
			"name":      "remote-worker-jobs",
			"namespace": namespace,
		},
		"spec": map[string]any{
			"hard": map[string]any{
				"count/jobs.batch": fmt.Sprintf("%d", limit),
			},
		},
	}
}
