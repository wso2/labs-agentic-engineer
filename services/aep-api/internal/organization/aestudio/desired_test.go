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

package aestudio

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/organization"
)

func TestDesired_SecretsFromReferencesAndRev(t *testing.T) {
	f := newFixture(t).withRefs(map[organization.OrgSecret]string{
		organization.OrgSecretGitHubPAT: "default-github-pat-aaaa0001", organization.OrgSecretGitHubWebhookSecret: "default-github-webhook-secret-aaaa0002",
		organization.OrgSecretPublisherClient: "default-ae-publisher-client-aaaa0003", organization.OrgSecretStudioClient: "default-ae-studio-client-aaaa0004",
	})
	d, err := f.svc.desired(ctx, "default")
	if err != nil {
		t.Fatal(err)
	}
	tools := d.Params.Secrets.StudioTools
	envs := []string{}
	for _, e := range tools.Data {
		envs = append(envs, e.Env)
	}
	if strings.Join(envs, ",") != "GITHUB_PAT,GITHUB_WEBHOOK_SECRET,AE_PUBLISHER_CLIENT_ID,AE_PUBLISHER_CLIENT_SECRET,AE_STUDIO_CLIENT_ID,AE_STUDIO_CLIENT_SECRET" {
		t.Fatalf("envs %v", envs)
	}
	if tools.Data[0].Key != "user-app-secrets/ns/default-github-pat-aaaa0001" || tools.Data[0].Property != "token" {
		t.Fatalf("key/property must come from the SecretReference spec.data: %+v", tools.Data[0])
	}
	if len(d.Params.Secrets.DesignAgent.Data) != 0 || d.Params.Secrets.DesignAgent.Rev != "" {
		t.Fatal("O-5: no Default key, no agent secret")
	}
	before := tools.Rev
	f.withRef(organization.OrgSecretGitHubPAT, "default-github-pat-bbbb0001")
	d2, _ := f.svc.desired(ctx, "default")
	if d2.Params.Secrets.StudioTools.Rev == before {
		t.Fatal("a new reference name must change the rev")
	}
	d3, _ := f.svc.desired(ctx, "default")
	if d3.Params.Secrets.StudioTools.Rev != d2.Params.Secrets.StudioTools.Rev {
		t.Fatal("the same reference names must keep the rev (a no-op save rolls nothing)")
	}
}

// O-5 / R6: the agent's ExternalSecret entry exists exactly while the
// Default key's row does.
func TestDesired_AgentKeyOnlyWhileDefaultKeyIsSet(t *testing.T) {
	f := newFixture(t).withAllRefs().withRef(organization.OrgSecretDefaultKey, "default-default-key-cccc0005")
	d, err := f.svc.desired(ctx, "default")
	if err != nil {
		t.Fatal(err)
	}
	agent := d.Params.Secrets.DesignAgent
	if len(agent.Data) != 1 || agent.Data[0].Env != "ANTHROPIC_API_KEY" || agent.Data[0].Property != "api-key" ||
		agent.Data[0].Key != "user-app-secrets/ns/default-default-key-cccc0005" || agent.Rev == "" {
		t.Fatalf("agent secret %+v", agent)
	}
	f.withoutRef(organization.OrgSecretDefaultKey) // a disconnect removes the row
	d2, _ := f.svc.desired(ctx, "default")
	if len(d2.Params.Secrets.DesignAgent.Data) != 0 || d2.Params.Secrets.DesignAgent.Rev != "" {
		t.Fatalf("a removed Default key must drop the entry: %+v", d2.Params.Secrets.DesignAgent)
	}
	raw, _ := json.Marshal(d2.Params)
	if !strings.Contains(string(raw), `"designAgent":{"rev":"","data":[]}`) {
		t.Fatalf("the RT requires data: %s", raw)
	}
}

