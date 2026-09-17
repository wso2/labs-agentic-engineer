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
	"strings"

	"github.com/wso2/aep/aep-api/internal/dependencies"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// ResolveComponentRunnerSecrets returns the per-run external-resource secret
// bundles (SM-API vault path + secret keys) for the coding runner of one
// component: it reads the component's `external` dependencies from the design,
// then reads each's per-env binding for its secretStorePath. The coding
// dispatcher materialises these into per-run ExternalSecrets so the agent can
// integration-test against the live service (dependency-management §1.4).
// Returns nil (no secrets) when the component binds no secret-bearing external
// resource. Only names/paths flow — no secret values.
func (s *Service) ResolveComponentRunnerSecrets(ctx context.Context, orgID, projectID, component, env string) ([]dependencies.ExternalResourceRunnerSecret, error) {
	if s.extProv == nil {
		return nil, nil
	}
	if strings.TrimSpace(env) == "" {
		env = defaultEnv()
	}
	comps, err := s.design.ReadDesignComponents(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	var names []string
	for i := range comps {
		if !strings.EqualFold(comps[i].Name, component) {
			continue
		}
		for _, d := range comps[i].Dependencies {
			if d.Kind == spec.DependencyKindExternal {
				names = append(names, d.Name)
			}
		}
	}
	if len(names) == 0 {
		return nil, nil
	}
	// orgID is the OC namespace (orgHandle); projectID is the OC project.
	return s.extProv.ResolveRunnerSecrets(ctx, orgID, projectID, env, names)
}
