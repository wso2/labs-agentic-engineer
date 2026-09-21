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
	"path"
	"strings"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/platform/apierr"
	"github.com/wso2/aep/aep-api/internal/platform/ocname"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// PromoteExternalResource makes a project's own external resource a Registered
// External resource of the organization, then turns the project's dependency
// into a copy of that record (ADR-0030: a dependency holds a full copy of its
// resource, with `ref`).
//
// The record is the project's block as it stands — name, provider, keys,
// description, contract document — plus what only the organization can add:
// how consumers should use it, and a value for every key in every
// environment. An environment the request leaves out is carried over from the
// project's own values when its binding has them (plain values from the
// binding, secrets copied vault to vault); an environment with neither is
// refused. Validation matches Register, and so does the uniqueness rule: a
// name the organization already registered is refused — the project should
// reuse that record instead.
//
// Order: contract committed, value plane written, type ensured, then the
// project's file rewritten. A failure after the type exists leaves a record
// the project does not yet reference; the next Promote reads "already
// registered" and the project's Reconsider → reuse path takes it from there.
func (s *Service) PromoteExternalResource(ctx context.Context, orgID, projectID, name string, req gen.PromoteExternalResourceRequest) (ExternalResourceView, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return ExternalResourceView{}, apierr.BadRequest("name is required")
	}
	if s.promoter == nil {
		return ExternalResourceView{}, fmt.Errorf("provisioning: project resource promoter is not configured")
	}
	if s.rtCatalog == nil {
		return ExternalResourceView{}, fmt.Errorf("provisioning: external RT catalog is not configured")
	}
	instructions := strings.TrimSpace(req.ConsumptionInstructions)
	if instructions == "" {
		return ExternalResourceView{}, apierr.BadRequest("consumptionInstructions is required")
	}

	pr, err := s.promoter.ReadProjectResource(ctx, orgID, projectID, name)
	if err != nil {
		switch {
		case errors.Is(err, spec.ErrDependencyNotFound):
			return ExternalResourceView{}, apierr.NotFound(fmt.Sprintf("project %q has no external dependency %q", projectID, name))
		case errors.Is(err, spec.ErrDependencyIsCopy):
			return ExternalResourceView{}, apierr.Conflict(fmt.Sprintf("%q already reuses the organization's record", name))
		}
		return ExternalResourceView{}, fmt.Errorf("provisioning: read project resource %q: %w", name, err)
	}
	res := pr.Definition.Resource
	provider := strings.TrimSpace(res.Provider)
	if provider == "" {
		return ExternalResourceView{}, apierr.BadRequest(fmt.Sprintf("%q names no provider yet — choose one in the project first", name))
	}
	if len(res.Config) == 0 {
		return ExternalResourceView{}, apierr.BadRequest(fmt.Sprintf("%q declares no config keys — the organization has nothing to hold values for", name))
	}
	description := strings.TrimSpace(req.Description)
	if description == "" {
		description = strings.TrimSpace(res.Description)
	}
	if description == "" {
		return ExternalResourceView{}, apierr.BadRequest("description is required")
	}
	keys := rtConfigKeysFromSpec(res.Config)

	existing, err := s.rtCatalog.List(ctx, orgID)
	if err != nil {
		return ExternalResourceView{}, fmt.Errorf("provisioning: list external resources: %w", err)
	}
	for _, def := range existing {
		if strings.EqualFold(def.Name, name) {
			return ExternalResourceView{}, apierr.Conflict("external resource " + name + " is already registered — have the project reuse it instead")
		}
	}

	envInfos, err := s.ListOrgEnvironments(ctx, orgID)
	if err != nil {
		return ExternalResourceView{}, err
	}
	envNames := environmentNames(envInfos)
	valueByEnvKey, carried, err := s.promotedValues(ctx, orgID, projectID, name, keys, envNames, req.EnvValues)
	if err != nil {
		return ExternalResourceView{}, err
	}

	var contract *openchoreo.ResourceContractPointer
	var provenance *openchoreo.ResourceRecordProvenance
	if c := res.Contract; c != nil && c.Path != "" {
		if pr.Document == "" {
			// The block names a document that is not on disk: the dependency
			// reads needs-contract in the project, and a record made from it
			// would silently drop the contract. Refuse until the project has it.
			return ExternalResourceView{}, apierr.BadRequest(fmt.Sprintf("contract: %q names %s but the project holds no such document — provide it in the project first", name, c.Path))
		}
		if len(pr.Document) > maxContractBytes {
			return ExternalResourceView{}, apierr.BadRequest("contract: the project's document is larger than 5 MiB — trim it in the project first")
		}
		contract, provenance, err = s.commitResourceContract(ctx, orgID, name, &contractWrite{Type: c.Type, FileName: path.Base(c.Path), Content: pr.Document})
		if err != nil {
			return ExternalResourceView{}, err
		}
		if pr.Definition.Provenance != nil && provenance != nil {
			provenance.SourceURL = pr.Definition.Provenance.SourceURL
		}
	}
	rt, err := openchoreo.BuildExternalResourceType(openchoreo.ExternalResourceTypeSpec{
		Name:                    name,
		Description:             description,
		Keys:                    keys,
		Scope:                   openchoreo.ExternalResourceScopeOrg,
		Provider:                provider,
		Contract:                contract,
		Provenance:              provenance,
		ConsumptionInstructions: instructions,
	})
	if err != nil {
		return ExternalResourceView{}, apierr.BadRequest(err.Error())
	}
	record, ok := openchoreo.ExternalDefinitionFromRT(rt)
	if !ok {
		return ExternalResourceView{}, fmt.Errorf("provisioning: the built type for %q does not read back as an external resource record", name)
	}

	cells, err := s.writeOrgValuePlane(ctx, orgID, orgValuePlaneWrite{
		Name: name, Keys: keys, EnvNames: envNames, ValueByEnvKey: valueByEnvKey, CarriedFrom: carried,
	})
	if err != nil {
		return ExternalResourceView{}, err
	}
	if err := s.rtCatalog.Ensure(ctx, orgID, rt); err != nil {
		return ExternalResourceView{}, fmt.Errorf("provisioning: ensure external resource type %q: %w", name, err)
	}
	if err := s.promoter.RewriteAsRegistryCopy(ctx, orgID, projectID, name, RegisteredResourceFromRecord(record, pr.Document)); err != nil {
		return ExternalResourceView{}, fmt.Errorf("provisioning: rewrite %s/%s as a copy of the record: %w", projectID, name, err)
	}

	consumers, err := s.consumersOf(ctx, orgID, name)
	if err != nil {
		return ExternalResourceView{}, err
	}
	return ExternalResourceView{
		Name:                    name,
		Description:             description,
		Provider:                provider,
		Config:                  toConfigKeys(keys),
		Contract:                contract,
		Provenance:              provenance,
		Scope:                   openchoreo.ExternalResourceScopeOrg,
		Consumers:               consumers,
		ConsumptionInstructions: instructions,
		EnvCells:                cells,
	}, nil
}

