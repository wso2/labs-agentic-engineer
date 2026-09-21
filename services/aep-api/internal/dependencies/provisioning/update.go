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
	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/platform/apierr"
)

const envCellStatusUnset = "unset"

// UpdateExternalResource mutates a Registered External resource identified by
// the path name. The request body reuses RegisterExternalResourceRequest; the
// body's name is ignored. Config key identity (key + secret) is immutable.
// Empty secret env values keep the current value-plane Value; an empty value
// on an Unset secret cell is rejected. Catalog fields persist via
// rtCatalog.Update (PUT), not Ensure.
func (s *Service) UpdateExternalResource(ctx context.Context, orgID, name string, req gen.RegisterExternalResourceRequest) (ExternalResourceView, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return ExternalResourceView{}, apierr.BadRequest("name is required")
	}
	if s.rtCatalog == nil {
		return ExternalResourceView{}, fmt.Errorf("provisioning: external RT catalog is not configured")
	}
	existing, err := s.rtCatalog.List(ctx, orgID)
	if err != nil {
		return ExternalResourceView{}, fmt.Errorf("provisioning: list external resources: %w", err)
	}
	var found *openchoreo.ExternalResourceDefinition
	for i := range existing {
		if strings.EqualFold(existing[i].Name, name) {
			found = &existing[i]
			break
		}
	}
	if found == nil {
		return ExternalResourceView{}, apierr.NotFound("external resource " + name + " not found")
	}
	canonical := found.Name

	var currentCells []EnvCell
	if s.catalogValuePlane != nil {
		currentCells = s.catalogValuePlane.EnvCells(orgID, canonical)
	}
	if len(currentCells) == 0 {
		return ExternalResourceView{}, apierr.Conflict("external resource " + canonical + " is a Project External resource")
	}

	keys, writes, envNames, valueByEnvKey, err := s.validateUpdateRequest(ctx, orgID, req, found.Config, currentCells)
	if err != nil {
		return ExternalResourceView{}, err
	}

	docs, err := s.commitResourceDocs(ctx, orgID, canonical, writes)
	if err != nil {
		return ExternalResourceView{}, err
	}
	// The contract document is replaced only when the request carries one;
	// otherwise the record keeps its pointer and provenance.
	contractWrite, err := validateContractWrite(req.Contract)
	if err != nil {
		return ExternalResourceView{}, err
	}
	contract, provenance := found.Contract, found.Provenance
	if contractWrite != nil {
		if contract, provenance, err = s.commitResourceContract(ctx, orgID, canonical, contractWrite); err != nil {
			return ExternalResourceView{}, err
		}
	}
	provider := strings.TrimSpace(req.Provider)
	if provider == "" {
		provider = found.Provider
	}

	rt, err := openchoreo.BuildExternalResourceType(openchoreo.ExternalResourceTypeSpec{
		Name:                    canonical,
		Description:             strings.TrimSpace(req.Description),
		Keys:                    keys,
		Scope:                   openchoreo.ExternalResourceScopeOrg,
		Provider:                provider,
		Contract:                contract,
		Provenance:              provenance,
		ConsumptionInstructions: strings.TrimSpace(req.ConsumptionInstructions),
		ResourceDocs:            docs,
	})
	if err != nil {
		return ExternalResourceView{}, apierr.BadRequest(err.Error())
	}

	existingVaultByEnv := map[string]string{}
	for _, c := range currentCells {
		if c.SecretStorePath != "" {
			existingVaultByEnv[c.Environment] = c.SecretStorePath
		}
	}
	cells, err := s.writeOrgValuePlane(ctx, orgID, orgValuePlaneWrite{
		Name: canonical, Keys: keys, EnvNames: envNames, ValueByEnvKey: valueByEnvKey, ExistingVaultByEnv: existingVaultByEnv,
	})
	if err != nil {
		return ExternalResourceView{}, err
	}
	if err := s.rtCatalog.Update(ctx, orgID, rt); err != nil {
		return ExternalResourceView{}, fmt.Errorf("provisioning: update external resource type %q: %w", canonical, err)
	}

	return ExternalResourceView{
		Name:                    canonical,
		Description:             strings.TrimSpace(req.Description),
		Provider:                provider,
		Config:                  toConfigKeys(keys),
		Contract:                contract,
		Provenance:              provenance,
		Scope:                   openchoreo.ExternalResourceScopeOrg,
		ConsumptionInstructions: strings.TrimSpace(req.ConsumptionInstructions),
		EnvCells:                cells,
		ResourceDocs:            docs,
	}, nil
}

