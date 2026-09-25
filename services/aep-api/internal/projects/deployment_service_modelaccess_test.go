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

package projects

import (
	"context"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// The collaborator is optional and nil-by-default, so "nobody called the
// setter" is indistinguishable from "this org has no key" at the call site —
// both yield an agent with no MODEL_*. That is exactly how it shipped: the
// provider existed, the composition point existed, and app.go never wired
// them, so every deployed ai-agent came up without a model key and answered
// 500 on its first turn. This pins the wiring itself, not just the branch.
type stubModelAccess struct{ called bool }

func (s *stubModelAccess) ModelAccessEnvVars(context.Context, string, string) ([]openchoreo.WorkflowEnvVarRef, error) {
	s.called = true
	return []openchoreo.WorkflowEnvVarRef{{Key: "MODEL_API_KEY"}}, nil
}

func TestSetModelAccess_WiresTheProviderForAnAIAgent(t *testing.T) {
	t.Parallel()
	svc := NewDeploymentService(nil, nil)
	if svc.modelAccess != nil {
		t.Fatal("modelAccess should start nil")
	}

	stub := &stubModelAccess{}
	svc.SetModelAccess(stub)
	if svc.modelAccess == nil {
		t.Fatal("SetModelAccess did not wire the provider")
	}

	got := svc.envVarsWithModelAccess(context.Background(), "acme", "web", "chat-agent", spec.ComponentTypeAIAgent)
	if !stub.called {
		t.Fatal("an ai-agent deploy did not ask for model access")
	}
	var names []string
	for _, v := range got {
		names = append(names, v.Key)
	}
	if len(names) != 1 || names[0] != "MODEL_API_KEY" {
		t.Fatalf("want MODEL_API_KEY appended, got %v", names)
	}
}

// A non-agent must never receive MODEL_* — the key is the platform's spend.
func TestSetModelAccess_NotConsultedForANonAgent(t *testing.T) {
	t.Parallel()
	svc := NewDeploymentService(nil, nil)
	stub := &stubModelAccess{}
	svc.SetModelAccess(stub)

	svc.envVarsWithModelAccess(context.Background(), "acme", "web", "hotel-api", spec.ComponentTypeService)
	if stub.called {
		t.Fatal("a service asked for model access")
	}
}
