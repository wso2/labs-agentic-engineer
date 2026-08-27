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
	"strings"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/dependencies"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// ExternalResourceView is one org external-resource catalog entry with its
// config schema and current consumers (the in-use delete guard input).
//
// Registered External rows may carry env cells and instances from
// CatalogValuePlane when that collaborator is wired (MemoryValuePlane in
// production; tests inject a fake). Secret cell values are never copied
// onto the wire DTO.
type ExternalResourceView struct {
	Name                    string
	Description             string
	Config                  []spec.ConfigKey
	Consumers               []dependencies.ExternalResourceConsumer
	ConsumptionInstructions string
	EnvCells                []EnvCell
	ResourceDocs            []openchoreo.ResourceDoc
	Instances               []ResourceInstance
}

// EnvCell is one org-held environment × config-key cell on a Registered
// External resource. Status is "configured" or "unset". Value is never
// copied to the DTO when the matching config key is secret.
type EnvCell struct {
	Environment string
	Key         string
	Status      string
	Value       string
	// SecretStorePath is the org-catalog vault key for this environment,
	// persisted when OrgSecretWriter returns one at register/update. It is
	// never copied onto the wire DTO.
	SecretStorePath string
}

// stampSecretStorePath copies each environment's org-catalog vault key onto
// every cell of that environment. Empty keys are left unset (no writer, or
// the writer returned nothing).
func stampSecretStorePath(cells []EnvCell, vaultByEnv map[string]string) {
	for i := range cells {
		if p := vaultByEnv[cells[i].Environment]; p != "" {
			cells[i].SecretStorePath = p
		}
	}
}

// ResourceInstance is one observed instance of a Registered External
// resource (project × environment × status).
type ResourceInstance struct {
	Project     string
	Environment string
	Status      string
}

// ListExternalResources returns the org's external-resource catalog with each
// entry's consumers (the components across the org whose committed design
// declares an external dependency of that name — a design scan, since the
// upstream component_tasks table is gone). Definitions are sourced from the
// org's namespaced OpenChoreo ResourceTypes via rtCatalog (Slice 3's
// resources.ExternalResourceCatalog, reconstructed off each authored RT and
// deduped to the newest schema-version RT per name) — never the
// external_resources table (s.catalog is no longer read anywhere in this
// package; authoring now builds its definition off the design — see
// build_provision.go / value_service.go).
//
// Env cells come from CatalogValuePlane when present; when the plane is empty
// after restart, Registered rows (non-empty consumption instructions on the
// RT) get synthesized cells so list and preflight still treat them as org-held.
func (s *Service) ListExternalResources(ctx context.Context, orgID string) ([]ExternalResourceView, error) {
	defs, err := s.rtCatalog.List(ctx, orgID)
	if err != nil {
		return nil, fmt.Errorf("provisioning: list external resources: %w", err)
	}
	consumersByName, err := s.externalConsumersByName(ctx, orgID)
	if err != nil {
		return nil, err
	}
	out := make([]ExternalResourceView, 0, len(defs))
	for i := range defs {
		def := &defs[i]
		view := ExternalResourceView{
			Name:                    def.Name,
			Description:             def.Description,
			Config:                  toConfigKeys(def.Config),
			Consumers:               consumersByName[strings.ToLower(def.Name)],
			ConsumptionInstructions: def.ConsumptionInstructions,
			ResourceDocs:            def.ResourceDocs,
		}
		if s.catalogValuePlane != nil {
			view.Instances = append([]ResourceInstance(nil), s.catalogValuePlane.Instances(orgID, def.Name)...)
		}
		view.EnvCells = s.registeredEnvCells(ctx, orgID, def.Name)
		out = append(out, view)
	}
	return out, nil
}

// isRegisteredExternalDef reports whether an RT-backed catalog row is a
// Registered External (org value plane) rather than a Project External.
// Register always writes consumption instructions; project provision
// authors the RT with them empty (see ExternalResourceProvisioner).
func isRegisteredExternalDef(def openchoreo.ExternalResourceDefinition) bool {
	return strings.TrimSpace(def.ConsumptionInstructions) != ""
}

