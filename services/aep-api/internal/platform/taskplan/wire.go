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

// Package taskplan holds the BFF side of the task-planning tool contract
// (docs/design/tasks-github-native.md §10.3): the Go wire structs + decoders
// for the ok tool results the plan tap consumes as the pod's task-op events (this file),
// and the tasks/<issueNumber>.md context-file renderer (context_file.go). The
// tool INPUTS are validated agent-side against the published JSON Schemas
// (packages/contracts/schemas/{plan-task,update-task}.schema.json, registered
// by @aep/agent-stream) — the tap never re-validates inputs; it acts only on
// ok:true results whose fields the agent already normalized.
package taskplan

import (
	"encoding/json"
	"fmt"
)

// TaskRef targets a Task in an updateTask call: {issueNumber} for a pre-existing
// Task from context, or {title} for one planned earlier in the same turn. The
// plan tap resolves {title} against this-turn creations only (§9.3).
type TaskRef struct {
	IssueNumber *int    `json:"issueNumber,omitempty"`
	Title       *string `json:"title,omitempty"`
}

// Validate enforces the ref union invariant: exactly one of issueNumber/title.
func (r TaskRef) Validate() error {
	switch {
	case r.IssueNumber != nil && r.Title != nil:
		return fmt.Errorf("task ref: exactly one of issueNumber/title, got both")
	case r.IssueNumber == nil && r.Title == nil:
		return fmt.Errorf("task ref: exactly one of issueNumber/title, got neither")
	}
	return nil
}

// UpdateTaskSet is the patch payload of updateTask. Every field is a pointer so
// "omitted" (leave unchanged) is distinct from "set to empty" — the patch
// semantics the tap needs. A non-nil DependsOn replaces the whole list.
type UpdateTaskSet struct {
	Title     *string   `json:"title,omitempty"`
	DependsOn *[]string `json:"dependsOn,omitempty"`
	Rationale *string   `json:"rationale,omitempty"`
	Body      *string   `json:"body,omitempty"`
}

// ---- tool RESULT shapes (tool-result.output — self-contained) --------------
//
// The plan tap acts on tool-result frames, not tool-call frames: it applies a
// GitHub write only for a paired result whose output.ok is true (phase-1 rule).

// PlanTaskOk is a successful planTask result (origin default-filled by the agent).
type PlanTaskOk struct {
	OK        bool     `json:"ok"` // always true
	Op        string   `json:"op"` // "plan"
	Component string   `json:"component"`
	Title     string   `json:"title"`
	DependsOn []string `json:"dependsOn"`
	Origin    string   `json:"origin"`
	Rationale string   `json:"rationale"`
}

// UpdateTaskOk is a successful updateTask result. Ref is RESOLVED by the agent:
// {issueNumber} for a pre-existing Task, or {title:<canonical pre-rename>} for a
// this-turn planned Task (the tap normalizes the title identically — trim +
// lowercase — to match its title→issue map).
type UpdateTaskOk struct {
	OK  bool          `json:"ok"` // always true
	Op  string        `json:"op"` // "update"
	Ref TaskRef       `json:"ref"`
	Set UpdateTaskSet `json:"set"`
}

// DecodePlanTaskOk decodes a successful planTask result. It errors unless
// ok==true and op=="plan".
func DecodePlanTaskOk(raw json.RawMessage) (*PlanTaskOk, error) {
	var out PlanTaskOk
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode planTask result: %w", err)
	}
	if !out.OK || out.Op != "plan" {
		return nil, fmt.Errorf("not a successful planTask result (ok=%v op=%q)", out.OK, out.Op)
	}
	return &out, nil
}

// DecodeUpdateTaskOk decodes a successful updateTask result. It errors unless
// ok==true and op=="update", and enforces the resolved ref union.
func DecodeUpdateTaskOk(raw json.RawMessage) (*UpdateTaskOk, error) {
	var out UpdateTaskOk
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode updateTask result: %w", err)
	}
	if !out.OK || out.Op != "update" {
		return nil, fmt.Errorf("not a successful updateTask result (ok=%v op=%q)", out.OK, out.Op)
	}
	if err := out.Ref.Validate(); err != nil {
		return nil, err
	}
	return &out, nil
}
