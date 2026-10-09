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

package repo_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"syscall"
	"testing"

	"github.com/wso2/aep/ae-studio-tools/internal/repo"
)

// ErrorClass names an error for a log line by class: a bounded set of words,
// never the error's text (git's names the command and the clone URL; a
// filesystem error names the path).
func TestErrorClass(t *testing.T) {
	exitErr := exec.Command("false").Run()
	var ee *exec.ExitError
	if !errors.As(exitErr, &ee) {
		t.Fatalf("false did not exit non-zero: %v", exitErr)
	}
	_, statErr := os.Stat("/nonexistent/planted-token-0123456789abcdef")
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"canceled", fmt.Errorf("git fetch: %w", context.Canceled), "canceled"},
		{"timeout", fmt.Errorf("lock: %w", context.DeadlineExceeded), "timeout"},
		{"disk full by errno", &fs.PathError{Op: "write", Path: "/x", Err: syscall.ENOSPC}, "disk_full"},
		{"disk full by git text", errors.New("git push: exit status 1: fatal: No space left on device"), "disk_full"},
		{"git exit", fmt.Errorf("git fetch https://planted.example.invalid/r.git: %w", exitErr), "git_exit"},
		{"filesystem", fmt.Errorf("purge: %w", statErr), "fs"},
		{"anything else", errors.New("planted-token-0123456789abcdef"), "other"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := repo.ErrorClass(c.err); got != c.want {
				t.Fatalf("ErrorClass(%v) = %q, want %q", c.err, got, c.want)
			}
		})
	}
}
