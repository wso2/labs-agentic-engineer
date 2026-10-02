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

package projectstest

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/wso2/aep/ae-studio-tools/internal/projects"
)

func TestFake(t *testing.T) {
	want := projects.Repository{Owner: "o", Repo: "r", DefaultBranch: "main", CloneURL: "https://x/o/r.git"}
	f := NewFake(map[string]projects.Repository{"greeter": want})
	if got, err := f.Resolve(context.Background(), "greeter"); err != nil || got != want {
		t.Fatalf("got %+v, %v", got, err)
	}
	if _, err := f.Resolve(context.Background(), "other"); !errors.Is(err, projects.ErrUnknown) {
		t.Fatalf("unknown: err = %v", err)
	}
	f.Set("other", want)
	if _, err := f.Resolve(context.Background(), "other"); err != nil {
		t.Fatalf("after Set: err = %v", err)
	}
	f.SetErr(projects.ErrUnavailable)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := f.Resolve(context.Background(), "greeter"); !errors.Is(err, projects.ErrUnavailable) {
				t.Errorf("err = %v", err)
			}
		}()
	}
	wg.Wait()
	if f.CallCount() != 11 {
		t.Fatalf("calls = %d, want 11", f.CallCount())
	}
}
