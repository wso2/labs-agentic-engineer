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

package securityspec

// references.go — the referential rules of security.json v2: everything a
// standalone JSON Schema cannot express, because it is a statement ABOUT two
// places in the document, or about the document and a sibling spec file.
//
// It is the Go twin of packages/agent-stream/src/security-design-references.ts,
// rule for rule and message for message (the wording comes from the vendored
// table both sides read — messages.go). A document the agent's write gate
// accepts is one this gate accepts.
//
// Three kinds of rule, in the order they are checked:
//
//  1. **Within the document.** A grant names a catalog handle; a test user
//     names a declared admin-enrolment user role; a role name is not a group
//     name. These need nothing but the parsed document, so Parse runs them on
//     every read of the file.
//  2. **Against a sibling the bundle may hold.** A permission's component and a
//     screen's component are nodes of design.cell; a screen name exists in that
//     component's wireframes.dsl. The design lineup writes security.json before
//     some of those files exist, so a rule whose input is missing is SKIPPED IN
//     SILENCE — the build gate re-runs the whole list against the tag, where
//     every file is present by construction.
//  3. **The reachability cross-check.** For every role, the operation behind
//     every screen that role can reach must be granted by that role. This is
//     the rule the design's own Expense Tracker example fails, and on the
//     pinned gateway the failure is a bare 401 the SPA cannot tell from an
//     expired session — an infinite sign-in loop. Caught here it is one
//     sentence at authoring time.
//
// Nothing implies anything: the gateway compares scopes whole-string, so
// `claims:read-all` does not admit a caller to an operation guarded on
// `claims:read`. Rather than widen grants silently — which would make the gate
// disagree with the runtime — a role holding `X:read-all` without `X:read` is
// an error naming both (ADR-0030).

