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

package spec

// The Workspace-port write primitive behind Save (annotated tag at the pinned
// commit). Save creates no commit — the accepted draft is already on `main`.
// Push-CAS on origin arbitrates concurrent writers; Mutate owns the bounded
// fast-forward retry (design D5 — the retired REST path's org-keyed leaky
// bucket is not ported). (Discard's revert-commit primitive, revertSubtreeToTag, was
// removed with DiscardRequirements/DiscardDesign — dead once the
// requirements/design read+discard HTTP surface was removed.)

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// tagRetryAttempts is the bounded backoff schedule for tag-name collisions
// (an external pusher claimed the same name between the tag-list read and the
// push): 50ms / 200ms / 800ms, each with ±50% jitter. Collisions can only come
// from external pushers, so plain bounded attempts suffice (design §10).
var tagRetryAttempts = []time.Duration{
	50 * time.Millisecond,
	200 * time.Millisecond,
	800 * time.Millisecond,
}

// createVersionTag cuts a spec version's annotated tag at commitSHA.
//
// Its subject is `Spec <name>` — the marker that makes the tag a VERSION now
// that the name itself is the user's and carries no sequence (see
// version_naming.go). A caller's save message follows it as the body.
//
// `resuggest` says what a name collision means. FALSE for a name the user
// typed: it comes back as ErrVersionNameTaken, because a supplied name must
// never quietly become a different one. TRUE for a name the platform
// suggested: an external pusher can claim it between the tag-list read and the
// push, so the suggestion is recomputed against a fresh listing and retried,
// bounded by tagRetryAttempts. `name` carries the name actually cut back out.
func (s *artifactService) createVersionTag(
	ctx context.Context,
	ref sourcecontrol.RepoRef,
	tags *[]sourcecontrol.TagInfo,
	name *string,
	message, commitSHA string,
	resuggest bool,
) error {
	tagger, _ := s.git.ResolveSaveIdentities(ref.Cred)
	attempt := func() error {
		body := specTagSubject + *name
		if message != "" {
			body = body + "\n\n" + message
		}
		return s.git.Workspace().Tag(ctx, ref, sourcecontrol.TagSpec{
			Name:    *name,
			Target:  commitSHA,
			Message: body,
			Tagger:  tagger,
		})
	}
	err := attempt()
	for _, delay := range tagRetryAttempts {
		if !errors.Is(err, sourcecontrol.ErrTagAlreadyExists) {
			return err
		}
		if !resuggest {
			return fmt.Errorf("%w: %q", ErrVersionNameTaken, *name)
		}
		if jerr := jitterSleep(ctx, delay); jerr != nil {
			return jerr
		}
		if refreshed, ferr := s.listVersionTags(ctx, ref); ferr == nil {
			*tags = refreshed
			*name = suggestedVersionName(refreshed)
		}
		err = attempt()
	}
	if errors.Is(err, sourcecontrol.ErrTagAlreadyExists) && !resuggest {
		return fmt.Errorf("%w: %q", ErrVersionNameTaken, *name)
	}
	return err
}

// jitterSleep waits for `base` with ±50% uniform jitter, respecting ctx
// cancellation. Returns ctx.Err() if the context is cancelled mid-sleep.
func jitterSleep(ctx context.Context, base time.Duration) error {
	delay := base/2 + rand.N(base) // uniform in [base/2, base*3/2)
	select {
	case <-time.After(delay):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
