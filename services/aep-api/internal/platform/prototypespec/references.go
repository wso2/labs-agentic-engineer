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

package prototypespec

// references.go — the rules no standalone JSON Schema can express, the Go twin
// of packages/prototype-model/src/references.ts. Run only on a document that
// already has the v1 shape. Two passes, in this order:
//
//  1. Index. Every id goes into ONE namespace. A repeat is DUPLICATE_ID, and
//     any duplicate ends validation there: a reference into an ambiguous
//     namespace has no single target to check.
//  2. Resolve. Every reference names an entry of the right kind — and, for a
//     target that lives on a screen (tabs, stepper, table, dialog, drawer), an
//     entry on the screen the action runs on. A navigation item runs on every
//     screen that shows its navigation.
//
// Both passes walk the document in its serialized key order, so issues come
// out in document order — the same order, codes, paths and sentences as the
// TypeScript validator.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// entry is what an id names. Node kinds are used as-is; the rest are named
// here (role, state, flow, screen, navigation, navigation-item, button, field,
// row, tab, step, breadcrumb, timeline-entry, dialog, drawer).
type entry struct {
	kind     string
	path     string
	screenID string // the screen an on-screen entry belongs to
	parentID string // the tabs, stepper or table a tab, step or row belongs to
}

// referenceIssues is the reference findings for a structurally valid model;
// empty when every id is unique and every reference resolves.
func referenceIssues(m *Model, component string) []Issue {
	index, duplicates := buildIndex(m)
	if len(duplicates) > 0 {
		return duplicates
	}
	r := &resolver{index: index}
	r.model(m, component)
	return r.issues
}

// quote spells a value the way JSON.stringify does, so messages match the
// TypeScript validator byte for byte (no HTML escaping).
func quote(s string) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimSuffix(buf.String(), "\n")
}

// -- Pass 1: the global id index ---------------------------------------------

type indexer struct {
	index      map[string]entry
	duplicates []Issue
}

func buildIndex(m *Model) (map[string]entry, []Issue) {
	x := &indexer{index: map[string]entry{}}
	for i, r := range m.Roles {
		x.add(r.ID, fmt.Sprintf("roles[%d].id", i), entry{kind: "role"})
	}
	for i, s := range m.States {
		x.add(s.ID, fmt.Sprintf("states[%d].id", i), entry{kind: "state"})
	}
	for i, f := range m.Flows {
		x.add(f.ID, fmt.Sprintf("flows[%d].id", i), entry{kind: "flow"})
	}
	for i, s := range m.Screens {
		path := fmt.Sprintf("screens[%d]", i)
		x.add(s.ID, path+".id", entry{kind: "screen"})
		x.nodes(s.Content, path+".content", s.ID)
		for j, o := range s.Overlays {
			at := fmt.Sprintf("%s.overlays[%d]", path, j)
			x.add(o.ID, at+".id", entry{kind: o.Kind, screenID: s.ID})
			x.nodes(o.Content, at+".content", s.ID)
			if o.Kind == "dialog" {
				x.buttons(o.Actions, at+".actions", s.ID)
			}
		}
	}
	for i, n := range m.Navigation {
		x.add(n.ID, fmt.Sprintf("navigation[%d].id", i), entry{kind: "navigation"})
		for j, item := range n.Items {
			x.add(item.ID, fmt.Sprintf("navigation[%d].items[%d].id", i, j), entry{kind: "navigation-item"})
		}
	}
	return x.index, x.duplicates
}

func (x *indexer) add(id, path string, e entry) {
	if first, taken := x.index[id]; taken {
		x.duplicates = append(x.duplicates, Issue{
			Code: CodeDuplicateID,
			Path: path,
			Message: fmt.Sprintf("id %s is already used at %s; ids are unique across the whole prototype",
				quote(id), first.path),
		})
		return
	}
	e.path = path
	x.index[id] = e
}

func (x *indexer) buttons(list []Button, path, screenID string) {
	for i, b := range list {
		x.add(b.ID, fmt.Sprintf("%s[%d].id", path, i), entry{kind: "button", screenID: screenID})
	}
}

func (x *indexer) fields(list []Field, path, screenID string) {
	for i, f := range list {
		x.add(f.ID, fmt.Sprintf("%s[%d].id", path, i), entry{kind: "field", screenID: screenID})
	}
}

func (x *indexer) nodes(list []Node, path, screenID string) {
	for i, n := range list {
		x.node(n, fmt.Sprintf("%s[%d]", path, i), screenID)
	}
}

