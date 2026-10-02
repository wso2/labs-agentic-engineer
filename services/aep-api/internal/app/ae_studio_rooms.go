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

// ae_studio_rooms.go — TEMPORARY (phase 3 deletes): old agents joins the pod
// Room. spec.RoomLocator over the org's AE Studio status (R18 port): a ready
// AE Studio's Room is its public collab URL plus ae-collab's `/v1/rooms`.

import (
	"context"
	"fmt"

	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// aeStudioRoomsPath is ae-collab's public Room path.
const aeStudioRoomsPath = "/v1/rooms" // TEMPORARY (phase 3 deletes): old agents joins the pod Room

// aeStudioRooms answers an org's Room from its AE Studio status. The status
// reader reads OpenChoreo as aep-api's own identity, never the caller's
// JWT (aeStudioOCConfig), and may start an idempotent converge on drift.
type aeStudioRooms struct { // TEMPORARY (phase 3 deletes): old agents joins the pod Room
	status organization.AEStudioStatusReader // TEMPORARY (phase 3 deletes): old agents joins the pod Room
} // TEMPORARY (phase 3 deletes): old agents joins the pod Room

var _ spec.RoomLocator = aeStudioRooms{} // TEMPORARY (phase 3 deletes): old agents joins the pod Room

// RoomURL maps absent to spec.ErrAEStudioAbsent, and provisioning, failed
// (which an install without AE_STUDIO_* answers), a ready status with no URLs
// and a failed read to spec.ErrAEStudioUnavailable.
func (r aeStudioRooms) RoomURL(ctx context.Context, org string) (string, error) { // TEMPORARY (phase 3 deletes): old agents joins the pod Room
	st, err := r.status.Status(ctx, org) // TEMPORARY (phase 3 deletes): old agents joins the pod Room
	if err != nil {                      // TEMPORARY (phase 3 deletes): old agents joins the pod Room
		return "", fmt.Errorf("%w: read AE Studio status: %w", spec.ErrAEStudioUnavailable, err) // TEMPORARY (phase 3 deletes): old agents joins the pod Room
	} // TEMPORARY (phase 3 deletes): old agents joins the pod Room
	switch { // TEMPORARY (phase 3 deletes): old agents joins the pod Room
	case st.State == organization.AEStudioAbsent: // TEMPORARY (phase 3 deletes): old agents joins the pod Room
		return "", spec.ErrAEStudioAbsent // TEMPORARY (phase 3 deletes): old agents joins the pod Room
	case st.State == organization.AEStudioReady && st.URLs != nil && st.URLs.Collab != "": // TEMPORARY (phase 3 deletes): old agents joins the pod Room
		return st.URLs.Collab + aeStudioRoomsPath, nil // TEMPORARY (phase 3 deletes): old agents joins the pod Room
	default: // TEMPORARY (phase 3 deletes): old agents joins the pod Room
		return "", fmt.Errorf("%w: AE Studio is %s", spec.ErrAEStudioUnavailable, st.State) // TEMPORARY (phase 3 deletes): old agents joins the pod Room
	} // TEMPORARY (phase 3 deletes): old agents joins the pod Room
} // TEMPORARY (phase 3 deletes): old agents joins the pod Room
