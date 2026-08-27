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

package httpapi

import (
	"github.com/wso2/aep/aep-api/internal/dependencies/mcpdiscovery"
	"github.com/wso2/aep/aep-api/internal/dependencies/provisioning"
)

// Deps carries every service the dependencies slice handlers call. It lives in
// the aggregator (not the dependencies root) because the domain is kernel-root:
// its services live in sub-packages the root may not import — the httpapi
// aggregator is the one package allowed to name them.
type Deps struct {
	ProvisioningSvc *provisioning.Service
	ResourceTypes   mcpdiscovery.ResourceTypeLister
	OrgEndpoints    mcpdiscovery.OrgEndpointLister
}

// Both slices name their handler type Handler, so embedding them directly would
// be "Handler redeclared". Local aliases give distinct field names (§6).
type (
	provisioningHandler = provisioning.Handler
	mcpdiscoveryHandler = mcpdiscovery.Handler
)

// Handlers is the dependencies domain's slice handlers, embedded so Go promotes
// each operation exactly once into the edge's composite. It declares nothing.
type Handlers struct {
	*provisioningHandler
	*mcpdiscoveryHandler
}

// New assembles the domain: pure wiring, constructor injection only. Both slices
// are nil-tolerant (a wired-but-nil service degrades to 503, matching the
// pre-migration edge), so zero Deps is a supported configuration.
func New(d Deps) (*Handlers, error) {
	// The provisioning service backs the platform-resource "used by" overlay via
	// its PlatformResourceConsumersByType port. Guard the concrete→interface
	// conversion: a nil *provisioning.Service wrapped in the interface would be a
	// non-nil interface over a nil pointer (a panic on first call), so pass the
	// port only when the service is actually wired, keeping the overlay off (nil
	// lister ⇒ empty consumers) otherwise.
	var consumers mcpdiscovery.PlatformResourceConsumerLister
	if d.ProvisioningSvc != nil {
		consumers = d.ProvisioningSvc
	}
	return &Handlers{
		provisioningHandler: provisioning.NewHandler(d.ProvisioningSvc),
		mcpdiscoveryHandler: mcpdiscovery.NewHandler(d.ResourceTypes, consumers, d.OrgEndpoints),
	}, nil
}
