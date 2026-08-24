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

// Package tenant is the pure (gorm-free) tenancy kernel. It owns the universal
// tenant key (OrgHandle), the authorized Caller produced only by the auth
// middleware, the Source provenance enum, and the central per-route gate
// (gate.go). Keeping it gorm-free lets HTTP/middleware read a Caller without
// dragging the database layer in. See docs/design/aep-service-modularization.md
// §6.1.
package tenant

// OrgHandle is the universal tenant key. It is the OC org handle and the value
// stored in every tenant table's org column (the "oc_org_id"/"org_id" columns
// all hold this). It is NOT the Thunder ouId/UUID.
type OrgHandle string

// Source records how a Caller's org was established, so non-user paths
// (S2S/webhook/runner) can assert their provenance.
type Source uint8

const (
	// SourcePublisherCC — a publisher client-credentials token.
	SourcePublisherCC Source = iota
)

func (s Source) String() string {
	switch s {
	case SourcePublisherCC:
		return "publisher-cc"
	default:
		return "unknown"
	}
}

// Caller is the authorized tenant context. It is produced ONLY by the auth
// middleware (the Bind* gates) and read by downstream handlers/repositories.
type Caller struct {
	// Org is the universal tenant key — never trust a path/body org without a
	// gate that set this.
	Org OrgHandle
	// ThunderUUID is the Thunder ouId, carried only for impersonation
	// (X-Impersonate-Org) and wc- namespace derivation; never a tenant column.
	ThunderUUID string
	Subject     string
	Source      Source
}