// promotedValues resolves one value per key × environment for the record. A
// request row wins; an environment the request does not fully cover is carried
// over from the project's own binding — plain keys from its values, the secret
// keys by copying its vault key (returned in carried) — and an environment with
// neither is a 400 naming the first missing key.
func (s *Service) promotedValues(ctx context.Context, orgID, projectID, name string, keys []openchoreo.ExternalResourceConfigKey, envNames []string, rows []gen.EnvValueWriteDTO) (valueByEnvKey map[string]string, carried map[string]string, err error) {
	valueByEnvKey = make(map[string]string, len(rows))
	for _, row := range rows {
		env, key := strings.TrimSpace(row.Environment), strings.TrimSpace(row.Key)
		if env == "" || key == "" || strings.TrimSpace(row.Value) == "" {
			continue
		}
		valueByEnvKey[envValueKey(env, key)] = row.Value
	}
	carried = map[string]string{}
	for _, env := range envNames {
		var missingPlain, missingSecret []openchoreo.ExternalResourceConfigKey
		for _, k := range keys {
			if _, ok := valueByEnvKey[envValueKey(env, k.Key)]; ok {
				continue
			}
			if k.Secret {
				missingSecret = append(missingSecret, k)
			} else {
				missingPlain = append(missingPlain, k)
			}
		}
		if len(missingPlain) == 0 && len(missingSecret) == 0 {
			continue
		}
		bound := s.projectBindingValues(ctx, orgID, projectID, name, env)
		for _, k := range missingPlain {
			v := bound[k.Key]
			if strings.TrimSpace(v) == "" {
				return nil, nil, apierr.BadRequest(fmt.Sprintf("missing env value for key %q in environment %q", k.Key, env))
			}
			valueByEnvKey[envValueKey(env, k.Key)] = v
		}
		if len(missingSecret) == 0 {
			continue
		}
		vaultKey := bound[openchoreo.SecretStorePathField]
		if vaultKey == "" {
			return nil, nil, apierr.BadRequest(fmt.Sprintf("missing env value for key %q in environment %q", missingSecret[0].Key, env))
		}
		for _, k := range keys {
			if k.Secret && valueByEnvKey[envValueKey(env, k.Key)] != "" {
				// The vault copy carries every secret key of the environment at
				// once; a request that also types one of them would be
				// silently ignored, so it is refused instead.
				return nil, nil, apierr.BadRequest(fmt.Sprintf("environment %q: give every secret value, or leave them all to be carried over from the project", env))
			}
		}
		carried[env] = vaultKey
	}
	return valueByEnvKey, carried, nil
}

