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
	"fmt"
	"log/slog"

	"github.com/wso2/aep/aep-api/internal/platform/auth"
)

// Remove deletes org's AE Studio: the Resource ae-studio, as aep-api's own
// identity. OpenChoreo's Resource finalizer deletes its bindings and releases
// first, so the pod — and the clones, reference documents and gitpat it
// held — goes with it (the gitpat disconnect). The Project ae-system and
// the ResourceType stay (Cloud cannot delete a ResourceType); a
// reconnect converges a new Resource. A Resource already gone is success.
//
// Remove HOLDS the org first: until Release, no converge starts for it (a
// Status that sees the Resource gone triggers nothing), and a converge
// already running is waited for before the delete, so it cannot re-create
// what Remove deletes. The caller removes the org's gitpat rows and flips
// its credential before it releases; from then on the desired state answers
// absent, so nothing converges the pod back until the next gitpat submit.
// The org's converge records on this replica are dropped with it.
func (s *Service) Remove(ctx context.Context, org string) error {
	s.mu.Lock()
	s.held[org] = true
	f := s.flights[org]
	s.mu.Unlock()
	if f != nil {
		select {
		case <-f.done:
		case <-ctx.Done():
			return fmt.Errorf("wait for the running converge: %w", ctx.Err())
		}
	}
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

// Release ends Remove's hold on org: converges may start again (and answer
// absent once the org's GitHub connection is gone).
func (s *Service) Release(org string) {
	s.mu.Lock()
	delete(s.held, org)
	s.mu.Unlock()
}
