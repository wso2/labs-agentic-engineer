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

package sreagent

import (
	"testing"

	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/modelconn"
)

func TestDesiredFrom(t *testing.T) {
	e := organization.EffectiveSRE{Source: organization.SRESourceOverride, Key: "sk-xxxxxxxxxxxx",
		Conn: modelconn.Connection{Format: modelconn.FormatOpenAICompatible, BaseURL: "https://api.openai.com/v1", Model: "gpt-5.4"}}
	d := DesiredFrom(e, "tok")
	if !d.Configured || d.Model != "openai:gpt-5.4" || d.Replicas() != 1 {
		t.Fatalf("%+v", d)
	}
	none := DesiredFrom(organization.EffectiveSRE{Source: organization.SRESourceNone}, "tok")
	if none.Configured || none.Replicas() != 0 || string(none.SecretData()["RCA_LLM_API_KEY"]) != "" {
		t.Fatalf("%+v", none)
	}
	if len(none.SecretData()) != 4 {
		t.Fatal("SecretData must always carry all four keys (the Deployment's secretKeyRefs require them)")
	}
}

func TestHashChangesWithAnyValue(t *testing.T) {
	a := Desired{Configured: true, Model: "openai:m", BaseURL: "https://h/v1", APIKey: "k1", MCPToken: "t"}
	b := a
	b.APIKey = "k2"
	if a.Hash() == b.Hash() {
		t.Fatal("key rotation must change the hash (forces a restart)")
	}
}

func TestStatusOf_CrashLoopIsFailed(t *testing.T) {
	d := Desired{Configured: true, Model: "openai:m", APIKey: "k"}
	dep := DeploymentState{Replicas: 1, TemplateHash: d.Hash(), Generation: 2, ObservedGeneration: 2}
	st, reason := StatusOf(d, dep, []PodState{{Hash: d.Hash(), WaitingReason: "CrashLoopBackOff", ExitCode: 3}})
	if st != StatusFailed || reason == "" {
		t.Fatalf("%s %q", st, reason)
	}
}

func TestStatusOf(t *testing.T) {
	d := Desired{Configured: true, Model: "openai:m", APIKey: "k"}
	h := d.Hash()
	cases := []struct {
		name string
		dep  DeploymentState
		pods []PodState
		want Status
	}{
		{"old hash on template", DeploymentState{Replicas: 1, TemplateHash: "old"}, nil, StatusApplying},
		{"rolled out and available", DeploymentState{Replicas: 1, UpdatedReplicas: 1, AvailableReplicas: 1, TemplateHash: h, Generation: 3, ObservedGeneration: 3}, []PodState{{Hash: h}}, StatusRunning},
		{"new pod not ready yet", DeploymentState{Replicas: 1, TemplateHash: h, Generation: 3, ObservedGeneration: 3}, []PodState{{Hash: h, WaitingReason: "ContainerCreating"}}, StatusApplying},
	}
	for _, tc := range cases {
		if got, _ := StatusOf(d, tc.dep, tc.pods); got != tc.want {
			t.Errorf("%s: got %s want %s", tc.name, got, tc.want)
		}
	}
	if got, _ := StatusOf(Desired{}, DeploymentState{}, nil); got != StatusUnconfigured {
		t.Errorf("unconfigured: %s", got)
	}
}