// projectBindingValues returns the project's per-environment binding values for
// its own external resource — plain keys by name, plus secretStorePath — or an
// empty map when there is no binding to read.
func (s *Service) projectBindingValues(ctx context.Context, orgID, projectID, name, env string) map[string]string {
	if s.bindings == nil {
		return map[string]string{}
	}
	b, err := s.bindings.GetBinding(ctx, orgID, ocname.ExternalResourceBindingName(projectID, name, env))
	if err != nil {
		return map[string]string{}
	}
	values, err := bindingValues(b)
	if err != nil {
		return map[string]string{}
	}
	return values
}

func rtConfigKeysFromSpec(in []spec.ConfigKey) []openchoreo.ExternalResourceConfigKey {
	out := make([]openchoreo.ExternalResourceConfigKey, 0, len(in))
	for _, k := range in {
		out = append(out, openchoreo.ExternalResourceConfigKey{
			Key:          strings.TrimSpace(k.Key),
			Secret:       k.Secret,
			Description:  strings.TrimSpace(k.Description),
			DefaultValue: k.DefaultValue,
		})
	}
	return out
}

// RegisteredResourceFromRecord is the registry record as the copy renderer
// takes it: the record's block in the one resource shape, with the document
// the organization holds for it. The Apply-time copy (a design turn naming a
// registered resource) and Promote both build the project's copy from it, so
// the two land byte-identical files.
func RegisteredResourceFromRecord(def openchoreo.ExternalResourceDefinition, document string) spec.RegisteredResource {
	res := spec.ResourceDefinition{
		Name:                    def.Name,
		Description:             def.Description,
		Provider:                def.Provider,
		ConsumptionInstructions: def.ConsumptionInstructions,
		Config:                  toConfigKeys(def.Config),
	}
	if def.Contract != nil {
		res.Contract = &spec.ResourceContract{Type: def.Contract.Type, Path: def.Contract.Path}
	}
	if def.Provenance != nil {
		res.Provenance = &spec.ResourceProvenance{SourceURL: def.Provenance.SourceURL, SHA256: def.Provenance.SHA256, ReadOn: def.Provenance.ReadOn}
	}
	return spec.RegisteredResource{Resource: res, Document: document}
}
