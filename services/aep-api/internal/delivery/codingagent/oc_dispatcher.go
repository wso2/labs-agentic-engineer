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

package codingagent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/platform/orgconfig"
)

// RetentionEnforcer frees finished coding-agent Component slots before create.
// Task 5 fills the LRU implementation; nil or a no-op is fine for Task 4.
type RetentionEnforcer interface {
	Enforce(ctx context.Context, orgID, projectID string) error
}

// OCJobSurface is the narrow OpenChoreo port the dispatcher needs. ComponentClient
// satisfies it once EnsureWorkload / EnsureRelease / EnsureReleaseBinding land;
// tests use a fake so Dispatch never hits the network.
type OCJobSurface interface {
	EnsureComponentType(ctx context.Context, orgName string, body map[string]any) error
	CreateComponent(ctx context.Context, orgName, projectName string, req *openchoreo.CreateComponentRequest) (*gen.Component, error)
	EnsureWorkload(ctx context.Context, orgName, projectName string, in openchoreo.WorkloadInput) error
	EnsureRelease(ctx context.Context, orgName, projectName, componentName, releaseName string) (string, error)
	EnsureReleaseBinding(ctx context.Context, orgName, projectName, componentName, environment, releaseName string) error
}

// OCDispatchInputs is one cycle's launch payload. Secret values never appear —
// only SecretEnv refs (name + key) for ESO.
type OCDispatchInputs struct {
	OrgID, ProjectID, CycleID string
	// RunID is the milestone run the cycle belongs to — Component description only.
	RunID           string
	MilestoneNumber int
	MilestoneTitle  string
	Kind            string // coding|validation|…
	RunName         string // ca-… deterministic for this attempt
	// Runtime is the coding-agent runtime this cycle runs on. It picks the
	// runner image and is stamped as the ComponentType's `runtime` parameter
	// and the aep.wso2.com/runtime label. Empty means the platform default.
	Runtime               orgconfig.AgentRuntime
	Image                 string
	ActiveDeadlineSeconds int
	Env                   map[string]string
	// SecretEnv is refs-only (anthropic/github/publisher/external) — values
	// never written by the BFF.
	SecretEnv []SecretEnvRef
}

// SecretEnvRef is one Workload env entry backed by a SecretReference (ESO).
type SecretEnvRef struct {
	Key        string
	SecretName string
	SecretKey  string
}

// codingAgentWorkspacePath mirrors the runner's own default (remote-worker's
// config.ts) and the ComponentType's volume mount.
const codingAgentWorkspacePath = "/home/aep/aep-workspace"

// The runner's secret env var names. They are the RUNNER's contract, not a
// credential's identity: the value behind each arrives from whichever
// SecretReference the org's row names.
const (
	envAnthropicAPIKey = "ANTHROPIC_API_KEY"
	envGitHubToken     = "GITHUB_TOKEN"

	// envEvalAnthropicAPIKey carries the org's DEFAULT Anthropic key for the
	// build's agent-evaluation step, which needs a model twice over: for the
	// generated agent it boots and for the judge that grades it.
	//
	// It has its OWN name rather than reusing ANTHROPIC_API_KEY because that
	// variable already belongs to Claude Code, which ranks it above
	// CLAUDE_CODE_OAUTH_TOKEN (ADR-0016). Mounting the evaluation key under it
	// would silently move the coding session of every OAuth-billing org onto the
	// credential that org deliberately moved away from — the exact mis-bill
	// ADR-0016 exists to prevent. A distinct name keeps the two budgets apart,
	// and the harness reads it in preference to ANTHROPIC_API_KEY.
	envEvalAnthropicAPIKey = "AEP_EVAL_ANTHROPIC_API_KEY"

	// envEvalKeyManaged declares that the PLATFORM owns this pod's evaluation
	// credential: if envEvalAnthropicAPIKey is not set here, this run has no
	// evaluation key at all.
	//
	// It exists because the pod's ANTHROPIC_API_KEY, when present, is the CODING
	// credential — possibly an override the org chose to bill coding and nothing
	// else. Without this declaration the harness cannot tell that pod apart from
	// a developer's machine, where ANTHROPIC_API_KEY is simply "the key", and it
	// would quietly grade agents on a budget the org ring-fenced. Set on EVERY
	// dispatch, including the ones carrying no evaluation key: that is the case
	// it exists for.
	envEvalKeyManaged = "AEP_EVAL_KEY_MANAGED"

	// The organization's coding-agent setting, as the runner reads it
	// (`runtime/registry.ts`). Plain env, never a secret: they are enum values,
	// and the runner needs them before it can start a session.
	envAgentRuntime = "AEP_AGENT_RUNTIME"
	envAgentModel   = "AEP_AGENT_MODEL"
)

