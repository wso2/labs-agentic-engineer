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
	"os"
	"path/filepath"
	"testing"
)

// A cold clone records its bytes on the engine (ticket 20 §2: usage between
// sweeps is the last du plus the clones since), and a warm read records none.
func TestCloneRecordsUsage(t *testing.T) {
	fx := NewFixture(t, seedFiles())
	ctx := context.Background()
	if got := fx.Engine.UsedBytes(); got != 0 {
		t.Fatalf("UsedBytes before any clone = %d, want 0", got)
	}
	if _, err := fx.Engine.Head(ctx, fx.Ref, ""); err != nil {
		t.Fatalf("Head: %v", err)
	}
	cloned := fx.Engine.UsedBytes()
	if want := repoDirBytes(t, fx); cloned != want || cloned == 0 {
		t.Fatalf("UsedBytes after clone = %d, want the mirror's %d bytes", cloned, want)
	}
	if _, err := fx.Engine.Head(ctx, fx.Ref, ""); err != nil {
		t.Fatalf("Head: %v", err)
	}
	if got := fx.Engine.UsedBytes(); got != cloned {
		t.Fatalf("UsedBytes after a warm read = %d, want unchanged %d", got, cloned)
	}
}

func TestUsageSetAndAdd(t *testing.T) {
	fx := NewFixture(t, seedFiles())
	fx.Engine.SetUsedBytes(1000)
	fx.Engine.AddUsage(24)
	if got := fx.Engine.UsedBytes(); got != 1024 {
		t.Fatalf("UsedBytes = %d, want 1024", got)
	}
}

// repoDirBytes is the apparent size of every regular file in the mirror's
// git dir, measured independently of the engine.
func repoDirBytes(t *testing.T, fx *Fixture) int64 {
	t.Helper()
	var total int64
	err := filepath.Walk(mirrorGitDir(t, fx), func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return total
}
