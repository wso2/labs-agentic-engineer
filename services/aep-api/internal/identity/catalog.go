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

package identity

// catalog.go — the READ side: the directory groups that already exist on the
// identity provider of the org's environment.
//
// It exists because the directory is shared within that environment. A design
// agent that cannot see the groups already there mints a near-duplicate of one
// that exists — `Compliance Admin` beside `Compliance Officer` — and the two
// then diverge in who they contain while naming the same set of people. Showing
// the catalog at design time is the same reuse-before-invent rule the
// architecture skill already applies to external resources and platform
// resource types.
//
// This is read-only in the strongest sense: the design agent reaches it through
// an MCP tool (`list_groups`) with no write counterpart, and nothing on this
// path can create a group. Creation
// happens only at build time, in ensure.go, with no model in the loop.

import (
	"context"
	"log/slog"
	"sort"
	"strings"
)

// CatalogEntry is one row of the catalog: one group on the environment's
// directory.
type CatalogEntry struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// PlatformCreated is true when the platform created this group and may
	// therefore enrol test users into it. A hand-made group — `Administrators`
	// above all — reads false, and the ensure will leave it alone.
	//
	// It is answered from the platform's OWN record of what it created
	// (Store.ListRoles over `idp_roles`), never from anything the directory
	// says: the directory cannot tell us whose object a group is.
	PlatformCreated bool `json:"platformCreated"`
	// MemberCount is how many accounts are in the group today. Best-effort: a
	// per-group read failure leaves it 0 rather than failing the whole catalog,
	// because a missing count must not stop a design from reusing a good name.
	MemberCount int `json:"memberCount"`
	// Projects is how many projects already bind a role to this group. It tells
	// a design agent that a group is load-bearing elsewhere — reusing it is a
	// decision about people who already hold roles, not a free name, and it is
	// the "reused · holds roles in n projects" the console's role cards show.
	//
	// It counts DISTINCT projects on THIS directory
	// (Store.CountProjectsBindingGroups over `idp_role_bindings`), so a group two
	// projects assign a role to reads 2 however many roles each of them binds to
	// it. Best-effort: a store failure leaves every count 0 rather than failing
	// the whole catalog, for the same reason the member count does.
	Projects int `json:"projects"`
}

// CatalogService reads the group catalog of one org's environment.
type CatalogService struct {
	targets TargetResolver
	store   Store
}

// readCatalog is the ONE join behind every "what groups exist" answer: the
// directory's groups, marked with whether the platform created each, plus a
// best-effort member count.
//
// It is a package-level function over the two ports rather than a method,
// because two very different surfaces need the same join — this catalog (behind
// the design-time `list_groups` tool) and the console's Security panel — and they
// project it into different view types. Having them share the join is what stops
// "which roles are ours" from being answered two ways.
//
// The member count is best-effort by design: the identity provider exposes no
// count on the listing, so it costs one call per group, and losing one must not
// cost the caller the row it belongs to.
func readCatalog(ctx context.Context, target Target, store Store) ([]CatalogEntry, error) {
	groups, err := target.Directory.ListGroups(ctx)
	if err != nil {
		return nil, err
	}
	recorded, err := store.ListRoles(ctx, target.Scope())
	if err != nil {
		return nil, err
	}
	ours := make(map[string]bool, len(recorded))
	for _, r := range recorded {
		ours[strings.ToLower(r.Name)] = true
	}

	// How many projects hold a role assigned to each group. It is a store read
	// rather than a directory one: the directory knows a group is assigned to a
	// role, but only the platform's own bindings say which PROJECT that role
	// belongs to.
	//
	// One query for every group, and best-effort like the member count below: a
	// number missing from a reuse hint must not cost the caller the listing it
	// belongs to.
	projects, perr := store.CountProjectsBindingGroups(ctx, target.Scope())
	if perr != nil {
		slog.WarnContext(ctx, "group catalog: project counts unavailable",
			"scope", target.Scope().String(), "error", perr)
		projects = nil
	}

	out := make([]CatalogEntry, 0, len(groups))
	for _, g := range groups {
		entry := CatalogEntry{
			Name:            g.Name,
			Description:     g.Description,
			PlatformCreated: ours[strings.ToLower(g.Name)],
			Projects:        projects[strings.ToLower(g.Name)],
		}
		if members, merr := target.Directory.GroupMembers(ctx, g.ID); merr != nil {
			slog.WarnContext(ctx, "group catalog: member count unavailable", "group", g.Name, "error", merr)
		} else {
			entry.MemberCount = len(members)
		}
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// NewCatalogService builds the read side. Both collaborators are required; a
// nil one means no identity provider can be resolved, and the caller should not
// wire the catalog at all (see Enabled).
func NewCatalogService(targets TargetResolver, store Store) *CatalogService {
	return &CatalogService{targets: targets, store: store}
}

// Enabled reports whether the catalog can be read.
func (s *CatalogService) Enabled() bool {
	return s != nil && s.targets != nil && s.store != nil
}

// List returns every group on the identity provider serving orgID's
// environment, name-ordered, joined against the platform's own record to
// compute PlatformCreated. See readCatalog.
//
// It takes the ORG because the catalog is that org's environment's catalog and
// nobody else's. While one identity provider served the whole cluster this read
// showed one org's design agent the group names another org had created; with a
// directory per (org, environment) that disclosure is closed by construction.
func (s *CatalogService) List(ctx context.Context, orgID string) ([]CatalogEntry, error) {
	target, err := s.targets.Resolve(ctx, orgID)
	if err != nil {
		return nil, err
	}
	return readCatalog(ctx, target, s.store)
}
