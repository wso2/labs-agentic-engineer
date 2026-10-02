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
	Command []string `json:"command"`
	Env     []struct {
		Name string `json:"name"`
	} `json:"env"`
}

func deploymentContainers(t *testing.T, dep []byte) map[string]podContainer {
	t.Helper()
	var d struct {
		Spec struct {
			Template struct {
				Spec struct {
					Containers []podContainer `json:"containers"`
				} `json:"spec"`
			} `json:"template"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(dep, &d); err != nil {
		t.Fatal(err)
	}
	out := map[string]podContainer{}
	for _, c := range d.Spec.Template.Spec.Containers {
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
		`"name":"AE_MODEL_CONNECTION"`, `"name":"AE_GITHUB_OWNER"`} {
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
	// I-2: a retired org-secret path must not wipe the pod's Secret before
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
	rt, err := Template()
	if err != nil {
		t.Fatal(err)
	}
	var dep []byte
	for _, m := range rt.Spec.Resources {
		if m.ID == "deployment" {
			dep = m.Template
		}
	}
	cs := deploymentContainers(t, dep)
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
