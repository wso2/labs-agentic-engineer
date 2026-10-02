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
	"io/fs"
	"path/filepath"
)

// Studio-data usage (ticket 20 §2). The reaper owns the measurement: each
// sweep it records a fresh du of the root with SetUsedBytes. Between sweeps
// every cold clone adds its own bytes with AddUsage, so the budget is not
// blind for up to one sweep interval while repos are being cloned.

// SetUsedBytes records the reaper's du of the studio-data root.
//
//deadcode:keep wired in Task 2.7
func (e *Engine) SetUsedBytes(n int64) { e.usedBytes.Store(n) }

// AddUsage adds n bytes written since the last sweep (one cold clone).
//
//deadcode:keep wired in Task 2.7
func (e *Engine) AddUsage(n int64) { e.usedBytes.Add(n) }

// UsedBytes is the last sweep's du plus the clones recorded since.
//
//deadcode:keep wired in Task 2.7
func (e *Engine) UsedBytes() int64 { return e.usedBytes.Load() }

// DirBytes sums the apparent size of the regular files under path. Symlinks
// are not followed and unreadable entries are skipped (a trash rename mid-walk
// is normal). Apparent size, not allocated blocks: git keeps its bulk in
// packfiles, where the two agree, and the budget sits 1 GiB under the
// volume's sizeLimit to absorb the block rounding of small files.
//
//deadcode:keep wired in Task 2.7
func DirBytes(path string) int64 {
	var total int64
	_ = filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // skip unreadable entries, keep walking
		}
		if d.Type().IsRegular() {
			if info, err := d.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total
}