// HasOrgEnvCells reports whether `name` is a Registered External in this org
// — non-empty process-local cells, or a catalog RT that still carries
// consumption instructions after the value plane was wiped (aep-api restart).
func (s *Service) HasOrgEnvCells(ctx context.Context, orgID, name string) bool {
	if s == nil {
		return false
	}
	if s.catalogValuePlane != nil && len(s.catalogValuePlane.EnvCells(orgID, name)) > 0 {
		return true
	}
	def, ok := s.registeredCatalogDef(ctx, orgID, name)
	return ok && isRegisteredExternalDef(def)
}

// registeredCatalogDef looks up the RT-backed catalog row for `name`.
func (s *Service) registeredCatalogDef(ctx context.Context, orgID, name string) (openchoreo.ExternalResourceDefinition, bool) {
	if s == nil || s.rtCatalog == nil {
		return openchoreo.ExternalResourceDefinition{}, false
	}
	defs, err := s.rtCatalog.List(ctx, orgID)
	if err != nil {
		return openchoreo.ExternalResourceDefinition{}, false
	}
	want := strings.ToLower(strings.TrimSpace(name))
	for i := range defs {
		if strings.ToLower(defs[i].Name) == want {
			return defs[i], true
		}
	}
	return openchoreo.ExternalResourceDefinition{}, false
}

// registeredEnvCells returns org value-plane cells for a Registered External
// name. Process-local cells win; when they are empty after restart, cells are
// synthesized from the RT (consumption instructions mark Registered) and
// warmed back onto the plane.
func (s *Service) registeredEnvCells(ctx context.Context, orgID, name string) []EnvCell {
	if s == nil {
		return nil
	}
	if s.catalogValuePlane != nil {
		if cells := s.catalogValuePlane.EnvCells(orgID, name); len(cells) > 0 {
			return s.ensureRegisteredVaultPaths(ctx, orgID, name, cells)
		}
	}
	def, ok := s.registeredCatalogDef(ctx, orgID, name)
	if !ok || !isRegisteredExternalDef(def) {
		return nil
	}
	cells := s.synthesizeRegisteredEnvCells(ctx, orgID, def)
	if s.catalogValuePlane != nil && len(cells) > 0 {
		s.catalogValuePlane.PutEnvCells(orgID, name, cells)
	}
	return cells
}

// synthesizeRegisteredEnvCells rebuilds org env cells from the RT schema and
// the org's environments when the process-local plane is empty. Secret values
// are never copied — only status, and SecretStorePath when the vault key can
// be derived from the request JWT.
func (s *Service) synthesizeRegisteredEnvCells(ctx context.Context, orgID string, def openchoreo.ExternalResourceDefinition) []EnvCell {
	envs := []string{defaultEnv}
	if names, err := s.ListOrgEnvironments(ctx, orgID); err == nil && len(names) > 0 {
		envs = names
	}
	keys := toConfigKeys(def.Config)
	if len(keys) == 0 {
		return nil
	}
	out := make([]EnvCell, 0, len(keys)*len(envs))
	vaultByEnv := map[string]string{}
	if r, ok := s.orgSecrets.(orgCatalogVaultKeyResolver); ok {
		for _, env := range envs {
			path, err := r.OrgCatalogVaultKey(ctx, orgID, def.Name+"-"+env)
			if err == nil && path != "" {
				vaultByEnv[env] = path
			}
		}
	}
	for _, env := range envs {
		for _, k := range keys {
			out = append(out, EnvCell{
				Environment: env,
				Key:         k.Key,
				Status:      "configured",
			})
		}
	}
	stampSecretStorePath(out, vaultByEnv)
	return out
}

// ensureRegisteredVaultPaths fills empty SecretStorePath on already-warmed
// cells when the org-catalog vault key can be derived from this request.
// List may have synthesized cells before JWT claims were on the context
// (preflight uses Background); provision then restamps from the user JWT.
func (s *Service) ensureRegisteredVaultPaths(ctx context.Context, orgID, name string, cells []EnvCell) []EnvCell {
	need := false
	for _, c := range cells {
		if c.SecretStorePath == "" {
			need = true
			break
		}
	}
	if !need {
		return cells
	}
	r, ok := s.orgSecrets.(orgCatalogVaultKeyResolver)
	if !ok {
		return cells
	}
	vaultByEnv := map[string]string{}
	seen := map[string]struct{}{}
	for _, c := range cells {
		if _, dup := seen[c.Environment]; dup {
			continue
		}
		seen[c.Environment] = struct{}{}
		path, err := r.OrgCatalogVaultKey(ctx, orgID, name+"-"+c.Environment)
		if err != nil || path == "" {
			continue
		}
		vaultByEnv[c.Environment] = path
	}
	if len(vaultByEnv) == 0 {
		return cells
	}
	out := append([]EnvCell(nil), cells...)
	stampSecretStorePath(out, vaultByEnv)
	if s.catalogValuePlane != nil {
		s.catalogValuePlane.PutEnvCells(orgID, name, out)
	}
	return out
}