// OCDispatcher creates the ephemeral coding-agent Component chain:
// EnsureComponentType → CreateComponent → EnsureWorkload → EnsureRelease →
// EnsureReleaseBinding into openchoreo.DevEnvironmentName.
type OCDispatcher struct {
	oc        OCJobSurface
	retention RetentionEnforcer
	// images is the runner image per runtime, used when OCDispatchInputs.Image
	// is empty. Two tags built from one Dockerfile, sharing every heavy layer.
	images map[orgconfig.AgentRuntime]runnerImage
}

// runnerImage is one runtime's runner image and the deploy setting it comes
// from. A cycle whose runtime has no image fails its dispatch naming the
// setting, never falling back to another runtime's image (which would start a
// pod without that runtime's binary).
type runnerImage struct {
	ref     string
	setting string
}

// NewOCDispatcher wires the dispatcher against an OC surface.
func NewOCDispatcher(oc OCJobSurface) *OCDispatcher {
	return &OCDispatcher{oc: oc, images: map[orgconfig.AgentRuntime]runnerImage{
		orgconfig.AgentRuntimeClaudeCode: {setting: "AGENT_RUNNER_IMAGE"},
		orgconfig.AgentRuntimeOpenCode:   {setting: "AGENT_RUNNER_IMAGE_OPENCODE"},
	}}
}

// WithImage sets the Claude Code runner image. Returns the receiver for
// chained construction.
func (d *OCDispatcher) WithImage(image string) *OCDispatcher {
	return d.withRunnerImage(orgconfig.AgentRuntimeClaudeCode, image)
}

// WithOpenCodeImage sets the OpenCode runner image.
func (d *OCDispatcher) WithOpenCodeImage(image string) *OCDispatcher {
	return d.withRunnerImage(orgconfig.AgentRuntimeOpenCode, image)
}

func (d *OCDispatcher) withRunnerImage(runtime orgconfig.AgentRuntime, image string) *OCDispatcher {
	img := d.images[runtime]
	img.ref = strings.TrimSpace(image)
	d.images[runtime] = img
	return d
}

// WithRetention sets the pre-create retention helper (Task 5). Nil skips.
func (d *OCDispatcher) WithRetention(r RetentionEnforcer) *OCDispatcher {
	d.retention = r
	return d
}

// Dispatch launches one cycle and returns the Component (= RunName).
func (d *OCDispatcher) Dispatch(ctx context.Context, in OCDispatchInputs) (string, error) {
	if err := d.validate(in); err != nil {
		return "", err
	}

	if err := d.oc.EnsureComponentType(ctx, in.OrgID, openchoreo.CodingAgentComponentType()); err != nil {
		return "", fmt.Errorf("oc dispatch: ensure ComponentType: %w", err)
	}

	if d.retention != nil {
		if err := d.retention.Enforce(ctx, in.OrgID, in.ProjectID); err != nil {
			slog.WarnContext(ctx, "oc dispatch: retention enforce failed; continuing create",
				"org", in.OrgID, "project", in.ProjectID, "error", err)
		}
	}

	image := d.resolveImage(in)
	labels := d.markers(in)
	desc := fmt.Sprintf("AEP internal: agent run cycle %s.", in.CycleID)
	if in.RunID != "" {
		desc = fmt.Sprintf("AEP internal: agent run cycle %s of run %s.", in.CycleID, in.RunID)
	}
	req := &openchoreo.CreateComponentRequest{
		Name:        in.RunName,
		DisplayName: displayNameFor(in),
		Description: desc,
		Type:        openchoreo.CodingAgentComponentTypeRef,
		AutoBuild:   false,
		AutoDeploy:  false,
		Labels:      labels,
		Parameters:  componentParameters(in),
	}
	if _, err := d.oc.CreateComponent(ctx, in.OrgID, in.ProjectID, req); err != nil {
		if errors.Is(err, openchoreo.ErrPaymentRequired) {
			return "", fmt.Errorf("%w: create component %q", delivery.ErrAgentQuotaExceeded, in.RunName)
		}
		return "", fmt.Errorf("oc dispatch: create component %q: %w", in.RunName, err)
	}

	if err := d.oc.EnsureWorkload(ctx, in.OrgID, in.ProjectID, openchoreo.WorkloadInput{
		ComponentName: in.RunName,
		Image:         image,
		Env:           workloadEnv(in),
		Labels:        labels,
	}); err != nil {
		return "", fmt.Errorf("oc dispatch: workload for %q: %w", in.RunName, err)
	}

	releaseName, err := d.oc.EnsureRelease(ctx, in.OrgID, in.ProjectID, in.RunName, releaseNameFor(in.ProjectID, in.RunName))
	if err != nil {
		return "", fmt.Errorf("oc dispatch: release for %q: %w", in.RunName, err)
	}
	if err := d.oc.EnsureReleaseBinding(ctx, in.OrgID, in.ProjectID, in.RunName,
		openchoreo.DevEnvironmentName, releaseName); err != nil {
		return "", fmt.Errorf("oc dispatch: release binding for %q: %w", in.RunName, err)
	}

	return in.RunName, nil
}

