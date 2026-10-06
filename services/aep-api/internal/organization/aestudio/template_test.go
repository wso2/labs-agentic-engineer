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

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
)

type podContainer struct {
	Name    string   `json:"name"`
	Image   string   `json:"image"`
	Command []string `json:"command"`
	Args    []string `json:"args"`
	Env     []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"env"`
	EnvFrom         []any          `json:"envFrom"`
	Ports           []any          `json:"ports"`
	VolumeMounts    []any          `json:"volumeMounts"`
	Resources       map[string]any `json:"resources"`
	SecurityContext map[string]any `json:"securityContext"`
}

func (c podContainer) env(name string) (string, bool) {
	for _, e := range c.Env {
		if e.Name == name {
			return e.Value, true
		}
	}
	return "", false
}

// templateOf is the template of the RT resource id.
func templateOf(t *testing.T, id string) json.RawMessage {
	t.Helper()
	rt, err := Template()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range rt.Spec.Resources {
		if m.ID == id {
			return m.Template
		}
	}
	t.Fatalf("no resource %s", id)
	return nil
}

// renderedContainers renders the deployment with the relay channel relayURL
// ("" = no relay) and returns its containers in order.
func renderedContainers(t *testing.T, relayURL string) []podContainer {
	t.Helper()
	raw, err := json.Marshal(renderRT(t, templateOf(t, "deployment"), rtVars(relayURL)))
	if err != nil {
		t.Fatal(err)
	}
	var d struct {
		Spec struct {
			Template struct {
				Spec struct {
					Containers []podContainer `json:"containers"`
				} `json:"spec"`
			} `json:"template"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatal(err)
	}
	return d.Spec.Template.Spec.Containers
}

func byName(cs []podContainer) map[string]podContainer {
	out := map[string]podContainer{}
	for _, c := range cs {
		out[c.Name] = c
	}
	return out
}

// TestTemplate_Invariants pins what the tickets fix.
func TestTemplate_Invariants(t *testing.T) {
	rt, err := Template()
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]openchoreo.ResourceTypeManifest{}
	for _, m := range rt.Spec.Resources {
		byID[m.ID] = m
	}
	for _, id := range []string{"es-tools", "es-agent", "deployment", "service", "route-design", "route-collab", "route-tools", "route-tools-turns", "streams-idle", "netpol", "cnp-apiserver"} {
		if _, ok := byID[id]; !ok {
			t.Errorf("missing %s", id)
		}
	}
	dep := string(byID["deployment"].Template)
	for _, want := range []string{`"type":"Recreate"`, `"terminationGracePeriodSeconds":30`, `"automountServiceAccountToken":false`,
		`"name":"studio-data"`, `"subPath":"snapshots"`, `"medium":"Memory"`, `"port":9080`, `"port":9081`, `"port":9082`,
		`"name":"AE_MODEL_CONNECTION"`, `"name":"AE_GITHUB_OWNER"`, `"name":"AE_WEBHOOK_URL"`} {
		if !strings.Contains(dep, want) {
			t.Errorf("deployment lacks %s", want)
		}
	}
	if strings.Contains(dep, "AE_MODEL_FORMAT") {
		t.Error("R18: one AE_MODEL_CONNECTION env, not six AE_MODEL_* env")
	}
	if strings.Contains(dep, "PLACEHOLDER") || strings.Contains(dep, "python") {
		t.Error("test-only command left in the real type")
	}
	if !strings.Contains(byID["es-agent"].IncludeWhen, "size(parameters.secrets.designAgent.data) > 0") {
		t.Error("O-5: es-agent must be conditional")
	}
	// A retired org-secret path must not wipe the pod's Secret before
	// the converge repoints it.
	for _, id := range []string{"es-tools", "es-agent", "es-pull"} {
		var es struct {
			Spec struct {
				Target struct {
					DeletionPolicy string `json:"deletionPolicy"`
				} `json:"target"`
			} `json:"spec"`
		}
		if err := json.Unmarshal(byID[id].Template, &es); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		if es.Spec.Target.DeletionPolicy != "Retain" {
			t.Errorf("%s target.deletionPolicy = %q, want Retain set explicitly", id, es.Spec.Target.DeletionPolicy)
		}
	}
	if strings.Contains(string(byID["route-design"].Template), "Last-Event-ID") {
		t.Error("07 §14: Last-Event-ID allow-header removed")
	}
	names := []string{}
	for _, o := range rt.Spec.Outputs {
		names = append(names, o.Name)
	}
	if strings.Join(names, ",") != "designUrl,collabUrl,toolsUrl,webhookUrl" {
		t.Errorf("outputs = %v", names)
	}
	if len(TemplateHash()) != 16 {
		t.Error("hash length")
	}
}

// TestTemplate_ContainerCommandsAndEnv pins the SIGTERM ruling (the app is PID 1
// for the node containers) and the env each container must never receive.
func TestTemplate_ContainerCommandsAndEnv(t *testing.T) {
	cs := byName(renderedContainers(t, ""))
	for _, name := range []string{"ae-design-agent", "ae-collab"} {
		got := strings.Join(cs[name].Command, " ")
		if got != "node --import tsx src/main.ts" {
			t.Errorf("%s command = %q, want node as PID 1 (no pnpm start)", name, got)
		}
	}
	if len(cs["ae-studio-tools"].Command) != 0 {
		t.Error("ae-studio-tools runs its image entrypoint")
	}
	forbidden := map[string][]string{
		"ae-collab":       {"COLLAB_DEV", "COLLAB_MOCK_BFF", "AEP_API_BASE"}, // pod mode refuses to boot with them
		"ae-design-agent": {"AGENT_JWT_SECRET", "AGENT_JWT_ISSUER", "AGENT_JWT_AUDIENCE"},
	}
	for name, envs := range forbidden {
		for _, e := range cs[name].Env {
			if strings.HasPrefix(e.Name, "AGENT_JWT_") && name == "ae-design-agent" {
				t.Errorf("%s renders %s", name, e.Name)
			}
			for _, f := range envs {
				if e.Name == f {
					t.Errorf("%s renders %s", name, f)
				}
			}
		}
	}
}