// orgCatalogVaultKeyResolver is the optional OrgSecretWriter capability that
// reconstructs the org-catalog vault path without writing. *organization.SecretRefWriter
// satisfies it; test fakes typically do not.
type orgCatalogVaultKeyResolver interface {
	OrgCatalogVaultKey(ctx context.Context, orgID, entityName string) (string, error)
}

// toConfigKeys adapts the OC client's leaf-level config-key type (kept
// import-free of spec by design — see openchoreo.ExternalResourceConfigKey's
// doc) to the spec.ConfigKey the ExternalResourceDTO wire shape carries.
// Field-for-field identical; this is purely a package boundary crossing.
func toConfigKeys(keys []openchoreo.ExternalResourceConfigKey) []spec.ConfigKey {
	out := make([]spec.ConfigKey, 0, len(keys))
	for _, k := range keys {
		out = append(out, spec.ConfigKey{
			Key:          k.Key,
			Secret:       k.Secret,
			Description:  k.Description,
			DefaultValue: k.DefaultValue,
		})
	}
	return out
}

// DeleteExternalResource removes an org external-resource catalog entry —
// every namespaced OpenChoreo ResourceType registered under this logical name
// (rtCatalog.Delete; more than one schema-version RT can carry the same name —
// see openchoreo.ExternalResourceRTName). It is guarded: a resource with
// consumers returns ErrExternalResourceInUse (→ 409) and NO ResourceType is
// deleted — the design-sweep guard runs BEFORE the OC delete call.
func (s *Service) DeleteExternalResource(ctx context.Context, orgID, name string) error {
	consumers, err := s.consumersOf(ctx, orgID, name)
	if err != nil {
		return fmt.Errorf("provisioning: check consumers of %q: %w", name, err)
	}
	if len(consumers) > 0 {
		return ErrExternalResourceInUse
	}
	if err := s.rtCatalog.Delete(ctx, orgID, name); err != nil {
		return fmt.Errorf("provisioning: delete external resource %q: %w", name, err)
	}
	return nil
}

// consumersOf scans every project's committed design for components declaring an
// `external` dependency of the given name. Best-effort per project (a design read
// error skips that project). Returns nil when no project lister is wired.
func (s *Service) consumersOf(ctx context.Context, orgID, externalName string) ([]dependencies.ExternalResourceConsumer, error) {
	byName, err := s.externalConsumersByName(ctx, orgID)
	if err != nil {
		return nil, err
	}
	return byName[strings.ToLower(externalName)], nil
}

// externalConsumersByName builds, in one project sweep, the consumers of every
// external dependency name in the org (lowercased name → consumers). One sweep
// serves both the list (all entries) and a single-name delete guard.
func (s *Service) externalConsumersByName(ctx context.Context, orgID string) (map[string][]dependencies.ExternalResourceConsumer, error) {
	out := map[string][]dependencies.ExternalResourceConsumer{}
	if s.projects == nil {
		return out, nil
	}
	refs, err := s.projects.ListProjects(ctx, orgID)
	if err != nil {
		return nil, fmt.Errorf("provisioning: list projects: %w", err)
	}
	for _, ref := range refs {
		comps, derr := s.design.ReadDesignComponents(ctx, ref.OrgID, ref.ProjectID)
		if derr != nil {
			continue // best-effort: a project without a readable design has no consumers
		}
		for _, c := range comps {
			for _, d := range c.Dependencies {
				if d.Kind != spec.DependencyKindExternal {
					continue
				}
				key := strings.ToLower(d.Name)
				out[key] = append(out[key], dependencies.ExternalResourceConsumer{
					ProjectID:     ref.ProjectID,
					ComponentName: c.Name,
				})
			}
		}
	}
	return out, nil
}
