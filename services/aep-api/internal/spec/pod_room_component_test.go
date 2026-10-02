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

// TEMPORARY (phase 3 deletes): old agents joins the pod Room. Until phase 3
// moves turns into the pod, aep-api tells the old agents service which Room
// to join: the org's AE Studio collab URL, read through a RoomLocator.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/wso2/aep/aep-api/internal/platform/auth"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// testRoomURL is the pod Room URL the default rig's locator answers.
const testRoomURL = "ws://ae-collab-x.openchoreoapis.localhost:19080/v1/rooms"

// staticRoom is a RoomLocator whose org's AE Studio is ready at a fixed URL.
type staticRoom string

func (s staticRoom) RoomURL(context.Context, string) (string, error) { return string(s), nil }

// refusingRoom is a RoomLocator that answers one fixed error, and counts calls.
type refusingRoom struct {
	err   error
	calls *int
}

func (r refusingRoom) RoomURL(context.Context, string) (string, error) {
	if r.calls != nil {
		*r.calls++
	}
	return "", r.err
}

func withRooms(l spec.RoomLocator) rigOption {
	return func(c *rigConfig) { c.rooms = l }
}

func postCollabTurn(t *testing.T, r *genaiRig) *http.Response {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"instruction": "edit the doc live", "collab": true})
	return r.h.AsOrg(testOrg).Post(turnsPath(convUUID), string(body)).Result()
}

func errorCode(t *testing.T, res *http.Response) string {
	t.Helper()
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	return body.Code
}

// The turn body carries the pod Room's URL, so the old agents service joins
// the org's AE Studio Room rather than a platform-wide collab server.
func TestStartTurn_CollabCarriesPodRoomURL(t *testing.T) {
	const url = "ws://ae-collab-y.openchoreoapis.localhost:19080/v1/rooms"
	r := newGenaiRig(t, map[string]string{"specs/requirements/prd.md": "# Reqs\n"}, withRooms(staticRoom(url)))

	r.fake.parts = []string{textPart("joined")}

	res := postCollabTurn(t, r)
	var out struct {
		TurnID string `json:"turnId"`
	}
	if res.StatusCode != http.StatusAccepted || json.NewDecoder(res.Body).Decode(&out) != nil {
		t.Fatalf("POST collab turn: code %d", res.StatusCode)
	}
	r.waitTerminal(t, out.TurnID)
	sent := r.fake.sentTurn(t, 0)
	if sent.req.Collab == nil || sent.req.Collab.URL != url {
		t.Fatalf("collab block = %+v, want url %q", sent.req.Collab, url)
	}
}

// 05 §5: an org with no AE Studio (no GitHub token) answers 409
// github_not_connected; one that is provisioning, failed or unreachable answers
// 503 ae_studio_unavailable with Retry-After: 5. Both are pre-202: no turn row,
// no dispatch.
func TestStartTurn_PodRoomRefusalsArePreAccept(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		status     int
		code       string
		retryAfter string
	}{
		{"absent", spec.ErrAEStudioAbsent, http.StatusConflict, "github_not_connected", ""},
		{"unavailable", spec.ErrAEStudioUnavailable, http.StatusServiceUnavailable, "ae_studio_unavailable", "5"},
		{"unreachable", fmt.Errorf("%w: read status: oc down", spec.ErrAEStudioUnavailable), http.StatusServiceUnavailable, "ae_studio_unavailable", "5"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newGenaiRig(t, map[string]string{"specs/requirements/prd.md": "# Reqs\n"}, withRooms(refusingRoom{err: c.err}))

			res := postCollabTurn(t, r)
			if res.StatusCode != c.status {
				t.Fatalf("status = %d, want %d", res.StatusCode, c.status)
			}
			if got := res.Header.Get("Retry-After"); got != c.retryAfter {
				t.Errorf("Retry-After = %q, want %q", got, c.retryAfter)
			}
			if got := errorCode(t, res); got != c.code {
				t.Errorf("code = %q, want %q", got, c.code)
			}
			if n := r.fake.turns(t); n != 0 {
				t.Errorf("dispatched turns = %d, want 0", n)
			}
			if newest, err := r.turns.Newest(t.Context(), testOrg, testProj); err != nil || newest != nil {
				t.Errorf("a refused turn left a row: %+v (err %v)", newest, err)
			}
		})
	}
}

// Q-47: aep-api boots without AE_STUDIO_*, and its locator then answers
// unavailable. Only room-scoped turns read it: a plain turn still runs.
func TestStartTurn_NonCollabTurnIgnoresTheRoom(t *testing.T) {
	calls := 0
	r := newGenaiRig(t, map[string]string{"specs/requirements/prd.md": "# Reqs\n"},
		withRooms(refusingRoom{err: spec.ErrAEStudioUnavailable, calls: &calls}))
	r.fake.parts = []string{textPart("hi")}

	r.waitTerminal(t, r.startTurn(t, convUUID, "", "hi"))
	if calls != 0 {
		t.Fatalf("locator calls = %d, want 0 for a non-collab turn", calls)
	}
}

// The kickoff is room-scoped, so it fails fast when the pod is not ready (as
// phase 3 will): the project reports never-started and offers Retry.
func TestStartKickoff_PodUnavailableStartsNoTurn(t *testing.T) {
	r := newGenaiRig(t, map[string]string{
		spec.DescriptorPath: descriptorTOML(t, testIdea),
	}, withConversations(&memConversationRepo{}), withRooms(refusingRoom{err: spec.ErrAEStudioUnavailable}))

	ctx := auth.WithAuthToken(t.Context(), "bearer-from-the-create-request")
	if _, err := r.svc.StartKickoff(ctx, testOrg, testProj); !errors.Is(err, spec.ErrAEStudioUnavailable) {
		t.Fatalf("StartKickoff error = %v, want ErrAEStudioUnavailable", err)
	}
	if n := r.fake.turns(t); n != 0 {
		t.Fatalf("dispatched turns = %d, want 0", n)
	}
}