func TestDesired_ModelConnectionAndGitHubOwner(t *testing.T) {
	f := newFixture(t).withAllRefs().withConnection(anthropicConn("claude-x")).withGitHubLogin("acme-gh")
	d, _ := f.svc.desired(ctx, "default")
	if d.Params.GitHubOwner != "acme-gh" || strings.Contains(d.Params.ModelConnection, "apiKey") || !strings.Contains(d.Params.ModelConnection, `"capabilities"`) {
		t.Fatalf("owner %q connection %s", d.Params.GitHubOwner, d.Params.ModelConnection)
	}
	var conn map[string]any
	if err := json.Unmarshal([]byte(d.Params.ModelConnection), &conn); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"format", "baseURL", "authScheme", "model", "capabilities"} {
		if _, ok := conn[k]; !ok {
			t.Errorf("AE_MODEL_CONNECTION lacks %s: %s", k, d.Params.ModelConnection)
		}
	}
	if conn["model"] != "claude-x" {
		t.Errorf("model %v", conn["model"])
	}
	f.svc.Trigger(userCtx(), "default")
	f.waitConverged(t)
	f.withConnection(anthropicConn("claude-y")) // a connection edit
	if st, _ := f.svc.Status(userCtx(), "default"); st.State != StateProvisioning {
		t.Fatal("a connection edit must drift the parameters and re-pin (07 §4)")
	}
}

func TestDesired_NoConnectionNoOwnerAreEmpty(t *testing.T) {
	f := newFixture(t).withAllRefs()
	d, err := f.svc.desired(ctx, "default")
	if err != nil || d.Params.ModelConnection != "" || d.Params.GitHubOwner != "" {
		t.Fatalf("%+v %v", d.Params, err)
	}
}

func TestDesired_OrgAndEnvConfigs(t *testing.T) {
	f := newFixture(t).withAllRefs()
	d, err := f.svc.desired(ctx, "default")
	if err != nil {
		t.Fatal(err)
	}
	if d.Params.Org.ID != testOU || d.Params.Org.Handle != "default" || d.Params.AgentClientID != "ae-studio-default" {
		t.Fatalf("org %+v client %q", d.Params.Org, d.Params.AgentClientID)
	}
	raw, _ := json.Marshal(d.EnvConfigs)
	for _, want := range []string{`"budgetBytes":"2147483648"`, `"aeOnlyClientId":"ae-studio-internal-client"`,
		`"userAudiences":["aep-console-client"]`, `"pullSecret":{"remoteKey":"","property":""}`, `"extraEgress":[{`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("env configs lack %s: %s", want, raw)
		}
	}
}

// Task 1.8: the pod must never be configured into a legacy mode.
func TestDesired_NoLegacyModeInputs(t *testing.T) {
	f := newFixture(t).withAllRefs().withConnection(anthropicConn("claude-x"))
	d, _ := f.svc.desired(ctx, "default")
	p, _ := json.Marshal(d.Params)
	e, _ := json.Marshal(d.EnvConfigs)
	for _, bad := range []string{"COLLAB_DEV", "COLLAB_MOCK_BFF", "AEP_API_BASE", "AGENT_JWT_"} {
		if strings.Contains(string(p)+string(e), bad) {
			t.Errorf("binding inputs carry %s", bad)
		}
	}
}

func TestDesired_ExtraEgressMustBeAnArray(t *testing.T) {
	for _, raw := range []string{`{}`, `null`, `"x"`} {
		f := newFixture(t).withAllRefs()
		f.svc.cfg.ExtraEgress = json.RawMessage(raw)
		if st, err := f.svc.Status(userCtx(), "default"); err != nil || st.State != StateFailed {
			t.Errorf("extraEgress %s: state %s err %v", raw, st.State, err)
		}
	}
	f := newFixture(t).withAllRefs()
	f.svc.cfg.ExtraEgress = json.RawMessage(`[]`)
	if _, err := f.svc.desired(ctx, "default"); err != nil {
		t.Fatalf("[] is valid: %v", err)
	}
}

func TestDesired_MissingInputsAreFailed(t *testing.T) {
	cases := map[string]func(*fixture){
		"no webhook secret":          func(f *fixture) { f.withoutRef(organization.OrgSecretGitHubWebhookSecret) },
		"no studio client":           func(f *fixture) { f.withoutRef(organization.OrgSecretStudioClient) },
		"reference gone from the CP": func(f *fixture) { delete(f.oc.refs, "default-ae-publisher-client-aaaa0003") },
		"config missing":             func(f *fixture) { f.withoutConfig("AE_STUDIO_GATEWAY_HOST") },
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t).withAllRefs()
			setup(f)
			if st, err := f.svc.Status(userCtx(), "default"); err != nil || st.State != StateFailed {
				t.Fatalf("state %s err %v", st.State, err)
			}
			if f.oc.writes() != 0 {
				t.Fatalf("nothing to ensure, yet wrote: %v", f.oc.calls)
			}
		})
	}
}
