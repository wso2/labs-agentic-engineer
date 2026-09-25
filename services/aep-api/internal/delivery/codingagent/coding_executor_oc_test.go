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
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/orgconfig"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// chainRecorder is the OC create-chain fake under the brief's name so the
// path-selection tests read clearly. Same package as fakeOCSurface.
type chainRecorder = fakeOCSurface

func (r *chainRecorder) client() OCJobSurface { return r }

func strPtr(s string) *string { return &s }

type fakeOrgRepo struct {
	org *organization.Organization
}

func (f fakeOrgRepo) ListByNames(context.Context, []string) ([]organization.Organization, error) {
	return nil, nil
}
func (f fakeOrgRepo) GetByName(context.Context, string) (*organization.Organization, error) {
	return f.org, nil
}
func (f fakeOrgRepo) Create(context.Context, *organization.Organization) error { return nil }
func (f fakeOrgRepo) SetThunderOrgUUID(context.Context, string, uuid.UUID) error {
	return nil
}

// fakeCodingKey stands in for the organization domain's answer to "which
// Anthropic credential does a run on this runtime bill". WHICH credential that
// is — the subscription or the API key — is decided and tested in the
// organization package (TestResolveCodingSecretRef_*); dispatch's job is to ask
// with the runtime the run will use, mount whatever it is handed, and abort
// when nothing can be handed to it. asked records the runtime it was asked
// with.
type fakeCodingKey struct {
	ref   organization.SecretRefTriplet
	err   error
	asked *orgconfig.AgentRuntime

	// The DEFAULT-role key is a separate answer to a separate question: which
	// credential the build's EVALUATION step bills. It is not always the same
	// row as the coding one, which is exactly what the evaluation tests below
	// exercise.
	defaultRef organization.SecretRefTriplet
	defaultErr error
}

func (f fakeCodingKey) ResolveCodingSecretRef(_ context.Context, _ string, runtime orgconfig.AgentRuntime) (organization.SecretRefTriplet, error) {
	if f.asked != nil {
		*f.asked = runtime
	}
	return f.ref, f.err
}

func (f fakeCodingKey) DefaultKeyRef(context.Context, string) (organization.SecretRefTriplet, error) {
	return f.defaultRef, f.defaultErr
}

type fakeGitHubCreds struct {
	row *organization.OrgCredential
}

func (f fakeGitHubCreds) GetByOrg(context.Context, string) (*organization.OrgCredential, error) {
	return f.row, nil
}
func (f fakeGitHubCreds) GetByInstallationID(context.Context, int64) (*organization.OrgCredential, error) {
	return nil, nil
}
func (f fakeGitHubCreds) UpdateColumns(context.Context, string, map[string]any) error { return nil }
func (f fakeGitHubCreds) ListActiveRows(context.Context) ([]organization.OrgCredential, error) {
	return nil, nil
}
func (f fakeGitHubCreds) ListBoundInstallations(context.Context) ([]organization.BoundInstallation, error) {
	return nil, nil
}
func (f fakeGitHubCreds) OrgIDByRepoURL(context.Context, string) (string, error) { return "", nil }
func (f fakeGitHubCreds) Tx(context.Context, func(organization.OrgCredentialTx) error) error {
	return nil
}

func fullSecretRefs() (fakeCodingKey, *organization.OrgCredential) {
	// No subscription — the org bills its API key — is the common case, so both
	// answers name the same row here. Tests that care about the difference set
	// defaultRef themselves.
	defaultRef := organization.SecretRefTriplet{
		Name:     "acme-anthropic-secrets",
		KVPath:   "user-app-secrets/wc-acme/acme-anthropic-secrets",
		Property: "api-key",
		EnvVar:   "ANTHROPIC_API_KEY",
	}
	return fakeCodingKey{ref: defaultRef, defaultRef: defaultRef}, &organization.OrgCredential{
		SecretRefName:     strPtr("acme-github-pat-secrets"),
		SecretRefKVPath:   strPtr("user-app-secrets/wc-acme/acme-github-pat-secrets"),
		SecretRefProperty: strPtr("token"),
	}
}

func newCodingDispatchExecutor(anthropic fakeCodingKey, github *organization.OrgCredential) *CodingExecutor {
	orgUUID := uuid.MustParse("d3adbeef-1234-4321-abcd-c0ffee123456")
	return NewCodingExecutor(
		nil,
		fakeRepos{repo: &sourcecontrol.GitRepository{RepoURL: "https://github.com/acme/widgets", RepoSlug: "acme-widgets"}},
		fakeIdentities{},
		newFakeExecRepo(),
		"http://git",
		"http://platform",
		fakeOrgRepo{org: &organization.Organization{Name: "acme", UUID: orgUUID}},
		anthropic,
		fakeGitHubCreds{row: github},
		nil,
	)
}

func codingMilestoneDispatch() delivery.MilestoneDispatch {
	return delivery.MilestoneDispatch{
		OrgID: "acme", ProjectID: "widgets",
		MilestoneNumber: 1, MilestoneTitle: "v1",
		Kind:    delivery.CycleKindCoding,
		RunID:   "run-1",
		CycleID: "11111111-1111-1111-1111-111111111111",
	}
}

func newOCDispatchExecutor(rec *chainRecorder) *CodingExecutor {
	anthropic, github := fullSecretRefs()
	e := newCodingDispatchExecutor(anthropic, github)
	e.WithPublisherCredentials(fakePublisher{name: "acme-publisher-secrets"}, "http://thunder.example/oauth2/token")
	e.WithOCDispatch(NewOCDispatcher(rec.client()).WithImage("ghcr.io/wso2/aep/remote-worker:latest"))
	return e
}

