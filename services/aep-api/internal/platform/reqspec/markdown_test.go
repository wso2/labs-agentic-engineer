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

package reqspec

import (
	"strings"
	"testing"
)

// The collab room commits markdown with its escapes: a source tag the agent
// wrote as `[org default]` arrives as `\[org default\]`. Read as markdown, it
// is the same line — and the same basis as the console reads from the room.
func TestEscapedMarkdownReadsAsItsWords(t *testing.T) {
	escaped := map[string]string{
		"product-wide.md":          "# Product-wide\n\n## Requirements\n\n- P1 Every user signs in via SSO. Applies to: all. \\[org default\\]\n",
		"features/F1-inventory.md": "# Borrow &amp; Return\n\n## User Stories\n\n- F1.1 As a manager, I add an item \\(name, tag\\).\\\n",
	}
	plain := map[string]string{
		"product-wide.md":          "# Product-wide\n\n## Requirements\n\n- P1 Every user signs in via SSO. Applies to: all. [org default]\n",
		"features/F1-inventory.md": "# Borrow & Return\n\n## User Stories\n\n- F1.1 As a manager, I add an item (name, tag).\n",
	}
	got, want := Parse(escaped), Parse(plain)
	if got.ProductWide[0].Text != want.ProductWide[0].Text || strings.Join(got.ProductWide[0].AppliesTo, ",") != "all" {
		t.Errorf("P1 = %+v, want %+v", got.ProductWide[0], want.ProductWide[0])
	}
	if got.Features[0].Name != "Borrow & Return" {
		t.Errorf("name = %q, want the entity read as its character", got.Features[0].Name)
	}
	if Basis(escaped, "F1") != Basis(plain, "F1") {
		t.Errorf("basis\n%q\nwant\n%q", Basis(escaped, "F1"), Basis(plain, "F1"))
	}
}
