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
	"sort"

	"github.com/wso2/aep/aep-api/internal/contracts"
	"github.com/wso2/aep/aep-api/internal/gen"
)

// UsageByProjectFunc rolls up captured agent usage per project (slug) across an
// org — the shape both capture domains expose (spec turns, delivery
// executions). CostUsd sums the frozen per-row stamps; nil when a project has
// no stamped row.
type UsageByProjectFunc func(ctx context.Context, orgID string) (map[string]contracts.StampedUsage, error)

// ProjectNameLister resolves the org's live projects to slug → display name.
// It is the *Service's ListProjects behind a narrow port so the usage service
// can mark deleted projects (a usage slug with no live project) and label the
// rest. Returning only the live set is enough: any slug missing from it is a
// deleted project whose spend still counts.
type ProjectNameLister func(ctx context.Context, orgID string) (map[string]string, error)

// DeliveryPhaseUsageFunc rolls up delivery's captured agent usage split into the
// build and validation phases — the delivery ledger's shape.
//
// It is keyed by contracts.UsageScope, not by slug, because a slug is not an
// identity: a project deleted and recreated under the same name is two
// lifetimes, and the roll-up hands them over separately so this service can
// render them as two cards instead of one inflated one.
type DeliveryPhaseUsageFunc func(ctx context.Context, orgID string) (build, validation map[contracts.UsageScope]contracts.StampedUsage, err error)

// UsageService assembles the org-wide Settings → Usage roll-up (#291): per
// project it folds the three SDLC phases (spec/design turns, build + coding
// executions, validation executions) into a lifetime total and a per-phase
// split, labels each project (live name, or the slug for a since-deleted one),
// and orders the cards so the biggest spenders lead. It reprices nothing —
// every costUsd is a sum of stamps frozen at capture (amended ADR-0011).
type UsageService struct {
	specUsage          UsageByProjectFunc
	deliveryPhaseUsage DeliveryPhaseUsageFunc
	liveNames          ProjectNameLister
}

// NewUsageService wires the roll-up from the spec-turn source, the phase-split
// execution source, and the live project lister.
func NewUsageService(specUsage UsageByProjectFunc, deliveryPhaseUsage DeliveryPhaseUsageFunc, liveNames ProjectNameLister) *UsageService {
	return &UsageService{specUsage: specUsage, deliveryPhaseUsage: deliveryPhaseUsage, liveNames: liveNames}
}

// ListProjectUsage returns a card for every LIVE project in the org (zero usage
// shown as $0 for ones that have not run an agent yet) plus every since-deleted
// project that still carries usage (greyed). Each card carries a per-phase
// split alongside its total. Ordered: stamped cost desc, then unpriced-but-
// active by tokens, then the $0 idle projects last.
func (s *UsageService) ListProjectUsage(ctx context.Context, orgID string) (gen.ProjectUsageList, error) {
	spec, err := s.specUsage(ctx, orgID)
	if err != nil {
		return gen.ProjectUsageList{}, err
	}
	build, validation, err := s.deliveryPhaseUsage(ctx, orgID)
	if err != nil {
		return gen.ProjectUsageList{}, err
	}
	names, err := s.liveNames(ctx, orgID)
	if err != nil {
		return gen.ProjectUsageList{}, err
	}

	// The set of LIFETIMES with real spend is the union of the phase maps (each
	// already drops zero-token entries). Delivery's two are already scoped; spec
	// turns are not — see specScope.
	scopes := map[contracts.UsageScope]struct{}{}
	for scope := range build {
		scopes[scope] = struct{}{}
	}
	for scope := range validation {
		scopes[scope] = struct{}{}
	}
	for slug := range spec {
		scopes[specScope(slug, names)] = struct{}{}
	}

	cards := make([]gen.ProjectUsageCard, 0, len(scopes)+len(names))
	for scope := range scopes {
		displayName, live := names[scope.ProjectID]
		if scope.Retired || !live {
			displayName = scope.ProjectID // deleted project: fall back to the stored slug
		}
		// Spec-turn usage joins the lifetime specScope assigned it to, and only
		// that one, so it is never counted on both cards of a recreated slug.
		specUsage := contracts.StampedUsage{}
		if specScope(scope.ProjectID, names) == scope {
			specUsage = spec[scope.ProjectID]
		}
		total := specUsage.Add(build[scope]).Add(validation[scope])
		cards = append(cards, gen.ProjectUsageCard{
			ProjectName: scope.ProjectID,
			DisplayName: displayName,
			Deleted:     scope.Retired || !live,
			Usage:       toGenUsage(total),
			Phases: gen.PhaseUsage{
				Spec:       stampedOrZero(specUsage),
				Build:      scopeUsage(build, scope),
				Validation: scopeUsage(validation, scope),
			},
		})
	}
	// Every live project with no spend yet gets a $0 card so the page lists the
	// org's projects, not only the ones that happen to have run an agent.
	for slug, displayName := range names {
		if _, hasSpend := scopes[contracts.UsageScope{ProjectID: slug}]; hasSpend {
			continue
		}
		cards = append(cards, gen.ProjectUsageCard{
			ProjectName: slug,
			DisplayName: displayName,
			Deleted:     false,
			Usage:       zeroUsage(),
			Phases:      gen.PhaseUsage{Spec: zeroUsage(), Build: zeroUsage(), Validation: zeroUsage()},
		})
	}
	sortUsageCards(cards)
	return gen.ProjectUsageList{Projects: cards}, nil
}

