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

package organization_test

// External test package: dbtest imports migrate, which imports organization —
// an in-package dbtest file would be an import cycle.

import (
	"context"
	"testing"

	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
)

// The record the govern stage reads to tell a moved endpoint from an unchanged
// one: absent until written, one row per (org, component, environment), and a
// later write replaces the earlier one.
func TestAIAgentModelEndpointRepository_RecordsTheLatestEndpoint_DB(t *testing.T) {
	t.Parallel()
	repo := organization.NewAIAgentModelEndpointRepository(dbtest.New(t))
	ctx := context.Background()

	if got, ok, err := repo.Get(ctx, "acme", "checkout-agent", "default"); err != nil || ok || got != "" {
		t.Fatalf("before any write: %q, %v, %v; want none", got, ok, err)
	}
	if err := repo.Put(ctx, "acme", "checkout-agent", "default", "http://gw/aep-x/v1"); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := repo.Put(ctx, "acme", "checkout-agent", "default", "http://gw/aep-x/compatible-mode/v1"); err != nil {
		t.Fatalf("put again: %v", err)
	}
	if got, ok, err := repo.Get(ctx, "acme", "checkout-agent", "default"); err != nil || !ok || got != "http://gw/aep-x/compatible-mode/v1" {
		t.Fatalf("after two writes: %q, %v, %v; want the later endpoint", got, ok, err)
	}
	// Keyed on all three: the same agent in another environment, or another
	// org's agent of the same name, is its own record.
	for _, k := range [][3]string{{"acme", "checkout-agent", "staging"}, {"globex", "checkout-agent", "default"}} {
		if _, ok, err := repo.Get(ctx, k[0], k[1], k[2]); err != nil || ok {
			t.Fatalf("%v: ok=%v err=%v; want no record", k, ok, err)
		}
	}
}