func (x *indexer) node(n Node, path, screenID string) {
	x.add(n.ID, path+".id", entry{kind: n.Kind, screenID: screenID})
	switch n.Kind {
	case "stack", "grid":
		x.nodes(n.Content, path+".content", screenID)
	case "split":
		x.nodes(n.Left, path+".left", screenID)
		x.nodes(n.Right, path+".right", screenID)
	case "breadcrumbs":
		for i, c := range n.Items {
			x.add(c.ID, fmt.Sprintf("%s.items[%d].id", path, i), entry{kind: "breadcrumb", screenID: screenID})
		}
	case "tabs":
		x.sections(n.Tabs, n.ID, path+".tabs", "tab", screenID)
	case "stepper":
		x.sections(n.Steps, n.ID, path+".steps", "step", screenID)
	case "heading", "approval-panel":
		x.buttons(n.Actions, path+".actions", screenID)
	case "empty-state":
		if n.Button != nil {
			x.add(n.Button.ID, path+".action.id", entry{kind: "button", screenID: screenID})
		}
	case "form":
		x.fields(n.Fields, path+".fields", screenID)
		x.buttons(n.Actions, path+".actions", screenID)
	case "filters":
		x.fields(n.Fields, path+".fields", screenID)
	case "table", "task-queue":
		for i, r := range n.Rows {
			x.add(r.ID, fmt.Sprintf("%s.rows[%d].id", path, i), entry{kind: "row", screenID: screenID, parentID: n.ID})
		}
	case "timeline":
		for i, e := range n.Entries {
			x.add(e.ID, fmt.Sprintf("%s.entries[%d].id", path, i), entry{kind: "timeline-entry", screenID: screenID})
		}
	}
}

func (x *indexer) sections(list []Section, parentID, path, kind, screenID string) {
	for i, s := range list {
		x.add(s.ID, fmt.Sprintf("%s[%d].id", path, i), entry{kind: kind, screenID: screenID, parentID: parentID})
		x.nodes(s.Content, fmt.Sprintf("%s[%d].content", path, i), screenID)
	}
}

// -- Pass 2: resolve every reference -----------------------------------------

type resolver struct {
	index  map[string]entry
	issues []Issue
}

func (r *resolver) unknown(path, value, what string) {
	r.issues = append(r.issues, Issue{
		Code:    CodeUnknownReference,
		Path:    path,
		Message: fmt.Sprintf("%s does not name %s", quote(value), what),
	})
}

func (r *resolver) expect(value, path, kind, what string) {
	if e, ok := r.index[value]; !ok || e.kind != kind {
		r.unknown(path, value, what)
	}
}

func (r *resolver) list(values []string, path, kind, what string) {
	for i, v := range values {
		r.expect(v, fmt.Sprintf("%s[%d]", path, i), kind, what)
	}
}

func (r *resolver) states(values []string, path string) {
	r.list(values, path, "state", "a declared display state")
}

func (r *resolver) roles(values []string, path string) {
	r.list(values, path, "role", "a declared role")
}

// onScreen reports whether value names an entry of one of kinds on every screen
// in screens (the screens the action can run on).
func (r *resolver) onScreen(value string, screens []string, kinds ...string) bool {
	e, ok := r.index[value]
	if !ok || !slices.Contains(kinds, e.kind) || len(screens) == 0 {
		return false
	}
	for _, s := range screens {
		if s != e.screenID {
			return false
		}
	}
	return true
}

// member checks a two-part target: the container on this screen, then a member
// of that container.
func (r *resolver) member(containerID, memberID, path string, keys [2]string, screens []string,
	containerKinds []string, memberKind, containerWhat, memberWhat string,
) {
	if !r.onScreen(containerID, screens, containerKinds...) {
		r.unknown(path+"."+keys[0], containerID, containerWhat)
		return
	}
	if m, ok := r.index[memberID]; !ok || m.kind != memberKind || m.parentID != containerID {
		r.unknown(path+"."+keys[1], memberID, memberWhat+" of "+quote(containerID))
	}
}

