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

package provisioning

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/wso2/aep/aep-api/internal/spec"
)

// DeprovisionProject tears down the OC Resource model a project provisioned:
// every distinct external + platform-resource dependency in its design is
// deprovisioned (the OC Resource + per-env bindings). OpenChoreo's Project delete
// does NOT cascade these — they carry a logical spec.owner.projectName, not a k8s
// ownerReference — so a project delete would otherwise orphan them. Called BEFORE
// the design/repo is removed (the design read is the dependency inventory).
// Best-effort: a missing design (project never generated one) is a no-op.
func (s *Service) DeprovisionProject(ctx context.Context, orgID, projectID string) error {
	comps, err := s.design.ReadDesignComponents(ctx, orgID, projectID)
	if err != nil {
		slog.WarnContext(ctx, "provisioning: read design for teardown failed — skipping deprovision", "project", projectID, "error", err)
		return nil
	}
	seen := map[string]bool{}
	var errs []error
	for _, c := range comps {
		for _, d := range c.Dependencies {
			key := strings.ToLower(d.Name)
			if seen[key] {
				continue
			}
			switch d.Kind {
			case spec.DependencyKindExternal:
				seen[key] = true
				if s.extProv != nil {
					if derr := s.extProv.Deprovision(ctx, orgID, projectID, d.Name, []string{defaultEnv()}); derr != nil {
						errs = append(errs, fmt.Errorf("deprovision external %q: %w", d.Name, derr))
					}
				}
			case spec.DependencyKindPlatformResource:
				seen[key] = true
				if s.platProv != nil {
					if derr := s.platProv.Deprovision(ctx, orgID, projectID, d.Name, []string{defaultEnv()}); derr != nil {
						errs = append(errs, fmt.Errorf("deprovision platform-resource %q: %w", d.Name, derr))
					}
				}
			}
		}
	}
	return errors.Join(errs...)
}
