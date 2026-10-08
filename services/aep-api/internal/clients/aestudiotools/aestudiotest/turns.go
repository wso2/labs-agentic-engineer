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

package aestudiotest

// turns.go — the turn port: scripted event streams.

import (
	"context"
	"fmt"
	"iter"
	"regexp"
	"slices"

	"github.com/google/uuid"

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// The start-repo-turn body's patterns (ae-studio-tools internal/v1
// openapi.yaml, TurnRequest and PlanScope).
var (
	scopeTagPattern  = regexp.MustCompile(`^[A-Za-z0-9._/-]{1,200}$`)
	storyIDPattern   = regexp.MustCompile(`^F[0-9]+\.[0-9]+$`)
	featureIDPattern = regexp.MustCompile(`^F[0-9]+$`)
	itemIDPattern    = regexp.MustCompile(`^P[0-9]+$`)
	appliesToPattern = regexp.MustCompile(`^(F[0-9]+|all)$`)
)

// validTurn refuses what the adapter refuses before sending (a turnId that
// is not a UUID, an unknown kind: plain errors) and what the pod's validator
// and handler answer 400 validation_failed to. A refused start is in Calls
// (begin records it first) but never in TurnCalls: a test asserting that no
// turn was attempted reads Calls.
func validTurn(req aestudiotools.TurnRequest) error {
	if _, err := uuid.Parse(req.TurnID); err != nil {
		return fmt.Errorf("ae studio: turnId %q is not a UUID", req.TurnID)
	}
	if req.Kind != aestudiotools.TurnKindStart && req.Kind != aestudiotools.TurnKindPlan {
		return fmt.Errorf("ae studio: unknown turn kind %q", req.Kind)
	}
	if req.At != "" && (!atPattern.MatchString(req.At) || req.Kind != aestudiotools.TurnKindPlan) {
		return podRefusal(OpStartTurn, "at must be tags/<name> or a full sha, on a plan turn")
	}
	if req.Scope == nil {
		return nil
	}
	sc := req.Scope
	if !scopeTagPattern.MatchString(sc.Tag) {
		return podRefusal(OpStartTurn, "scope.tag is not a tag name")
	}
	for _, s := range sc.Stories {
		if !storyIDPattern.MatchString(s.ID) {
			return podRefusal(OpStartTurn, "scope.stories[].id must be F<n>.<m>")
		}
	}
	for _, f := range sc.Features {
		if !featureIDPattern.MatchString(f.ID) || !allMatch(featureIDPattern, f.Needs) {
			return podRefusal(OpStartTurn, "scope.features[] ids must be F<n>")
		}
	}
	for _, it := range sc.ProductWide {
		if !itemIDPattern.MatchString(it.ID) || !allMatch(appliesToPattern, it.AppliesTo) {
			return podRefusal(OpStartTurn, "scope.productWide[] must be P<n> applying to F<n> or all")
		}
	}
	return nil
}

func allMatch(re *regexp.Regexp, values []string) bool {
	for _, v := range values {
		if !re.MatchString(v) {
			return false
		}
	}
	return true
}

// ScriptTurn sets the events every later turn streams. Without a result
// event, the stream ends with {result, completed}.
func (f *Fake) ScriptTurn(events ...aestudiotools.TurnEvent) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.script = slices.Clone(events)
}

// TurnCalls lists the turns started so far, in order (failed starts are not
// listed; Calls has them).
func (f *Fake) TurnCalls() []TurnCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.turns)
}

// StartTurn records the call and streams the scripted events. A request the
// adapter or the pod refuses (validTurn) is refused, and not recorded.
func (f *Fake) StartTurn(_ context.Context, ref aestudiotools.RepoRef, req aestudiotools.TurnRequest) (iter.Seq2[aestudiotools.TurnEvent, error], error) {
	if err := f.begin(Call{Op: OpStartTurn, Ref: ref}); err != nil {
		return nil, err
	}
	if err := validTurn(req); err != nil {
		return nil, err
	}
	if err := f.resolveTurnAt(ref, req.At); err != nil {
		return nil, err
	}
	f.mu.Lock()
	f.turns = append(f.turns, TurnCall{Ref: ref, Request: req})
	events := slices.Clone(f.script)
	f.mu.Unlock()
	if len(events) == 0 || events[len(events)-1].Type != aestudiotools.EventResult {
		events = append(events, aestudiotools.TurnEvent{Type: aestudiotools.EventResult, Status: "completed"})
	}
	return func(yield func(aestudiotools.TurnEvent, error) bool) {
		for _, ev := range events {
			if !yield(ev, nil) {
				return
			}
		}
	}, nil
}

// resolveTurnAt resolves a plan turn's `at` as the pod does before the turn
// starts: a repository the Fake lacks is the pod's 404 project_unknown, a tag
// or sha the repository lacks its permanent 404 ref_not_found, both in the
// adapter's shape.
func (f *Fake) resolveTurnAt(ref aestudiotools.RepoRef, at string) error {
	if at == "" {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	st, err := f.content(ref)
	if err != nil {
		return err
	}
	if _, err := st.resolve(at); err != nil {
		return fmt.Errorf("%w: %w", sourcecontrol.ErrRefNotFound,
			&aestudiotools.StatusError{Op: OpStartTurn, Status: 404, Code: "ref_not_found", Detail: at})
	}
	return nil
}