// TestDispatch_OCPathDispatchesThroughOpenChoreo pins the only dispatch path:
// a milestone cycle goes through OpenChoreo.
func TestDispatch_OCPathDispatchesThroughOpenChoreo(t *testing.T) {
	rec := &chainRecorder{}
	e := newOCDispatchExecutor(rec)

	runName, err := e.Dispatch(context.Background(), codingMilestoneDispatch())
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !strings.HasPrefix(runName, "ca-") {
		t.Errorf("run name = %q, want the ca- prefix (the watcher discriminator)", runName)
	}
	if len(rec.calls) == 0 {
		t.Fatal("the OC chain was never walked")
	}
	if rec.create.Name != runName {
		t.Errorf("component name %q != returned run name %q", rec.create.Name, runName)
	}
}

// anthropicSecretEnv returns the Anthropic entry of a dispatched Workload's
// secret env — the one whose ValueFrom names the Anthropic SecretReference —
// so a test can assert which env var name it was mounted under.
func anthropicSecretEnv(t *testing.T, in openchoreo.WorkloadInput, secretRefName string) openchoreo.WorkflowEnvVarRef {
	t.Helper()
	for _, ev := range in.Env {
		if ev.ValueFrom != nil && ev.ValueFrom.SecretKeyRef != nil && ev.ValueFrom.SecretKeyRef.Name == secretRefName {
			return ev
		}
	}
	t.Fatalf("no secret env entry found for secretRef %q in %+v", secretRefName, in.Env)
	return openchoreo.WorkflowEnvVarRef{}
}

// TestDispatch_AnthropicAPIKey_MountsAsAnthropicAPIKeyEnvVar pins ADR-0016's
// rule for the OC path: a Console API key credential rides the Job as
// ANTHROPIC_API_KEY, named by the resolver's EnvVar rather than hardcoded here.
func TestDispatch_AnthropicAPIKey_MountsAsAnthropicAPIKeyEnvVar(t *testing.T) {
	rec := &chainRecorder{}
	anthropic, github := fullSecretRefs()
	anthropic.ref.EnvVar = "ANTHROPIC_API_KEY"
	e := newCodingDispatchExecutor(anthropic, github)
	e.WithPublisherCredentials(fakePublisher{name: "acme-publisher-secrets"}, "http://thunder.example/oauth2/token")
	e.WithOCDispatch(NewOCDispatcher(rec.client()).WithImage("ghcr.io/wso2/aep/remote-worker:latest"))

	if _, err := e.Dispatch(context.Background(), codingMilestoneDispatch()); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	ev := anthropicSecretEnv(t, rec.load, anthropic.ref.Name)
	if ev.Key != "ANTHROPIC_API_KEY" {
		t.Errorf("anthropic env key = %q, want ANTHROPIC_API_KEY", ev.Key)
	}
}

// TestDispatch_AnthropicOAuthToken_MountsAsClaudeCodeOAuthTokenEnvVar pins the
// other half of ADR-0016's rule: a Claude Code OAuth credential rides the Job
// as CLAUDE_CODE_OAUTH_TOKEN, never alongside ANTHROPIC_API_KEY (Claude Code
// ranks the API key above the token, so mounting both would silently ignore
// the org's OAuth choice).
func TestDispatch_AnthropicOAuthToken_MountsAsClaudeCodeOAuthTokenEnvVar(t *testing.T) {
	rec := &chainRecorder{}
	anthropic, github := fullSecretRefs()
	anthropic.ref.EnvVar = "CLAUDE_CODE_OAUTH_TOKEN"
	e := newCodingDispatchExecutor(anthropic, github)
	e.WithPublisherCredentials(fakePublisher{name: "acme-publisher-secrets"}, "http://thunder.example/oauth2/token")
	e.WithOCDispatch(NewOCDispatcher(rec.client()).WithImage("ghcr.io/wso2/aep/remote-worker:latest"))

	if _, err := e.Dispatch(context.Background(), codingMilestoneDispatch()); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	ev := anthropicSecretEnv(t, rec.load, anthropic.ref.Name)
	if ev.Key != "CLAUDE_CODE_OAUTH_TOKEN" {
		t.Errorf("anthropic env key = %q, want CLAUDE_CODE_OAUTH_TOKEN", ev.Key)
	}
	for _, other := range rec.load.Env {
		if other.Key == "ANTHROPIC_API_KEY" {
			t.Errorf("ANTHROPIC_API_KEY must not also be mounted alongside an OAuth token, got %+v", other)
		}
	}
}

// TestDispatch_UnresolvableAnthropicKey_ErrorsNoFallback: a run whose
// Anthropic credential cannot be resolved must not quietly dispatch with no
// key mounted — the OC path is the only path, so a resolver failure aborts
// the dispatch and nothing is created.
func TestDispatch_UnresolvableAnthropicKey_ErrorsNoFallback(t *testing.T) {
	rec := &chainRecorder{}
	_, github := fullSecretRefs()
	anthropic := fakeCodingKey{err: errors.New(
		"coding-agent Anthropic key for org \"acme\" is configured but secret_ref_kv_path is not populated")}
	e := newCodingDispatchExecutor(anthropic, github)
	e.WithPublisherCredentials(fakePublisher{name: "acme-publisher-secrets"}, "http://thunder.example/oauth2/token")
	e.WithOCDispatch(NewOCDispatcher(rec.client()).WithImage("ghcr.io/wso2/aep/remote-worker:latest"))

	_, err := e.Dispatch(context.Background(), codingMilestoneDispatch())
	if err == nil {
		t.Fatal("expected error when the anthropic secret ref cannot be resolved")
	}
	if !strings.Contains(err.Error(), "Anthropic") || !strings.Contains(err.Error(), "secret_ref_kv_path") {
		t.Fatalf("error must carry the resolver's diagnosis, got: %v", err)
	}
	if len(rec.calls) != 0 {
		t.Errorf("nothing may be created before the refs resolve, saw %v", rec.calls)
	}
}

