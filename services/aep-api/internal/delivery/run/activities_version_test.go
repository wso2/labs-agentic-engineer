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

package run

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/wso2/aep/aep-api/internal/delivery"
)

// The ReadVersionState activity composes three ports, and the composition is
// where a silent mistake would live: the three of them have to agree on how a
// component is NAMED, or every component reads as behind and the reconcile
// re-promotes the whole design on every cycle. The classification itself is
// pinned as a pure function in policy_test.go.

type stubDesign struct{ paths map[string]string }

func (d stubDesign) ComponentPaths(context.Context, string, string) (map[string]string, error) {
	return d.paths, nil
}

type stubBuilds struct{ runs map[string][]BuildRunInfo }

func (b stubBuilds) ListBuildRuns(_ context.Context, _, _, component string) ([]BuildRunInfo, error) {
	return b.runs[component], nil
}

type stubDeployments struct {
	states []delivery.ComponentDeploy
	asked  [][]string
}

func (d *stubDeployments) DeploymentState(_ context.Context, _, _ string,
	components []string) ([]delivery.ComponentDeploy, error) {
	d.asked = append(d.asked, components)
	return d.states, nil
}

func TestReadVersionState_ComposesTheThreeReads(t *testing.T) {
	const project = "shop7321"
	greenAt := func(sha string) []BuildRunInfo {
		return []BuildRunInfo{{
			Name: "build-" + sha, Terminal: true, Succeeded: true, CommitSHA: sha,
			StartedAt: time.Now(),
		}}
	}
	deploys := &stubDeployments{states: []delivery.ComponentDeploy{
		{Component: "api", Release: delivery.ReleaseNameFor(project, "api", "aaa1"), Ready: true},
		// The webapp's binding was never written — the incident's own shape.
		{Component: "webapp"},
	}}
	acts := NewActivities(Deps{
		// App Paths are what the design read answers; the deploy set is its KEYS.
		Design:      stubDesign{paths: map[string]string{"webapp": "apps/web", "api": "services/api"}},
		Builds:      stubBuilds{runs: map[string][]BuildRunInfo{"api": greenAt("aaa1"), "webapp": greenAt("bbb2")}},
		Deployments: deploys,
	})

	got, err := acts.ReadVersionState(context.Background(), ProjectRef{OrgID: "acme", ProjectID: project})
	require.NoError(t, err)

	require.Equal(t, []string{"api", "webapp"}, deploys.asked[0],
		"the deployment read is asked about every component in the DESIGN, sorted — "+
			"a random map order would reorder every promote and log line the pass produces")
	require.Equal(t, []delivery.ComponentState{{
		Component: "api", State: delivery.ComponentStateServing,
		DesiredSHA: "aaa1", DesiredRelease: delivery.ReleaseNameFor(project, "api", "aaa1"),
		Pinned: delivery.ReleaseNameFor(project, "api", "aaa1"), Ready: true,
	}, {
		Component: "webapp", State: delivery.ComponentStateBehind,
		DesiredSHA: "bbb2", DesiredRelease: delivery.ReleaseNameFor(project, "webapp", "bbb2"),
	}}, got.Components)
	require.False(t, got.Serving(), "a component with a green build and no binding is not serving")
	require.Equal(t, "webapp=behind", got.Describe())
}

// A plane with no OpenChoreo behind it must not have its runs fail on a gate
// that has nothing to gate — the same degradation every other optional
// collaborator makes, and the reason VersionState.Serving() reads an empty state
// as serving.
func TestReadVersionState_UnwiredIsAnEmptyStateThatServes(t *testing.T) {
	got, err := NewActivities(Deps{}).ReadVersionState(context.Background(),
		ProjectRef{OrgID: "acme", ProjectID: "shop"})

	require.NoError(t, err)
	require.Empty(t, got.Components)
	require.True(t, got.Serving())
}
