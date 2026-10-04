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

package app

import (
	"testing"

	"github.com/wso2/aep/aep-api/internal/delivery/codingagent"
)

func countRetention(ws []Watcher) int {
	n := 0
	for _, w := range ws {
		if _, ok := w.(*codingagent.RecordingRetention); ok {
			n++
		}
	}
	return n
}

// The retention watcher follows the workspace root: registered when the mount
// is configured, absent when Fake() leaves it empty.
func TestAssemble_RecordingRetentionFollowsWorkspaceRoot(t *testing.T) {
	cfg := baseCfg()
	app, err := Assemble(cfg, Fake(), Seam{})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if got := countRetention(app.Watchers); got != 0 {
		t.Fatalf("empty Workspace.Root: %d retention watchers, want 0", got)
	}

	cfg.Workspace.Root = t.TempDir()
	app, err = Assemble(cfg, Fake(), Seam{})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if got := countRetention(app.Watchers); got != 1 {
		t.Fatalf("set Workspace.Root: %d retention watchers, want 1", got)
	}
}