func (s *Service) validateUpdateRequest(
	ctx context.Context,
	orgID string,
	req gen.RegisterExternalResourceRequest,
	existing []openchoreo.ExternalResourceConfigKey,
	currentCells []EnvCell,
) (
	keys []openchoreo.ExternalResourceConfigKey,
	writes []resourceDocWrite,
	envNames []string,
	valueByEnvKey map[string]string,
	err error,
) {
	if strings.TrimSpace(req.Description) == "" {
		return nil, nil, nil, nil, apierr.BadRequest("description is required")
	}
	if strings.TrimSpace(req.ConsumptionInstructions) == "" {
		return nil, nil, nil, nil, apierr.BadRequest("consumptionInstructions is required")
	}
	if len(req.Config) == 0 {
		return nil, nil, nil, nil, apierr.BadRequest("config must have at least one key")
	}
	keys = make([]openchoreo.ExternalResourceConfigKey, 0, len(req.Config))
	for i, k := range req.Config {
		key := strings.TrimSpace(k.Key)
		if key == "" {
			return nil, nil, nil, nil, apierr.BadRequest(fmt.Sprintf("config[%d]: key is required", i))
		}
		if strings.TrimSpace(k.Description) == "" {
			return nil, nil, nil, nil, apierr.BadRequest(fmt.Sprintf("config key %q: description is required", key))
		}
		keys = append(keys, openchoreo.ExternalResourceConfigKey{
			Key:          key,
			Secret:       k.Secret,
			Description:  strings.TrimSpace(k.Description),
			DefaultValue: k.DefaultValue,
		})
	}
	if !sameConfigIdentity(existing, keys) {
		return nil, nil, nil, nil, apierr.BadRequest("config key identity cannot be changed")
	}

	writes, err = validateResourceDocWrites(req.ResourceDocs)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	envInfos, err := s.ListOrgEnvironments(ctx, orgID)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	envNames = environmentNames(envInfos)

	currentByEnvKey := make(map[string]EnvCell, len(currentCells))
	for _, c := range currentCells {
		currentByEnvKey[envValueKey(c.Environment, c.Key)] = c
	}

	valueByEnvKey = make(map[string]string, len(req.EnvValues))
	for _, row := range req.EnvValues {
		env, key := strings.TrimSpace(row.Environment), strings.TrimSpace(row.Key)
		if env == "" || key == "" {
			continue
		}
		valueByEnvKey[envValueKey(env, key)] = row.Value
	}
	for _, env := range envNames {
		for _, k := range keys {
			id := envValueKey(env, k.Key)
			val, ok := valueByEnvKey[id]
			if k.Secret && (!ok || strings.TrimSpace(val) == "") {
				cur, exists := currentByEnvKey[id]
				if !exists || cur.Status == envCellStatusUnset || strings.TrimSpace(cur.Value) == "" {
					return nil, nil, nil, nil, apierr.BadRequest(fmt.Sprintf("missing env value for key %q in environment %q", k.Key, env))
				}
				valueByEnvKey[id] = cur.Value
				continue
			}
			if !ok || strings.TrimSpace(val) == "" {
				return nil, nil, nil, nil, apierr.BadRequest(fmt.Sprintf("missing env value for key %q in environment %q", k.Key, env))
			}
		}
	}
	return keys, writes, envNames, valueByEnvKey, nil
}

func sameConfigIdentity(existing, next []openchoreo.ExternalResourceConfigKey) bool {
	if len(existing) != len(next) {
		return false
	}
	got := make(map[string]bool, len(next))
	for _, k := range next {
		if _, dup := got[k.Key]; dup {
			return false
		}
		got[k.Key] = k.Secret
	}
	if len(got) != len(existing) {
		return false
	}
	for _, k := range existing {
		secret, ok := got[k.Key]
		if !ok || secret != k.Secret {
			return false
		}
	}
	return true
}
