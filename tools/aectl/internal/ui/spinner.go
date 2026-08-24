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
	"sync"
	"time"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Spinner displays an animated waiting indicator for long-running operations.
// When stdout is not a TTY it falls back to plain step/detail lines.
type Spinner struct {
	mu   sync.Mutex
	msg  string
	quit chan struct{}
	done chan struct{}
}

// NewSpinner creates a Spinner with the given initial message.
func NewSpinner(msg string) *Spinner {
	return &Spinner{msg: msg}
}

// Start begins the animation. Call Success, Fail, or Stop to end it.
func (s *Spinner) Start() {
	if !isTTY {
		fmt.Printf("  %s %s\n", colorize(ansiOrange, "◆"), s.msg)
		return
	}
	s.mu.Lock()
	if s.quit != nil { // already running
		s.mu.Unlock()
		return
	}
	quit := make(chan struct{})
	done := make(chan struct{})
	s.quit, s.done = quit, done
	s.mu.Unlock()
	go func() {
		defer close(done)
		i := 0
		for {
			select {
			case <-quit:
				return
			case <-time.After(80 * time.Millisecond):
			}
			s.mu.Lock()
			msg := s.msg
			s.mu.Unlock()
			frame := spinnerFrames[i%len(spinnerFrames)]
			fmt.Printf("\r  %s %s   ", colorize(ansiOrange, frame), msg)
			i++
		}
	}()
}

// Update changes the message shown next to the spinner.
func (s *Spinner) Update(msg string) {
	if !isTTY {
		fmt.Printf("     %s\n", colorize(ansiGray, "· "+msg))
		return
	}
	s.mu.Lock()
	s.msg = msg
	s.mu.Unlock()
}

// Success stops the spinner and prints a green success line.
func (s *Spinner) Success(msg string) {
	s.clearLine()
	fmt.Printf("  %s %s\n", colorize(ansiGreen, "✓"), msg)
}

// Fail stops the spinner and prints a red error line to stderr.
func (s *Spinner) Fail(msg string) {
	s.clearLine()
	fmt.Fprintf(os.Stderr, "  %s %s\n", colorize(ansiRed, "✗"), msg)
}

// Stop stops the animation without printing a result line.
// Callers should follow with Step/Success/Fail as appropriate.
func (s *Spinner) Stop() {
	s.clearLine()
}

func (s *Spinner) clearLine() {
	if !isTTY {
		return
	}
	s.mu.Lock()
	if s.quit == nil {
		s.mu.Unlock()
		return
	}
	quit, done := s.quit, s.done
	s.quit, s.done = nil, nil
	s.mu.Unlock()
	close(quit)
	<-done
	// Overwrite the spinner line with spaces then return to column 0.
	fmt.Printf("\r%-80s\r", "")
}
