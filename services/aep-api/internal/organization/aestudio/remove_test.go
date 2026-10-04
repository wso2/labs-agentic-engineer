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

package aestudio

import (
	"slices"
	"testing"
)

// 06 §9 gitpat disconnect: the org's Resource ae-studio is deleted (its
// binding, release and pod go with it in OpenChoreo), as aep-api's own
// identity. Nothing else is touched: the Project ae-system and the
// ResourceType stay (Cloud cannot delete a ResourceType, ticket 16), and a
// reconnect converges a new Resource.
func TestRemove_DeletesOnlyTheResource(t *testing.T) {
	f := newFixture(t).withAllRefs().converged()
	f.oc.resetCalls()
	if err := f.svc.Remove(userCtx(), "default"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if want := []string{"DELETE resource ae-studio"}; !slices.Equal(f.oc.calls, want) {
		t.Fatalf("calls %v, want %v", f.oc.calls, want)
	}
	if f.oc.res != nil || f.oc.rrb != nil || f.oc.rt == nil || !f.oc.project {
		t.Fatalf("after Remove: resource=%v binding=%v resourcetype=%v project=%v", f.oc.res != nil, f.oc.rrb != nil, f.oc.rt != nil, f.oc.project)
	}
	if len(f.oc.violations) != 0 {
		t.Fatalf("violations: %v", f.oc.violations)
	}
	// Already gone is the state Remove exists to reach.
	if err := f.svc.Remove(userCtx(), "default"); err != nil {
		t.Fatalf("second Remove: %v", err)
	}
}
