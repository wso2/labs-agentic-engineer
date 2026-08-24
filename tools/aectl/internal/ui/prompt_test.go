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
	"os"
	"testing"
)

// withStdin replaces os.Stdin for the duration of fn with a pipe pre-loaded
// with input, then restores the original stdin. The write end is closed before
// fn runs so the scanner sees a clean EOF after reading the last line.
func withStdin(t *testing.T, input string, fn func()) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	old := os.Stdin
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = old
		_ = r.Close()
	})
	if _, err := w.WriteString(input); err != nil {
		t.Fatalf("write to stdin pipe: %v", err)
	}
	_ = w.Close()
	fn()
}

func TestConfirm_Affirmative(t *testing.T) {
	for _, input := range []string{"y\n", "yes\n", "Y\n", "YES\n", "Yes\n"} {
		t.Run(input, func(t *testing.T) {
			var got bool
			withStdin(t, input, func() { got = Confirm("proceed?") })
			if !got {
				t.Errorf("Confirm with input %q = false, want true", input)
			}
		})
	}
}

func TestConfirm_Negative(t *testing.T) {
	for _, input := range []string{"n\n", "no\n", "\n", "maybe\n", "  \n"} {
		t.Run(input, func(t *testing.T) {
			var got bool
			withStdin(t, input, func() { got = Confirm("proceed?") })
			if got {
				t.Errorf("Confirm with input %q = true, want false", input)
			}
		})
	}
}

func TestConfirm_EOF(t *testing.T) {
	// A closed pipe (immediate EOF) must return false, not panic.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	_ = w.Close() // EOF on first read
	old := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = old; _ = r.Close() })
	if Confirm("proceed?") {
		t.Error("Confirm on EOF = true, want false")
	}
}
