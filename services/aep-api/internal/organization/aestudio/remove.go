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
	"context"
	"log/slog"

	"github.com/wso2/aep/aep-api/internal/platform/auth"
)

// Remove deletes org's AE Studio: the Resource ae-studio, as aep-api's own
// identity. OpenChoreo's Resource finalizer deletes its bindings and releases
// first, so the pod — and the clones, reference documents and gitpat it
// held — goes with it (06 §9 gitpat disconnect, before the credential's
// references are removed). The Project ae-system and the ResourceType stay
// (Cloud cannot delete a ResourceType, ticket 16); a reconnect converges a
// new Resource. A Resource already gone is success.
//
// The org's converge records on this replica are dropped with it. Remove
// does not stop a converge already running (one a save started seconds
// before the disconnect): that converge can re-create the Resource, which a
// later Remove deletes. Every converge started after the credential is gone
// answers absent and writes nothing.
func (s *Service) Remove(ctx context.Context, org string) error {
	if err := s.oc.Resources.DeleteResource(auth.WithServiceIdentity(ctx), org, ResourceName); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.failures, org)
	delete(s.converged, org)
	delete(s.statusFailures, org)
	s.mu.Unlock()
	slog.InfoContext(ctx, "ae_studio.removed", "org", org)
	return nil
}