import (
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

// Finding severities. Only SeverityError refuses a write or a build.
const (
	SeverityError   = "error"
	SeverityWarning = "warning"
	SeverityInfo    = "info"
)

// Finding is one thing the referential gate has to say about a document.
type Finding struct {
	Severity string
	// Key is the message-catalog key — the stable, cross-language name of the
	// rule, which is what a caller keys on rather than the sentence.
	Key string
	// Params are the slots the template was rendered with, for a caller that
	// re-renders (the console) or filters (the build gate).
	Params map[string]string
	// Message is the rendered sentence.
	Message string
}

// FileSource reads sibling spec files by REPO-RELATIVE path
// (`specs/design/components/<id>/openapi.yaml`), the same paths the agent's
// FileBundle uses, so the two rule sets read the same names.
type FileSource interface {
	Read(path string) (string, bool)
}

// DesignBundle adapts a design bundle — the map the spec domain passes around,
// whose keys are relative to `specs/design/` — to FileSource.
type DesignBundle map[string]string

// Read implements FileSource for the design bundle.
func (b DesignBundle) Read(path string) (string, bool) {
	rel, ok := strings.CutPrefix(path, "specs/design/")
	if !ok {
		return "", false
	}
	content, ok := b[rel]
	return content, ok
}

const designCellPath = "specs/design/design.cell"

func componentDir(component string) string {
	return "specs/design/components/" + component
}

// readOpenAPI returns a component's spec, `.yaml` or `.yml`.
func readOpenAPI(src FileSource, component string) (string, bool) {
	if content, ok := src.Read(componentDir(component) + "/openapi.yaml"); ok {
		return content, true
	}
	return src.Read(componentDir(component) + "/openapi.yml")
}

// cellComponentRE matches a component node of design.cell. The full cell
// grammar lives in internal/spec; only the node IDs are needed here, and
// reading them with one line keeps this package free of that dependency.
var cellComponentRE = regexp.MustCompile(`^component\s+(\S+)`)

// cellComponents returns the component ids design.cell declares, or nil when
// the cell is not in the bundle (the rule is then skipped).
func cellComponents(src FileSource) map[string]bool {
	source, ok := src.Read(designCellPath)
	if !ok || strings.TrimSpace(source) == "" {
		return nil
	}
	ids := map[string]bool{}
	for _, raw := range strings.Split(source, "\n") {
		text := strings.TrimSpace(raw)
		if text == "" || strings.HasPrefix(text, "#") || strings.HasPrefix(text, "//") || strings.Contains(text, "->") {
			continue
		}
		if m := cellComponentRE.FindStringSubmatch(text); m != nil {
			ids[m[1]] = true
		}
	}
	return ids
}

// dslScreenRE is the wireframe grammar's screen declaration, mirroring
// packages/excalidraw-dsl/src/index.ts: `screen MyClaims "A description" 800x600`,
// where the description and the WxH are optional and NOTHING else may follow.
// The whole line is anchored on purpose — `screen Foo bar baz` is not a screen
// declaration to the compiler, so a looser pattern here would invent a screen
// the wireframe does not have and then refuse the security document for citing
// a different one.
var dslScreenRE = regexp.MustCompile(`(?i)^screen[ \t]+([\w-]+)(?:[ \t]+"(?:[^"\\]|\\.)*")?(?:[ \t]+\d+[ \t]*x[ \t]*\d+)?$`)

// wireframeScreens returns the screen names a wireframes.dsl declares,
// normalized, or nil when the file yields none. It reads the declarations
// rather than compiling the DSL — the compiler lives in a TypeScript package,
// and the only fact needed here is which names exist.
//
// Nil means UNREADABLE, not "no screens": a wireframes.dsl with no declaration
// this grammar recognizes is one this package cannot judge, and the caller must
// skip the screen rule rather than refuse every screen the document cites. It
// is the same verdict the agent side reaches when compileWireframes fails
// (packages/agent-stream/src/security-design-references.ts).
func wireframeScreens(dsl string) map[string]bool {
	var names map[string]bool
	for _, raw := range strings.Split(dsl, "\n") {
		// A declaration is legal only at indent level 0, so any leading
		// whitespace disqualifies the line; trailing whitespace is stripped
		// before the match, as the compiler strips it.
		line := strings.TrimRight(raw, " \t\r")
		if line == "" || line != strings.TrimLeft(line, " \t") {
			continue
		}
		m := dslScreenRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		if names == nil {
			names = map[string]bool{}
		}
		names[NormalizeScreenName(m[1])] = true
	}
	return names
}

// httpMethods are the OpenAPI operation keys of a path item.
var httpMethods = []string{"get", "put", "post", "delete", "options", "head", "patch", "trace"}

// safeMethods only READ. A screen cannot render without one.
var safeMethods = map[string]bool{"get": true, "head": true}

// specOperation is one operation of one component spec, reduced to what
// authorization cares about.
type specOperation struct {
	Method string
	Path   string
	// Scope is the single handle the operation requires, "" for "any signed-in
	// caller".
	Scope string
	// Public is true for `security: []` — no token at all.
	Public bool
}

// operationSecurity reads the one scope an OpenAPI `security` value names, in
// the three shapes the design allows: absent (inherit the document default),
// `[]` (public), or one requirement object with one scope.
//
// Anything else is the openapi.yaml gate's business and is read here as
// leniently as possible, so a malformed spec produces ONE message from ONE gate.
func operationSecurity(value any) (op specOperation, ok bool) {
	list, isList := value.([]any)
	if !isList {
		return specOperation{}, false
	}
	if len(list) == 0 {
		return specOperation{Public: true}, true
	}
	first, isMap := list[0].(map[string]any)
	if !isMap {
		return specOperation{}, false
	}
	for _, scopes := range first {
		list, isList := scopes.([]any)
		if !isList || len(list) == 0 {
			return specOperation{}, true
		}
		if scope, isString := list[0].(string); isString {
			return specOperation{Scope: scope}, true
		}
		return specOperation{}, true
	}
	return specOperation{}, true
}

// specOperations returns every operation a component spec declares with its
// effective security, or nil when the spec does not parse (the openapi gate
// owns saying so).
func specOperations(source string) []specOperation {
	var doc any
	if err := yaml.Unmarshal([]byte(source), &doc); err != nil {
		return nil
	}
	root, ok := doc.(map[string]any)
	if !ok {
		return nil
	}
	paths, ok := root["paths"].(map[string]any)
	if !ok {
		return nil
	}
	documentDefault, _ := operationSecurity(root["security"])

	var out []specOperation
	for path, item := range paths {
		pathItem, ok := item.(map[string]any)
		if !ok {
			continue
		}
		for _, method := range httpMethods {
			operation, ok := pathItem[method].(map[string]any)
			if !ok {
				continue
			}
			effective := documentDefault
			if raw, declared := operation["security"]; declared {
				if own, read := operationSecurity(raw); read {
					effective = own
				}
			}
			effective.Method, effective.Path = method, path
			out = append(out, effective)
		}
	}
	// yaml maps iterate in a random order; the findings must not.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Method < out[j].Method
	})
	return out
}

// findings accumulates rendered findings in rule order.
type findings struct{ all []Finding }

func (f *findings) add(severity, key string, kv ...string) {
	params := make(map[string]string, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		params[kv[i]] = kv[i+1]
	}
	f.all = append(f.all, Finding{
		Severity: severity, Key: key, Params: params, Message: Msg(key, kv...),
	})
}

