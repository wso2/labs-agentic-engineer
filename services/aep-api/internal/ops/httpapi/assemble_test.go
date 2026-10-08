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

package httpapi_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/wso2/aep/aep-api/internal/ops"
	"github.com/wso2/aep/aep-api/internal/ops/httpapi"
)

// The per-domain assembly test (§8, §19.1).
//
// It exists because `var _ gen.StrictServerInterface = (*apiServer)(nil)` proves
// the METHOD SET and never the wiring: it uses a nil pointer, so a Handlers with
// a nil sub-handler satisfies the interface, builds green, and panics on the
// first request. This builds the REAL graph and asserts every embed is non-nil.
//
// It runs in microseconds because New is pure — no DB, no clients, no I/O. That
// is the whole point of keeping Resolve (impure) out of the domain modules.

type fakeRepo struct{}

func (fakeRepo) Get(context.Context, string, string) (*ops.RcaAgentReport, error) {
	return nil, nil
}
func (fakeRepo) List(context.Context, string, string, int) ([]ops.RcaAgentReport, string, error) {
	return nil, "", nil
}

func TestAssembleWiresEverySlice(t *testing.T) {
	h, err := httpapi.New(ops.Deps{Reports: fakeRepo{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	v := reflect.ValueOf(h).Elem()
	for i := 0; i < v.NumField(); i++ {
		f := v.Type().Field(i)
		if !f.Anonymous {
			continue
		}
		if v.Field(i).IsNil() {
			t.Errorf("slice handler %s is nil after assembly — the interface assertion would still "+
				"pass and the first request would panic", f.Name)
		}
	}
}

// TestAssembleRejectsMissingPorts pins the fail-fast: a Deps that cannot produce
// a working domain must be an error at construction, not a nil-pointer panic on
// the first request.
func TestAssembleRejectsMissingPorts(t *testing.T) {
	if _, err := httpapi.New(ops.Deps{}); err == nil {
		t.Fatal("New(Deps{}) succeeded with no repository — a nil repo panics on first use, so " +
			"assembly must refuse it")
	}
}

// TestAssembleAllowsNilExecutionReader: correlation is optional by design, and
// P1 must not require the delivery domain (P6) to exist.
func TestAssembleAllowsNilExecutionReader(t *testing.T) {
	if _, err := httpapi.New(ops.Deps{Reports: fakeRepo{}, Execs: nil}); err != nil {
		t.Fatalf("New with a nil ExecutionReader failed: %v — correlation is optional", err)
	}
}
