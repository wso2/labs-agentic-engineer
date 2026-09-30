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

package kubeobs

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/config"
	"github.com/wso2/aep/aep-api/internal/sreagent"
)

// request is what the fake apiserver saw.
type request struct {
	Method, Path, Query, ContentType, Auth string
	Body                                   []byte
}

// apiserver answers every request with status and body, recording it.
func apiserver(t *testing.T, status int, body string) (*Client, *request) {
	t.Helper()
	got := &request{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		*got = request{Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery,
			ContentType: r.Header.Get("Content-Type"), Auth: r.Header.Get("Authorization"), Body: b}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	c, err := New(config.KubeAPIConfig{BaseURL: srv.URL, BearerToken: "sa-token"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c, got
}

func jsonOf(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("body %q is not JSON: %v", b, err)
	}
	return m
}

func TestNew_RequiresBaseURL(t *testing.T) {
	if _, err := New(config.KubeAPIConfig{}); err == nil {
		t.Fatal("New with no BaseURL: want an error")
	}
}

func TestPatchSecretData(t *testing.T) {
	c, got := apiserver(t, http.StatusOK, `{}`)
	err := c.PatchSecretData(context.Background(), "ns", "sre-agent-aep", map[string][]byte{
		"RCA_LLM_API_KEY": []byte("sk-secret"), "RCA_MODEL_NAME": []byte("openai:gpt-5.4"),
	})
	if err != nil {
		t.Fatalf("PatchSecretData: %v", err)
	}
	if got.Method != http.MethodPatch || got.Path != "/api/v1/namespaces/ns/secrets/sre-agent-aep" {
		t.Errorf("request = %s %s", got.Method, got.Path)
	}
	if got.ContentType != "application/merge-patch+json" {
		t.Errorf("Content-Type = %q", got.ContentType)
	}
	if got.Auth != "Bearer sa-token" {
		t.Errorf("Authorization = %q", got.Auth)
	}
	data, _ := jsonOf(t, got.Body)["data"].(map[string]any)
	// base64("sk-secret"), base64("openai:gpt-5.4")
	if data["RCA_LLM_API_KEY"] != "c2stc2VjcmV0" || data["RCA_MODEL_NAME"] != "b3BlbmFpOmdwdC01LjQ=" || len(data) != 2 {
		t.Errorf("data = %v, want the two values base64-encoded", data)
	}
}

func TestPatchSecretData_ErrorCarriesNoBody(t *testing.T) {
	c, _ := apiserver(t, http.StatusUnprocessableEntity, `{"kind":"Status","message":"echo sk-secret"}`)
	err := c.PatchSecretData(context.Background(), "ns", "sre-agent-aep", map[string][]byte{"RCA_LLM_API_KEY": []byte("sk-secret")})
	if err == nil {
		t.Fatal("want an error on 422")
	}
	msg := err.Error()
	if strings.Contains(msg, "sk-secret") {
		t.Errorf("error %q carries the response body of a Secret call", msg)
	}
	if !strings.Contains(msg, "PATCH") || !strings.Contains(msg, "/api/v1/namespaces/ns/secrets/sre-agent-aep") || !strings.Contains(msg, "422") {
		t.Errorf("error %q, want method, path and status code", msg)
	}
}

func TestPatchTemplateAnnotation(t *testing.T) {
	c, got := apiserver(t, http.StatusOK, `{}`)
	if err := c.PatchTemplateAnnotation(context.Background(), "ns", "sre-agent", sreagent.HashAnnotation, "abc"); err != nil {
		t.Fatalf("PatchTemplateAnnotation: %v", err)
	}
	if got.Method != http.MethodPatch || got.Path != "/apis/apps/v1/namespaces/ns/deployments/sre-agent" {
		t.Errorf("request = %s %s", got.Method, got.Path)
	}
	if got.ContentType != "application/merge-patch+json" {
		t.Errorf("Content-Type = %q", got.ContentType)
	}
	want := `{"spec":{"template":{"metadata":{"annotations":{"aep.wso2.com/sre-llm-hash":"abc"}}}}}`
	if string(got.Body) != want {
		t.Errorf("body = %s, want %s", got.Body, want)
	}
}

func TestScale(t *testing.T) {
	c, got := apiserver(t, http.StatusOK, `{}`)
	if err := c.Scale(context.Background(), "ns", "sre-agent", 0); err != nil {
		t.Fatalf("Scale: %v", err)
	}
	if got.Method != http.MethodPatch || got.Path != "/apis/apps/v1/namespaces/ns/deployments/sre-agent/scale" {
		t.Errorf("request = %s %s", got.Method, got.Path)
	}
	if got.ContentType != "application/merge-patch+json" {
		t.Errorf("Content-Type = %q", got.ContentType)
	}
	if string(got.Body) != `{"spec":{"replicas":0}}` {
		t.Errorf("body = %s", got.Body)
	}
}

func TestDeployment(t *testing.T) {
	c, got := apiserver(t, http.StatusOK, `{
	  "metadata": {"generation": 4},
	  "spec": {
	    "replicas": 1,
	    "selector": {"matchLabels": {"app.kubernetes.io/name": "sre-agent"}},
	    "template": {"metadata": {"annotations": {"aep.wso2.com/sre-llm-hash": "h1"}}}
	  },
	  "status": {"observedGeneration": 3, "updatedReplicas": 1, "availableReplicas": 1}
	}`)
	dep, sel, err := c.Deployment(context.Background(), "ns", "sre-agent")
	if err != nil {
		t.Fatalf("Deployment: %v", err)
	}
	if got.Method != http.MethodGet || got.Path != "/apis/apps/v1/namespaces/ns/deployments/sre-agent" {
		t.Errorf("request = %s %s", got.Method, got.Path)
	}
	want := sreagent.DeploymentState{Replicas: 1, UpdatedReplicas: 1, AvailableReplicas: 1,
		Generation: 4, ObservedGeneration: 3, TemplateHash: "h1"}
	if dep != want {
		t.Errorf("state = %+v, want %+v", dep, want)
	}
	if len(sel) != 1 || sel["app.kubernetes.io/name"] != "sre-agent" {
		t.Errorf("selector = %v", sel)
	}
}

func TestDeployment_ScaledToZeroAndNoAnnotation(t *testing.T) {
	c, _ := apiserver(t, http.StatusOK, `{"spec":{"replicas":0,"selector":{"matchLabels":{"a":"b"}},"template":{"metadata":{}}}}`)
	dep, _, err := c.Deployment(context.Background(), "ns", "sre-agent")
	if err != nil {
		t.Fatalf("Deployment: %v", err)
	}
	if dep.Replicas != 0 || dep.TemplateHash != "" {
		t.Errorf("state = %+v, want replicas 0 and no hash", dep)
	}
}

func TestDeployment_NotFound(t *testing.T) {
	c, _ := apiserver(t, http.StatusNotFound, `{"kind":"Status","message":"deployments.apps \"sre-agent\" not found"}`)
	_, _, err := c.Deployment(context.Background(), "ns", "sre-agent")
	if err == nil || !strings.Contains(err.Error(), "404") || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err = %v, want the 404 with the apiserver's message", err)
	}
}

func TestPods(t *testing.T) {
	c, got := apiserver(t, http.StatusOK, `{"items": [
	  {"metadata": {"annotations": {"aep.wso2.com/sre-llm-hash": "h1"}},
	   "status": {"containerStatuses": [{
	     "state": {"waiting": {"reason": "CrashLoopBackOff"}},
	     "lastState": {"terminated": {"exitCode": 3, "reason": "Error"}}}]}},
	  {"metadata": {"annotations": {"aep.wso2.com/sre-llm-hash": "h0"}},
	   "status": {"containerStatuses": [{"state": {"terminated": {"reason": "Error", "exitCode": 1}}}]}},
	  {"metadata": {}, "status": {}}
	]}`)
	pods, err := c.Pods(context.Background(), "ns", map[string]string{"b": "2", "a": "1"})
	if err != nil {
		t.Fatalf("Pods: %v", err)
	}
	if got.Method != http.MethodGet || got.Path != "/api/v1/namespaces/ns/pods" {
		t.Errorf("request = %s %s", got.Method, got.Path)
	}
	if got.Query != "labelSelector=a%3D1%2Cb%3D2" {
		t.Errorf("query = %q, want the selector's labels sorted", got.Query)
	}
	want := []sreagent.PodState{
		{Hash: "h1", WaitingReason: "CrashLoopBackOff", ExitCode: 3},
		{Hash: "h0", TerminatedReason: "Error", ExitCode: 1},
		{},
	}
	if len(pods) != len(want) {
		t.Fatalf("pods = %+v, want %+v", pods, want)
	}
	for i := range want {
		if pods[i] != want[i] {
			t.Errorf("pod %d = %+v, want %+v", i, pods[i], want[i])
		}
	}
}

func TestPods_RefusesAnEmptySelector(t *testing.T) {
	c, _ := apiserver(t, http.StatusOK, `{"items":[]}`)
	if _, err := c.Pods(context.Background(), "ns", nil); err == nil {
		t.Fatal("an empty selector lists every pod in the namespace: want an error")
	}
}

func TestTokenFileReadPerRequest(t *testing.T) {
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		_, _ = io.WriteString(w, `{}`)
	}))
	defer srv.Close()
	file := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(file, []byte("first\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := New(config.KubeAPIConfig{BaseURL: srv.URL, TokenFile: file})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := c.Scale(context.Background(), "ns", "d", 1); err != nil || auth != "Bearer first" {
		t.Fatalf("err=%v auth=%q", err, auth)
	}
	if err := os.WriteFile(file, []byte("rotated"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := c.Scale(context.Background(), "ns", "d", 1); err != nil || auth != "Bearer rotated" {
		t.Fatalf("err=%v auth=%q, want the rotated token", err, auth)
	}
}
