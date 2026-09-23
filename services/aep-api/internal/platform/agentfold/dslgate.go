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

package agentfold

// dslgate.go — the wireframes `.dsl` syntax write-gate, an EXACT accept/reject
// port of packages/excalidraw-dsl validateWireframeSyntax as consumed by
// packages/agent-stream/src/wireframe-layout.ts checkWireframeLayout. The flow
// dialect computes geometry from structure, so the gate polices STRUCTURE:
// unknown keywords, misplaced `left`/`right`/table-`row` lines, and the
// retired coordinate dialect. Like designgate.go, only the accept/reject
// decision is parity-critical (a write the agent applied must fold here too);
// message text is log-only.

import (
	"regexp"
	"strings"
)

var (
	dslScreenRe   = regexp.MustCompile(`(?i)^screen\s+[\w-]+(?:\s+"(?:[^"\\]|\\.)*")?(?:\s+\d+\s*x\s*\d+)?\s*$`)
	dslFlowRe     = regexp.MustCompile(`(?i)^flow\b`)
	dslTableRowRe = regexp.MustCompile(`(?i)^row\s+"`)
	dslLayoutRow  = regexp.MustCompile(`(?i)^row\s*$`)
	dslSplitRe    = regexp.MustCompile(`(?i)^split\s+\d+\s*/\s*\d+\s*$`)
	dslColRe      = regexp.MustCompile(`(?i)^(left|right)\s*$`)
	dslKindRe     = regexp.MustCompile(`(?i)^(rect|ellipse|button|text|heading|input|card|image|table|navbar|sidebar|tabs|list|select|search|textarea|checkbox|radio|toggle|badge|avatar|progress|divider|breadcrumb|chart|icon|link)\b`)
	dslNavRe      = regexp.MustCompile(`\s*->\s*[\w-]+\s*$`)
	dslQuotedRe   = regexp.MustCompile(`"(?:[^"\\]|\\.)*"`)
	dslLegacyRe   = regexp.MustCompile(`(?:^|\s)\d+\s*,\s*\d+(?:\s|$)`)
)

// isWireframesDslPath mirrors wireframe-layout.ts: `.dsl` files except the
// erd/domain domain-model grammar.
func isWireframesDslPath(path string) bool {
	if !strings.HasSuffix(path, ".dsl") {
		return false
	}
	parts := strings.Split(path, "/")
	base := strings.ToLower(parts[len(parts)-1])
	return !(strings.HasPrefix(base, "erd") || strings.HasPrefix(base, "domain"))
}

type dslCtx struct {
	level int
	kind  string // root | row | split | col | card | table
}

// checkWireframeDslGuard mirrors checkWireframeLayout: a wireframes .dsl body
// with syntax problems is rejected (INVALID_DSL) so a typo can't silently
// vanish from the drawing — and so this fold's verdict matches the TS
// bundle's, keeping the D14 manifest check green.
func checkWireframeDslGuard(path, content string) (ErrCode, string) {
	if !isWireframesDslPath(path) {
		return "", ""
	}
	var (
		stack   []dslCtx
		screen  bool
		inFlow  bool
		nErrors int
		first   string
	)
	report := func(msg string) {
		nErrors++
		if first == "" {
			first = msg
		}
	}

	lines := strings.Split(content, "\n")
	for i, raw := range lines {
		line := strings.TrimRight(raw, " \t\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		level := (indent + 1) / 2
		_ = i

		if level == 0 {
			switch {
			case dslScreenRe.MatchString(trimmed):
				screen = true
				inFlow = false
				stack = []dslCtx{{level: 0, kind: "root"}}
			case dslFlowRe.MatchString(trimmed):
				screen = false
				inFlow = true
			default:
				screen = false
				inFlow = false
				report("unknown top-level line — expected `screen <Name>`")
			}
			continue
		}
		if inFlow {
			continue // flow edges (and stray lines) are tolerated, as in TS
		}
		if !screen {
			continue
		}

		for len(stack) > 1 && stack[len(stack)-1].level >= level {
			stack = stack[:len(stack)-1]
		}
		parent := stack[len(stack)-1]
		inStack := parent.kind == "root" || parent.kind == "col"

		switch {
		case dslTableRowRe.MatchString(trimmed):
			if parent.kind != "table" {
				report("a quoted `row \"…\"` is table data and must nest under a `table`")
			}
		case dslLayoutRow.MatchString(trimmed):
			if inStack || parent.kind == "card" {
				stack = append(stack, dslCtx{level: level, kind: "row"})
			} else {
				report("a layout `row` can only sit in a screen stack, a split column, or inside a `card`")
			}
		case dslSplitRe.MatchString(trimmed):
			if !inStack {
				report("`split` can only sit in a screen stack")
			} else {
				stack = append(stack, dslCtx{level: level, kind: "split"})
			}
		case dslColRe.MatchString(trimmed):
			word := strings.ToLower(strings.TrimSpace(trimmed))
			switch {
			case parent.kind == "split":
				stack = append(stack, dslCtx{level: level, kind: "col"})
			case word == "right" && parent.kind == "row":
				// right-packing marker — no ctx change
			default:
				report("`" + word + "` only makes sense under a `split` or inside a `row`")
			}
		default:
			m := dslKindRe.FindString(trimmed)
			if m == "" {
				report("unknown element — not a DSL keyword or element kind")
				continue
			}
			kind := strings.ToLower(m)
			after := strings.TrimSpace(trimmed[len(m):])
			// Strip the trailing `-> Screen`, then the quoted label, then test
			// for the retired coordinate dialect — same order as the TS parser.
			after = dslNavRe.ReplaceAllString(after, "")
			if loc := dslQuotedRe.FindStringIndex(after); loc != nil {
				after = strings.TrimSpace(after[:loc[0]] + " " + after[loc[1]:])
			}
			if dslLegacyRe.MatchString(" " + after + " ") {
				report("absolute x,y coordinates are retired — layout is computed from structure")
				continue
			}
			if kind == "navbar" || kind == "sidebar" {
				continue
			}
			if parent.kind != "row" && parent.kind != "card" && !inStack {
				report("an element cannot nest under a `" + parent.kind + "` here")
				continue
			}
			if kind == "card" {
				stack = append(stack, dslCtx{level: level, kind: "card"})
			} else if kind == "table" {
				stack = append(stack, dslCtx{level: level, kind: "table"})
			}
		}
	}

	if nErrors > 0 {
		return ErrInvalidDSL, path + " has DSL syntax problems (first: " + first + ") — the file is unchanged; fix every line and re-emit the whole file."
	}
	return "", ""
}
