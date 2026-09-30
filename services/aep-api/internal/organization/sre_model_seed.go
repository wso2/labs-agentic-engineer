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

// sre_model_seed.go — the install-time SRE model seed `aectl sre install`
// writes without a user token (Task A2's `sre-model-seed` Secret,
// config.SREAgentConfig.Seed). ApplySeed is the one place that seed is turned
// into an SRE model connection: it runs the same Check/Persist path a console
// save takes, applies at most once per distinct seed, and never blocks the
// sreagent reconciler's pass on a refusal.
package organization

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/wso2/aep/aep-api/internal/platform/orgconfig"
	"github.com/wso2/aep/aep-api/internal/platform/secrets"
)

// seedAppliedStoreKey is the `org_secrets` key remembering which seed (by
// hash) was last tried for the org, and with what outcome: "<hash>:applied"
// or "<hash>:refused". A seed whose hash matches is never tried again; a
// changed seed (a different model, say) is.
const seedAppliedStoreKey = "sre-model/seed-applied"

// Seed is an install-time SRE model connection candidate: the three values
// `aectl sre install` seeds via config.SREAgentConfig.Seed, before any user
// has signed in to save one from the console.
type Seed struct {
	BaseURL, Model, APIKey string
}

// SeedOutcome is what ApplySeed did with a seed.
type SeedOutcome string

const (
	// SeedApplied means the seed was validated, probed and persisted.
	SeedApplied SeedOutcome = "applied"
	// SeedSkippedStored means the org already has an SRE model connection;
	// the seed was not even probed.
	SeedSkippedStored SeedOutcome = "skipped-stored"
	// SeedSkippedSeen means this exact seed was already tried (applied or
	// refused) and nothing about it has changed since.
	SeedSkippedSeen SeedOutcome = "skipped-seen"
	// SeedRefused means the seed failed validation or the probe; the SRE
	// agent stays unconfigured (or on the org connection) until a console
	// save or a changed seed succeeds.
	SeedRefused SeedOutcome = "refused"
)

// ApplySeed applies seed as org's SRE model connection the first time it is
// asked to: nothing happens when a connection is already stored (a console
// save always wins over a seed), and nothing happens twice for the same seed
// (tracked by a hash of its three values under seedAppliedStoreKey), so a
// reconciler pass that calls this on every tick costs one org_secrets read
// once the seed has been tried. A validation/probe refusal is recorded and
// returned as SeedRefused rather than an error: the caller (the sreagent
// reconciler) must keep reconciling on whatever connection already applies.
func (s *SreModelConnectionService) ApplySeed(ctx context.Context, org string, seed Seed) (SeedOutcome, error) {
	stored, err := s.conns.GetByOrg(ctx, org)
	if err != nil {
		return "", fmt.Errorf("sre model seed: read: %w", err)
	}
	if stored != nil {
		return SeedSkippedStored, nil
	}

	hash := seedHash(seed)
	marker, err := s.seedMarker(ctx, org)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(marker, hash+":") {
		return SeedSkippedSeen, nil
	}

	w := orgconfig.SreLlmWrite{BaseURL: &seed.BaseURL, APIKey: &seed.APIKey, Model: &seed.Model}
	draft, err := s.Check(ctx, org, w)
	if err != nil {
		var se *SectionError
		if errors.As(err, &se) {
			slog.WarnContext(ctx, "sre_model.seed_refused", "org", org, "code", se.Code)
		} else {
			slog.WarnContext(ctx, "sre_model.seed_refused", "org", org, "code", "")
		}
		if werr := s.writeSeedMarker(ctx, org, hash+":refused"); werr != nil {
			return "", werr
		}
		return SeedRefused, nil
	}

	if err := s.Persist(ctx, org, "aectl-seed", draft); err != nil {
		return "", err
	}
	if err := s.writeSeedMarker(ctx, org, hash+":applied"); err != nil {
		return "", err
	}
	slog.InfoContext(ctx, "sre_model.seeded", "org", org, "host", draft.Host, "model", draft.Model)
	return SeedApplied, nil
}

// seedHash is sha256(baseURL \x00 model \x00 apiKey) hex: the identity a seed
// is remembered by, so a changed value (a rotated key, a different model) is
// tried again even though the org still has no stored connection.
func seedHash(seed Seed) string {
	sum := sha256.Sum256([]byte(seed.BaseURL + "\x00" + seed.Model + "\x00" + seed.APIKey))
	return hex.EncodeToString(sum[:])
}

func (s *SreModelConnectionService) seedMarker(ctx context.Context, org string) (string, error) {
	v, err := s.store.Get(ctx, org, seedAppliedStoreKey)
	if errors.Is(err, secrets.ErrSecretNotFound) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("sre model seed: read marker: %w", err)
	}
	return string(v), nil
}

func (s *SreModelConnectionService) writeSeedMarker(ctx context.Context, org, marker string) error {
	if err := s.store.Put(ctx, org, seedAppliedStoreKey, []byte(marker)); err != nil {
		return fmt.Errorf("sre model seed: write marker: %w", err)
	}
	return nil
}