// TestCodingAgentRunNameFor_IsStableAcrossRetries: a Temporal retry after a
// crash must recreate the same Component name so CreateComponent's 409 path
// re-reads instead of minting a second billed Component.
func TestCodingAgentRunNameFor_IsStableAcrossRetries(t *testing.T) {
	id := "11111111-1111-1111-1111-111111111111"
	a := codingAgentRunNameFor("widgets", id)
	b := codingAgentRunNameFor("widgets", id)
	if a != b {
		t.Fatalf("unstable run name: %q then %q", a, b)
	}
	if !strings.HasPrefix(a, "ca-") {
		t.Fatalf("run name = %q, want ca- prefix", a)
	}
}

// TestDispatch_ValidationCycleDispatchesOnOCPath: validation carries task kind
// and deadline through the OpenChoreo Component path.
func TestDispatch_ValidationCycleDispatchesOnOCPath(t *testing.T) {
	rec := &chainRecorder{}
	e := newOCDispatchExecutor(rec)

	req := codingMilestoneDispatch()
	req.Kind = delivery.CycleKindValidation
	req.IssueNumber = 77

	if _, err := e.Dispatch(context.Background(), req); err != nil {
		t.Fatalf("a validation cycle must dispatch on the OC path: %v", err)
	}
	var kind string
	for _, ev := range rec.load.Env {
		if ev.Key == "AEP_TASK_KIND" {
			kind = ev.Value
		}
	}
	if kind != validationTaskKind {
		t.Errorf("AEP_TASK_KIND = %q, want %q", kind, validationTaskKind)
	}
	if rec.create.Parameters["activeDeadlineSeconds"] != int(validationDeadlineSeconds) {
		t.Errorf("validation deadline = %v, want %d", rec.create.Parameters["activeDeadlineSeconds"], validationDeadlineSeconds)
	}
	if rec.create.DisplayName != "Validation cycle — milestone #1 v1" {
		t.Errorf("displayName = %q", rec.create.DisplayName)
	}
}

// TestDispatch_ValidationCycleNamesItsIssueToThePod: the runner posts the
// validation issue's status line itself, and nothing else in the pod's
// environment answers "which issue" — AEP_TASK_ID is the cycle's uuid, and the
// number reaches the agent only as prose inside AEP_PROMPT.
func TestDispatch_ValidationCycleNamesItsIssueToThePod(t *testing.T) {
	rec := &chainRecorder{}
	e := newOCDispatchExecutor(rec)

	req := codingMilestoneDispatch()
	req.Kind = delivery.CycleKindValidation
	req.IssueNumber = 77

	if _, err := e.Dispatch(context.Background(), req); err != nil {
		t.Fatalf("a validation cycle must dispatch on the OC path: %v", err)
	}
	if got := podEnv(rec, envValidationIssue); got != "77" {
		t.Errorf("%s = %q, want %q", envValidationIssue, got, "77")
	}
}

// TestDispatch_CodingCycleNamesNoIssue: a coding cycle discovers a whole
// working set rather than being anchored to one issue, so it has no issue to
// name. The var is ABSENT rather than "0" — the runner reads presence, and a
// stamped zero would be a number it has to know is not one.
func TestDispatch_CodingCycleNamesNoIssue(t *testing.T) {
	rec := &chainRecorder{}
	e := newOCDispatchExecutor(rec)

	if _, err := e.Dispatch(context.Background(), codingMilestoneDispatch()); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	for _, ev := range rec.load.Env {
		if ev.Key == envValidationIssue {
			t.Fatalf("a coding cycle stamped %s = %q", envValidationIssue, ev.Value)
		}
	}
}

// podEnv reads one env var off the dispatched Workload.
func podEnv(rec *chainRecorder, key string) string {
	for _, ev := range rec.load.Env {
		if ev.Key == key {
			return ev.Value
		}
	}
	return ""
}

// TestDispatch_OCPathStillRequiresTheOrgsSecretRefs: refs-only means the run
// cannot start without them, and the message must name which one is missing.
// (The Anthropic side's equivalent — a resolver that cannot answer — is
// TestDispatch_UnresolvableAnthropicKey_ErrorsNoFallback below.)
func TestDispatch_OCPathStillRequiresTheOrgsSecretRefs(t *testing.T) {
	rec := &chainRecorder{}
	anthropic, github := fullSecretRefs()
	github.SecretRefName = nil
	e := newCodingDispatchExecutor(anthropic, github)
	e.WithOCDispatch(NewOCDispatcher(rec.client()).WithImage("runner:1"))

	_, err := e.Dispatch(context.Background(), codingMilestoneDispatch())
	if err == nil {
		t.Fatal("expected an error when the github secret ref is missing")
	}
	if !strings.Contains(err.Error(), "github") {
		t.Errorf("error must name the missing credential, got %v", err)
	}
	if len(rec.calls) != 0 {
		t.Errorf("nothing may be created before the refs resolve, saw %v", rec.calls)
	}
}

type fakePublisher struct {
	name string
	err  error
}

func (f fakePublisher) SecretRefName(context.Context, string) (string, error) {
	return f.name, f.err
}

func secretEnvByKey(t *testing.T, in openchoreo.WorkloadInput, key string) openchoreo.WorkflowEnvVarRef {
	t.Helper()
	for _, ev := range in.Env {
		if ev.Key == key {
			return ev
		}
	}
	t.Fatalf("no env/secretEnv entry %q in %+v", key, in.Env)
	return openchoreo.WorkflowEnvVarRef{}
}

