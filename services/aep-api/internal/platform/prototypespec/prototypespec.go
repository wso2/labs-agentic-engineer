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

// Package prototypespec parses and validates a web-application component's
// `specs/design/components/<component>/prototype.json`, version 1 — the
// read-only prototype the Prototype stage renders and the reviewer annotates.
//
// It is the sibling of securityspec, and for the same reason: the single
// schema definition is packages/contracts/schemas/prototype-model.schema.json
// (generated from the Zod prototypeModelSchema in @aep/prototype-model, the
// package the agent's FileBundle write gate and the console both use),
// vendored here as an embed because go:embed cannot cross the aep-api module
// boundary. Agent and BFF therefore judge ONE shape.
//
// Beyond the schema, this package owns what a standalone JSON Schema cannot
// express (references.go): one global id namespace, every reference resolving
// to an entry of the right kind on the right screen, and the component
// matching its directory. The codes and JSON paths are a contract with the
// TypeScript validator — packages/prototype-model/test/validation-cases.json
// is the table both sides assert — so a file one gate refuses, the other
// refuses with the same code at the same path.
package prototypespec

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/wso2/aep/aep-api/internal/platform/jsonschema"
)

//go:embed prototype-model.schema.json
var schemaJSON []byte

var prototypeSchema = jsonschema.MustParse(schemaJSON)

// SchemaVersion is the only version this platform reads.
const SchemaVersion = 1

// Issue codes, identical to @aep/prototype-model's PrototypeIssueCode (plus
// INVALID_JSON, which the TypeScript side raises at its gate because its parser
// takes an already-decoded value).
const (
	CodeInvalidJSON        = "INVALID_JSON"
	CodeSchemaViolation    = "SCHEMA_VIOLATION"
	CodeUnsupportedVersion = "UNSUPPORTED_VERSION"
	CodeDuplicateID        = "DUPLICATE_ID"
	CodeUnknownReference   = "UNKNOWN_REFERENCE"
	CodeComponentMismatch  = "PROTOTYPE_COMPONENT_MISMATCH"
)

// Issue is one finding: a stable code, where (a JSON path spelled
// `screens[0].content[2].id`, empty for the root), and what is wrong.
type Issue struct {
	Code    string
	Path    string
	Message string
}

// Located is the finding's message led by its JSON path, the way a per-file
// validation row reports it; just the message for a root finding.
func (i Issue) Located() string {
	if i.Path == "" {
		return i.Message
	}
	return i.Path + ": " + i.Message
}

// Model is a parsed prototype.json, version 1.
type Model struct {
	SchemaVersion   int            `json:"schemaVersion"`
	Component       string         `json:"component"`
	Name            string         `json:"name"`
	DefaultScreenID string         `json:"defaultScreenId"`
	Roles           []Role         `json:"roles"`
	States          []DisplayState `json:"states"`
	Flows           []Flow         `json:"flows"`
	Screens         []Screen       `json:"screens"`
	Navigation      []Navigation   `json:"navigation"`
}

// Role is a role the reviewer can view the prototype as.
type Role struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// DisplayState is a named presentation (default, failed, …) a reviewer can
// switch to.
type DisplayState struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Flow is a named path through the application, walked as one role.
type Flow struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	RoleID    string   `json:"roleId"`
	ScreenIDs []string `json:"screenIds"`
}

// Screen is one page of the application.
type Screen struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	RoleIDs      []string  `json:"roleIds"`
	NavigationID string    `json:"navigationId"`
	Content      []Node    `json:"content"`
	Overlays     []Overlay `json:"overlays"`
}

// Overlay is a dialog or drawer over the screen that owns it.
type Overlay struct {
	Kind    string   `json:"kind"`
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Content []Node   `json:"content"`
	Actions []Button `json:"actions"`
}

// Navigation is a side or top navigation shown by the screens that name it.
type Navigation struct {
	Kind  string           `json:"kind"`
	ID    string           `json:"id"`
	Items []NavigationItem `json:"items"`
}

// NavigationItem is one entry of a navigation.
type NavigationItem struct {
	ID      string   `json:"id"`
	Label   string   `json:"label"`
	Action  Action   `json:"action"`
	RoleIDs []string `json:"roleIds"`
}

// Action is what activating a control does; which target fields are set
// depends on Kind.
type Action struct {
	Kind      string `json:"kind"`
	ScreenID  string `json:"screenId"`
	TabsID    string `json:"tabsId"`
	TabID     string `json:"tabId"`
	StepperID string `json:"stepperId"`
	StepID    string `json:"stepId"`
	TableID   string `json:"tableId"`
	RowID     string `json:"rowId"`
	DialogID  string `json:"dialogId"`
	DrawerID  string `json:"drawerId"`
}

// Button is a button wherever one appears.
type Button struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Action Action `json:"action"`
}

// Field is one input of a form or filter bar.
type Field struct {
	ID      string   `json:"id"`
	Label   string   `json:"label"`
	ErrorIn []string `json:"errorIn"`
}

// Row is one mock record of a table or task queue.
type Row struct {
	ID     string            `json:"id"`
	Values map[string]string `json:"values"`
}

// Section is one tab of a tabs node or one step of a stepper.
type Section struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Content []Node `json:"content"`
}