// TestTemplate_WebhookURLEnvIsTheOutput: without a relay,
// ae-studio-tools registers its repo hooks at AE_WEBHOOK_URL, which must
// render to the very URL the webhookUrl output publishes, or GitHub delivers
// where nothing listens.
func TestTemplate_WebhookURLEnvIsTheOutput(t *testing.T) {
	rt, err := Template()
	if err != nil {
		t.Fatal(err)
	}
	var output string
	for _, o := range rt.Spec.Outputs {
		if o.Name == "webhookUrl" {
			output = o.Value
		}
	}
	want := renderString(t, celEnv(t), output, rtVars(""))
	got, _ := byName(renderedContainers(t, ""))["ae-studio-tools"].env("AE_WEBHOOK_URL")
	if want != "http://default-ae-studio-tools.openchoreoapis.localhost:19080/webhooks/github" || got != want {
		t.Fatalf("AE_WEBHOOK_URL = %q, want the webhookUrl output %q", got, want)
	}
}

// TestTemplate_WebhookRelay: webhookRelayUrl set adds the
// webhook-relay container (4 in the pod) and points the repo hooks at the
// relay channel; empty renders the three containers and the public route.
func TestTemplate_WebhookRelay(t *testing.T) {
	const relay = "https://smee.io/AbCdEfGhIjKlMnOpQrStUv"
	on := renderedContainers(t, relay)
	names := []string{}
	for _, c := range on {
		names = append(names, c.Name)
	}
	if strings.Join(names, ",") != "ae-design-agent,ae-collab,ae-studio-tools,webhook-relay" {
		t.Fatalf("relay on: containers = %v", names)
	}
	if got, _ := byName(on)["ae-studio-tools"].env("AE_WEBHOOK_URL"); got != relay {
		t.Errorf("relay on: AE_WEBHOOK_URL = %q, want the relay channel", got)
	}
	r := byName(on)["webhook-relay"]
	if r.Image != "ghcr.io/chmouel/gosmee@sha256:abc" {
		t.Errorf("relay image = %q, want environmentConfigs.webhookRelay.image", r.Image)
	}
	if strings.Join(r.Args, " ") != "client "+relay+" http://127.0.0.1:8082/webhooks/github" || len(r.Command) != 0 {
		t.Errorf("relay command/args = %v %v", r.Command, r.Args)
	}
	// No socket dir (any mount), no port, no Secret, no env.
	if len(r.VolumeMounts) != 0 || len(r.Ports) != 0 || len(r.EnvFrom) != 0 || len(r.Env) != 0 {
		t.Errorf("relay must mount, expose and read nothing: %+v", r)
	}
	sc, _ := json.Marshal(r.SecurityContext)
	if string(sc) != `{"allowPrivilegeEscalation":false,"capabilities":{"drop":["ALL"]},"readOnlyRootFilesystem":true}` {
		t.Errorf("relay securityContext = %s", sc)
	}
	res, _ := json.Marshal(r.Resources)
	if !strings.Contains(string(res), `"requests":{"cpu":"10m","memory":"32Mi"}`) {
		t.Errorf("relay resources = %s", res)
	}

	off := renderedContainers(t, "")
	if len(off) != 3 {
		t.Fatalf("relay off: %d containers, want 3", len(off))
	}
	if got, _ := byName(off)["ae-studio-tools"].env("AE_WEBHOOK_URL"); got != "http://default-ae-studio-tools.openchoreoapis.localhost:19080/webhooks/github" {
		t.Errorf("relay off: AE_WEBHOOK_URL = %q, want the webhookUrl output", got)
	}
}

// The tools ExternalSecret the RT renders from a converge's own params lists
// exactly the pod's keys: its gitpat, its webhook secret and its ae-studio
// client, never the org's publisher client (Task 9.H18), even while the org
// has the publisher row its coding Jobs mount.
func TestTemplate_ToolsExternalSecretHoldsOnlyThePodsClient(t *testing.T) {
	d, err := newFixture(t).withAllRefs().svc.desired(ctx, "default")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(d.Params)
	if err != nil {
		t.Fatal(err)
	}
	vars := rtVars("")
	var p map[string]any
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	vars["parameters"] = p
	// spec.data alone: the harness has no dataplane variable (secretStoreRef).
	var es struct {
		Spec struct {
			Data json.RawMessage `json:"data"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(templateOf(t, "es-tools"), &es); err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(renderRT(t, es.Spec.Data, vars))
	if err != nil {
		t.Fatal(err)
	}
	var data []struct {
		SecretKey string `json:"secretKey"`
	}
	if err := json.Unmarshal(out, &data); err != nil {
		t.Fatal(err)
	}
	keys := []string{}
	for _, e := range data {
		keys = append(keys, e.SecretKey)
	}
	if strings.Join(keys, ",") != "GITHUB_PAT,GITHUB_WEBHOOK_SECRET,AE_STUDIO_CLIENT_ID,AE_STUDIO_CLIENT_SECRET" {
		t.Fatalf("es-tools secret keys = %v", keys)
	}
}