func TestDispatch_MountsPublisherSecretEnvAndTokenURL(t *testing.T) {
	rec := &chainRecorder{}
	anthropic, github := fullSecretRefs()
	e := newCodingDispatchExecutor(anthropic, github)
	e.platformURL = "https://gateway.example/app-factory-api"
	e.WithPublisherCredentials(fakePublisher{name: "acme-publisher-secrets"}, "https://platform-idp.example/oauth2/token")
	e.WithOCDispatch(NewOCDispatcher(rec.client()).WithImage("ghcr.io/wso2/aep/remote-worker:latest"))

	if _, err := e.Dispatch(context.Background(), codingMilestoneDispatch()); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	id := secretEnvByKey(t, rec.load, "PUBLISHER_CLIENT_ID")
	sec := secretEnvByKey(t, rec.load, "PUBLISHER_CLIENT_SECRET")
	if id.ValueFrom == nil || id.ValueFrom.SecretKeyRef == nil {
		t.Fatalf("PUBLISHER_CLIENT_ID must be secretKeyRef, got %+v", id)
	}
	if id.ValueFrom.SecretKeyRef.Name != "acme-publisher-secrets" {
		t.Errorf("client_id secret name = %q", id.ValueFrom.SecretKeyRef.Name)
	}
	if id.ValueFrom.SecretKeyRef.Key != organization.PublisherSecretFieldClientID {
		t.Errorf("client_id field = %q, want %q", id.ValueFrom.SecretKeyRef.Key, organization.PublisherSecretFieldClientID)
	}
	if sec.ValueFrom.SecretKeyRef.Name != "acme-publisher-secrets" {
		t.Errorf("client_secret must share the SecretReference, got %q", sec.ValueFrom.SecretKeyRef.Name)
	}
	if sec.ValueFrom.SecretKeyRef.Key != organization.PublisherSecretFieldClientSecret {
		t.Errorf("client_secret field = %q, want %q", sec.ValueFrom.SecretKeyRef.Key, organization.PublisherSecretFieldClientSecret)
	}
	if sec.ValueFrom.SecretKeyRef.Key == "publisher" {
		t.Fatal("must not use triplet Property 'publisher' as SecretKey")
	}
	tok := secretEnvByKey(t, rec.load, "PUBLISHER_TOKEN_URL")
	if tok.Value != "https://platform-idp.example/oauth2/token" {
		t.Errorf("PUBLISHER_TOKEN_URL = %q (must be plain env, not a secret)", tok.Value)
	}
	if tok.ValueFrom != nil {
		t.Errorf("PUBLISHER_TOKEN_URL must not be secretKeyRef, got %+v", tok.ValueFrom)
	}
}

func TestDispatch_MissingPublisher_ErrorsNoCreate(t *testing.T) {
	rec := &chainRecorder{}
	anthropic, github := fullSecretRefs()
	e := newCodingDispatchExecutor(anthropic, github)
	e.platformURL = "https://gateway.example/app-factory-api"
	e.WithOCDispatch(NewOCDispatcher(rec.client()).WithImage("ghcr.io/wso2/aep/remote-worker:latest"))

	_, err := e.Dispatch(context.Background(), codingMilestoneDispatch())
	if err == nil {
		t.Fatal("expected error when publisher credentials are not wired")
	}
	if !strings.Contains(err.Error(), "publisher") {
		t.Fatalf("error must name publisher, got: %v", err)
	}
	if len(rec.calls) != 0 {
		t.Errorf("nothing may be created before publisher creds resolve, saw %v", rec.calls)
	}
}

func TestDispatch_EmptyTokenURL_ErrorsNoCreate(t *testing.T) {
	rec := &chainRecorder{}
	anthropic, github := fullSecretRefs()
	e := newCodingDispatchExecutor(anthropic, github)
	e.platformURL = "https://gateway.example/app-factory-api"
	e.WithPublisherCredentials(fakePublisher{name: "acme-publisher-secrets"}, "")
	e.WithOCDispatch(NewOCDispatcher(rec.client()).WithImage("ghcr.io/wso2/aep/remote-worker:latest"))

	_, err := e.Dispatch(context.Background(), codingMilestoneDispatch())
	if err == nil {
		t.Fatal("expected error when publisher token URL cannot be derived")
	}
	if !strings.Contains(err.Error(), "JWKS") && !strings.Contains(strings.ToLower(err.Error()), "token") {
		t.Fatalf("error must name JWKS/token URL, got: %v", err)
	}
	if len(rec.calls) != 0 {
		t.Errorf("nothing may be created, saw %v", rec.calls)
	}
}

func TestDispatch_ProfileLoadError_ErrorsNoCreate(t *testing.T) {
	rec := &chainRecorder{}
	anthropic, github := fullSecretRefs()
	e := newCodingDispatchExecutor(anthropic, github)
	e.platformURL = "https://gateway.example/app-factory-api"
	e.WithPublisherCredentials(fakePublisher{err: errors.New("db down")}, "https://idp.example/oauth2/token")
	e.WithOCDispatch(NewOCDispatcher(rec.client()).WithImage("ghcr.io/wso2/aep/remote-worker:latest"))

	_, err := e.Dispatch(context.Background(), codingMilestoneDispatch())
	if err == nil {
		t.Fatal("expected error when profile load fails")
	}
	if !strings.Contains(err.Error(), "db down") {
		t.Fatalf("error must wrap the resolver diagnosis, got: %v", err)
	}
	if len(rec.calls) != 0 {
		t.Errorf("nothing may be created, saw %v", rec.calls)
	}
}

func TestDispatch_EmptySecretRef_ErrorsNoCreate(t *testing.T) {
	rec := &chainRecorder{}
	anthropic, github := fullSecretRefs()
	e := newCodingDispatchExecutor(anthropic, github)
	e.platformURL = "https://gateway.example/app-factory-api"
	e.WithPublisherCredentials(fakePublisher{name: "  "}, "https://idp.example/oauth2/token")
	e.WithOCDispatch(NewOCDispatcher(rec.client()).WithImage("ghcr.io/wso2/aep/remote-worker:latest"))

	_, err := e.Dispatch(context.Background(), codingMilestoneDispatch())
	if err == nil {
		t.Fatal("expected error when SecretReference name is empty")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "secret") {
		t.Fatalf("error must name SecretReference, got: %v", err)
	}
	if strings.Contains(err.Error(), "RegenerateClientSecret") {
		t.Fatalf("dispatch must not tell operators to rotate, got: %v", err)
	}
	if !strings.Contains(err.Error(), "SecretReference is not stamped") {
		t.Fatalf("empty SecretReference must name the missing stamp, got: %v", err)
	}
	if len(rec.calls) != 0 {
		t.Errorf("nothing may be created, saw %v", rec.calls)
	}
}

