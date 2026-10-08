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
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// A requirements file is user-written and aep-api reads every file on each
// basis (several per request), so reading a line must stay linear in its
// length however its markup is arranged. Each line here is >= 200 KB of a
// pattern that makes a naive reader rescan the line per delimiter or bracket
// (seconds, measured); a linear one reads it in milliseconds. The bound
// is 500 ms: ~50x the linear time, so host load does not flake it, and still
// well under the seconds a quadratic read takes.
func TestReadInline_IsLinearOnAdversarialLines(t *testing.T) {
	const size = 200 << 10
	repeat := func(unit string) string { return strings.Repeat(unit, size/len(unit)+1) }
	nested := strings.Repeat("[", size/5) + "a" + strings.Repeat("](u)", size/5)
	lines := map[string]string{
		"unpaired emphasis":  repeat("a* "),
		"unpaired underline": repeat("a_ b"),
		"unclosed links":     repeat("[x]("),
		"nested links":       nested,
		"open brackets":      repeat("[a "),
		"unclosed code":      repeat("` "),
		"code in brackets":   repeat("[` "),
		"strike runs":        repeat("a~ "),
		"autolink starts":    repeat("<a:b"),
	}
	for name, line := range lines {
		start := time.Now()
		readLine(line)
		if took := time.Since(start); took > 500*time.Millisecond {
			t.Errorf("%s (%d KB): read in %v, want linear time", name, len(line)>>10, took)
		}
	}
}

// The cases the console is held to as well (console spec/collab/
// inlineMarkup.test.ts): each line's text is what the collab room's parser
// makes of it, and the closing tag the line keeps. A case may wrap with a hard
// break (`··` or `\` at a line's end): the reader joins an item's wrapped lines
// before it reads the markup, so the case is read as readDoc reads the item.
func TestReadLine_SharedInlineMarkupCases(t *testing.T) {
	raw, err := os.ReadFile("../../../../../packages/contracts/requirements/inline-markup-cases.json")
	if err != nil {
		t.Fatalf("read inline-markup-cases.json: %v", err)
	}
	var fixture struct {
		Cases []struct{ Name, Markdown, Text, Tag string }
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) == 0 {
		t.Fatal("no cases")
	}
	for _, c := range fixture.Cases {
		line := asOneLine(c.Markdown)
		if got := readInline(line).text; got != c.Text {
			t.Errorf("%s: %q reads %q, the room %q", c.Name, c.Markdown, got, c.Text)
		}
		if got := parseLine(readLine(line)).tag; got != c.Tag {
			t.Errorf("%s: tag %q, want %q", c.Name, got, c.Tag)
		}
		doc := readDoc("## User Stories\n\n- " + c.Markdown + "\n")
		if got, want := doc.sections[0].items[0].text, readLine(line); got != want {
			t.Errorf("%s: the item reads %q, the joined line %q", c.Name, got, want)
		}
	}
}

// asOneLine joins a case's wrapped lines as readDoc joins an item's: each
// line's hard break dropped, and the lines joined with a space.
func asOneLine(markdown string) string {
	lines := strings.Split(markdown, "\n")
	for i, l := range lines {
		lines[i] = dropHardBreak(strings.TrimSpace(l))
	}
	return strings.Join(lines, " ")
}
