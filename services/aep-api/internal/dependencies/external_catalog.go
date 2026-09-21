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

package dependencies

import (
	"context"
	"fmt"
	"sort"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
)

// ExternalResourceCatalog is the org-level external-resource registry sourced
// from the org's namespaced OpenChoreo ResourceTypes instead of the
// external_resources table: each entry is reconstructed off an authored RT via
// openchoreo.ExternalDefinitionFromRT. Mirrors ResourceTypeCatalog above,
// scoped to one org namespace instead of the cluster.
//
// List/Get back the read-only MCP-facing discovery surface
// (dependencies.ExternalResourceReader); Delete backs the org-settings prune
// surface (provisioning.Service.DeleteExternalResource, guarded upstream by
// its own design-sweep in-use check).
//
// An authored RT is present for both Project Externals (provisioner — see
// NewExternalResourceProvisioner) and Registered resources (Ensure authors
// the RT at register). MCP List is therefore RT-backed: a
// zero-consumer Registered row is already listable. A design-only `external`
// dependency that never went through register/Ensure still has no RT and is
// not discoverable here (deliberate; no design-sweep is added).
type ExternalResourceCatalog struct{ rc openchoreo.ResourceClient }

// NewExternalResourceCatalog wires the catalog over the OC client.
func NewExternalResourceCatalog(rc openchoreo.ResourceClient) *ExternalResourceCatalog {
	return &ExternalResourceCatalog{rc: rc}
}

