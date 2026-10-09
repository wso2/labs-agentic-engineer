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

package files_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/wso2/aep/ae-studio-tools/internal/files"
	"github.com/wso2/aep/ae-studio-tools/internal/gen/aepapi"
	"github.com/wso2/aep/ae-studio-tools/internal/projects"
)

// fakeAEPAPI answers POST /ae-studio/dependency-completions with status and
// body, and hands each decoded request to seen.
func fakeAEPAPI(t *testing.T, status int, body string, seen func(aepapi.AEStudioDependencyCompletionsRequest)) aepapi.ClientInterface {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/ae-studio/dependency-completions" {
			t.Errorf("request %s %s", r.Method, r.URL.Path)
		}
		var req aepapi.AEStudioDependencyCompletionsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if seen != nil {
			seen(req)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	c, err := aepapi.NewClient(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestAEPAPICompleter_SendsProjectAndStubs(t *testing.T) {
	var got aepapi.AEStudioDependencyCompletionsRequest
	answer := `{"completed":[{"path":"specs/design/dependencies/payments/dependency.json","definition":"{}","files":[{"path":"specs/design/dependencies/payments/openapi.yaml","content":"openapi: 3.0.3\n"}]}],
	"warnings":[{"path":"specs/design/dependencies/payments/dependency.json","code":"registry-copied","message":"copied"}]}`
	c := files.NewAEPAPICompleter(fakeAEPAPI(t, http.StatusOK, answer, func(r aepapi.AEStudioDependencyCompletionsRequest) { got = r }))

	completed, warnings, err := c.Complete(ctx, "greeter", []files.WriteOp{
		{Path: "specs/design/dependencies/payments/dependency.json", Content: paymentsStub, BaseSHA: "ignored"},
	})
	if err != nil {
		t.Fatal(err)
	}
	wantReq := aepapi.AEStudioDependencyCompletionsRequest{Project: "greeter", Writes: []aepapi.AEStudioFile{
		{Path: "specs/design/dependencies/payments/dependency.json", Content: paymentsStub},
	}}
	if !reflect.DeepEqual(got, wantReq) {
		t.Fatalf("request = %+v, want %+v", got, wantReq)
	}
	wantCompleted := map[string]files.Completed{"specs/design/dependencies/payments/dependency.json": {
		Definition: "{}",
		Files:      map[string]string{"specs/design/dependencies/payments/openapi.yaml": "openapi: 3.0.3\n"},
	}}
	if !reflect.DeepEqual(completed, wantCompleted) {
		t.Fatalf("completed = %+v", completed)
	}
	if len(warnings) != 1 || warnings[0] != (files.Warning{Path: "specs/design/dependencies/payments/dependency.json", Code: "registry-copied", Message: "copied"}) {
		t.Fatalf("warnings = %+v", warnings)
	}
}

// Errors map by HTTP status alone; the body is never read for a decision.
func TestAEPAPICompleter_MapsStatus(t *testing.T) {
	cases := []struct {
		status        int
		body          string
		want          error
		misconfigured bool
	}{
		{http.StatusNotFound, `{"code":"project_unknown"}`, projects.ErrUnknown, false},
		{http.StatusUnauthorized, `{}`, projects.ErrUnavailable, true},
		{http.StatusForbidden, `{}`, projects.ErrUnavailable, true},
		{http.StatusBadRequest, `{"code":"path_invalid"}`, projects.ErrUnavailable, false},
		{http.StatusBadGateway, `{}`, projects.ErrUnavailable, false},
		{http.StatusOK, `not json`, projects.ErrUnavailable, false},
		{http.StatusOK, `{"completed":[{"path":"","definition":"{}","files":[]}],"warnings":[]}`, projects.ErrUnavailable, false},
		{http.StatusOK, `{"completed":[{"path":"a","definition":"{}","files":[]},{"path":"a","definition":"{}","files":[]}],"warnings":[]}`, projects.ErrUnavailable, false},
		{http.StatusOK, `{"completed":[{"path":"a","definition":"{}","files":[{"path":"f","content":"1"},{"path":"f","content":"2"}]}],"warnings":[]}`, projects.ErrUnavailable, false},
	}
	for _, c := range cases {
		cmp := files.NewAEPAPICompleter(fakeAEPAPI(t, c.status, c.body, nil))
		_, _, err := cmp.Complete(ctx, "greeter", []files.WriteOp{{Path: "specs/design/dependencies/payments/dependency.json", Content: paymentsStub}})
		if !errors.Is(err, c.want) || errors.Is(err, projects.ErrMisconfigured) != c.misconfigured {
			t.Errorf("%d %s: err = %v, want %v (misconfigured %v)", c.status, c.body, err, c.want, c.misconfigured)
		}
	}
}

func TestAEPAPICompleter_Unreachable(t *testing.T) {
	c, err := aepapi.NewClient("http://127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = files.NewAEPAPICompleter(c).Complete(ctx, "greeter", []files.WriteOp{{Path: "specs/design/dependencies/payments/dependency.json", Content: paymentsStub}})
	if !errors.Is(err, projects.ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
}