func TestDispatch_HTTPPlatformURL_MountsPublisher(t *testing.T) {
	rec := &chainRecorder{}
	anthropic, github := fullSecretRefs()
	e := newCodingDispatchExecutor(anthropic, github)
	e.platformURL = "http://host.k3d.internal:9090"
	e.WithPublisherCredentials(fakePublisher{name: "acme-publisher-secrets"}, "http://thunder-service.thunder.svc.cluster.local:8090/oauth2/token")
	e.WithOCDispatch(NewOCDispatcher(rec.client()).WithImage("ghcr.io/wso2/aep/remote-worker:latest"))

	if _, err := e.Dispatch(context.Background(), codingMilestoneDispatch()); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	id := secretEnvByKey(t, rec.load, "PUBLISHER_CLIENT_ID")
	if id.ValueFrom == nil || id.ValueFrom.SecretKeyRef == nil || id.ValueFrom.SecretKeyRef.Name != "acme-publisher-secrets" {
		t.Fatalf("http dispatch must mount PUBLISHER_CLIENT_ID from the SecretReference, got %+v", id)
	}
	tok := secretEnvByKey(t, rec.load, "PUBLISHER_TOKEN_URL")
	if tok.Value != "http://thunder-service.thunder.svc.cluster.local:8090/oauth2/token" {
		t.Errorf("PUBLISHER_TOKEN_URL = %q", tok.Value)
	}
	for _, ev := range rec.load.Env {
		if ev.Key == "AEP_BEARER" || ev.Key == "AEP_MCP_TOKEN" {
			t.Errorf("coding-agent Job must not inject %s", ev.Key)
		}
	}
}

// ---------------------------------------------------------------------------
// The evaluation credential (build-time agent evaluation)
// ---------------------------------------------------------------------------

// hasEnvKey reports whether the dispatched Workload carries an env entry under
// key, without asserting anything about its value — secret entries are refs, and
// a test must never handle key material.
func hasEnvKey(in openchoreo.WorkloadInput, key string) bool {
	for _, ev := range in.Env {
		if ev.Key == key {
			return true
		}
	}
	return false
}

// TestDispatch_MountsTheOrgDefaultKeyForEvaluation: a build that evaluates the
// agent it just generated needs a model credential twice over — for the agent
// under test and for the judge grading it. The org's DEFAULT key is the one that
// is correct on both counts: it is always an API key (a coding OAuth token
// authenticates no API call the judge makes), and the spend belongs to the org
// whose agent is being graded.
func TestDispatch_MountsTheOrgDefaultKeyForEvaluation(t *testing.T) {
	rec := &chainRecorder{}
	anthropic, github := fullSecretRefs()
	// A DIFFERENT SecretReference from the coding credential, so the assertion
	// below can only pass if the default key — not the coding one — was mounted.
	anthropic.defaultRef = organization.SecretRefTriplet{
		Name:     "acme-anthropic-default-secrets",
		KVPath:   "user-app-secrets/wc-acme/acme-anthropic-default-secrets",
		Property: "api-key",
		EnvVar:   "ANTHROPIC_API_KEY",
	}
	e := newCodingDispatchExecutor(anthropic, github)
	e.WithPublisherCredentials(fakePublisher{name: "acme-publisher-secrets"}, "http://thunder.example/oauth2/token")
	e.WithOCDispatch(NewOCDispatcher(rec.client()).WithImage("ghcr.io/wso2/aep/remote-worker:latest"))

	if _, err := e.Dispatch(context.Background(), codingMilestoneDispatch()); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	ev := secretEnvByKey(t, rec.load, envEvalAnthropicAPIKey)
	if ev.ValueFrom == nil || ev.ValueFrom.SecretKeyRef == nil {
		t.Fatalf("%s must be a SecretReference, not an inline value: %+v", envEvalAnthropicAPIKey, ev)
	}
	if ev.ValueFrom.SecretKeyRef.Name != anthropic.defaultRef.Name {
		t.Errorf("%s resolves from %q, want the org's DEFAULT key %q",
			envEvalAnthropicAPIKey, ev.ValueFrom.SecretKeyRef.Name, anthropic.defaultRef.Name)
	}
	if ev.ValueFrom.SecretKeyRef.Key != anthropic.defaultRef.Property {
		t.Errorf("%s property = %q, want %q", envEvalAnthropicAPIKey,
			ev.ValueFrom.SecretKeyRef.Key, anthropic.defaultRef.Property)
	}
}

// TestDispatch_EvaluationKeyRidesItsOwnVariable: the evaluation credential must
// never be mounted as ANTHROPIC_API_KEY when the org bills its coding agent to a
// Claude Code OAuth token. Claude Code ranks ANTHROPIC_API_KEY above
// CLAUDE_CODE_OAUTH_TOKEN (ADR-0016), so doing so would silently move the whole
// coding session onto the credential the org moved away from.
func TestDispatch_EvaluationKeyRidesItsOwnVariable(t *testing.T) {
	rec := &chainRecorder{}
	anthropic, github := fullSecretRefs()
	anthropic.ref.EnvVar = "CLAUDE_CODE_OAUTH_TOKEN"
	e := newCodingDispatchExecutor(anthropic, github)
	e.WithPublisherCredentials(fakePublisher{name: "acme-publisher-secrets"}, "http://thunder.example/oauth2/token")
	e.WithOCDispatch(NewOCDispatcher(rec.client()).WithImage("ghcr.io/wso2/aep/remote-worker:latest"))

	if _, err := e.Dispatch(context.Background(), codingMilestoneDispatch()); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !hasEnvKey(rec.load, envEvalAnthropicAPIKey) {
		t.Fatalf("an org billing its coding agent to an OAuth token still needs %s for evaluation", envEvalAnthropicAPIKey)
	}
	if hasEnvKey(rec.load, "ANTHROPIC_API_KEY") {
		t.Error("the evaluation key must not be mounted as ANTHROPIC_API_KEY beside an OAuth token")
	}
}