// resourceOf is the resource half of a `<resource>:<action>` handle.
func resourceOf(handle string) string {
	resource, _, _ := strings.Cut(handle, ":")
	return resource
}

func isReadAll(handle string) bool { return strings.HasSuffix(handle, ":read-all") }

// validRoleName reports whether a role name is made only of letters, digits,
// spaces, "-", "_" and ".".
//
// The TS copy is `ROLE_NAME` in
// `packages/agent-stream/src/security-design-references.ts`; the two gates must
// refuse the same documents, so a change here is a change there.
func validRoleName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
		case r == ' ', r == '.', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

// ReferenceFindings returns every referential finding for doc, in rule order.
//
// src is optional: with none, only the rules that read the document alone run
// and the rest are skipped in silence — which is what lets the agent's
// per-write gate and the platform's whole-bundle build gate share one rule set.
func ReferenceFindings(doc *Document, src FileSource) []Finding {
	found := &findings{}
	catalog := map[string]bool{}
	for _, handle := range CatalogHandles(doc) {
		catalog[handle] = true
	}

	// --- the catalog --------------------------------------------------------
	seenResources := map[string]bool{}
	ownerOf := map[string]string{} // resource → the component that owns it
	for _, permission := range doc.Permissions {
		if seenResources[permission.Resource] {
			found.add(SeverityError, MsgDuplicateResource, "resource", permission.Resource)
			continue
		}
		seenResources[permission.Resource] = true
		ownerOf[permission.Resource] = permission.Component
		seenActions := map[string]bool{}
		for _, action := range permission.Actions {
			// Uniqueness is PER RESOURCE, not per resource server: Thunder has
			// no cross-resource rule, and `claims:read` and `reports:read`
			// legally coexist.
			if seenActions[action.Handle] {
				found.add(SeverityError, MsgDuplicateAction, "resource", permission.Resource, "handle", action.Handle)
				continue
			}
			seenActions[action.Handle] = true
		}
	}

	var cellIDs map[string]bool
	if src != nil {
		cellIDs = cellComponents(src)
	}
	if cellIDs != nil {
		for _, permission := range doc.Permissions {
			if !cellIDs[permission.Component] {
				found.add(SeverityError, MsgResourceComponentUnknown,
					"resource", permission.Resource, "component", permission.Component)
			}
		}
	}

	// --- roles --------------------------------------------------------------
	groupNames := map[string]bool{}
	groupNamesLower := map[string]bool{}
	for _, group := range doc.Groups {
		groupNames[group.Name] = true
		groupNamesLower[strings.ToLower(group.Name)] = true
	}
	declaredRoles := map[string]bool{}
	for _, role := range doc.Roles {
		key := strings.ToLower(role.Name)
		if declaredRoles[key] {
			found.add(SeverityError, MsgDuplicateRoleName, "role", role.Name)
			continue
		}
		declaredRoles[key] = true
		if role.Name != strings.TrimSpace(role.Name) {
			found.add(SeverityError, MsgRoleNameWhitespace, "role", role.Name)
		}
		// The charset. A role name travels into two places that cannot escape
		// it: the directory, where it becomes `<project>/<Role>` and the "/" is
		// the platform's own ownership separator, and the build ticket's
		// markdown table, where a "|" or a line break would break the row the
		// validation agent parses. Everything a PRD actor noun needs is still
		// allowed.
		if !validRoleName(strings.TrimSpace(role.Name)) {
			found.add(SeverityError, MsgRoleNameInvalid, "role", role.Name)
		}
		// A reused org group is NOT redeclared in groups[], so this rule can
		// only judge the names THIS document introduces — the directory check
		// at the build gate catches a collision with a group somebody else made.
		if groupNamesLower[key] {
			found.add(SeverityError, MsgRoleNameIsGroupName, "role", role.Name)
		}
		grants := map[string]bool{}
		for _, handle := range role.Grants {
			grants[handle] = true
			if !catalog[handle] {
				found.add(SeverityError, MsgGrantUnknownHandle, "role", role.Name, "handle", handle)
			}
		}
		// The "all" handle widens the ROWS a caller may see; it does not replace
		// the operation's own handle, and the gateway compares scopes
		// whole-string (ADR-0030).
		for _, handle := range role.Grants {
			if !isReadAll(handle) {
				continue
			}
			read := resourceOf(handle) + ":read"
			if !catalog[read] || grants[read] {
				continue
			}
			found.add(SeverityError, MsgReadAllWithoutRead,
				"role", role.Name, "allHandle", handle, "readHandle", read)
		}
	}

	for _, role := range doc.Roles {
		for _, ref := range role.AssignableBy {
			if !declaredRoles[strings.ToLower(ref)] {
				found.add(SeverityError, MsgAssignableByUnknownRole, "role", role.Name, "ref", ref)
			}
		}
		switch {
		case role.RoleKind() == KindService && len(role.AssignTo) > 0:
			found.add(SeverityError, MsgNonAdminRoleHasAssignTo, "role", role.Name, "kind", KindService)
		case role.RoleKind() == KindUser && role.EnrolmentKind() == EnrolmentSelfService && len(role.AssignTo) > 0:
			found.add(SeverityError, MsgNonAdminRoleHasAssignTo, "role", role.Name, "kind", EnrolmentSelfService)
		case role.RoleKind() == KindUser && role.EnrolmentKind() == EnrolmentAdmin && len(role.AssignTo) == 0:
			found.add(SeverityError, MsgAdminRoleNeedsAssignTo, "role", role.Name)
		}
		for _, group := range role.AssignTo {
			// Legal, and recorded rather than refused: a group the org directory
			// already holds is deliberately not redeclared here. The build's
			// ensure resolves it against the directory.
			if !groupNames[group] {
				found.add(SeverityInfo, MsgAssignToDirectoryChecked, "role", role.Name, "group", group)
			}
		}
	}

	// --- test users ---------------------------------------------------------
	roleByLowerName := make(map[string]Role, len(doc.Roles))
	for _, role := range doc.Roles {
		roleByLowerName[strings.ToLower(role.Name)] = role
	}
	seenUsers := map[string]bool{}
	for _, user := range doc.TestUsers {
		if !usernameRE.MatchString(user.Username) {
			found.add(SeverityError, MsgInvalidTestUsername, "username", user.Username)
			continue
		}
		if seenUsers[user.Username] {
			found.add(SeverityError, MsgDuplicateTestUser, "username", user.Username)
			continue
		}
		seenUsers[user.Username] = true
		for _, roleName := range user.Roles {
			role, declared := roleByLowerName[strings.ToLower(roleName)]
			if !declared {
				found.add(SeverityError, MsgTestUserUnknownRole, "username", user.Username, "role", roleName)
				continue
			}
			if !role.NeedsTestUser() {
				found.add(SeverityError, MsgTestUserRoleNotUserKind, "username", user.Username, "role", roleName)
			}
		}
	}

	// --- screens ------------------------------------------------------------
	for _, screen := range doc.Screens {
		if cellIDs != nil && !cellIDs[screen.Component] {
			found.add(SeverityError, MsgScreenComponentUnknown,
				"component", screen.Component, "screen", screen.Screen)
			continue
		}
		if handle, required := screen.RequiresHandle(); required && !catalog[handle] {
			found.add(SeverityError, MsgScreenRequiresUnknownHandle,
				"component", screen.Component, "screen", screen.Screen, "handle", handle)
		}
		if src == nil {
			continue
		}
		dsl, ok := src.Read(componentDir(screen.Component) + "/wireframes.dsl")
		if !ok || strings.TrimSpace(dsl) == "" {
			continue
		}
		declared := wireframeScreens(dsl)
		if declared == nil {
			continue // unreadable wireframes.dsl — the rule has no ground truth
		}
		if !declared[NormalizeScreenName(screen.Screen)] {
			found.add(SeverityError, MsgScreenUnknown,
				"component", screen.Component, "screen", screen.Screen)
		}
	}

	if src != nil {
		reachabilityRules(doc, src, catalog, ownerOf, found)
	}
	return found.all
}