// specScope decides which lifetime a project's SPEC-turn spend belongs to.
//
// Spec turns are the one capture surface with no lifetime marker: `agent_turns`
// is never purged and carries no retirement stamp, so a recreated slug's turns sit
// in the same rows as its predecessor's and cannot be told apart. The honest
// placement is therefore the whole slug's spec spend on the LIVE card while the
// project exists, and on the retired card once it does not — which is exactly what
// the page showed before delivery's half learned to distinguish lifetimes.
//
// A recreated project consequently inherits the deleted one's SPEC figure while
// keeping none of its build or validation figure. Splitting it needs a lifetime
// marker on the turns themselves, which is the spec domain's to add.
func specScope(slug string, names map[string]string) contracts.UsageScope {
	_, live := names[slug]
	return contracts.UsageScope{ProjectID: slug, Retired: !live}
}

// scopeUsage maps one lifetime's phase entry to wire Usage: a captured phase
// keeps its stamped/unpriced figures; a phase that never ran shows a real $0
// rather than a null "0 tok", so all three phase rows read cleanly.
func scopeUsage(m map[contracts.UsageScope]contracts.StampedUsage, scope contracts.UsageScope) gen.Usage {
	if u, ok := m[scope]; ok {
		return toGenUsage(u)
	}
	return zeroUsage()
}

// stampedOrZero renders a phase aggregate the caller already holds, showing a
// real $0 for one that never ran.
func stampedOrZero(u contracts.StampedUsage) gen.Usage {
	if u.Tokens.IsZero() && u.CostUsd == nil {
		return zeroUsage()
	}
	return toGenUsage(u)
}

// zeroUsage is an idle phase/project: a real $0 (the model is known to be
// nothing spent, distinct from an unpriced null on a captured row).
func zeroUsage() gen.Usage {
	zero := 0.0
	return gen.Usage{CostUsd: &zero}
}

// toGenUsage maps the internal stamped aggregate to the wire Usage shape.
func toGenUsage(u contracts.StampedUsage) gen.Usage {
	return gen.Usage{
		InputTokens:         u.Tokens.InputTokens,
		OutputTokens:        u.Tokens.OutputTokens,
		CacheReadTokens:     u.Tokens.CacheReadTokens,
		CacheCreationTokens: u.Tokens.CacheCreationTokens,
		Model:               u.Tokens.Model,
		CostUsd:             u.CostUsd,
	}
}

// sortUsageCards orders cards into three intuitive tiers, each broken by slug:
//  0. real stamped spend (costUsd > 0) — by cost descending (biggest first)
//  1. captured usage the platform could not price (null cost) — by total tokens
//     descending, so a busy unstamped project still ranks above idle ones
//  2. idle projects ($0, no tokens) — last
func sortUsageCards(cards []gen.ProjectUsageCard) {
	tier := func(c gen.ProjectUsageCard) int {
		switch {
		case c.Usage.CostUsd != nil && *c.Usage.CostUsd > 0:
			return 0
		case tokenTotal(c.Usage) > 0:
			return 1 // has usage but null (or $0-but-nonzero-token) cost
		default:
			return 2 // idle $0
		}
	}
	sort.SliceStable(cards, func(i, j int) bool {
		ti, tj := tier(cards[i]), tier(cards[j])
		if ti != tj {
			return ti < tj
		}
		switch ti {
		case 0:
			if a, b := *cards[i].Usage.CostUsd, *cards[j].Usage.CostUsd; a != b {
				return a > b
			}
		case 1:
			if a, b := tokenTotal(cards[i].Usage), tokenTotal(cards[j].Usage); a != b {
				return a > b
			}
		}
		return cards[i].ProjectName < cards[j].ProjectName
	})
}

func tokenTotal(u gen.Usage) int64 {
	return u.InputTokens + u.OutputTokens + u.CacheReadTokens + u.CacheCreationTokens
}