// TestDispatch_NoDefaultKeyConnected_StillDispatches: evaluation reports, it
// never fails a build. An org with no connected default key dispatches WITHOUT
// the variable — absent, not present-and-empty, so the harness sees "no key"
// rather than "a key that does not authenticate".
func TestDispatch_NoDefaultKeyConnected_StillDispatches(t *testing.T) {
	rec := &chainRecorder{}
	anthropic, github := fullSecretRefs()
	anthropic.defaultErr = &organization.NotFoundError{What: "org_anthropic_credentials.acme.default"}
	e := newCodingDispatchExecutor(anthropic, github)
	e.WithPublisherCredentials(fakePublisher{name: "acme-publisher-secrets"}, "http://thunder.example/oauth2/token")
	e.WithOCDispatch(NewOCDispatcher(rec.client()).WithImage("ghcr.io/wso2/aep/remote-worker:latest"))

	if _, err := e.Dispatch(context.Background(), codingMilestoneDispatch()); err != nil {
		t.Fatalf("a build must not fail because evaluation cannot run: %v", err)
	}
	if hasEnvKey(rec.load, envEvalAnthropicAPIKey) {
		t.Errorf("an absent key must be absent, not mounted: %+v", rec.load.Env)
	}
}

// TestDispatch_IncompleteDefaultKeyRef_IsNotMounted: a half-mirrored row can
// resolve to a triplet ESO cannot follow. Mounting it would put the variable on
// the pod pointing at nothing, and the harness would then report the agent as
// misbehaving rather than as unconfigured — a worse outcome than no evaluation.
func TestDispatch_IncompleteDefaultKeyRef_IsNotMounted(t *testing.T) {
	rec := &chainRecorder{}
	anthropic, github := fullSecretRefs()
	anthropic.defaultRef = organization.SecretRefTriplet{Name: "acme-anthropic-secrets", EnvVar: "ANTHROPIC_API_KEY"}
	e := newCodingDispatchExecutor(anthropic, github)
	e.WithPublisherCredentials(fakePublisher{name: "acme-publisher-secrets"}, "http://thunder.example/oauth2/token")
	e.WithOCDispatch(NewOCDispatcher(rec.client()).WithImage("ghcr.io/wso2/aep/remote-worker:latest"))

	if _, err := e.Dispatch(context.Background(), codingMilestoneDispatch()); err != nil {
		t.Fatalf("a build must not fail because evaluation cannot run: %v", err)
	}
	if hasEnvKey(rec.load, envEvalAnthropicAPIKey) {
		t.Errorf("a triplet with no property must not be mounted: %+v", rec.load.Env)
	}
}

// TestDispatch_DeclaresThePlatformOwnsTheEvaluationKey: the pod's
// ANTHROPIC_API_KEY, when present, is the CODING credential — an org may bill
// coding to a key it did not choose for anything else. So the harness must not
// treat it as an evaluation credential just because no evaluation key was
// mounted. Every dispatch therefore says so out loud, and says it even when
// there IS no evaluation key to mount: that is the case the declaration exists
// for, and a marker that appeared only alongside the key would be useless.
func TestDispatch_DeclaresThePlatformOwnsTheEvaluationKey(t *testing.T) {
	for _, tc := range []struct {
		name       string
		defaultErr error
	}{
		{name: "with a default key connected"},
		{name: "with none connected", defaultErr: &organization.NotFoundError{What: "org_anthropic_credentials.acme.default"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := &chainRecorder{}
			anthropic, github := fullSecretRefs()
			anthropic.defaultErr = tc.defaultErr
			e := newCodingDispatchExecutor(anthropic, github)
			e.WithPublisherCredentials(fakePublisher{name: "acme-publisher-secrets"}, "http://thunder.example/oauth2/token")
			e.WithOCDispatch(NewOCDispatcher(rec.client()).WithImage("ghcr.io/wso2/aep/remote-worker:latest"))

			if _, err := e.Dispatch(context.Background(), codingMilestoneDispatch()); err != nil {
				t.Fatalf("Dispatch: %v", err)
			}
			ev := secretEnvByKey(t, rec.load, envEvalKeyManaged)
			if ev.Value != "1" {
				t.Errorf("%s = %q, want \"1\"", envEvalKeyManaged, ev.Value)
			}
			if ev.ValueFrom != nil {
				t.Errorf("%s is a plain declaration, not a credential: %+v", envEvalKeyManaged, ev)
			}
		})
	}
}

// TestDispatch_CodingCycleCarriesTheThreeHourDeadline: a coding cycle names its
// own activeDeadlineSeconds rather than falling through to the ComponentType's
// 1h default — the cycle's browser verification wave does not fit in an hour,
// and the pod was being reaped mid-run.
func TestDispatch_CodingCycleCarriesTheThreeHourDeadline(t *testing.T) {
	rec := &chainRecorder{}
	e := newOCDispatchExecutor(rec)

	if _, err := e.Dispatch(context.Background(), codingMilestoneDispatch()); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if got := rec.create.Parameters["activeDeadlineSeconds"]; got != int(codingDeadlineSeconds) {
		t.Errorf("coding deadline = %v, want %d", got, codingDeadlineSeconds)
	}
	if codingDeadlineSeconds != 10800 {
		t.Errorf("codingDeadlineSeconds = %d, want 10800 (3h)", codingDeadlineSeconds)
	}
}

