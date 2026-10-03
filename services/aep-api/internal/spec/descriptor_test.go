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

package spec

import (
	"testing"

	"github.com/BurntSushi/toml"
)

// The idea is free text a user typed: newlines, straight quotes, apostrophes
// and backslashes all have to survive a write→read cycle intact. This is the
// whole reason the descriptor uses a real TOML encoder instead of hand-rolled
// key writing.
func TestDescriptorRoundTripsAwkwardIdeaText(t *testing.T) {
	t.Parallel()
	idea := "A \"claims\" tracker for Ops.\nDon't lose receipts — path C:\\temp matters.\n\n[not-a-section]\nidea = \"not a key\""
	in := Descriptor{
		APIVersion: DescriptorAPIVersion,
		Name:       "expense-tracker",
		CreatedAt:  "2026-07-29T10:14:00Z",
		Idea:       idea,
	}

	raw, err := MarshalDescriptor(in)
	if err != nil {
		t.Fatalf("MarshalDescriptor: %v", err)
	}
	// The design agent reads the file; any TOML decoder must get it back.
	var got Descriptor
	if _, err := toml.Decode(string(raw), &got); err != nil {
		t.Fatalf("decode %q: %v", raw, err)
	}
	if got.Idea != idea {
		t.Fatalf("idea round-trip:\n got %q\nwant %q\nencoded as:\n%s", got.Idea, idea, raw)
	}
	if got.Name != in.Name || got.APIVersion != in.APIVersion || got.CreatedAt != in.CreatedAt {
		t.Fatalf("identity round-trip = %+v, want %+v", got, in)
	}
}

// NewDescriptor stamps the current apiVersion so callers cannot forget it —
// the field is what identifies the file as an Agentic Engineer descriptor.
func TestNewDescriptorStampsAPIVersion(t *testing.T) {
	t.Parallel()
	d := NewDescriptor("expense-tracker", "an expense tracker", "2026-07-29T10:14:00Z")
	if d.APIVersion != DescriptorAPIVersion {
		t.Fatalf("apiVersion = %q, want %q", d.APIVersion, DescriptorAPIVersion)
	}
}
