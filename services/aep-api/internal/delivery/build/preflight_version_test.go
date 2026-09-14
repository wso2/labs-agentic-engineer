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

package build

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wso2/aep/aep-api/internal/spec"
)

type fakeVersions struct {
	facts spec.VersionFacts
	err   error
	calls int
}

func (f *fakeVersions) BuildVersionFacts(context.Context, string, string) (spec.VersionFacts, error) {
	f.calls++
	return f.facts, f.err
}

// The Build click makes ONE request, so preflight answers both halves of it:
// what is still unresolved, and what the version is called and changes.
func TestPreflight_CarriesTheVersionFacts(t *testing.T) {
	versions := &fakeVersions{facts: spec.VersionFacts{
		CurrentVersion:   "payments-v2",
		SuggestedVersion: "v3",
		Changes: []spec.VersionChange{
			{Name: "orders-api", Kind: spec.VersionChangeKindComponent, State: spec.VersionChangeChanged},
			{Name: "reports-web", Kind: spec.VersionChangeKindComponent, State: spec.VersionChangeNew},
		},
	}}
	svc := NewPreflightService(PreflightDeps{
		Design:   fakeDesign{},
		Status:   fakeStatus{},
		Versions: versions,
	})

	pf, err := svc.Preflight(context.Background(), "acme", "shop")

	require.NoError(t, err)
	require.Equal(t, 1, versions.calls)
	require.Equal(t, "payments-v2", pf.CurrentVersion)
	require.Equal(t, "v3", pf.SuggestedVersion)
	require.False(t, pf.SpecUnchanged)
	require.Equal(t, []BuildChange{
		{Name: "orders-api", Kind: "component", State: "changed"},
		{Name: "reports-web", Kind: "component", State: "new"},
	}, pf.Changes)
}

// An unchanged tree cuts nothing, so the dialog locks its name field and says
// Rebuild — and has no change list to show.
func TestPreflight_UnchangedSpecTree(t *testing.T) {
	svc := NewPreflightService(PreflightDeps{
		Design: fakeDesign{},
		Status: fakeStatus{},
		Versions: &fakeVersions{facts: spec.VersionFacts{
			CurrentVersion:   "v2",
			SuggestedVersion: "v3",
			SpecUnchanged:    true,
		}},
	})

	pf, err := svc.Preflight(context.Background(), "acme", "shop")

	require.NoError(t, err)
	require.True(t, pf.SpecUnchanged)
	require.Empty(t, pf.Changes)
}

// A version read that fails costs the dialog its name and its list. It must not
// cost the BUILD: refusing the whole preflight would refuse the click over a
// list nobody has to act on.
func TestPreflight_VersionReadFailureDegrades(t *testing.T) {
	svc := NewPreflightService(PreflightDeps{
		Design:   fakeDesign{},
		Status:   fakeStatus{},
		Versions: &fakeVersions{err: errors.New("mirror unreachable")},
	})

	pf, err := svc.Preflight(context.Background(), "acme", "shop")

	require.NoError(t, err)
	require.Empty(t, pf.CurrentVersion)
	require.Empty(t, pf.SuggestedVersion)
	require.Empty(t, pf.Changes)
}

// The feature unwired is not the feature broken: no version reader means no
// version half, and preflight still reports what needs resolving.
func TestPreflight_NoVersionReaderWired(t *testing.T) {
	svc := NewPreflightService(PreflightDeps{Design: fakeDesign{}, Status: fakeStatus{}})

	pf, err := svc.Preflight(context.Background(), "acme", "shop")

	require.NoError(t, err)
	require.Empty(t, pf.SuggestedVersion)
}
