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

package cmd

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// ensureSREAgentSecret must create sre-agent-aep with its placeholder values
// when absent, but never touch it again once it exists — a re-run of
// `aectl sre install` must not wipe what aep-api's reconciler already pushed.
func TestEnsureSREAgentSecretCreatesOnlyWhenMissing(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	p := sreParams{ObsNamespace: "obs"}

	if err := ensureSREAgentSecret(ctx, client, "obs", p); err != nil {
		t.Fatalf("first ensure: %v", err)
	}
	sec, err := client.CoreV1().Secrets("obs").Get(ctx, "sre-agent-aep", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get secret after first ensure: %v", err)
	}
	if got := sec.StringData["RCA_LLM_API_KEY"]; got != "" {
		t.Fatalf("stringData.RCA_LLM_API_KEY = %q, want empty placeholder", got)
	}

	// Simulate aep-api's reconciler pushing real values.
	sec.Data = map[string][]byte{
		"RCA_LLM_API_KEY":  []byte("sk-real-key"),
		"RCA_MODEL_NAME":   []byte("gpt-4"),
		"RCA_LLM_BASE_URL": []byte("https://api.example.com"),
		"AEP_MCP_TOKEN":    []byte("tok-real"),
	}
	sec.StringData = nil
	if _, err := client.CoreV1().Secrets("obs").Update(ctx, sec, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("simulate aep-api push: %v", err)
	}

	// A second install run must not overwrite it.
	if err := ensureSREAgentSecret(ctx, client, "obs", p); err != nil {
		t.Fatalf("second ensure: %v", err)
	}
	sec, err = client.CoreV1().Secrets("obs").Get(ctx, "sre-agent-aep", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get secret after second ensure: %v", err)
	}
	if got := string(sec.Data["RCA_LLM_API_KEY"]); got != "sk-real-key" {
		t.Fatalf("data.RCA_LLM_API_KEY = %q, want the value aep-api pushed to survive a re-run", got)
	}
}
