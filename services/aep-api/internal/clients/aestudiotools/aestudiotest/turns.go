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
	"iter"
	"slices"

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools"
)

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

// StartTurn records the call and streams the scripted events.
func (f *Fake) StartTurn(_ context.Context, ref aestudiotools.RepoRef, req aestudiotools.TurnRequest) (iter.Seq2[aestudiotools.TurnEvent, error], error) {
	if err := f.begin(Call{Op: OpStartTurn, Ref: ref}); err != nil {
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
