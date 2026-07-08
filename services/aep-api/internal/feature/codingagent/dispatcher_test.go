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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/clustergatewayproxy"
)

func minimalDispatchInputs() Inputs {
	return Inputs{
		OrgUUID: "org-uuid-1",
		Job: JobInputs{
			RunName:       "run-1",
			TaskID:        "task-1",
			OrgID:         "acme",
			ProjectID:     "widgets",
			ComponentName: "order-service",
			RunnerImage:   "agent-runner:latest",
			RepoURL:       "https://github.com/acme/widgets.git",
			Prompt:        "do the thing",
			IdentityName:  "AEP Bot",
			IdentityEmail: "bot@aep.dev",
			GitServiceURL: "https://git.internal",
			CallbackURL:   "https://aep-api.internal",
		},
		AnthropicSR:            SecretRef{SecretRefName: "anthropic-ref", KVPath: "kv/anthropic", Property: "key"},
		GitHubSR:               SecretRef{SecretRefName: "github-ref", KVPath: "kv/github", Property: "token"},
		ClusterSecretStoreName: "application-secrets-read",
	}
}

// TestDispatch_ResourceQuotaUnavailable_StillDispatches covers the §R3.4
// graceful-degrade requirement: when the cluster-gateway-proxy's allow-list
// doesn't yet support "resourcequotas" (a real cross-service dependency, not
// hypothetical — see docs/design/orchestration/README.md), the quota
// ensure step fails but MUST NOT block dispatch — the DB admission mutex
// remains the active concurrency gate.
func TestDispatch_ResourceQuotaUnavailable_StillDispatches(t *testing.T) {
	var jobApplied bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/resourcequotas"):
			// Simulate the proxy's allow-list not (yet) covering this verb.
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"the server could not find the requested resource"}`))
		case strings.Contains(r.URL.Path, "/jobs") && r.Method == http.MethodPost:
			jobApplied = true
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"metadata":{"name":"run-1"}}`))
		default:
			// Namespace / ServiceAccount / ExternalSecret applies — all succeed.
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"metadata":{"name":"ok"}}`))
		}
	}))
	defer srv.Close()

	proxy := clustergatewayproxy.New(clustergatewayproxy.Config{BaseURL: srv.URL})
	d := New(proxy)

	runName, err := d.Dispatch(context.Background(), minimalDispatchInputs())
	if err != nil {
		t.Fatalf("Dispatch must degrade gracefully when the quota ensure fails, got: %v", err)
	}
	if runName != "run-1" {
		t.Errorf("runName = %q, want run-1", runName)
	}
	if !jobApplied {
		t.Error("expected the Job to still be applied despite the quota ensure failure")
	}
}

// TestDispatch_ZeroConcurrencyLimit_SkipsQuotaEnsure covers
// WithConcurrencyLimit(0) — dispatch never even attempts the quota call.
func TestDispatch_ZeroConcurrencyLimit_SkipsQuotaEnsure(t *testing.T) {
	var sawQuotaCall bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/resourcequotas") {
			sawQuotaCall = true
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"metadata":{"name":"ok"}}`))
	}))
	defer srv.Close()

	proxy := clustergatewayproxy.New(clustergatewayproxy.Config{BaseURL: srv.URL})
	d := New(proxy).WithConcurrencyLimit(0)

	if _, err := d.Dispatch(context.Background(), minimalDispatchInputs()); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if sawQuotaCall {
		t.Error("WithConcurrencyLimit(0) must skip the quota ensure call entirely")
	}
}
