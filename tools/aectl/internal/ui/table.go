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

package ui

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

var ansiStripper = regexp.MustCompile(`\033\[[0-9;]*m`)

// visibleLen returns the display width of s in runes, ignoring ANSI escape codes.
func visibleLen(s string) int {
	return utf8.RuneCountInString(ansiStripper.ReplaceAllString(s, ""))
}

// padRight pads s on the right with spaces until its visible width equals width.
func padRight(s string, width int) string {
	vl := visibleLen(s)
	if vl >= width {
		return s
	}
	return s + strings.Repeat(" ", width-vl)
}

// Table renders a left-aligned table with muted column headers.
// Callers may pass pre-colored cell values; the table accounts for ANSI codes
// when computing column widths.
type Table struct {
	headers []string
	widths  []int
	rows    [][]string
}

// NewTable creates a table with the given column headers.
func NewTable(headers ...string) *Table {
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = utf8.RuneCountInString(h)
	}
	return &Table{headers: headers, widths: widths}
}

// AddRow appends a data row. Extra values are ignored; missing values are empty.
func (t *Table) AddRow(values ...string) {
	row := make([]string, len(t.headers))
	for i := range t.headers {
		if i < len(values) {
			row[i] = values[i]
		}
		if vl := visibleLen(row[i]); vl > t.widths[i] {
			t.widths[i] = vl
		}
	}
	t.rows = append(t.rows, row)
}

// Print renders the table to stdout with 3-space column padding.
func (t *Table) Print() {
	const pad = 3
	// Header row — muted gray.
	var sb strings.Builder
	sb.WriteString("  ")
	for i, h := range t.headers {
		gh := colorize(ansiGray, h)
		if i == len(t.headers)-1 {
			sb.WriteString(gh)
		} else {
			sb.WriteString(padRight(gh, t.widths[i]+pad))
		}
	}
	fmt.Println(sb.String())

	// Data rows.
	for _, row := range t.rows {
		sb.Reset()
		sb.WriteString("  ")
		for i, v := range row {
			if i == len(row)-1 {
				sb.WriteString(v)
			} else {
				sb.WriteString(padRight(v, t.widths[i]+pad))
			}
		}
		fmt.Println(sb.String())
	}
}
