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

package genaiturns

import (
	"testing"

	"github.com/wso2/aep/aep-api/internal/gen"
)

func anchorFixture() gen.TurnAnchor {
	return gen.TurnAnchor{
		File: "specs/requirements/PRD.md",
		Nodes: []gen.TurnAnchorNode{
			{Name: "Rounds close automatically", Kind: "paragraph", Context: "Product Decisions"},
		},
	}
}

// An ordinary chat turn must reach the agents service byte-identical to one
// sent before aiming existed — nil, not an empty block.
func TestAimAbsentWhenNeitherFieldSent(t *testing.T) {
	aim, err := aimFromJSON(gen.TurnAnchor{}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if aim != nil {
		t.Fatalf("expected no aim, got %+v", aim)
	}
}

// Both or neither. An intent with nothing to point at says nothing, and an
// anchor with no intent leaves the agents service guessing which preamble to
// render — rejecting here keeps that guess from ever existing.
func TestAimHalfSetIsRejected(t *testing.T) {
	if _, err := aimFromJSON(anchorFixture(), ""); err == nil {
		t.Fatal("anchor without intent should be rejected")
	}
	if _, err := aimFromJSON(gen.TurnAnchor{}, "change"); err == nil {
		t.Fatal("intent without anchor should be rejected")
	}
}

func TestAimCarriesEveryNode(t *testing.T) {
	anchor := anchorFixture()
	anchor.Nodes = append(anchor.Nodes, gen.TurnAnchorNode{Name: "One message per round", Kind: "list item"})
	aim, err := aimFromJSON(anchor, "discuss")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if aim.Intent != "discuss" || aim.Anchor.File != anchor.File {
		t.Fatalf("aim did not survive conversion: %+v", aim)
	}
	if len(aim.Anchor.Nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(aim.Anchor.Nodes))
	}
	// A name that stands alone carries no context, and must not grow a blank one.
	if aim.Anchor.Nodes[1].Context != "" {
		t.Fatalf("expected no context, got %q", aim.Anchor.Nodes[1].Context)
	}
}

func TestAimRejectsUnknownIntent(t *testing.T) {
	if _, err := aimFromJSON(anchorFixture(), "rewrite"); err == nil {
		t.Fatal("an intent outside change|discuss should be rejected")
	}
}

func TestAimRejectsIncompleteNode(t *testing.T) {
	anchor := anchorFixture()
	anchor.Nodes = append(anchor.Nodes, gen.TurnAnchorNode{Name: "no kind"})
	if _, err := aimFromJSON(anchor, "change"); err == nil {
		t.Fatal("a node without a kind should be rejected")
	}
}

func TestAimRejectsEmptyNodeList(t *testing.T) {
	if _, err := aimFromJSON(gen.TurnAnchor{File: "a.md"}, "change"); err == nil {
		t.Fatal("an anchor with no nodes should be rejected")
	}
}

// Past this many nodes the user is pointing at the document, not a passage, and
// the preamble stops naming anything the agent can act on.
func TestAimRejectsAnUnboundedSelection(t *testing.T) {
	anchor := gen.TurnAnchor{File: "a.md"}
	for i := 0; i <= maxAnchorNodes; i++ {
		anchor.Nodes = append(anchor.Nodes, gen.TurnAnchorNode{Name: "n", Kind: "paragraph"})
	}
	if _, err := aimFromJSON(anchor, "change"); err == nil {
		t.Fatal("a selection past the node cap should be rejected")
	}
}

// The anchor locates and never carries (console ADR-0024): a hand-built
// request must not smuggle a prompt payload through the locator fields the
// console keeps short by construction.
func TestAimRejectsOversizedFields(t *testing.T) {
	long := func(n int) string {
		b := make([]byte, n)
		for i := range b {
			b[i] = 'x'
		}
		return string(b)
	}
	oversized := []gen.TurnAnchor{
		{File: long(513), Nodes: []gen.TurnAnchorNode{{Name: "n", Kind: "paragraph"}}},
		{File: "a.md", Nodes: []gen.TurnAnchorNode{{Name: long(201), Kind: "paragraph"}}},
		{File: "a.md", Nodes: []gen.TurnAnchorNode{{Name: "n", Kind: long(65)}}},
		{File: "a.md", Nodes: []gen.TurnAnchorNode{{Name: "n", Kind: "paragraph", Context: long(513)}}},
	}
	for i, anchor := range oversized {
		if _, err := aimFromJSON(anchor, "change"); err == nil {
			t.Fatalf("case %d: an oversized anchor field should be rejected", i)
		}
	}
	// At the ceiling is fine — the cap is a ceiling, not a headroom rule.
	atLimit := gen.TurnAnchor{File: "a.md", Nodes: []gen.TurnAnchorNode{{Name: long(200), Kind: "paragraph"}}}
	if _, err := aimFromJSON(atLimit, "change"); err != nil {
		t.Fatalf("an anchor at the ceiling should pass: %v", err)
	}
}

// The multipart form declares this part application/json: a nested object has
// no scalar form, unlike `collab`, which rides as the string "true".
func TestAnchorFieldDecodesTheJSONPart(t *testing.T) {
	anchor, err := parseAnchorField(`{"file":"a.md","nodes":[{"name":"n","kind":"paragraph"}]}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if anchor.File != "a.md" || len(anchor.Nodes) != 1 {
		t.Fatalf("anchor did not decode: %+v", anchor)
	}
}

func TestAnchorFieldRejectsMalformedJSON(t *testing.T) {
	if _, err := parseAnchorField("{not json"); err == nil {
		t.Fatal("malformed anchor JSON should be rejected")
	}
}

// An absent part is absent, not an error: a multipart send with attachments and
// no selection is an ordinary turn.
func TestAnchorFieldAbsentIsNotAnError(t *testing.T) {
	anchor, err := parseAnchorField("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if anchor.File != "" || len(anchor.Nodes) != 0 {
		t.Fatalf("expected a zero anchor, got %+v", anchor)
	}
}
