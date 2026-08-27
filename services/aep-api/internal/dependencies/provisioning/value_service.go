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
	"fmt"
	"log/slog"

	"github.com/wso2/aep/aep-api/internal/dependencies"
	"github.com/wso2/aep/aep-api/internal/platform/apierr"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// SaveValues collects an external dependency's per-env values, splits them into
// plain / secret by the UNION of the dependency's config schema across every
// component in the project's committed design.json that declares it (the user
// never marks secrecy; secret always wins on conflict), writes the secrets to
// SM-API + authors the OC external Resource model, then completes an existing
// provision gate when one exists. Builds do not mint an external value-collection
// gate; the external ResourceType remains readyWhen:${true}
// while AEP reports value readiness separately as configured/unset.
//
// orgID is the OC namespace + issues org; ocOrgID is the SM-API org id (the
// ctx must carry the user JWT — the SM-API writer reads the ouId claim for the
// vault path). No secret value is persisted outside SM-API or echoed anywhere.
func (s *Service) SaveValues(ctx context.Context, orgID, ocOrgID, projectID, depName string, envValues map[string]map[string]string) error {
	// Read the design ONCE: validate depName exists as an external dependency
	// (ErrDepNotFound/ErrDepWrongKind on failure, exactly as findDepInProject),
	// then classify by the UNION of Config[] across EVERY component that
	// declares an external dependency of this name — not just the first
	// match — so a key marked secret on ANY component is never misclassified
	// plain merely because a different, secret-blind component happened to be
	// scanned first (the CRITICAL leak this fixes). Build authoring and readiness
	// consume the same UnionExternalConfigFor/Keys helpers. A design read
	// failure still fails here (the underlying design-read error) — a value
	// is never misclassified plain for lack of a schema.
	comps, err := s.design.ReadDesignComponents(ctx, orgID, projectID)
	if err != nil {
		return fmt.Errorf("provisioning: read design: %w", err)
	}
	dep, err := matchDependency(comps, depName, spec.DependencyKindExternal)
	if err != nil {
		return err
	}
	// Registered External resources hold values on the org catalog plane;
	// project POST .../values is Project External only.
	if s.catalogValuePlane != nil && len(s.catalogValuePlane.EnvCells(orgID, depName)) > 0 {
		return apierr.Conflict("values live on the org record")
	}
	unionConfig, _ := spec.UnionExternalConfigFor(comps, depName)

	// The external definition the RT is authored against is built straight off
	// the design — name + the matched dependency's Description + the UNION
	// config schema just computed above — NOT fetched from the external_resources
	// table (that reader is gone; the design is now the only source, same as
	// classification above).
	er := &dependencies.ExternalResource{Name: depName, Description: dep.Description, ConfigKeys: unionConfig}
	if len(envValues) == 0 {
		return fmt.Errorf("provisioning: no environment values provided for %q", depName)
	}
	byEnv := make(map[string]dependencies.EnvValues, len(envValues))
	for env, vals := range envValues {
		byEnv[env] = splitBySchema(unionConfig, vals)
	}

	// Resolve a legacy gate if one exists. New builds do not mint external value
	// gates, so the normal path authors the resource without a provision row.
	issueNumber, _, err := s.findProvisionIssue(ctx, orgID, projectID, depName)
	if err != nil {
		return err
	}
	var execID string
	if issueNumber > 0 {
		repo, rerr := s.repos.RepoFullName(ctx, orgID, projectID)
		if rerr != nil {
			return fmt.Errorf("provisioning: resolve repo: %w", rerr)
		}
		row, admitted, aerr := s.admitProvisionRow(ctx, orgID, projectID, repo, depName, issueNumber)
		if aerr != nil {
			return fmt.Errorf("provisioning: admit provision run: %w", aerr)
		}
		if admitted {
			execID = row.ID
		}
	}

	result, perr := s.extProv.Provision(ctx, orgID, ocOrgID, projectID, er, byEnv)
	if perr != nil {
		if execID != "" {
			s.failProvisionRow(ctx, orgID, projectID, issueNumber, execID, perr.Error())
		}
		return fmt.Errorf("%w: %v", dependencies.ErrProvisionFailed, perr)
	}

	if execID != "" {
		ref := result.BindingByEnv[defaultEnv]
		if ref == "" {
			ref = result.ResourceName
		}
		if _, serr := s.execs.StartWithRun(ctx, execID, ref); serr != nil {
			slog.WarnContext(ctx, "provisioning: start external provision run failed", "execution", execID, "error", serr)
		}
		s.completeProvisionRow(ctx, orgID, projectID, depName, issueNumber, execID,
			fmt.Sprintf("External resource `%s` configured (OC binding `%s`).", depName, ref))
	}
	return nil
}

// splitBySchema partitions a flat env value map into plain / secret entries by
// the dependency's design.json config schema. A value whose key is not in the
// schema is treated as plain (forward-tolerant).
func splitBySchema(keys []spec.ConfigKey, vals map[string]string) dependencies.EnvValues {
	secret := make(map[string]bool, len(keys))
	for _, k := range keys {
		if k.Secret {
			secret[k.Key] = true
		}
	}
	out := dependencies.EnvValues{Plain: map[string]string{}, Secret: map[string]string{}}
	for k, v := range vals {
		if secret[k] {
			out.Secret[k] = v
		} else {
			out.Plain[k] = v
		}
	}
	return out
}
