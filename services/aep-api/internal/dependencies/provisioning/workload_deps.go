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
	"sort"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
)

const (
	kindClusterResourceType = "ClusterResourceType"
	kindResourceType        = "ResourceType"
	visibilityNamespace     = "namespace"
	annotationExternalName  = "aep.wso2.com/external-name"

	depKindResource   = "resource"
	depKindOrgService = "org-service"
	depTagPlatform    = "platform"
	depTagExternal    = "external"
)

// WorkloadDependencyView is one deduped Overview row sourced from deployed
// Workload consumer refs (not design.json).
type WorkloadDependencyView struct {
	Kind      string
	Ref       string
	Tag       string
	Name      string
	Project   string
	Component string
}

// ListWorkloadDependencies returns the project's deployed-workload dependencies
// as deduped Overview rows. A nil Workloads port on a non-nil service is a
// lister failure (the handler maps it to the default 500). Dangling GetResource
// 404s are omitted. Lister transport failures propagate so the handler can map
// them to the default 500. A nil handler service stays 503.
func (s *Service) ListWorkloadDependencies(ctx context.Context, orgID, projectName string) ([]WorkloadDependencyView, error) {
	if s.workloads == nil {
		return nil, fmt.Errorf("provisioning: list workload consumer deps: workloads port is nil")
	}
	consumers, err := s.workloads.ListWorkloadConsumerDeps(ctx, orgID, projectName)
	if err != nil {
		return nil, fmt.Errorf("provisioning: list workload consumer deps: %w", err)
	}

	seenResource := map[string]struct{}{}
	seenOrgService := map[string]struct{}{}
	out := make([]WorkloadDependencyView, 0)

	for _, wl := range consumers {
		for _, ref := range wl.ResourceRefs {
			row, ok, err := s.resolveResourceRow(ctx, orgID, ref)
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
			key := row.Tag + "\x00" + row.Ref
			if _, dup := seenResource[key]; dup {
				continue
			}
			seenResource[key] = struct{}{}
			out = append(out, row)
		}
		for _, ep := range wl.Endpoints {
			row, ok := orgServiceRow(projectName, ep)
			if !ok {
				continue
			}
			key := row.Project + "\x00" + row.Component
			if _, dup := seenOrgService[key]; dup {
				continue
			}
			seenOrgService[key] = struct{}{}
			out = append(out, row)
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		if out[i].Ref != out[j].Ref {
			return out[i].Ref < out[j].Ref
		}
		if out[i].Project != out[j].Project {
			return out[i].Project < out[j].Project
		}
		return out[i].Component < out[j].Component
	})
	return out, nil
}

func (s *Service) resolveResourceRow(ctx context.Context, orgID, instanceName string) (WorkloadDependencyView, bool, error) {
	if instanceName == "" {
		return WorkloadDependencyView{}, false, nil
	}
	res, err := s.workloads.GetResource(ctx, orgID, instanceName)
	if err != nil {
		if errors.Is(err, openchoreo.ErrNotFound) {
			return WorkloadDependencyView{}, false, nil
		}
		return WorkloadDependencyView{}, false, fmt.Errorf("provisioning: get resource %q: %w", instanceName, err)
	}
	if res == nil {
		return WorkloadDependencyView{}, false, nil
	}

	typeKind := res.Spec.Type.Kind
	if typeKind == "" {
		typeKind = kindResourceType
	}
	typeName := res.Spec.Type.Name
	if typeName == "" {
		return WorkloadDependencyView{}, false, nil
	}

	switch typeKind {
	case kindClusterResourceType:
		return WorkloadDependencyView{
			Kind: depKindResource,
			Tag:  depTagPlatform,
			Ref:  typeName,
			Name: typeName,
		}, true, nil
	case kindResourceType:
		rt, err := s.workloads.GetResourceType(ctx, orgID, typeName)
		if err != nil {
			if errors.Is(err, openchoreo.ErrNotFound) {
				return WorkloadDependencyView{}, false, nil
			}
			return WorkloadDependencyView{}, false, fmt.Errorf("provisioning: get resource type %q: %w", typeName, err)
		}
		if rt == nil {
			return WorkloadDependencyView{}, false, nil
		}
		logical := ""
		if rt.Metadata.Annotations != nil {
			logical = rt.Metadata.Annotations[annotationExternalName]
		}
		if logical == "" {
			logical = typeName
		}
		return WorkloadDependencyView{
			Kind: depKindResource,
			Tag:  depTagExternal,
			Ref:  logical,
			Name: logical,
		}, true, nil
	default:
		return WorkloadDependencyView{}, false, nil
	}
}

func orgServiceRow(consumerProject string, ep openchoreo.WorkloadConsumerEndpoint) (WorkloadDependencyView, bool) {
	if ep.Visibility != visibilityNamespace || ep.Component == "" {
		return WorkloadDependencyView{}, false
	}
	providerProject := ep.Project
	if providerProject == "" || providerProject == consumerProject {
		return WorkloadDependencyView{}, false
	}
	return WorkloadDependencyView{
		Kind:      depKindOrgService,
		Name:      ep.Component,
		Project:   providerProject,
		Component: ep.Component,
	}, true
}
