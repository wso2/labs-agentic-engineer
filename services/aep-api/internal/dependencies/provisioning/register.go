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

// RegisterExternalResource authors a Registered External resource on the org
// catalog: an OpenChoreo ResourceType (Ensure only — no project Resource
// instance) plus org value-plane cells. Secret bytes are optionally written
// through OrgSecretWriter with projectName "org-catalog"; the returned vault
// key is stored on those cells as SecretStorePath.
func (s *Service) RegisterExternalResource(ctx context.Context, orgID string, req gen.RegisterExternalResourceRequest) (ExternalResourceView, error) {
	name, keys, writes, envNames, valueByEnvKey, err := s.validateRegisterRequest(ctx, orgID, req)
	if err != nil {
		return ExternalResourceView{}, err
	}

	if s.rtCatalog == nil {
		return ExternalResourceView{}, fmt.Errorf("provisioning: external RT catalog is not configured")
	}
	existing, err := s.rtCatalog.List(ctx, orgID)
	if err != nil {
		return ExternalResourceView{}, fmt.Errorf("provisioning: list external resources: %w", err)
	}
	for _, def := range existing {
		if strings.EqualFold(def.Name, name) {
			return ExternalResourceView{}, apierr.Conflict("external resource " + name + " is already registered")
		}
	}

	docs, err := s.commitResourceDocs(ctx, orgID, name, writes)
	if err != nil {
		return ExternalResourceView{}, err
	}

	rt, err := openchoreo.BuildExternalResourceType(name, strings.TrimSpace(req.Description), keys, strings.TrimSpace(req.ConsumptionInstructions), docs)
	if err != nil {
		return ExternalResourceView{}, apierr.BadRequest(err.Error())
	}

	cells := make([]EnvCell, 0, len(keys)*len(envNames))
	for _, env := range envNames {
		for _, k := range keys {
			cell := EnvCell{
				Environment: env,
				Key:         k.Key,
				Status:      "configured",
				Value:       valueByEnvKey[envValueKey(env, k.Key)],
			}
			cells = append(cells, cell)
		}
	}
	vaultByEnv := map[string]string{}
	if s.orgSecrets != nil {
		for _, env := range envNames {
			secrets := map[string]string{}
			for _, k := range keys {
				if k.Secret {
					secrets[k.Key] = valueByEnvKey[envValueKey(env, k.Key)]
				}
			}
			if len(secrets) == 0 {
				continue
			}
			vaultKey, err := s.orgSecrets.WriteOrgCatalogSecret(ctx, orgID, name+"-"+env, secrets)
			if err != nil {
				return ExternalResourceView{}, fmt.Errorf("provisioning: write org-catalog secret %q: %w", name+"-"+env, err)
			}
			vaultByEnv[env] = vaultKey
		}
	}
	stampSecretStorePath(cells, vaultByEnv)
	if s.catalogValuePlane != nil {
		s.catalogValuePlane.PutEnvCells(orgID, name, cells)
	}
	// Ensure last: List treats the name as registered only after the RT exists.
	// A failed Ensure must not 409 a retry solely because cells/secrets already landed.
	if err := s.rtCatalog.Ensure(ctx, orgID, rt); err != nil {
		return ExternalResourceView{}, fmt.Errorf("provisioning: ensure external resource type %q: %w", name, err)
	}

	return ExternalResourceView{
		Name:                    name,
		Description:             strings.TrimSpace(req.Description),
		Config:                  toConfigKeys(keys),
		ConsumptionInstructions: strings.TrimSpace(req.ConsumptionInstructions),
		EnvCells:                cells,
		ResourceDocs:            docs,
	}, nil
}

func (s *Service) validateRegisterRequest(ctx context.Context, orgID string, req gen.RegisterExternalResourceRequest) (
	name string,
	keys []openchoreo.ExternalResourceConfigKey,
	writes []resourceDocWrite,
	envNames []string,
	valueByEnvKey map[string]string,
	err error,
) {
	name = strings.TrimSpace(req.Name)
	if name == "" {
		return "", nil, nil, nil, nil, apierr.BadRequest("name is required")
	}
	if strings.TrimSpace(req.Description) == "" {
		return "", nil, nil, nil, nil, apierr.BadRequest("description is required")
	}
	if strings.TrimSpace(req.ConsumptionInstructions) == "" {
		return "", nil, nil, nil, nil, apierr.BadRequest("consumptionInstructions is required")
	}
	if len(req.Config) == 0 {
		return "", nil, nil, nil, nil, apierr.BadRequest("config must have at least one key")
	}
	keys = make([]openchoreo.ExternalResourceConfigKey, 0, len(req.Config))
	for i, k := range req.Config {
		key := strings.TrimSpace(k.Key)
		if key == "" {
			return "", nil, nil, nil, nil, apierr.BadRequest(fmt.Sprintf("config[%d]: key is required", i))
		}
		if strings.TrimSpace(k.Description) == "" {
			return "", nil, nil, nil, nil, apierr.BadRequest(fmt.Sprintf("config key %q: description is required", key))
		}
		keys = append(keys, openchoreo.ExternalResourceConfigKey{
			Key:          key,
			Secret:       k.Secret,
			Description:  strings.TrimSpace(k.Description),
			DefaultValue: k.DefaultValue,
		})
	}

	writes, err = validateResourceDocWrites(req.ResourceDocs)
	if err != nil {
		return "", nil, nil, nil, nil, err
	}

	envNames, err = s.ListOrgEnvironments(ctx, orgID)
	if err != nil {
		return "", nil, nil, nil, nil, err
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
			val, ok := valueByEnvKey[envValueKey(env, k.Key)]
			if !ok || strings.TrimSpace(val) == "" {
				return "", nil, nil, nil, nil, apierr.BadRequest(fmt.Sprintf("missing env value for key %q in environment %q", k.Key, env))
			}
		}
	}
	return name, keys, writes, envNames, valueByEnvKey, nil
}

func envValueKey(env, key string) string {
	return env + "\x00" + key
}
