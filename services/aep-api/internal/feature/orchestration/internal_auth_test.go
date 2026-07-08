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

package orchestration

import (
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
)

func resolveBearer(t *testing.T, header string) []error {
	t.Helper()
	r := httptest.NewRequest("POST", "/internal/v1/orchestration/tasks/dispatch", nil)
	if header != "" {
		r.Header.Set("Authorization", header)
	}
	ctx := humatest.NewContext(&huma.Operation{}, r, httptest.NewRecorder())
	return bearerAuth{}.Resolve(ctx)
}

func statusOf(t *testing.T, errs []error) int {
	t.Helper()
	if len(errs) == 0 {
		return 0
	}
	se, ok := errs[0].(huma.StatusError)
	if !ok {
		t.Fatalf("expected a huma.StatusError, got %T (%v)", errs[0], errs[0])
	}
	return se.GetStatus()
}

func TestBearerAuthResolve_NotConfigured(t *testing.T) {
	SetInternalBearer("")
	if got := statusOf(t, resolveBearer(t, "Bearer anything")); got != 503 {
		t.Fatalf("unconfigured secret should 503, got %d", got)
	}
}

func TestBearerAuthResolve_MissingHeader(t *testing.T) {
	SetInternalBearer("s3cret")
	defer SetInternalBearer("")
	if got := statusOf(t, resolveBearer(t, "")); got != 401 {
		t.Fatalf("missing Authorization header should 401, got %d", got)
	}
}

func TestBearerAuthResolve_WrongScheme(t *testing.T) {
	SetInternalBearer("s3cret")
	defer SetInternalBearer("")
	if got := statusOf(t, resolveBearer(t, "Basic s3cret")); got != 401 {
		t.Fatalf("non-bearer scheme should 401, got %d", got)
	}
}

func TestBearerAuthResolve_WrongToken(t *testing.T) {
	SetInternalBearer("s3cret")
	defer SetInternalBearer("")
	if got := statusOf(t, resolveBearer(t, "Bearer wrong")); got != 401 {
		t.Fatalf("wrong bearer should 401, got %d", got)
	}
}

func TestBearerAuthResolve_CorrectToken(t *testing.T) {
	SetInternalBearer("s3cret")
	defer SetInternalBearer("")
	if errs := resolveBearer(t, "Bearer s3cret"); len(errs) != 0 {
		t.Fatalf("correct bearer should pass, got %v", errs)
	}
}
