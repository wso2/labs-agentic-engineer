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

package spec

// room_locator.go — TEMPORARY (phase 3 deletes): old agents joins the pod
// Room. Until phase 3 moves turns into the org's AE Studio pod, the old agents
// service joins the pod's Room as a live peer; aep-api finds the Room's URL
// through this port and carries it in the turn body.

import (
	"context"
	"errors"
)

// RoomLocator answers the public URL of an org's spec Rooms (ae-collab's
// `/v1/rooms`). It fails with ErrAEStudioAbsent or ErrAEStudioUnavailable
// when the org's AE Studio cannot host one. org is the org's handle.
type RoomLocator interface { // TEMPORARY (phase 3 deletes): old agents joins the pod Room
	RoomURL(ctx context.Context, org string) (string, error) // TEMPORARY (phase 3 deletes): old agents joins the pod Room
} // TEMPORARY (phase 3 deletes): old agents joins the pod Room

var (
	// ErrAEStudioAbsent: the org has no AE Studio because it has no GitHub
	// token (05 §5: 409 github_not_connected, permanent).
	ErrAEStudioAbsent = errors.New("connect GitHub to continue") // TEMPORARY (phase 3 deletes): old agents joins the pod Room
	// ErrAEStudioUnavailable: the org's AE Studio is provisioning, failed or
	// unreachable (05 §5: 503 ae_studio_unavailable, Retry-After: 5).
	ErrAEStudioUnavailable = errors.New("AE Studio is not available yet — try again shortly") // TEMPORARY (phase 3 deletes): old agents joins the pod Room
)

// roomURL is the pod Room a room-scoped turn of org joins. With no locator
// wired there is no Room to join.
func (s *Service) roomURL(ctx context.Context, org string) (string, error) { // TEMPORARY (phase 3 deletes): old agents joins the pod Room
	if s.rooms == nil { // TEMPORARY (phase 3 deletes): old agents joins the pod Room
		return "", ErrAEStudioUnavailable // TEMPORARY (phase 3 deletes): old agents joins the pod Room
	} // TEMPORARY (phase 3 deletes): old agents joins the pod Room
	return s.rooms.RoomURL(ctx, org) // TEMPORARY (phase 3 deletes): old agents joins the pod Room
} // TEMPORARY (phase 3 deletes): old agents joins the pod Room