// TestDeadlinesFitTheComponentTypeSchema: the dispatched deadline and the schema
// that validates it are two halves of one change. The ComponentType's maximum is
// what OpenChoreo rejects against, so a deadline raised on this side alone would
// not time a run out — it would fail the dispatch outright.
func TestDeadlinesFitTheComponentTypeSchema(t *testing.T) {
	spec, _ := openchoreo.CodingAgentComponentType()["spec"].(map[string]any)
	params, _ := spec["parameters"].(map[string]any)
	schema, _ := params["openAPIV3Schema"].(map[string]any)
	props, _ := schema["properties"].(map[string]any)
	deadline, _ := props["activeDeadlineSeconds"].(map[string]any)
	ceiling, ok := deadline["maximum"].(int)
	if !ok {
		t.Fatalf("activeDeadlineSeconds.maximum missing or not an int: %#v", deadline["maximum"])
	}
	for name, want := range map[string]int64{
		"coding":     codingDeadlineSeconds,
		"validation": validationDeadlineSeconds,
	} {
		if want > int64(ceiling) {
			t.Errorf("%s deadline %d exceeds the ComponentType schema maximum %d — the dispatch would be rejected", name, want, ceiling)
		}
	}
}

// --- the org's coding-agent setting, and the run's own deadline -------------

// fakeCodingAgentSettings stands in for organization.AgentSettingsService.
type fakeCodingAgentSettings struct {
	proj orgconfig.AgentsProjection
	err  error
}

func (f fakeCodingAgentSettings) Effective(context.Context, string) (orgconfig.AgentsProjection, error) {
	return f.proj, f.err
}

// An org that never opened the setting must get exactly the run it had before
// the setting existed — which is what an unwired resolver stands in for here.
func TestDispatch_NoCodingAgentSettingStampsThePlatformDefaults(t *testing.T) {
	rec := &chainRecorder{}
	e := newOCDispatchExecutor(rec)

	if _, err := e.Dispatch(context.Background(), codingMilestoneDispatch()); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if got := secretEnvByKey(t, rec.load, "AEP_AGENT_RUNTIME").Value; got != string(orgconfig.DefaultAgentRuntime) {
		t.Errorf("AEP_AGENT_RUNTIME = %q, want %q", got, orgconfig.DefaultAgentRuntime)
	}
	if got := secretEnvByKey(t, rec.load, "AEP_AGENT_MODEL").Value; got != orgconfig.DefaultAgentModel {
		t.Errorf("AEP_AGENT_MODEL = %q, want %q", got, orgconfig.DefaultAgentModel)
	}
	// A Claude Code run on the Claude Code image, and the cluster can see so.
	if rec.load.Image != "ghcr.io/wso2/aep/remote-worker:latest" {
		t.Errorf("image = %q, want the Claude Code runner image", rec.load.Image)
	}
	if got := rec.create.Labels["aep.wso2.com/runtime"]; got != "claude-code" {
		t.Errorf("component runtime label = %q, want claude-code", got)
	}
	if got := rec.create.Parameters["runtime"]; got != "claude-code" {
		t.Errorf("runtime parameter = %v, want claude-code", got)
	}
	// Two plain values, never a secret — a secretKeyRef here would need a
	// SecretReference nobody creates and the dispatch would fail to render.
	if secretEnvByKey(t, rec.load, "AEP_AGENT_MODEL").ValueFrom != nil {
		t.Error("AEP_AGENT_MODEL was mounted as a secret ref")
	}
}

// The setting is COPIED onto the run, which is what makes "applies from the next
// cycle" true: a run already in flight keeps the model it was launched with, so
// its usage lines and the tokens they were billed for name the same model.
func TestDispatch_TheOrgsCodingAgentSettingIsCopiedOntoTheRun(t *testing.T) {
	rec := &chainRecorder{}
	e := newOCDispatchExecutor(rec)
	e.WithCodingAgentSettings(fakeCodingAgentSettings{
		proj: orgconfig.AgentsProjection{Runtime: "claude-code", Model: "claude-haiku-4-5"},
	})

	if _, err := e.Dispatch(context.Background(), codingMilestoneDispatch()); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if got := secretEnvByKey(t, rec.load, "AEP_AGENT_MODEL").Value; got != "claude-haiku-4-5" {
		t.Errorf("AEP_AGENT_MODEL = %q, want the org's chosen model", got)
	}
	if got := secretEnvByKey(t, rec.load, "AEP_AGENT_RUNTIME").Value; got != "claude-code" {
		t.Errorf("AEP_AGENT_RUNTIME = %q", got)
	}
}

// --- OpenCode: its own image, its own label, and an API key or nothing -------

const openCodeRunnerImage = "aep-runner-opencode:dev"

func newOpenCodeDispatchExecutor(rec *chainRecorder, anthropic fakeCodingKey, github *organization.OrgCredential, opencodeImage string) *CodingExecutor {
	e := newCodingDispatchExecutor(anthropic, github)
	e.WithPublisherCredentials(fakePublisher{name: "acme-publisher-secrets"}, "http://thunder.example/oauth2/token")
	e.WithOCDispatch(NewOCDispatcher(rec.client()).
		WithImage("ghcr.io/wso2/aep/remote-worker:latest").
		WithOpenCodeImage(opencodeImage))
	e.WithCodingAgentSettings(fakeCodingAgentSettings{proj: orgconfig.AgentsProjection{
		Runtime: "opencode", Model: "claude-sonnet-5",
	}})
	return e
}

