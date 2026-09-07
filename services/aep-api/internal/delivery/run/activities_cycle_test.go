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

	"github.com/stretchr/testify/require"

	"github.com/wso2/aep/aep-api/internal/delivery"
)

// AppendCycle's PROJECTION — what the activity actually writes onto the row.
//
// The workflow tests next door mock this activity out, so they pin what the LOOP
// passes and nothing about what the activity does with it: drop
// `ValidationIssue: in.ValidationIssue` from the struct literal and every one of
// them still passes, while the console loses the number it needs to reach a
// running validation. That gap is the whole reason this file exists.

// stubCycles captures the row AppendCycle builds. Only Append is exercised; the
// rest of CycleStore is present to satisfy the port and must never be called
// here — a test that reached them would be testing the loop, not the projection.
type stubCycles struct{ appended []delivery.RunCycle }

func (s *stubCycles) Append(_ context.Context, cycle *delivery.RunCycle) (string, error) {
	s.appended = append(s.appended, *cycle)
	return "cycle-1", nil
}

func (s *stubCycles) NoteDispatch(context.Context, string, string) error { return nil }
func (s *stubCycles) Finish(context.Context, string, string) error       { return nil }
func (s *stubCycles) SetValidationVerdict(context.Context, string, string, int, string) error {
	return nil
}

func (s *stubCycles) LatestValidationDigest(context.Context, string, []string) (string, error) {
	return "", nil
}

func (s *stubCycles) Latest(context.Context, string, string) (*delivery.RunCycle, error) {
	return nil, nil
}

// A validation cycle's row carries the issue it was dispatched at, from the open.
//
// This is what the console reads while a run is still going — the issue link, and
// the agent's status line on that issue's newest comment. The run's own copy is
// not written until the verdict, so if this projection is dropped the number
// exists nowhere until the run has ended, which is after the only window those
// two render in.
func TestAppendCycle_ValidationCycleCarriesItsIssue(t *testing.T) {
	cycles := &stubCycles{}
	acts := NewActivities(Deps{Cycles: cycles})

	id, err := acts.AppendCycle(context.Background(), AppendCycleInput{
		RunID: "run-1", OrgID: "acme", ProjectID: "shop",
		Kind: delivery.CycleKindValidation, ValidationIssue: 77,
	})
	require.NoError(t, err)
	require.Equal(t, "cycle-1", id)

	require.Len(t, cycles.appended, 1)
	got := cycles.appended[0]
	require.Equal(t, delivery.CycleKindValidation, got.Kind)
	require.Equal(t, 77, got.ValidationIssue,
		"the row must carry the issue at OPEN — the run's copy does not exist until the verdict")
	require.Equal(t, "run-1", got.RunID)
	require.Equal(t, "acme", got.OrgID)
	require.Equal(t, "shop", got.ProjectID)
}

// A cycle over a whole WORKING SET carries no issue, whatever it was passed.
//
// Guarded here as well as at the call site (noAnchorIssue) because the two
// protect different things: the call site decides what a coding cycle is
// dispatched with, and this pins that the column stays a validation fact even if
// something upstream ever hands one a number.
func TestAppendCycle_CodingCycleCarriesNoIssue(t *testing.T) {
	cycles := &stubCycles{}
	acts := NewActivities(Deps{Cycles: cycles})

	_, err := acts.AppendCycle(context.Background(), AppendCycleInput{
		RunID: "run-1", OrgID: "acme", ProjectID: "shop",
		Kind: delivery.CycleKindCoding,
	})
	require.NoError(t, err)

	require.Len(t, cycles.appended, 1)
	require.Zero(t, cycles.appended[0].ValidationIssue,
		"a coding cycle is anchored to no issue — it re-lists the milestone and picks its own")
}

// An unwired store is an ERROR, not a silent success. AppendCycle opens the
// record every later write keys on, so a supervisor that could not open one must
// not proceed as though it had.
func TestAppendCycle_UnwiredStoreFails(t *testing.T) {
	_, err := NewActivities(Deps{}).AppendCycle(context.Background(), AppendCycleInput{
		RunID: "run-1", Kind: delivery.CycleKindValidation, ValidationIssue: 77,
	})
	require.ErrorIs(t, err, errNotConfigured)
}
