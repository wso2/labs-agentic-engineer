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
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"golang.org/x/term"
)

// SelectItem is one entry shown in a MultiSelect list.
type SelectItem struct {
	Label       string
	Description string
}

// MultiSelect renders an interactive checkbox list.
// Returns (selected []bool, confirmed bool).
// confirmed is false when the user presses Esc — nothing should be applied.
// Falls back to a numbered prompt when stdout is not a TTY.
func MultiSelect(title string, items []SelectItem) (selected []bool, confirmed bool) {
	selected = make([]bool, len(items))
	if !isTTY || len(items) == 0 {
		return multiSelectFallback(title, items, selected)
	}

	cursor := 0

	// render redraws all item rows and the hint footer.
	// On the first call (initial=true) the cursor is already below the title line;
	// subsequent calls move the cursor back up and overwrite.
	render := func(initial bool) {
		if !initial {
			// Move up past: blank(1) + items(N) + hint(1) + note(1).
			fmt.Printf("\033[%dA", len(items)+3)
		}
		fmt.Print("\r\033[K\n") // blank separator line
		for i, item := range items {
			var check string
			if selected[i] {
				check = colorize(ansiOrange, "[✓]")
			} else {
				check = colorize(ansiGray, "[ ]")
			}
			marker := "  "
			if i == cursor {
				marker = colorize(ansiOrange, "▶ ")
			}
			fmt.Printf("\r\033[K  %s%s %-20s%s\n",
				marker, check, item.Label, colorize(ansiGray, item.Description))
		}
		fmt.Printf("\r\033[K  %s\n",
			colorize(ansiGray, "↑↓ navigate   space select   enter install   esc skip"))
		fmt.Printf("\r\033[K  %s\n",
			colorize(ansiGray, "· Custom resource types can also be applied later with kubectl apply"))
	}

	fmt.Printf("\n  %s\n", colorize(ansiGray, title))
	render(true)

	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return multiSelectFallback(title, items, selected)
	}
	restore := func() { _ = term.Restore(int(os.Stdin.Fd()), oldState) }

	// bufio buffers the full OS read, so r.Buffered() tells us whether the
	// ESC byte arrived alone (bare Esc key) or with companions (escape sequence
	// like \033[A for arrow-up). This is the reliable way to distinguish them
	// without needing a timeout.
	r := bufio.NewReaderSize(os.Stdin, 32)

	for {
		b, err := r.ReadByte()
		if err != nil {
			restore()
			fmt.Println()
			return selected, false
		}

		switch b {
		case 27: // ESC or start of escape sequence (e.g. arrow keys)
			if r.Buffered() == 0 {
				// No bytes arrived with the ESC → standalone Esc key → skip.
				restore()
				fmt.Println()
				return selected, false
			}
			next, _ := r.ReadByte()
			if next != '[' {
				break // unexpected sequence; ignore
			}
			seq, _ := r.ReadByte()
			switch seq {
			case 'A': // ↑
				if cursor > 0 {
					cursor--
				}
			case 'B': // ↓
				if cursor < len(items)-1 {
					cursor++
				}
			}
		case ' ':
			selected[cursor] = !selected[cursor]
		case 13, 10: // Enter / Return
			restore()
			fmt.Println()
			return selected, true
		case 3: // Ctrl+C — treat as cancel, let the caller decide how to exit
			restore()
			fmt.Println()
			return selected, false
		}

		render(false)
	}
}

func multiSelectFallback(title string, items []SelectItem, selected []bool) ([]bool, bool) {
	fmt.Printf("\n  %s\n\n", title)
	for i, item := range items {
		fmt.Printf("  [%d] %s — %s\n", i+1, item.Label, item.Description)
	}
	fmt.Printf("\n  %s\n", "· Custom resource types can also be applied later with kubectl apply")
	fmt.Print("\n  Enter numbers to install (e.g. 1,2) or blank to skip: ")
	sc := bufio.NewScanner(os.Stdin)
	var line string
	if sc.Scan() {
		line = strings.TrimSpace(sc.Text())
	}
	if line == "" {
		return selected, false
	}
	for _, part := range strings.Split(line, ",") {
		part = strings.TrimSpace(part)
		n, err := strconv.Atoi(part)
		if err != nil || n < 1 || n > len(items) {
			fmt.Printf("  ignoring invalid entry %q\n", part)
			continue
		}
		selected[n-1] = true
	}
	return selected, true
}
