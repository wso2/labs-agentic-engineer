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

package app

import (
	"context"
	"fmt"

	"github.com/wso2/aep/aep-api/internal/dependencies"
	"github.com/wso2/aep/aep-api/internal/dependencies/provisioning"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// registeredResourceReader adapts the org resource registry — the OC-RT-backed
// catalog plus the org docs repo — onto the two ports the spec domain reads
// it through: spec.ExternalResourceResolver (the design read's registry
// lookup for a copy) and spec.RegisteredResourceReader (the design write's
// copy source). spec names no feature type and the dependencies feature
// names no spec type: the projection from the client's record shape to the
// shared ResourceDefinition happens here, at the composition root.
type registeredResourceReader struct {
	catalog *dependencies.ExternalResourceCatalog
	docs    provisioning.OrgResourceDocs
}

// Lookup is the design-read registry answer for `name`: Registered only when
// the catalog holds an org-scoped record under that name — the type a
// project's build authored for its own resource is not one — and, when that
// record names a contract document, its hash.
func (r registeredResourceReader) Lookup(ctx context.Context, orgID, name string) (spec.RegistryHit, error) {
	if r.catalog == nil {
		return spec.RegistryHit{}, nil
	}
	def, err := r.catalog.Get(ctx, orgID, name)
	if err != nil {
		return spec.RegistryHit{}, err
	}
	if def == nil || !def.Registered() {
		return spec.RegistryHit{}, nil
	}
	return spec.RegistryHit{Registered: true, DocumentSHA256: def.DocumentSHA256()}, nil
}

func (r registeredResourceReader) RegisteredResource(ctx context.Context, orgID, name string) (*spec.RegisteredResource, error) {
	if r.catalog == nil {
		return nil, fmt.Errorf("external resource catalog is not configured")
	}
	def, err := r.catalog.Get(ctx, orgID, name)
	if err != nil {
		return nil, err
	}
	if def == nil || !def.Registered() {
		return nil, nil
	}
	document := ""
	if def.Contract != nil && r.docs != nil {
		doc, err := r.docs.ReadUTF8(ctx, orgID, def.Contract.Path)
		if err != nil {
			return nil, fmt.Errorf("read registered resource %q document: %w", name, err)
		}
		document = doc
	}
	rec := provisioning.RegisteredResourceFromRecord(*def, document)
	return &rec, nil
}

var (
	_ spec.ExternalResourceResolver = registeredResourceReader{}
	_ spec.RegisteredResourceReader = registeredResourceReader{}
)
