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

package task

import (
	"fmt"
	"strings"
)

// A planned Task's issue body is PROSE, for the coding agent to read. Nothing
// parses it platform-side: the milestone is the version pin, the `aep` label is
// the working-set marker, and ordering is the "Depends on #N" lines the AGENT
// honours. That is the whole structure — the body is free to read like a brief
// because no code depends on its shape.
//
// The one structured thing a body still carries is a REFERENCE: the issue
// numbers of the Tasks this one depends on, so the agent can follow them.

// foundation is the feature a component's shared Task names: its setup and the
// product-wide requirements the version carries (B3).
const foundation = "foundation"

// plannedTask is one Task's facts as the plan tap tracks them: what the planner
// said at creation, plus whatever a later updateTask patched. The tap
// re-renders the whole body from this state on every patch, so a body is always
// the current facts rather than an accumulation of edits.
type plannedTask struct {
	// Component is the design component this Task builds.
	Component string
	// Feature is the feature it builds there ("F2"), or "foundation" (B3);
	// empty when the planner named none.
	Feature string
	// AppPath is the component's source directory relative to the repo root,
	// resolved from the design. Empty when the design does not pin one (the
	// component builds from the repo root) or no design reader is wired.
	AppPath string
	// DependsOn holds design COMPONENT names, as the planner emits them. They
	// are resolved to issue numbers at render time, because the issue a
	// dependency will get may not exist yet when this Task is planned.
	DependsOn []string
	// Rationale is the planner's one-line justification; Body is the fuller
	// brief a later updateTask writes.
	Rationale string
	Body      string
}

// taskLink is one "Depends on" line: the issue the platform resolved, or, when
// it has none yet, the name the agent can search for.
type taskLink struct {
	Number int
	Name   string
}

// taskBrief is what a body states beyond the planner's facts: the feature's
// words and the product-wide requirements the Task must honour, both from the
// version's scope.
type taskBrief struct {
	FeatureName string
	ProductWide []string
}

// composeTaskBody renders a Task's issue body. links are its dependencies in
// order, already resolved to issues where the platform has one; an unresolved
// one is still named — losing the ordering hint entirely would be worse than a
// name the agent can search for.
func composeTaskBody(p plannedTask, brief taskBrief, links []taskLink) string {
	var sb strings.Builder
	if r := strings.TrimSpace(p.Rationale); r != "" {
		sb.WriteString(r)
		sb.WriteString("\n\n")
	}
	if c := strings.TrimSpace(p.Component); c != "" {
		fmt.Fprintf(&sb, "**Component:** `%s`\n", c)
	}
	switch {
	case p.Feature == foundation:
		sb.WriteString("**Feature:** foundation — the component's shared setup and product-wide requirements\n")
	case p.Feature != "" && brief.FeatureName != "":
		fmt.Fprintf(&sb, "**Feature:** %s %s\n", p.Feature, brief.FeatureName)
	case p.Feature != "":
		fmt.Fprintf(&sb, "**Feature:** %s\n", p.Feature)
	}
	if len(brief.ProductWide) > 0 {
		fmt.Fprintf(&sb, "**Product-wide:** %s\n", strings.Join(brief.ProductWide, ", "))
	}
	if ap := strings.TrimSpace(p.AppPath); ap != "" {
		fmt.Fprintf(&sb, "**App Path:** `%s`\n", ap)
	}
	for _, l := range links {
		if l.Number > 0 {
			fmt.Fprintf(&sb, "Depends on #%d\n", l.Number)
			continue
		}
		fmt.Fprintf(&sb, "Depends on the %s task\n", l.Name)
	}
	if b := strings.TrimSpace(p.Body); b != "" {
		sb.WriteString("\n")
		sb.WriteString(b)
		sb.WriteString("\n")
	}
	return sb.String()
}
