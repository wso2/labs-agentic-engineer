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

// catalog.go — the READ side: the roles that already exist on the identity
// provider.
//
// It exists because roles are shared. A design agent that cannot see the
// existing roles mints a near-duplicate of one already there — `Compliance
// Admin` beside `Compliance Officer` — and the two then diverge in what they
// can do while naming the same job. Showing the catalog at design time is the
// same reuse-before-invent rule the architecture skill already applies to
// external resources and platform resource types.
//
// This is read-only in the strongest sense: the design agent reaches it through
// an MCP tool with no write counterpart, and nothing on this path can create a
// role. Creation happens only at build time, in ensure.go, with no model in the
// loop.

import (
	"context"
	"log/slog"
	"sort"
	"strings"
)

// CatalogEntry is one row of the role catalog.
type CatalogEntry struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// PlatformCreated is true when the platform created this role and may
	// therefore enrol test users into it. A hand-made group — `Administrators`
	// above all — reads false, and the ensure will leave it alone.
	PlatformCreated bool `json:"platformCreated"`
	// MemberCount is how many accounts currently hold the role. Best-effort: a
	// per-role read failure leaves it 0 rather than failing the whole catalog,
	// because a missing count must not stop a design from reusing a good name.
	MemberCount int `json:"memberCount"`
}

// CatalogService reads the role catalog.
type CatalogService struct {
	dir   Directory
	store Store
}

// readCatalog is the ONE join behind every "what roles exist" answer: the
// directory's groups, marked with whether the platform created each, plus a
// best-effort member count.
//
// It is a package-level function over the two ports rather than a method,
// because two very different surfaces need the same join — this catalog (behind
// the design-time `list_roles` tool) and the console's Security panel — and they
// project it into different view types. Having them share the join is what stops
// "which roles are ours" from being answered two ways.
//
// The member count is best-effort by design: the identity provider exposes no
// count on the listing, so it costs one call per role, and losing one must not
// cost the caller the row it belongs to.
func readCatalog(ctx context.Context, dir Directory, store Store) ([]CatalogEntry, error) {
	groups, err := dir.ListGroups(ctx)
	if err != nil {
		return nil, err
	}
	recorded, err := store.ListRoles(ctx)
	if err != nil {
		return nil, err
	}
	ours := make(map[string]bool, len(recorded))
	for _, r := range recorded {
		ours[strings.ToLower(r.Name)] = true
	}

	out := make([]CatalogEntry, 0, len(groups))
	for _, g := range groups {
		entry := CatalogEntry{
			Name:            g.Name,
			Description:     g.Description,
			PlatformCreated: ours[strings.ToLower(g.Name)],
		}
		if members, merr := dir.GroupMembers(ctx, g.ID); merr != nil {
			slog.WarnContext(ctx, "role catalog: member count unavailable", "role", g.Name, "error", merr)
		} else {
			entry.MemberCount = len(members)
		}
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// NewCatalogService builds the read side. Both collaborators are required; a
// nil one means the identity provider is not configured, and the caller should
// not wire the catalog at all (see Enabled).
func NewCatalogService(dir Directory, store Store) *CatalogService {
	return &CatalogService{dir: dir, store: store}
}

// Enabled reports whether the catalog can be read.
func (s *CatalogService) Enabled() bool {
	return s != nil && s.dir != nil && s.store != nil
}

// List returns every role on the identity provider, name-ordered, joined against
// the platform's own record to compute PlatformCreated. See readCatalog.
func (s *CatalogService) List(ctx context.Context) ([]CatalogEntry, error) {
	return readCatalog(ctx, s.dir, s.store)
}