// resolveImage picks the runner image: an explicit one on the inputs, else the
// image configured for the cycle's runtime.
func (d *OCDispatcher) resolveImage(in OCDispatchInputs) string {
	if img := strings.TrimSpace(in.Image); img != "" {
		return img
	}
	return d.images[runtimeOrDefault(in.Runtime)].ref
}

func runtimeOrDefault(runtime orgconfig.AgentRuntime) orgconfig.AgentRuntime {
	if runtime == "" {
		return orgconfig.DefaultAgentRuntime
	}
	return runtime
}

func (d *OCDispatcher) validate(in OCDispatchInputs) error {
	var missing []string
	check := func(name, v string) {
		if strings.TrimSpace(v) == "" {
			missing = append(missing, name)
		}
	}
	check("OrgID", in.OrgID)
	check("ProjectID", in.ProjectID)
	check("CycleID", in.CycleID)
	check("RunName", in.RunName)
	check(d.imageField(in), d.resolveImage(in))
	if len(missing) > 0 {
		return fmt.Errorf("oc dispatch: missing required field(s): %s", strings.Join(missing, ", "))
	}
	return nil
}

// imageField names the image in a missing-field error by the setting that
// supplies it for the cycle's runtime.
func (d *OCDispatcher) imageField(in OCDispatchInputs) string {
	runtime := runtimeOrDefault(in.Runtime)
	if img, ok := d.images[runtime]; ok {
		return fmt.Sprintf("Image (%s)", img.setting)
	}
	return fmt.Sprintf("Image (no runner image for runtime %q)", runtime)
}

func (d *OCDispatcher) markers(in OCDispatchInputs) map[string]string {
	return map[string]string{
		string(openchoreo.LabelKeyAepInternal):  openchoreo.LabelValueAepInternal,
		string(openchoreo.LabelKeyAepMilestone): fmt.Sprintf("%d", in.MilestoneNumber),
		string(openchoreo.LabelKeyAepCycle):     in.CycleID,
		string(openchoreo.LabelKeyAepRunName):   in.RunName,
		string(openchoreo.LabelKeyAepRuntime):   string(runtimeOrDefault(in.Runtime)),
		string(openchoreo.LabelKeyK8sManagedBy): openchoreo.LabelValueAep,
		string(openchoreo.LabelKeyK8sPartOf):    openchoreo.LabelValueAep,
		string(openchoreo.LabelKeyK8sName):      openchoreo.CodingAgentComponentTypeName,
	}
}

// componentParameters are the ComponentType parameters this cycle sets. The
// runtime is always stamped, so the rendered Job's label states the runtime
// rather than inheriting the schema's default.
func componentParameters(in OCDispatchInputs) map[string]any {
	params := map[string]any{"runtime": string(runtimeOrDefault(in.Runtime))}
	if in.ActiveDeadlineSeconds > 0 {
		params["activeDeadlineSeconds"] = in.ActiveDeadlineSeconds
	}
	return params
}

func displayNameFor(in OCDispatchInputs) string {
	kind := "Coding"
	if strings.EqualFold(in.Kind, "validation") {
		kind = "Validation"
	}
	return fmt.Sprintf("%s cycle — milestone #%d %s", kind, in.MilestoneNumber, in.MilestoneTitle)
}

func releaseNameFor(projectID, runName string) string {
	return openchoreo.ScopedComponentName(projectID, runName) + "-release"
}

func workloadEnv(in OCDispatchInputs) []openchoreo.WorkflowEnvVarRef {
	out := make([]openchoreo.WorkflowEnvVarRef, 0, len(in.Env)+len(in.SecretEnv))
	for k, v := range in.Env {
		out = append(out, openchoreo.WorkflowEnvVarRef{Key: k, Value: v})
	}
	for _, s := range in.SecretEnv {
		name, key := s.SecretName, s.SecretKey
		out = append(out, openchoreo.WorkflowEnvVarRef{
			Key: s.Key,
			ValueFrom: &openchoreo.WorkflowEnvVarValueRef{
				SecretKeyRef: &openchoreo.WorkflowSecretKeyRef{Name: name, Key: key},
			},
		})
	}
	return out
}
