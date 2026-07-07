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

package activities_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/wso2/labs-agentic-engineer/packages/contracts/orchestration"
	"github.com/wso2/labs-agentic-engineer/services/orchestrator/internal/activities"
	"github.com/wso2/labs-agentic-engineer/services/orchestrator/internal/types"
)

func TestHTTPClientCallsInternalOrchestrationSurface(t *testing.T) {
	seen := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen[r.Method+" "+r.URL.Path]++
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/internal/v1/orchestration/design/components":
			var in struct {
				Org     string `json:"org"`
				Project string `json:"project"`
			}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&in))
			require.Equal(t, "acme", in.Org)
			require.Equal(t, "web", in.Project)
			_ = json.NewEncoder(w).Encode(struct {
				Components []activities.ComponentSpec `json:"components"`
			}{Components: []activities.ComponentSpec{{Name: "api"}, {Name: "web", DependsOn: []string{"api"}}}})
		case "/internal/v1/orchestration/gate-check":
			_ = json.NewEncoder(w).Encode(types.GateChecksResult{Passed: true, Detail: "ok"})
		case "/internal/v1/orchestration/tasks/dispatch",
			"/internal/v1/orchestration/tasks/deploy",
			"/internal/v1/orchestration/tasks/auto-merge":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := activities.NewHTTPClient(srv.URL, "bearer-token", srv.Client())
	ctx := context.Background()

	components, err := client.Components(ctx, "acme", "web")
	require.NoError(t, err)
	require.Equal(t, []activities.ComponentSpec{{Name: "api"}, {Name: "web", DependsOn: []string{"api"}}}, components)

	checks, err := client.RunChecks(ctx, types.GateChecksInput{Org: "acme", Project: "web", Stage: "design"})
	require.NoError(t, err)
	require.True(t, checks.Passed)

	task := types.TaskLifecycleInput{
		Org: "acme", Project: "web", TaskID: "api", ComponentName: "api", CodeReview: orchestration.GateHuman,
	}
	require.NoError(t, client.DispatchTask(ctx, task))
	require.NoError(t, client.DeployTask(ctx, task))
	require.NoError(t, client.MergePR(ctx, task))

	require.Equal(t, 1, seen["POST /internal/v1/orchestration/design/components"])
	require.Equal(t, 1, seen["POST /internal/v1/orchestration/gate-check"])
	require.Equal(t, 1, seen["POST /internal/v1/orchestration/tasks/dispatch"])
	require.Equal(t, 1, seen["POST /internal/v1/orchestration/tasks/deploy"])
	require.Equal(t, 1, seen["POST /internal/v1/orchestration/tasks/auto-merge"])
}
