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

package repo

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"syscall"
)

// ErrDiskFull is the sentinel underneath DiskFullError (errors.Is).
var ErrDiskFull = errors.New("repo: disk full")

// ErrDiskAdmission refuses a new snapshot or reference upload at
// DiskAdmissionRefusePct. It is an ErrDiskFull (errors.Is), so callers answer
// it as disk_full.
var ErrDiskAdmission = fmt.Errorf("%w: admission refused", ErrDiskFull)

// DiskFullError names the workspace root and last-recorded usage after an
// ENOSPC was observed and the reaper emergency sweep was triggered.
type DiskFullError struct {
	Root    string
	UsedPct int
}

func (e *DiskFullError) Error() string {
	return fmt.Sprintf("repo: ENOSPC on workspace %s (usage ~%d%%) — reaper emergency sweep triggered", e.Root, e.UsedPct)
}

func (e *DiskFullError) Unwrap() error { return ErrDiskFull }

// enospcMsg is the strerror text git and libc emit for ENOSPC. git()/gitStream
// wrap *exec.ExitError as "git …: exit status N: <stderr>", so the errno is
// not in the Go error chain — only this phrase (or "ENOSPC") survives.
const enospcMsg = "no space left on device"

// isENOSPC reports whether err indicates a disk-full failure: either
// errors.Is(..., syscall.ENOSPC) (os.PathError / extract / rename) or a
// git-wrapped ExitError whose stderr / Error() string carries the ENOSPC
// message (errno is not preserved through fmt.Errorf("%w: %s", exitErr, stderr)).
func isENOSPC(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.ENOSPC) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, enospcMsg) || strings.Contains(msg, "enospc")
}

// requestedURLStatus is how git's http transport reports GitHub's status.
var requestedURLStatus = regexp.MustCompile(`the requested url returned error: (\d{3})`)

// remoteHTTPStatus is GitHub's HTTP status for a failed clone, fetch or
// push, read from git's stderr in err's text (the exit error carries none);
// 0 when git reported none. GitHub answers a token without access to a
// repository with 404 ("Repository not found"), a wrong token with 401
// ("Authentication failed"), a token without push rights with 403.
func remoteHTTPStatus(err error) int {
	if err == nil {
		return 0
	}
	msg := strings.ToLower(err.Error())
	if m := requestedURLStatus.FindStringSubmatch(msg); m != nil {
		status, _ := strconv.Atoi(m[1])
		return status
	}
	switch {
	case strings.Contains(msg, "repository not found"):
		return 404
	case strings.Contains(msg, "authentication failed"), strings.Contains(msg, "invalid username or password"):
		return 401
	case strings.Contains(msg, "permission to") && strings.Contains(msg, "denied"):
		return 403
	}
	return 0
}

// ErrorClass names a git or filesystem failure for a log line without its
// text, which names the git command, the clone URL or a path: the request
// ended (canceled, timeout), the disk is full, git exited non-zero, a
// filesystem call failed, or anything else.
func ErrorClass(err error) string {
	var exitErr *exec.ExitError
	var pathErr *fs.PathError
	var linkErr *os.LinkError
	var sysErr *os.SyscallError
	switch {
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case isENOSPC(err):
		return "disk_full"
	case errors.As(err, &exitErr):
		return "git_exit"
	case errors.As(err, &pathErr), errors.As(err, &linkErr), errors.As(err, &sysErr):
		return "fs"
	default:
		return "other"
	}
}