// reachabilityRules are the rules that need the component specs: the
// screen→operation cross-check and the two catalog-coverage warnings.
//
// # How a screen's operations are derived
//
// The wireframes DSL has no screen→operation binding — its grammar is screens,
// elements and flows — so the operation set is derived through the CATALOG
// instead, from two facts the document does state:
//
//   - a screen gated on `<resource>:<action>` RENDERS that resource, and
//   - the component that owns the resource declares the operations on it.
//
// So the operation a screen cannot avoid calling is the resource's own read:
// the SAFE (GET/HEAD) operation guarded on `<resource>:read` in the owning
// component's spec. If the role that reaches the screen cannot call that, the
// page loads into a 401 — exactly the class the design's example broke on.
//
// The rule deliberately stops there rather than demanding every scoped operation
// of the resource: `GET /reports/export` guarded on `reports:export` sits behind
// a button the page can hide, while the list GET runs on load, so requiring the
// whole set would refuse a correct document. A screen requiring null names no
// resource and has no derivable operation set; a public screen is outside
// authorization entirely. Both are skipped, and a per-screen binding — if the
// DSL ever grows one — replaces this derivation without moving the rule.
//
// This mirrors reachabilityRules in the agent's security-design-references.ts.
func reachabilityRules(doc *Document, src FileSource, catalog map[string]bool, ownerOf map[string]string, found *findings) {
	// Owner components only: a resource is declared by the component that owns
	// it, so those are the specs the catalog can be judged against.
	owners := make([]string, 0, len(ownerOf))
	for _, component := range ownerOf {
		if !slices.Contains(owners, component) {
			owners = append(owners, component)
		}
	}
	sort.Strings(owners)
	operationsByComponent := map[string][]specOperation{}
	for _, component := range owners {
		source, ok := readOpenAPI(src, component)
		if !ok {
			continue
		}
		if operations := specOperations(source); operations != nil {
			operationsByComponent[component] = operations
		}
	}
	if len(operationsByComponent) == 0 {
		return
	}

	// --- the reachability cross-check ---------------------------------------
	for _, screen := range doc.Screens {
		requires, required := screen.RequiresHandle()
		if !required || !catalog[requires] {
			continue
		}
		owner, known := ownerOf[resourceOf(requires)]
		if !known {
			continue
		}
		operations, present := operationsByComponent[owner]
		if !present {
			continue
		}
		// The one operation the page cannot avoid: the resource's read, if some
		// safe operation of the owner is guarded on it.
		readHandle := resourceOf(requires) + ":read"
		var loads bool
		for _, op := range operations {
			if !op.Public && op.Scope == readHandle && safeMethods[op.Method] {
				loads = true
				break
			}
		}
		if !loads {
			continue
		}
		for _, role := range doc.Roles {
			// A service role holds an app principal's token; it reaches no screen.
			if role.RoleKind() != KindUser {
				continue
			}
			if !slices.Contains(role.Grants, requires) || slices.Contains(role.Grants, readHandle) {
				continue
			}
			found.add(SeverityError, MsgScreenOperationNotGranted,
				"role", role.Name, "screen", screen.Screen, "handle", readHandle)
		}
	}

	// --- the two coverage warnings ------------------------------------------
	requiredByOperations := map[string]bool{}
	for _, operations := range operationsByComponent {
		for _, op := range operations {
			if op.Scope != "" {
				requiredByOperations[op.Scope] = true
			}
		}
	}
	requiredByScreens := map[string]bool{}
	for _, screen := range doc.Screens {
		if handle, required := screen.RequiresHandle(); required {
			requiredByScreens[handle] = true
		}
	}
	granted := map[string]bool{}
	for _, role := range doc.Roles {
		for _, handle := range role.Grants {
			granted[handle] = true
		}
	}
	for _, handle := range CatalogHandles(doc) {
		// `X:read-all` is a ROW widener, not an operation guard: the design's own
		// example puts it on no operation and no screen, and the service reads it
		// off the token while serving `X:read`. Warning about it every time would
		// make this line noise, so it only fires when even `X:read` is unused.
		widensAUsedRead := isReadAll(handle) &&
			(requiredByOperations[resourceOf(handle)+":read"] || requiredByScreens[resourceOf(handle)+":read"])
		if !requiredByOperations[handle] && !requiredByScreens[handle] && !widensAUsedRead {
			found.add(SeverityWarning, MsgHandleUsedNowhere, "handle", handle)
		}
		if requiredByOperations[handle] && !granted[handle] {
			found.add(SeverityWarning, MsgHandleUnreachable, "handle", handle)
		}
	}
}

// FirstError is the first blocking finding's message, or "" when there is none.
//
// The write gate wants ONE sentence: the model fixes one thing per round trip,
// and a list of twenty would be spent re-reading. Callers that want the whole
// picture (the console's Security page, the build gate's warnings) read
// ReferenceFindings.
func FirstError(found []Finding) string {
	for _, finding := range found {
		if finding.Severity == SeverityError {
			return finding.Message
		}
	}
	return ""
}

// checkReferences is what Parse applies: the document-only rules, first error.
func checkReferences(doc *Document) string {
	return FirstError(ReferenceFindings(doc, nil))
}

// NormalizeScreenName is how a screen name in security.json is compared with a
// `screen <Name>` declaration in wireframes.dsl: everything that is not a
// letter or a digit dropped, the rest lowercased. `"My Claims"` and
// `screen MyClaims` are the same screen — the DSL grammar takes no space in a
// name and the design file is written for a reader, so neither format changes
// and the comparison absorbs the difference.
func NormalizeScreenName(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}