// Breadcrumb is one entry of a breadcrumbs node.
type Breadcrumb struct {
	ID     string  `json:"id"`
	Label  string  `json:"label"`
	Action *Action `json:"action"`
}

// TimelineEntry is one event of a timeline node.
type TimelineEntry struct {
	ID string `json:"id"`
}

// Node is one registry node. Only the fields the platform reasons about are
// decoded; presentation (labels, tones, columns) is the console's business.
// Kind says which fields apply, exactly as in the TypeScript union.
type Node struct {
	Kind    string
	ID      string
	ShowIn  []string
	Content []Node // stack, grid
	Left    []Node // split
	Right   []Node // split
	Tabs    []Section
	Steps   []Section
	Items   []Breadcrumb
	Actions []Button // heading, form, approval-panel
	Button  *Button  // empty-state's action
	Action  *Action  // button's and link's action
	Fields  []Field  // form, filters
	Rows    []Row    // table, task-queue
	OnRow   *Action  // table, task-queue
	Entries []TimelineEntry
}

// UnmarshalJSON decodes a node. `action` is the one key whose shape depends on
// the kind: an empty-state's is a button, a button's or link's is an action.
func (n *Node) UnmarshalJSON(raw []byte) error {
	var wire struct {
		Kind    string          `json:"kind"`
		ID      string          `json:"id"`
		ShowIn  []string        `json:"showIn"`
		Content []Node          `json:"content"`
		Left    []Node          `json:"left"`
		Right   []Node          `json:"right"`
		Tabs    []Section       `json:"tabs"`
		Steps   []Section       `json:"steps"`
		Items   []Breadcrumb    `json:"items"`
		Actions []Button        `json:"actions"`
		Action  json.RawMessage `json:"action"`
		Fields  []Field         `json:"fields"`
		Rows    []Row           `json:"rows"`
		OnRow   *Action         `json:"onRow"`
		Entries []TimelineEntry `json:"entries"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return err
	}
	*n = Node{
		Kind: wire.Kind, ID: wire.ID, ShowIn: wire.ShowIn,
		Content: wire.Content, Left: wire.Left, Right: wire.Right,
		Tabs: wire.Tabs, Steps: wire.Steps, Items: wire.Items,
		Actions: wire.Actions, Fields: wire.Fields, Rows: wire.Rows,
		OnRow: wire.OnRow, Entries: wire.Entries,
	}
	if len(wire.Action) == 0 {
		return nil
	}
	if wire.Kind == "empty-state" {
		n.Button = &Button{}
		return json.Unmarshal(wire.Action, n.Button)
	}
	n.Action = &Action{}
	return json.Unmarshal(wire.Action, n.Action)
}

// BundleKey is a component's prototype slot in the design bundle (relative to
// specs/design/): the key BundleComponent reads back.
func BundleKey(component string) string {
	return "components/" + component + "/prototype.json"
}

// BundleComponent reports the component a design-bundle key (relative to
// specs/design/) is the prototype of: `components/<component>/prototype.json`.
func BundleComponent(key string) (string, bool) {
	rest, ok := strings.CutPrefix(key, "components/")
	if !ok {
		return "", false
	}
	component, ok := strings.CutSuffix(rest, "/prototype.json")
	if !ok || component == "" || component == "." || component == ".." || strings.Contains(component, "/") {
		return "", false
	}
	return component, true
}

// Parse validates raw prototype.json bytes and returns the model, or the
// issues that refuse it. It runs, stopping at the first stage that fails: JSON
// decoding, the version check, the embedded schema, then the reference checks.
// component is the directory the file lives in; when non-empty, the model's
// `component` must equal it.
func Parse(component string, raw []byte) (*Model, []Issue) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, []Issue{{Code: CodeInvalidJSON, Message: "content is not valid JSON: " + err.Error()}}
	}
	if issue := unsupportedVersion(v); issue != nil {
		return nil, []Issue{*issue}
	}
	if found := jsonschema.Check(v, prototypeSchema); len(found) > 0 {
		issues := make([]Issue, 0, len(found))
		for _, f := range found {
			issues = append(issues, Issue{Code: CodeSchemaViolation, Path: f.Path, Message: f.Message})
		}
		return nil, issues
	}
	var model Model
	if err := json.Unmarshal(raw, &model); err != nil {
		return nil, []Issue{{Code: CodeInvalidJSON, Message: err.Error()}}
	}
	if issues := referenceIssues(&model, component); len(issues) > 0 {
		return nil, issues
	}
	return &model, nil
}

// unsupportedVersion refuses a document that SAYS it is another version, in one
// issue, before the schema reports a const mismatch plus whatever else that
// version does differently.
func unsupportedVersion(v any) *Issue {
	doc, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	version, present := doc["schemaVersion"]
	if !present {
		return nil
	}
	if n, ok := version.(float64); ok && n == SchemaVersion {
		return nil
	}
	spelled, _ := json.Marshal(version)
	return &Issue{
		Code: CodeUnsupportedVersion,
		Path: "schemaVersion",
		Message: fmt.Sprintf("schemaVersion %s is not supported; this platform reads version %d",
			spelled, SchemaVersion),
	}
}