// An OpenCode org's cycle runs on the OpenCode image and says so everywhere the
// cluster looks: the Component and Workload label, and the ComponentType
// parameter that renders the label onto the Job and its pod.
func TestDispatch_AnOpenCodeRunGetsItsImageAndLabel(t *testing.T) {
	rec := &chainRecorder{}
	anthropic, github := fullSecretRefs()
	e := newOpenCodeDispatchExecutor(rec, anthropic, github, openCodeRunnerImage)

	if _, err := e.Dispatch(context.Background(), codingMilestoneDispatch()); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if rec.load.Image != openCodeRunnerImage {
		t.Errorf("image = %q, want the OpenCode runner image", rec.load.Image)
	}
	if got := rec.create.Labels["aep.wso2.com/runtime"]; got != "opencode" {
		t.Errorf("component runtime label = %q, want opencode", got)
	}
	if got := rec.load.Labels["aep.wso2.com/runtime"]; got != "opencode" {
		t.Errorf("workload runtime label = %q, want opencode", got)
	}
	if got := rec.create.Parameters["runtime"]; got != "opencode" {
		t.Errorf("runtime parameter = %v, want opencode", got)
	}
	if got := secretEnvByKey(t, rec.load, "AEP_AGENT_RUNTIME").Value; got != "opencode" {
		t.Errorf("AEP_AGENT_RUNTIME = %q, want opencode", got)
	}
	if ev := anthropicSecretEnv(t, rec.load, anthropic.ref.Name); ev.Key != "ANTHROPIC_API_KEY" {
		t.Errorf("anthropic env key = %q, want ANTHROPIC_API_KEY", ev.Key)
	}
}

// The credential is chosen FOR the runtime: dispatch asks the resolver with the
// runtime this run will use, which is what keeps a subscription (Claude Code
// only) off an OpenCode run.
func TestDispatch_AsksForTheCredentialOfTheRunsRuntime(t *testing.T) {
	for _, runtime := range []orgconfig.AgentRuntime{orgconfig.AgentRuntimeClaudeCode, orgconfig.AgentRuntimeOpenCode} {
		rec := &chainRecorder{}
		anthropic, github := fullSecretRefs()
		var asked orgconfig.AgentRuntime
		anthropic.asked = &asked
		e := newOpenCodeDispatchExecutor(rec, anthropic, github, openCodeRunnerImage)
		e.WithCodingAgentSettings(fakeCodingAgentSettings{proj: orgconfig.AgentsProjection{
			Runtime: runtime, Model: "claude-sonnet-5",
		}})

		if _, err := e.Dispatch(context.Background(), codingMilestoneDispatch()); err != nil {
			t.Fatalf("%s dispatch: %v", runtime, err)
		}
		if asked != runtime {
			t.Errorf("resolver asked for runtime %q, want %q", asked, runtime)
		}
	}
}

// A platform with no OpenCode image cannot run an OpenCode cycle, and must not
// run it on the Claude Code image (no OpenCode binary in it). The failure names
// the setting that is missing.
func TestDispatch_AnOpenCodeRunWithNoOpenCodeImageFailsNamingTheSetting(t *testing.T) {
	rec := &chainRecorder{}
	anthropic, github := fullSecretRefs()
	e := newOpenCodeDispatchExecutor(rec, anthropic, github, "")

	_, err := e.Dispatch(context.Background(), codingMilestoneDispatch())
	if err == nil || !strings.Contains(err.Error(), "AGENT_RUNNER_IMAGE_OPENCODE") {
		t.Fatalf("dispatch err = %v, want it to name AGENT_RUNNER_IMAGE_OPENCODE", err)
	}
	if len(rec.calls) != 0 {
		t.Errorf("the OC chain was walked anyway: %v", rec.calls)
	}
}

// A read failure is not "no setting". Launching on the defaults here would bill
// an org for a model it deliberately moved off, and never say so.
func TestDispatch_AnUnreadableCodingAgentSettingFailsTheDispatch(t *testing.T) {
	rec := &chainRecorder{}
	e := newOCDispatchExecutor(rec)
	e.WithCodingAgentSettings(fakeCodingAgentSettings{err: errors.New("connection refused")})

	if _, err := e.Dispatch(context.Background(), codingMilestoneDispatch()); err == nil {
		t.Fatal("dispatch succeeded with an unreadable coding-agent setting")
	}
	if len(rec.calls) != 0 {
		t.Errorf("the OC chain was walked anyway: %v", rec.calls)
	}
}

// The Job's activeDeadlineSeconds kills the pod mid-sentence: no result line, no
// watchdog snapshot, and for a run with background subagents no way to tell
// "still working" from "wedged". The runner has a guard that ends the run
// cleanly BEFORE that, and it was inert because nothing passed it the number.
// One number, two consumers — the pod's own budget and the cluster's backstop —
// so they cannot disagree.
func TestDispatch_TheRunLearnsItsOwnDeadline(t *testing.T) {
	for _, tc := range []struct {
		name string
		disp delivery.MilestoneDispatch
		want int64
	}{
		{"coding", codingMilestoneDispatch(), codingDeadlineSeconds},
		{"validation", func() delivery.MilestoneDispatch {
			d := codingMilestoneDispatch()
			d.Kind = delivery.CycleKindValidation
			d.IssueNumber = 7
			return d
		}(), validationDeadlineSeconds},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := &chainRecorder{}
			e := newOCDispatchExecutor(rec)
			if _, err := e.Dispatch(context.Background(), tc.disp); err != nil {
				t.Fatalf("dispatch: %v", err)
			}
			want := strconv.FormatInt(tc.want, 10)
			if got := secretEnvByKey(t, rec.load, "AEP_RUN_DEADLINE_SECONDS").Value; got != want {
				t.Errorf("AEP_RUN_DEADLINE_SECONDS = %q, want %q", got, want)
			}
			// The same number the cluster enforces. A runner budget larger than
			// the Job's would fire after the kill it exists to beat.
			if got := rec.create.Parameters["activeDeadlineSeconds"]; got != int(tc.want) {
				t.Errorf("activeDeadlineSeconds = %v, want %d", got, tc.want)
			}
		})
	}
}
