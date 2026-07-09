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
	"context"
	"errors"
	"testing"

	"github.com/wso2/aep/aep-api/internal/feature/codingagent"
)

// fakeProjectTraitSyncer records the (orgID, projectID) pairs the deploy
// observer forwards to the project-wide trait re-emit, and can be scripted to
// fail so the best-effort propagation contract is assertable.
type fakeProjectTraitSyncer struct {
	calls [][2]string
	err   error
}

func (f *fakeProjectTraitSyncer) SyncProjectAPITraits(_ context.Context, orgID, projectID string) error {
	f.calls = append(f.calls, [2]string{orgID, projectID})
	return f.err
}

// traitDeployObserver satisfies the codingagent.DeployObserver port so the
// fan-out can drive it — pinned at compile time.
var _ codingagent.DeployObserver = traitDeployObserver{}

// The observer ignores the deployed component name and fans a project-wide
// re-emit (mirroring spaDeployObserver): a sibling SPA's deploy resolves a
// protected API's CORS origin, and the API's own deploy is when its
// ReleaseBinding first exists to carry the jwtAuth config.
func TestTraitDeployObserver_ForwardsProjectWideSync(t *testing.T) {
	f := &fakeProjectTraitSyncer{}
	o := traitDeployObserver{svc: f}

	if err := o.OnComponentDeployed(context.Background(), "acme", "widgets", "order-service"); err != nil {
		t.Fatalf("OnComponentDeployed: %v", err)
	}
	if len(f.calls) != 1 || f.calls[0] != [2]string{"acme", "widgets"} {
		t.Fatalf("expected one project-wide sync for (acme,widgets), got %v", f.calls)
	}
}

// The adapter returns the syncer's error verbatim; the MultiDeployObserver
// fan-out is what swallows+logs it (best-effort), so a trait-sync failure
// never re-drives the ExecWatcher's terminal build-success write.
func TestTraitDeployObserver_PropagatesErrorToFanOut(t *testing.T) {
	f := &fakeProjectTraitSyncer{err: errors.New("read design: boom")}
	o := traitDeployObserver{svc: f}

	if err := o.OnComponentDeployed(context.Background(), "acme", "widgets", "c"); err == nil {
		t.Fatal("expected the syncer error to propagate to the fan-out")
	}
}
