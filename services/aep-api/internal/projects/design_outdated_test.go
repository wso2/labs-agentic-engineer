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
	"errors"
	"testing"

	"github.com/wso2/aep/aep-api/internal/spec"
	"github.com/wso2/aep/aep-api/internal/spec/artifactstest"
)

// stubDesignTurns answers "the newest successful design run" and nothing else.
type stubDesignTurns struct {
	lastDesign *spec.AgentTurn
	err        error
	askedFlow  string
}

func (s *stubDesignTurns) Newest(context.Context, string, string) (*spec.AgentTurn, error) {
	return nil, nil
}

func (s *stubDesignTurns) NewestCompletedFlow(_ context.Context, _, _, flow string) (*spec.AgentTurn, error) {
	s.askedFlow = flow
	return s.lastDesign, s.err
}

func svcFor(turns specTurnRows, fingerprintAt string, fpErr error) *Service {
	svc := &Service{}
	svc.specTurns = turns
	svc.artifactSvc = &artifactstest.FakeArtifactService{
		RequirementsFingerprintAtFunc: func(context.Context, string, string, string) (string, error) {
			return fingerprintAt, fpErr
		},
	}
	return svc
}

// The whole point: the requirements as they stand, against the requirements as
// the last design run read them.
func TestDesignOutdated(t *testing.T) {
	t.Parallel()

	t.Run("requirements moved since the design run", func(t *testing.T) {
		t.Parallel()
		svc := svcFor(&stubDesignTurns{lastDesign: &spec.AgentTurn{BaseRef: "abc"}}, "was", nil)

		got, err := svc.designOutdated(context.Background(), "acme", "proj", "now")
		if err != nil {
			t.Fatalf("designOutdated: %v", err)
		}
		if !got {
			t.Fatal("requirements changed since the design run but the design reads as current")
		}
	})

	t.Run("requirements unchanged", func(t *testing.T) {
		t.Parallel()
		svc := svcFor(&stubDesignTurns{lastDesign: &spec.AgentTurn{BaseRef: "abc"}}, "same", nil)

		got, err := svc.designOutdated(context.Background(), "acme", "proj", "same")
		if err != nil {
			t.Fatalf("designOutdated: %v", err)
		}
		if got {
			t.Fatal("nothing moved but the design reads as behind")
		}
	})

	// Nothing to be behind. The ordinary state of every project that has not
	// designed yet, so it must not read as stale.
	t.Run("no design run on record", func(t *testing.T) {
		t.Parallel()
		svc := svcFor(&stubDesignTurns{lastDesign: nil}, "was", nil)

		got, err := svc.designOutdated(context.Background(), "acme", "proj", "now")
		if err != nil || got {
			t.Fatalf("designOutdated = (%v, %v), want (false, nil)", got, err)
		}
	})

	// Only a full re-derivation reconciles the design with the requirements —
	// a targeted edit to one document leaves the set inconsistent, so the
	// baseline may only ever come from a design run.
	t.Run("asks for the design flow specifically", func(t *testing.T) {
		t.Parallel()
		turns := &stubDesignTurns{lastDesign: &spec.AgentTurn{BaseRef: "abc"}}
		svc := svcFor(turns, "same", nil)

		if _, err := svc.designOutdated(context.Background(), "acme", "proj", "same"); err != nil {
			t.Fatalf("designOutdated: %v", err)
		}
		if turns.askedFlow != "design" {
			t.Fatalf("looked up flow %q, want the design flow", turns.askedFlow)
		}
	})

	// An unreadable baseline is an ERROR, never a quiet "unchanged". The two
	// failures are not symmetric: a spurious warning costs one re-derivation,
	// a swallowed one ships a design the user already changed their mind about.
	t.Run("an unreadable baseline fails loudly", func(t *testing.T) {
		t.Parallel()
		svc := svcFor(&stubDesignTurns{lastDesign: &spec.AgentTurn{BaseRef: "gone"}},
			"", errors.New("commit not in the mirror"))

		if _, err := svc.designOutdated(context.Background(), "acme", "proj", "now"); err == nil {
			t.Fatal("an unreadable baseline was reported as up to date")
		}
	})

	// An unwired source cannot answer, and guessing "stale" would block Build
	// on every project.
	t.Run("an unwired turn source reports nothing", func(t *testing.T) {
		t.Parallel()
		svc := svcFor(nil, "was", nil)

		got, err := svc.designOutdated(context.Background(), "acme", "proj", "now")
		if err != nil || got {
			t.Fatalf("designOutdated = (%v, %v), want (false, nil)", got, err)
		}
	})
}
