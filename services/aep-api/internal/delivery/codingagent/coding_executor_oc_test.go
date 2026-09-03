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
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/organization"
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
// Anthropic credential does this org's coding run bill". WHICH key that is —
// the coding override or the default — is decided and tested in the
// organization package (TestResolveCodingSecretRef_*); dispatch's job is only
// to mount whatever it is handed, and to abort when nothing can be handed to it.
type fakeCodingKey struct {
	ref organization.SecretRefTriplet
	err error
}

func (f fakeCodingKey) ResolveCodingSecretRef(context.Context, string) (organization.SecretRefTriplet, error) {
	return f.ref, f.err
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
	return fakeCodingKey{ref: organization.SecretRefTriplet{
			Name:     "acme-anthropic-secrets",
			KVPath:   "user-app-secrets/wc-acme/acme-anthropic-secrets",
			Property: "api-key",
			EnvVar:   "ANTHROPIC_API_KEY",
		}}, &organization.OrgCredential{
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
