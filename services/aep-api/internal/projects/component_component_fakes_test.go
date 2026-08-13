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

// Component-tier (package component_test, external) hand fakes for the
// out-of-process seams the component/config HTTP surface drives. Separate from
// the in-package fakes_test.go because the component tier is an external test
// package (the harness imports api, which imports component — an in-package
// test file would be an import cycle).
package projects_test

import (
	"context"
	"time"

	"github.com/wso2/aep/aep-api/internal/projects"

	"github.com/wso2/aep/aep-api/internal/gen"

	"github.com/wso2/aep/aep-api/internal/clients/observability"
)

// --- observability.Client -----------------------------------------------------

type extObservClient struct {
	GetBuildLogsFunc func(ctx context.Context, orgName, projectName, componentName, buildName string, since time.Time) (*gen.BuildLogs, error)
}

var _ observability.Client = (*extObservClient)(nil)

func (s *extObservClient) GetBuildLogs(ctx context.Context, orgName, projectName, componentName, buildName string, since time.Time) (*gen.BuildLogs, error) {
	if s.GetBuildLogsFunc == nil {
		panic("extObservClient: GetBuildLogs not set")
	}
	return s.GetBuildLogsFunc(ctx, orgName, projectName, componentName, buildName, since)
}

func (s *extObservClient) QueryComponentLogs(context.Context, observability.ComponentLogQuery) ([]observability.LogLine, error) {
	panic("extObservClient: QueryComponentLogs not expected")
}

// --- projects.ConfigRepository --------------------------------------------

type extConfigRepo struct {
	GetByComponentFunc func(ctx context.Context, orgID, projectName, componentName string) (*projects.ComponentConfig, error)
	UpsertFunc         func(ctx context.Context, config *projects.ComponentConfig) error
}

var _ projects.ConfigRepository = (*extConfigRepo)(nil)

func (s *extConfigRepo) GetByComponent(ctx context.Context, orgID, projectName, componentName string) (*projects.ComponentConfig, error) {
	if s.GetByComponentFunc == nil {
		panic("extConfigRepo: GetByComponent not set")
	}
	return s.GetByComponentFunc(ctx, orgID, projectName, componentName)
}
func (s *extConfigRepo) Upsert(ctx context.Context, config *projects.ComponentConfig) error {
	if s.UpsertFunc == nil {
		panic("extConfigRepo: Upsert not set")
	}
	return s.UpsertFunc(ctx, config)
}
func (s *extConfigRepo) DeleteAll(context.Context) error {
	panic("extConfigRepo: DeleteAll not expected")
}
