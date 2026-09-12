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

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
)

// tsMessages is the agent-side copy — the source of truth for the WORDING of
// every security-design refusal. securityspec → platform → internal → aep-api →
// services → repo root.
const tsMessages = "../../../../../packages/agent-stream/src/security-design-messages.json"

// tsOpenapiMessages is the same for the openapi.yaml SECURITY gate (task 1.6),
// whose sentences the agent authors in a second file next to the first.
const tsOpenapiMessages = "../../../../../packages/agent-stream/src/openapi-security-messages.json"

// TestVendoredMessagesMatchTheAgent is the anti-drift guard for the message
// table, the twin of TestVendoredSchemaMatchesContracts. Agent and platform
// refuse the same documents, and a refusal the model reads in two wordings is
// two rules as far as the model is concerned — so the text lives in one file
// and this test proves the vendored copy still is that file.
//
// Re-sync on failure: copy
// packages/agent-stream/src/security-design-messages.json over this package's
// security-design-messages.json (or the other way, once the platform's wording
// is the one that changed — but never let them differ).
func TestVendoredMessagesMatchTheAgent(t *testing.T) {
	for _, table := range []struct{ agent, vendored string }{
		{tsMessages, "security-design-messages.json"},
		{tsOpenapiMessages, "openapi-security-messages.json"},
	} {
		t.Run(table.vendored, func(t *testing.T) {
			assertSameMessageTable(t, table.agent, table.vendored)
		})
	}
}

// assertSameMessageTable compares one vendored artifact with the agent's copy.
func assertSameMessageTable(t *testing.T, agentPath, vendoredPath string) {
	t.Helper()
	want, err := os.ReadFile(agentPath)
	if err != nil {
		t.Fatalf("read agent message table (%s) — layout drift?: %v", agentPath, err)
	}
	got, err := os.ReadFile(vendoredPath)
	if err != nil {
		t.Fatalf("read vendored message table: %v", err)
	}
	// Compared as PARSED tables, not bytes: the two files are written by
	// different tool-chains and neither side's formatter should be able to fail
	// the build. What must not differ is a key or a sentence.
	var gotTable, wantTable map[string]string
	if err := json.Unmarshal(got, &gotTable); err != nil {
		t.Fatalf("vendored message table does not parse: %v", err)
	}
	if err := json.Unmarshal(want, &wantTable); err != nil {
		t.Fatalf("agent message table does not parse: %v", err)
	}
	for key, wantText := range wantTable {
		if gotText, ok := gotTable[key]; !ok {
			t.Errorf("message %q exists in the agent's table and not in the vendored one", key)
		} else if gotText != wantText {
			t.Errorf("message %q differs:\n  agent:    %s\n  vendored: %s", key, wantText, gotText)
		}
	}
	for key := range gotTable {
		if _, ok := wantTable[key]; !ok {
			t.Errorf("message %q exists in the vendored table and not in the agent's", key)
		}
	}
}

// TestEveryMessageKeyIsDeclaredAndUsed closes both drift directions inside Go: a
// check phrased from a key the table does not hold would render the
// "(no message text)" fallback into a build refusal, and a key nothing renders
// is dead wording that the next editor would keep in step for no reason.
func TestEveryMessageKeyIsDeclaredAndUsed(t *testing.T) {
	for _, key := range MessageKeys {
		if _, ok := messages[key]; !ok {
			t.Errorf("message key %q is used by the Go checks but the vendored table has no text for it", key)
		}
	}
	for key := range messages {
		if !slices.Contains(MessageKeys, key) {
			t.Errorf("message key %q is in the vendored table but no Go check renders it — add it to MessageKeys or drop it", key)
		}
	}
}

// TestMessagesRenderEveryPlaceholder pins the other half of a message: a
// template whose placeholder nobody fills reaches the model as a literal
// `{role}`, which reads as a platform defect rather than as a rule.
func TestMessagesRenderEveryPlaceholder(t *testing.T) {
	// Every placeholder each key is documented to take, from messages.go.
	byKey := map[string][]string{
		MsgGrantUnknownHandle:          {"handle", "role"},
		MsgAssignableByUnknownRole:     {"ref", "role"},
		MsgAdminRoleNeedsAssignTo:      {"role"},
		MsgNonAdminRoleHasAssignTo:     {"kind", "role"},
		MsgTestUserUnknownRole:         {"role", "username"},
		MsgTestUserRoleNotUserKind:     {"role", "username"},
		MsgDuplicateTestUser:           {"username"},
		MsgDuplicateResource:           {"resource"},
		MsgDuplicateAction:             {"handle", "resource"},
		MsgResourceComponentUnknown:    {"component", "resource"},
		MsgInvalidTestUsername:         {"username"},
		MsgRoleNameWhitespace:          {"role"},
		MsgRoleNameInvalid:             {"role"},
		MsgAssignToDirectoryChecked:    {"group", "role"},
		MsgDuplicateRoleName:           {"role"},
		MsgRoleNameIsGroupName:         {"role"},
		MsgScreenComponentUnknown:      {"component", "screen"},
		MsgScreenUnknown:               {"component", "screen"},
		MsgScreenRequiresUnknownHandle: {"component", "handle", "screen"},
		MsgScreenOperationNotGranted:   {"handle", "role", "screen"},
		MsgReadAllWithoutRead:          {"allHandle", "readHandle", "role"},
		MsgV1Document:                  {"fields"},
		MsgHandleUsedNowhere:           {"handle"},
		MsgHandleUnreachable:           {"handle"},

		// The openapi.yaml security gate (task 1.6).
		MsgMissingOAuth2Scheme:                   {"component"},
		MsgSchemeWrongType:                       {"component", "type"},
		MsgMissingDocumentSecurity:               {"component"},
		MsgSchemeWithoutDependency:               {"component"},
		MsgDocumentSecurityWithoutDependency:     {"component"},
		MsgSecurityWithoutDependency:             {"component", "method", "path"},
		MsgOperationSecurityNotAList:             {"method", "path"},
		MsgOperationMultipleRequirements:         {"method", "path"},
		MsgOperationMultipleScopes:               {"method", "path"},
		MsgOperationUnknownScheme:                {"method", "path", "scheme"},
		MsgScopeNotInCatalog:                     {"method", "path", "scope"},
		MsgScopeNotOwned:                         {"method", "owner", "path", "scope"},
		MsgFlowScopeNotInCatalog:                 {"scope"},
		MsgFlowScopeNotOwned:                     {"owner", "scope"},
		MsgReservedOIDCScope:                     {"scope", "where"},
		MsgIdentityHeaderRequired:                {"header", "method", "path"},
		MsgPublicOperationDeclaresIdentityHeader: {"header", "method", "path"},
	}
	for _, key := range MessageKeys {
		want, documented := byKey[key]
		if !documented {
			t.Errorf("message key %q has no documented placeholder list — add it here and to messages.go", key)
			continue
		}
		if got := MessagePlaceholders(key); !slices.Equal(got, want) {
			t.Errorf("message %q carries placeholders %v, documented as %v", key, got, want)
		}
		// Rendered with its own placeholder names as values, no documented slot
		// is left behind. (Prose braces — the v1 refusal quotes v2's own shape —
		// are not slots and are left alone.)
		var kv []string
		for _, name := range want {
			kv = append(kv, name, "«"+name+"»")
		}
		rendered := Msg(key, kv...)
		for _, name := range want {
			if strings.Contains(rendered, "{"+name+"}") {
				t.Errorf("message %q still carries {%s} after rendering: %s", key, name, rendered)
			}
		}
	}
}
