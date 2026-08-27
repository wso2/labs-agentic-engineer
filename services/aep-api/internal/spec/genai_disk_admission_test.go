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

package spec_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/platform/gitfs"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// refusingSnapshots is the live disk-admission shape: Ensure wraps
// gitfs.ErrDiskAdmission the same way StartTurn does (`ensure repo snapshot: %w`).
type refusingSnapshots struct{}

func (refusingSnapshots) Ensure(context.Context, sourcecontrol.RepoRef, string) error {
	return fmt.Errorf("ensure repo snapshot: %w (usage=92%%)", gitfs.ErrDiskAdmission)
}

// TestDiskAdmission_StartTurnIs503Not500 is the HTTP pin the mapper unit tests
// do not give: snapshots.Ensure refuses → StartTurn is 503 with a sentence,
// not opaque 500 "internal error", and agents are never dispatched.
func TestDiskAdmission_StartTurnIs503Not500(t *testing.T) {
	r := newGenaiRig(t, map[string]string{"specs/requirements/prd.md": "# Reqs\n"},
		withSnapshots(refusingSnapshots{}),
	)

	rec := r.post(t, convUUID, "requirements-chat", "x")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("disk-admission POST: code %d, want 503 (%s)", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"internal error"`) {
		t.Errorf("503 body must not be opaque internal error, got %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "workspace disk is full") {
		t.Errorf("503 body must carry the disk-admission sentence, got %s", rec.Body.String())
	}
	if r.fake.turns(t) != 0 {
		t.Error("agents dispatched despite disk admission")
	}
	if rec := r.h.AsOrg(testOrg).Get(turnPath("active")); rec.Code != http.StatusNoContent {
		t.Errorf("no turn row should exist: active = %d, want 204", rec.Code)
	}
}

// TestMarketplaceRegister_StartTurnDiskAdmissionIs503 is the live incident
// path: synthetic register chat dual-Ensure refused at ≥90%.
func TestMarketplaceRegister_StartTurnDiskAdmissionIs503(t *testing.T) {
	r := newGenaiRig(t, map[string]string{"specs/requirements/prd.md": "# Reqs\n"},
		withConversations(&memConversationRepo{}),
		withSnapshots(refusingSnapshots{}),
	)
	current := listMarketplaceConversations(t, r)[0].ConversationID
	payload, _ := json.Marshal(map[string]any{
		"instruction": "/register-external-resource Register a payment gateway",
		"collab":      true,
	})
	rec := r.h.AsOrg(testOrg).Post(marketplaceTurnsPath(current), string(payload))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("marketplace disk-admission POST: code %d, want 503 (%s)", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"internal error"`) {
		t.Errorf("503 body must not be opaque internal error, got %s", rec.Body.String())
	}
	if r.fake.turns(t) != 0 {
		t.Error("agents dispatched despite marketplace disk admission")
	}
}
