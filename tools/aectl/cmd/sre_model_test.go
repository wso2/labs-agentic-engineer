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
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func writeKeyFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestResolveSreModel(t *testing.T) {
	key := writeKeyFile(t, "  sk-abc123\n")

	if m, err := resolveSreModel("", "", "https://api.openai.com/v1"); m != nil || err != nil {
		t.Fatalf("no flags: model=%v err=%v, want nil, nil", m, err)
	}
	m, err := resolveSreModel(key, "openai:gpt-5.4", "https://api.openai.com/v1/")
	if err != nil {
		t.Fatal(err)
	}
	if m.APIKey != "sk-abc123" || m.Model != "gpt-5.4" || m.BaseURL != "https://api.openai.com/v1" {
		t.Fatalf("model = %+v, want the key trimmed, the prefix and the trailing slash dropped", m)
	}
	for name, args := range map[string][3]string{
		"key without model": {key, "", "https://api.openai.com/v1"},
		"model without key": {"", "gpt-5.4", "https://api.openai.com/v1"},
		"plain http":        {key, "gpt-5.4", "http://api.openai.com/v1"},
		"empty key file":    {writeKeyFile(t, " \n"), "gpt-5.4", "https://api.openai.com/v1"},
		"missing key file":  {filepath.Join(t.TempDir(), "missing"), "gpt-5.4", "https://api.openai.com/v1"},
	} {
		if _, err := resolveSreModel(args[0], args[1], args[2]); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestProbeSreModel(t *testing.T) {
	var gotAuth, gotPath string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotPath = r.Header.Get("Authorization"), r.URL.Path
		switch r.Header.Get("Authorization") {
		case "Bearer good":
			w.WriteHeader(http.StatusOK)
		case "Bearer redirect":
			http.Redirect(w, r, "https://elsewhere.example/models", http.StatusFound)
		default:
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	defer srv.Close()
	ctx := context.Background()

	if err := probeSreModel(ctx, srv.Client(), sreModel{APIKey: "good", BaseURL: srv.URL + "/v1"}); err != nil {
		t.Fatalf("good key: %v", err)
	}
	if gotAuth != "Bearer good" || gotPath != "/v1/models" {
		t.Fatalf("probe sent %q to %q", gotAuth, gotPath)
	}
	err := probeSreModel(ctx, srv.Client(), sreModel{APIKey: "sk-secret-bad", BaseURL: srv.URL})
	if err == nil || !strings.Contains(err.Error(), "rejected") || strings.Contains(err.Error(), "sk-secret-bad") {
		t.Fatalf("bad key: err = %v, want a rejection that does not echo the key", err)
	}
	if err := probeSreModel(ctx, srv.Client(), sreModel{APIKey: "redirect", BaseURL: srv.URL}); err == nil {
		t.Fatal("a redirect must not be followed or accepted")
	}
}

func TestEnsureSREHandoffToken(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()

	first, err := ensureSREHandoffToken(ctx, client, "wso2-aep", false)
	if err != nil || len(first) != 64 {
		t.Fatalf("first: token len %d err %v, want 64 hex chars", len(first), err)
	}
	again, err := ensureSREHandoffToken(ctx, client, "wso2-aep", false)
	if err != nil || again != first {
		t.Fatalf("a re-run must reuse the key: got %q err %v", again, err)
	}
	rotated, err := ensureSREHandoffToken(ctx, client, "wso2-aep", true)
	if err != nil || rotated == first {
		t.Fatalf("rotate must replace the key: got %q err %v", rotated, err)
	}
	sec, err := client.CoreV1().Secrets("wso2-aep").Get(ctx, sreHandoffSecretName, metav1.GetOptions{})
	if err != nil || string(sec.Data["token"]) != rotated {
		t.Fatalf("aep-api's Secret holds %q (err %v), want the rotated key", sec.Data["token"], err)
	}
}

func TestEnsureSREAgentSecret(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()

	state, err := ensureSREAgentSecret(ctx, client, "obs", nil, "tok0")
	if err != nil || state.HasModel || !state.Changed {
		t.Fatalf("a first install without a model: state=%+v err=%v, want the Secret written without a model", state, err)
	}
	m := &sreModel{APIKey: "sk-abc", Model: "gpt-5.4", BaseURL: "https://api.openai.com/v1"}
	state, err = ensureSREAgentSecret(ctx, client, "obs", m, "tok")
	if err != nil || !state.Changed || !state.HasModel {
		t.Fatalf("first model: state=%+v err=%v", state, err)
	}
	sec, _ := client.CoreV1().Secrets("obs").Get(ctx, sreAgentSecretName, metav1.GetOptions{})
	for k, want := range map[string]string{"RCA_LLM_API_KEY": "sk-abc", "RCA_MODEL_NAME": "openai:gpt-5.4", "RCA_LLM_BASE_URL": "https://api.openai.com/v1", "AEP_MCP_TOKEN": "tok"} {
		if got := string(sec.Data[k]); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}

	state, err = ensureSREAgentSecret(ctx, client, "obs", nil, "tok")
	if err != nil || state.Changed || !state.HasModel {
		t.Fatalf("a re-run without flags must keep everything: state=%+v err=%v", state, err)
	}
	state, err = ensureSREAgentSecret(ctx, client, "obs", nil, "tok2")
	if err != nil || !state.Changed {
		t.Fatalf("a rotated key must be written: state=%+v err=%v", state, err)
	}
	sec, _ = client.CoreV1().Secrets("obs").Get(ctx, sreAgentSecretName, metav1.GetOptions{})
	if string(sec.Data["RCA_LLM_API_KEY"]) != "sk-abc" || string(sec.Data["AEP_MCP_TOKEN"]) != "tok2" {
		t.Fatalf("after rotation: key %q token %q, want the model kept and the new token", sec.Data["RCA_LLM_API_KEY"], sec.Data["AEP_MCP_TOKEN"])
	}
}

func TestScaleSREAgent(t *testing.T) {
	ctx := context.Background()
	one := int32(1)
	client := fake.NewSimpleClientset(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "sre-agent", Namespace: "obs"},
		Spec:       appsv1.DeploymentSpec{Replicas: &one},
	})
	if err := scaleSREAgent(ctx, client, "obs", "sre-agent", 0); err != nil {
		t.Fatal(err)
	}
	d, err := client.AppsV1().Deployments("obs").Get(ctx, "sre-agent", metav1.GetOptions{})
	if err != nil || d.Spec.Replicas == nil || *d.Spec.Replicas != 0 {
		t.Fatalf("replicas = %v (err %v), want 0", d.Spec.Replicas, err)
	}
}
