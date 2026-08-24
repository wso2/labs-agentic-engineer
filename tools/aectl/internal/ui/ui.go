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
	"os"
	"strings"
	"unicode/utf8"

	"golang.org/x/term"
)

// isTTY is true when stdout is an interactive terminal.
// Used to gate cursor-based rendering (spinners, multi-select).
var isTTY = term.IsTerminal(int(os.Stdout.Fd()))

// useColor is true when the terminal supports color AND the user has not
// opted out via NO_COLOR. Kept separate from isTTY so that NO_COLOR only
// strips styling, not interactive UI like keyboard navigation.
var useColor = isTTY && os.Getenv("NO_COLOR") == ""

const (
	ansiReset  = "\033[0m"
	ansiBold   = "\033[1m"
	ansiOrange = "\033[38;5;208m" // primary accent — active state, markers, status keywords
	ansiGreen  = "\033[32m"       // success checkmark
	ansiRed    = "\033[31m"       // errors
	ansiYellow = "\033[33m"       // warnings
	ansiGray   = "\033[90m"       // muted text, borders, labels
	ansiCyan   = "\033[36m"       // secondary (used in non-panel URL contexts)
)

func colorize(code, s string) string {
	if !useColor {
		return s
	}
	return code + s + ansiReset
}

// Exported color helpers for callers that need colored cell values in tables/panels.
func Green(s string) string  { return colorize(ansiGreen, s) }
func Red(s string) string    { return colorize(ansiRed, s) }
func Orange(s string) string { return colorize(ansiOrange, s) }
func Yellow(s string) string { return colorize(ansiYellow, s) }
func Gray(s string) string   { return colorize(ansiGray, s) }
func Bold(s string) string   { return colorize(ansiBold, s) }
func Cyan(s string) string   { return colorize(ansiCyan, s) }

// Step prints an orange diamond for an in-progress or about-to-start operation.
func Step(msg string) {
	fmt.Printf("  %s %s\n", colorize(ansiOrange, "◆"), msg)
}

// Success prints a green checkmark for a completed operation.
func Success(msg string) {
	fmt.Printf("  %s %s\n", colorize(ansiGreen, "✓"), msg)
}

// Fail prints a red cross to stderr.
func Fail(msg string) {
	fmt.Fprintf(os.Stderr, "  %s %s\n", colorize(ansiRed, "✗"), msg)
}

// Warn prints a yellow warning symbol.
func Warn(msg string) {
	fmt.Printf("  %s %s\n", colorize(ansiYellow, "!"), msg)
}

// Detail prints a muted sub-step detail indented under the current step.
func Detail(msg string) {
	fmt.Printf("     %s\n", colorize(ansiGray, "· "+msg))
}

// Section prints a subtle grey phase label with a preceding blank line.
func Section(title string) {
	fmt.Printf("\n  %s\n", colorize(ansiGray, title))
}

// Print prints a plain line.
func Print(msg string) {
	fmt.Println(msg)
}

// Banner prints the aectl brand header. Only emits output when stdout is a TTY.
func Banner() {
	if !isTTY {
		return
	}
	title := colorize(ansiOrange, "◆") + " " + colorize(ansiBold, "aectl") + "  " + colorize(ansiGray, "AEP Platform CLI")
	rule := colorize(ansiGray, "─────────────────────────────────────────────")
	fmt.Printf("\n  %s\n  %s\n\n", title, rule)
}

// Panel prints a bordered box-drawing key-value summary panel, Daytona-style.
// Values may contain ANSI color codes — Panel uses visibleLen for correct alignment.
func Panel(title string, rows [][2]string) {
	if !isTTY {
		fmt.Printf("\n%s\n", title)
		for _, r := range rows {
			fmt.Printf("  %s: %s\n", r[0], r[1])
		}
		fmt.Println()
		return
	}

	maxKey := 0
	for _, r := range rows {
		if l := visibleLen(r[0]); l > maxKey {
			maxKey = l
		}
	}
	maxVal := 0
	for _, r := range rows {
		if l := visibleLen(r[1]); l > maxVal {
			maxVal = l
		}
	}
	// innerWidth = left-pad(2) + key + gap(2) + val + right-pad(2)
	innerWidth := 2 + maxKey + 2 + maxVal + 2
	titleVis := utf8.RuneCountInString(title) // title has no ANSI codes
	if innerWidth < titleVis+4 {
		innerWidth = titleVis + 4
	}

	// Top: ┌─ title ─────────┐
	topFill := innerWidth - titleVis - 3
	fmt.Printf("\n  %s%s%s\n",
		colorize(ansiGray, "┌─ "),
		colorize(ansiBold, title),
		colorize(ansiGray, " "+strings.Repeat("─", topFill)+"┐"),
	)

	// Body rows: │  key   value  │
	border := colorize(ansiGray, "│")
	for _, r := range rows {
		keyPad := strings.Repeat(" ", maxKey-visibleLen(r[0]))
		valPad := strings.Repeat(" ", maxVal-visibleLen(r[1]))
		fmt.Printf("  %s  %s%s  %s%s  %s\n",
			border,
			colorize(ansiGray, r[0]), keyPad,
			r[1], valPad,
			border,
		)
	}

	// Bottom: └─────────────┘
	fmt.Printf("  %s\n\n", colorize(ansiGray, "└"+strings.Repeat("─", innerWidth)+"┘"))
}

// Ready prints the "platform is ready" completion panel.
func Ready(consoleURL string) {
	rows := [][2]string{
		{"Status", colorize(ansiOrange, "READY")},
	}
	if consoleURL != "" {
		rows = [][2]string{
			{"Console", colorize(ansiOrange, consoleURL)},
			{"Status", colorize(ansiOrange, "READY")},
		}
	}
	Panel("AEP is ready", rows)
}

// Fatal prints an error to stderr and exits with code 1.
func Fatal(msg string) {
	Fail(msg)
	os.Exit(1)
}
