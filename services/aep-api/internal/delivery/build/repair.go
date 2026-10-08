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
	"fmt"
	"log/slog"

	"github.com/wso2/aep/aep-api/internal/spec"
)

// Repair builds a repair version of `of` (B4): "Fix" on a version whose
// validation failed. It cuts v1.1 at v1's commit — the same specs, so the same
// features — claims its milestone like any build (superseding v1's, whose open
// bugs move across), files one repair issue per scenario v1's final attempt
// failed, and starts the run with planning skipped: the repair issues ARE the
// version's work, and nothing in the spec asks for more.
//
// The final attempt files no repair issues of its own (it settles first, so a
// one-attempt allowance is a pure re-check); this is where a person decides
// the failures are worth another build.
func (s *Service) Repair(ctx context.Context, orgID, projectID, of string) (string, error) {
	if s.repairs == nil || s.plan == nil {
		return "", &EdgeError{Status: 503, Message: "repair builds are not available on this platform"}
	}
	if err := s.activeDevRun(ctx, orgID, projectID); err != nil {
		return "", err
	}
	if err := s.activeValidationRun(ctx, orgID, projectID); err != nil {
		return "", err
	}
	failures, err := s.repairs.FailuresOf(ctx, orgID, projectID, of)
	if err != nil {
		return "", &EdgeError{Status: 502, Message: "read " + of + "'s validation: " + err.Error()}
	}
	if failures == 0 {
		return "", &EdgeError{Status: 409, Message: of + " has no failing scenario to fix"}
	}
	tag, err := s.tagger.TagRepair(ctx, orgID, projectID, of)
	if err != nil {
		if errors.Is(err, spec.ErrNothingToRepair) {
			return "", &EdgeError{Status: 409, Message: err.Error()}
		}
		return "", mapTagError(err)
	}
	scope, err := s.tagger.BuildScopeAtTag(ctx, orgID, projectID, tag)
	if err != nil {
		return "", &EdgeError{Status: 502, Message: "read " + tag + "'s scope: " + err.Error()}
	}
	run, err := s.claimVersion(ctx, orgID, projectID, scope)
	if err != nil {
		return "", err
	}
	// BEFORE the run starts: a run whose milestone holds no work settles at
	// once, delivered, having built nothing.
	if err := s.repairs.FileRepairs(ctx, orgID, projectID, run.MilestoneNumber, of); err != nil {
		s.failRun(ctx, run, fmt.Errorf("file repair issues: %w", err))
		return "", &EdgeError{Status: 502, Message: "file repair issues: " + err.Error()}
	}
	if err := s.startRun(ctx, orgID, projectID, tag, run, nil, true); err != nil {
		return "", err
	}
	slog.InfoContext(ctx, "repair build started", "project", projectID, "tag", tag, "fixes", of, "failures", failures)
	return tag, nil
}
