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
	"strings"

	"golang.org/x/term"
)

// Confirm prints a [y/N] prompt and returns true only if the user types "y" or "yes".
// Pressing Enter (empty input) is treated as "no".
func Confirm(prompt string) bool {
	fmt.Printf("  %s [y/N]: ", colorize(ansiGray, prompt))
	sc := bufio.NewScanner(os.Stdin)
	if sc.Scan() {
		ans := strings.ToLower(strings.TrimSpace(sc.Text()))
		return ans == "y" || ans == "yes"
	}
	return false
}

// PromptSecret prints a masked input prompt (characters are hidden as the user types).
// Use for passwords, API keys, and other sensitive values.
func PromptSecret(label string) (string, error) {
	_, _ = fmt.Fprintf(os.Stderr, "  %s %s: ", colorize(ansiGray, "◇"), label)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	_, _ = fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// Prompt prints a visible text input prompt with an optional default value.
// Returns defaultValue if the user presses Enter without typing anything.
func Prompt(label, defaultValue string) (string, error) {
	if defaultValue != "" {
		fmt.Printf("  %s %s [%s]: ", colorize(ansiGray, "◇"), label, colorize(ansiGray, defaultValue))
	} else {
		fmt.Printf("  %s %s: ", colorize(ansiGray, "◇"), label)
	}
	sc := bufio.NewScanner(os.Stdin)
	if sc.Scan() {
		val := strings.TrimSpace(sc.Text())
		if val == "" {
			return defaultValue, nil
		}
		return val, nil
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	return defaultValue, nil
}
