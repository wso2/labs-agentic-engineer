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
	"errors"
	"fmt"
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