func (r *resolver) action(a *Action, path string, screens []string) {
	if a == nil {
		return
	}
	switch a.Kind {
	case "navigate":
		r.expect(a.ScreenID, path+".screenId", "screen", "a screen")
	case "set-tab":
		r.member(a.TabsID, a.TabID, path, [2]string{"tabsId", "tabId"}, screens,
			[]string{"tabs"}, "tab", "a tabs node on this screen", "a tab")
	case "set-step":
		r.member(a.StepperID, a.StepID, path, [2]string{"stepperId", "stepId"}, screens,
			[]string{"stepper"}, "step", "a stepper on this screen", "a step")
	case "select-row":
		r.member(a.TableID, a.RowID, path, [2]string{"tableId", "rowId"}, screens,
			[]string{"table", "task-queue"}, "row", "a table or task queue on this screen", "a row")
	case "show-dialog":
		if !r.onScreen(a.DialogID, screens, "dialog") {
			r.unknown(path+".dialogId", a.DialogID, "a dialog of this screen")
		}
	case "show-drawer":
		if !r.onScreen(a.DrawerID, screens, "drawer") {
			r.unknown(path+".drawerId", a.DrawerID, "a drawer of this screen")
		}
	}
}

func (r *resolver) buttons(list []Button, path string, screens []string) {
	for i := range list {
		r.action(&list[i].Action, fmt.Sprintf("%s[%d].action", path, i), screens)
	}
}

func (r *resolver) fields(list []Field, path string) {
	for i, f := range list {
		r.states(f.ErrorIn, fmt.Sprintf("%s[%d].errorIn", path, i))
	}
}

func (r *resolver) nodes(list []Node, path string, screens []string) {
	for i := range list {
		r.node(&list[i], fmt.Sprintf("%s[%d]", path, i), screens)
	}
}

func (r *resolver) node(n *Node, path string, screens []string) {
	switch n.Kind {
	case "stack", "grid":
		r.nodes(n.Content, path+".content", screens)
	case "split":
		r.nodes(n.Left, path+".left", screens)
		r.nodes(n.Right, path+".right", screens)
	case "breadcrumbs":
		for i := range n.Items {
			r.action(n.Items[i].Action, fmt.Sprintf("%s.items[%d].action", path, i), screens)
		}
	case "tabs":
		for i, t := range n.Tabs {
			r.nodes(t.Content, fmt.Sprintf("%s.tabs[%d].content", path, i), screens)
		}
	case "stepper":
		for i, s := range n.Steps {
			r.nodes(s.Content, fmt.Sprintf("%s.steps[%d].content", path, i), screens)
		}
	case "heading", "approval-panel":
		r.buttons(n.Actions, path+".actions", screens)
	case "empty-state":
		if n.Button != nil {
			r.action(&n.Button.Action, path+".action.action", screens)
		}
	case "button", "link":
		r.action(n.Action, path+".action", screens)
	case "form":
		r.fields(n.Fields, path+".fields")
		r.buttons(n.Actions, path+".actions", screens)
	case "filters":
		r.fields(n.Fields, path+".fields")
	case "table", "task-queue":
		r.action(n.OnRow, path+".onRow", screens)
	}
	r.states(n.ShowIn, path+".showIn")
}

func (r *resolver) model(m *Model, component string) {
	if component != "" && m.Component != component {
		r.issues = append(r.issues, Issue{
			Code: CodeComponentMismatch,
			Path: "component",
			Message: fmt.Sprintf("component %s does not match its directory %s",
				quote(m.Component), quote(component)),
		})
	}
	r.expect(m.DefaultScreenID, "defaultScreenId", "screen", "a screen")
	for i, f := range m.Flows {
		r.expect(f.RoleID, fmt.Sprintf("flows[%d].roleId", i), "role", "a declared role")
		r.list(f.ScreenIDs, fmt.Sprintf("flows[%d].screenIds", i), "screen", "a screen")
	}
	for i, s := range m.Screens {
		path := fmt.Sprintf("screens[%d]", i)
		on := []string{s.ID}
		r.roles(s.RoleIDs, path+".roleIds")
		if s.NavigationID != "" {
			r.expect(s.NavigationID, path+".navigationId", "navigation", "a navigation")
		}
		r.nodes(s.Content, path+".content", on)
		for j, o := range s.Overlays {
			at := fmt.Sprintf("%s.overlays[%d]", path, j)
			r.nodes(o.Content, at+".content", on)
			if o.Kind == "dialog" {
				r.buttons(o.Actions, at+".actions", on)
			}
		}
	}
	for i, n := range m.Navigation {
		var shownOn []string
		for _, s := range m.Screens {
			if s.NavigationID == n.ID {
				shownOn = append(shownOn, s.ID)
			}
		}
		for j := range n.Items {
			path := fmt.Sprintf("navigation[%d].items[%d]", i, j)
			r.action(&n.Items[j].Action, path+".action", shownOn)
			r.roles(n.Items[j].RoleIDs, path+".roleIds")
		}
	}
}
