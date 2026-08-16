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

package projects

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

type ConfigService interface {
	GetConfig(ctx context.Context, orgID, projectName, componentName string) (*ComponentConfig, error)
	UpdateConfig(ctx context.Context, orgID, projectName, componentName string, envVars EnvVarSlice) (*ComponentConfig, error)
	GetEnvVarsForDeploy(ctx context.Context, orgID, projectName, componentName string) (EnvVarSlice, error)
	// SetConverger attaches the binding converger after construction. It is a
	// setter because the two services are mutually dependent — the deployment
	// service reads env vars from here while composing a binding, and an edit
	// here pushes onto the live binding through there — and one of them has to be
	// built first.
	SetConverger(c BindingConverger)
}

// BindingConverger re-asserts a deployed component's wiring from its current
// desired state. Declared consumer-side; *DeploymentService satisfies it.
type BindingConverger interface {
	Converge(ctx context.Context, orgID, projectID string, components []string) error
}

type configService struct {
	repo        ConfigRepository
	deployments BindingConverger
}

// NewConfigService wires the config repo and (optionally) the binding converger
// that pushes an env-var edit onto the live deployment. Pass nil to disable that
// push — the env vars still land in the DB, and the next deploy carries them.
func NewConfigService(repo ConfigRepository, deployments BindingConverger) ConfigService {
	return &configService{repo: repo, deployments: deployments}
}

// SetConverger attaches the binding converger. Nil leaves an env-var edit
// landing in the DB only, which the next deploy picks up.
func (s *configService) SetConverger(c BindingConverger) {
	if s != nil {
		s.deployments = c
	}
}

func (s *configService) GetConfig(ctx context.Context, orgID, projectName, componentName string) (*ComponentConfig, error) {
	config, err := s.repo.GetByComponent(ctx, orgID, projectName, componentName)
	if err != nil {
		return nil, fmt.Errorf("get config: %w", err)
	}
	return config, nil
}

func (s *configService) UpdateConfig(ctx context.Context, orgID, projectName, componentName string, envVars EnvVarSlice) (*ComponentConfig, error) {
	// Validate env vars
	seen := make(map[string]bool, len(envVars))
	for _, ev := range envVars {
		key := strings.TrimSpace(ev.Key)
		if key == "" {
			return nil, fmt.Errorf("environment variable key cannot be empty")
		}
		if seen[key] {
			return nil, fmt.Errorf("duplicate environment variable key: %s", key)
		}
		seen[key] = true
	}

	config := &ComponentConfig{
		OrgID:         orgID,
		ProjectName:   projectName,
		ComponentName: componentName,
		EnvVars:       envVars,
	}

	if err := s.repo.Upsert(ctx, config); err != nil {
		return nil, fmt.Errorf("update config: %w", err)
	}

	slog.InfoContext(ctx, "updated component config",
		"org", orgID, "project", projectName, "component", componentName, "envVarCount", len(envVars))

	// Converge the component's ReleaseBinding so OC renders the new values into
	// the pod spec on its next reconcile — no rebuild needed.
	//
	// A CONVERGE, not a field patch: the binding has one writer, and it composes
	// the whole desired state from the DB record this function just wrote. That
	// is what stops an env-var edit from clobbering the trait config, which a
	// per-field write to the same object could do the moment the two disagreed
	// about anything else on it. The release in flight is never re-pinned.
	//
	// Best-effort: the DB is the canonical record, and a component with no
	// binding yet picks these values up when the deploy stage first creates one.
	if s.deployments != nil {
		if err := s.deployments.Converge(ctx, orgID, projectName, []string{componentName}); err != nil {
			slog.WarnContext(ctx, "converge component binding failed; DB is updated, the next deploy will carry the values",
				"org", orgID, "project", projectName, "component", componentName, "error", err)
		}
	}

	return config, nil
}

func (s *configService) GetEnvVarsForDeploy(ctx context.Context, orgID, projectName, componentName string) (EnvVarSlice, error) {
	config, err := s.repo.GetByComponent(ctx, orgID, projectName, componentName)
	if err != nil {
		return nil, fmt.Errorf("get config for deploy: %w", err)
	}
	if config == nil || len(config.EnvVars) == 0 {
		return nil, nil
	}
	return config.EnvVars, nil
}