// List returns every external resource with an authored RT in orgID's
// namespace (Registered or Project External), reconstructed from its
// ResourceType and sorted by name.
// A namespaced ResourceType that is not self-describing as an external
// (openchoreo.ExternalDefinitionFromRT's ok=false — e.g. it lacks the
// aep.wso2.com/external-name annotation) is silently skipped: it is not
// an external-resource RT.
//
// ResourceTypes are immutable and never deleted (see ExternalResourceRTName):
// a schema change mints a brand-new RT while the OLD one persists, and BOTH
// carry the same aep.wso2.com/external-name annotation. Without
// deduping, one logical name could surface twice — so results are grouped by
// the reconstructed logical name first, keeping only the newest RT per name
// (see newerExternalRT) before sorting. Only REGISTERED resources are listed
// (ExternalResourceDefinition.Registered): the type a project's build authors
// for its own resource is scoped to that project.
func (c *ExternalResourceCatalog) List(ctx context.Context, orgID string) ([]openchoreo.ExternalResourceDefinition, error) {
	rts, err := c.rc.ListResourceTypes(ctx, orgID)
	if err != nil {
		return nil, err
	}
	chosenRT := make(map[string]*openchoreo.ResourceType, len(rts))
	chosenDef := make(map[string]openchoreo.ExternalResourceDefinition, len(rts))
	for i := range rts {
		rt := &rts[i]
		def, ok := openchoreo.ExternalDefinitionFromRT(rt)
		if !ok {
			continue
		}
		// A project's own type is not a catalog entry: it belongs to its
		// project, is never offered for reuse, and never makes a name taken.
		// It is dropped BEFORE the per-name dedupe, or a project's newer type
		// would shadow the organization's record of the same logical name.
		if !def.Registered() {
			continue
		}
		if cur, exists := chosenRT[def.Name]; !exists || newerExternalRT(rt, cur) {
			chosenRT[def.Name] = rt
			chosenDef[def.Name] = def
		}
	}
	out := make([]openchoreo.ExternalResourceDefinition, 0, len(chosenDef))
	for _, def := range chosenDef {
		out = append(out, def)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Get returns the named REGISTERED external resource's definition, or
// (nil, nil) when no org-scoped RT in orgID's namespace carries that logical
// name (a project's own type does not count, exactly as in List). The RT's own
// metadata.name is a hash of (name, schema) — see
// openchoreo.ExternalResourceRTName — so it can never be derived from name
// alone; listing every namespaced RT and matching on the recovered logical
// name (via ExternalDefinitionFromRT) is the only way to look one up.
//
// When more than one RT matches (the same stale-RT-persists hazard List
// dedupes — see its doc), the newest one wins, via the SAME newerExternalRT
// tie-break List uses, so the two tools can never disagree on which schema is
// "current" for a given name.
func (c *ExternalResourceCatalog) Get(ctx context.Context, orgID, name string) (*openchoreo.ExternalResourceDefinition, error) {
	rts, err := c.rc.ListResourceTypes(ctx, orgID)
	if err != nil {
		return nil, err
	}
	var chosenRT *openchoreo.ResourceType
	var chosenDef openchoreo.ExternalResourceDefinition
	for i := range rts {
		rt := &rts[i]
		def, ok := openchoreo.ExternalDefinitionFromRT(rt)
		if !ok || def.Name != name || !def.Registered() {
			continue // same rule as List: a project's type is not the record
		}
		if chosenRT == nil || newerExternalRT(rt, chosenRT) {
			chosenRT, chosenDef = rt, def
		}
	}
	if chosenRT == nil {
		return nil, nil
	}
	return &chosenDef, nil
}

// Delete removes every ResourceType in orgID's namespace registered under the
// given logical external-resource name (matched via the SAME
// openchoreo.ExternalDefinitionFromRT reconstruction List/Get use). Because
// ResourceTypes are immutable and never deleted in place (see
// ExternalResourceRTName), more than one RT can carry the same
// aep.wso2.com/external-name annotation — a stale schema-version left
// behind by an earlier edit — so deleting a logical name removes ALL matching
// RTs, not just the newest one List/Get would surface. Idempotent: a name with
// no matching RT is a no-op, mirroring DeleteResourceType's own 404-tolerance.
//
// This method does not itself check whether the name is still in use by any
// project — the caller (provisioning.Service.DeleteExternalResource) already
// guards on that via its design-sweep before calling Delete.
func (c *ExternalResourceCatalog) Delete(ctx context.Context, orgID, name string) error {
	rts, err := c.rc.ListResourceTypes(ctx, orgID)
	if err != nil {
		return err
	}
	for i := range rts {
		rt := &rts[i]
		def, ok := openchoreo.ExternalDefinitionFromRT(rt)
		if !ok || def.Name != name || !def.Registered() {
			continue
		}
		if err := c.rc.DeleteResourceType(ctx, orgID, rt.Metadata.Name); err != nil {
			return err
		}
	}
	return nil
}

// Ensure get-or-creates the named ResourceType in orgID's namespace via
// ResourceClient.EnsureResourceType. Register uses this to author the org
// catalog RT without ApplyResource / EnsureBinding (no project instance).
// Ensure lands the record's ResourceType. An org-scoped type's name is a
// function of the logical name and the key schema, so a type of that name
// that is NOT yet a record — one a project authored before the scope marker
// existed — has the same schema and is adopted: it is rewritten as the record
// rather than left squatting the name while Ensure reports success. A type
// that already is a record is left as it is (Ensure is idempotent).
func (c *ExternalResourceCatalog) Ensure(ctx context.Context, orgID string, rt *openchoreo.ResourceType) error {
	if rt == nil {
		return fmt.Errorf("external resource catalog: nil ResourceType")
	}
	got, err := c.rc.EnsureResourceType(ctx, orgID, rt)
	if err != nil {
		return err
	}
	if got == nil {
		return nil
	}
	if existing, ok := openchoreo.ExternalDefinitionFromRT(got); ok && !existing.Registered() {
		if _, err := c.rc.UpdateResourceType(ctx, orgID, rt); err != nil {
			return fmt.Errorf("external resource catalog: adopt %q as the record: %w", rt.Metadata.Name, err)
		}
	}
	return nil
}

// Update replaces an existing namespaced ResourceType via
// ResourceClient.UpdateResourceType (PUT). Edit uses this so catalog fields
// persist when key identity — and therefore the hashed RT name — is unchanged.
// Do not route Update through Ensure (Ensure is get-or-create and will not PUT).
func (c *ExternalResourceCatalog) Update(ctx context.Context, orgID string, rt *openchoreo.ResourceType) error {
	if rt == nil {
		return fmt.Errorf("external resource catalog: nil ResourceType")
	}
	_, err := c.rc.UpdateResourceType(ctx, orgID, rt)
	return err
}

// newerExternalRT reports whether rt should be preferred over cur as the
// current-schema ResourceType for one logical external-resource name (used by
// both List and Get so they can never disagree). The RT with the NEWER
// metadata.creationTimestamp wins, reproducing the old DB reader's "last
// provisioned wins" semantic. When timestamps tie — including both being the
// zero value, e.g. test fixtures that never set one — the comparison falls
// back to the lexically GREATER metadata.name (the hashed RT name), a
// deterministic tie-break that never depends on ListResourceTypes' return
// order.
func newerExternalRT(rt, cur *openchoreo.ResourceType) bool {
	rtTS, curTS := rt.Metadata.CreationTimestamp, cur.Metadata.CreationTimestamp
	switch {
	case rtTS.After(curTS):
		return true
	case curTS.After(rtTS):
		return false
	default:
		return rt.Metadata.Name > cur.Metadata.Name
	}
}
