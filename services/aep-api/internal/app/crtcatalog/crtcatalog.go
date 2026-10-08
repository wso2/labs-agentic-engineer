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

// Package crtcatalog projects the dependencies resource-type catalog onto spec's
// CRTType vocabulary (ADR-0041).
package crtcatalog

import (
	"context"

	"github.com/wso2/aep/aep-api/internal/dependencies"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// Catalog adapts a dependencies.ResourceTypeCatalog onto spec's
// resourceTypeCatalog port.
type Catalog struct {
	cat *dependencies.ResourceTypeCatalog
}

// New wraps cat.
func New(cat *dependencies.ResourceTypeCatalog) Catalog {
	return Catalog{cat: cat}
}

// ResourceTypesByName returns every installed resource type, keyed by name, as
// the markers and outputs design-save derives from.
func (c Catalog) ResourceTypesByName(ctx context.Context) (map[string]spec.CRTType, error) {
	types, err := c.cat.TypesByName(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]spec.CRTType, len(types))
	for k, v := range types {
		out[k] = spec.CRTType{
			EndUserAuth:          v.Markers.EndUserAuth,
			ConsumerURLEnvConfig: v.Markers.ConsumerURLEnvConfig,
			ConsumerURLPath:      v.Markers.ConsumerURLPath,
			Skill:                v.Markers.Skill,
			Description:          v.Description,
			Outputs:              v.Outputs,
		}
	}
	return out, nil
}
