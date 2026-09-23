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

// stubFlowTurns answers "the newest successful run of a flow" per flow, and
// records which flows were asked for.
type stubFlowTurns struct {
	byFlow map[string]*spec.AgentTurn
	err    error
	asked  []string
}

func (s *stubFlowTurns) Newest(context.Context, string, string) (*spec.AgentTurn, error) {
	return nil, nil
}

func (s *stubFlowTurns) NewestCompletedDerivation(_ context.Context, _, _, flow string) (*spec.AgentTurn, error) {
	s.asked = append(s.asked, flow)
	return s.byFlow[flow], s.err
}

func prototypeSvcFor(turns specTurnRows, designAt string, fpErr error) *Service {
	svc := &Service{}
	svc.specTurns = turns
	svc.artifactSvc = &artifactstest.FakeArtifactService{
		DesignFingerprintAtFunc: func(context.Context, string, string, string) (string, error) {
			return designAt, fpErr
		},
	}
	return svc
}

func lastPrototype(baseRef string) *stubFlowTurns {
	return &stubFlowTurns{byFlow: map[string]*spec.AgentTurn{"prototype": {BaseRef: baseRef}}}
}

// The design as it stands, against the design as the last /prototype run read
// it (#818).
func TestPrototypeOutdated(t *testing.T) {
	t.Parallel()

	t.Run("the design moved since the prototype run", func(t *testing.T) {
		t.Parallel()
		svc := prototypeSvcFor(lastPrototype("abc"), "was", nil)
		got, err := svc.prototypeOutdated(context.Background(), "acme", "proj", "now")
		if err != nil || !got {
			t.Fatalf("prototypeOutdated = (%v, %v), want (true, nil)", got, err)
		}
	})

	// A feedback rewrite touches only prototype.json, which the design
	// fingerprint excludes — so the two values still agree.
	t.Run("design unchanged", func(t *testing.T) {
		t.Parallel()
		svc := prototypeSvcFor(lastPrototype("abc"), "same", nil)
		got, err := svc.prototypeOutdated(context.Background(), "acme", "proj", "same")
		if err != nil || got {
			t.Fatalf("prototypeOutdated = (%v, %v), want (false, nil)", got, err)
		}
	})

	t.Run("no prototype run on record", func(t *testing.T) {
		t.Parallel()
		svc := prototypeSvcFor(&stubFlowTurns{}, "was", nil)
		got, err := svc.prototypeOutdated(context.Background(), "acme", "proj", "now")
		if err != nil || got {
			t.Fatalf("prototypeOutdated = (%v, %v), want (false, nil)", got, err)
		}
	})

	t.Run("asks for the prototype flow specifically", func(t *testing.T) {
		t.Parallel()
		turns := lastPrototype("abc")
		svc := prototypeSvcFor(turns, "same", nil)
		if _, err := svc.prototypeOutdated(context.Background(), "acme", "proj", "same"); err != nil {
			t.Fatalf("prototypeOutdated: %v", err)
		}
		if len(turns.asked) != 1 || turns.asked[0] != "prototype" {
			t.Fatalf("looked up flows %v, want [prototype]", turns.asked)
		}
	})

	t.Run("an unreadable baseline fails loudly", func(t *testing.T) {
		t.Parallel()
		svc := prototypeSvcFor(lastPrototype("gone"), "", errors.New("commit not in the mirror"))
		if _, err := svc.prototypeOutdated(context.Background(), "acme", "proj", "now"); err == nil {
			t.Fatal("an unreadable baseline was reported as up to date")
		}
	})

	t.Run("a failing turn source fails loudly", func(t *testing.T) {
		t.Parallel()
		svc := prototypeSvcFor(&stubFlowTurns{err: errors.New("db down")}, "was", nil)
		if _, err := svc.prototypeOutdated(context.Background(), "acme", "proj", "now"); err == nil {
			t.Fatal("a failing turn lookup was reported as up to date")
		}
	})

	t.Run("an unwired turn source reports nothing", func(t *testing.T) {
		t.Parallel()
		svc := prototypeSvcFor(nil, "was", nil)
		got, err := svc.prototypeOutdated(context.Background(), "acme", "proj", "now")
		if err != nil || got {
			t.Fatalf("prototypeOutdated = (%v, %v), want (false, nil)", got, err)
		}
	})
}

// On the status read: the flag rides SpecStage, and a project with no
// prototype at head reads false whatever the turn history says.
func TestGetProjectStatus_PrototypeOutdated(t *testing.T) {
	t.Parallel()

	status := func(t *testing.T, snap spec.StatusSnapshot) bool {
		t.Helper()
		fx := statusFixture{snap: snap}
		svc := fx.service()
		fake := svc.artifactSvc.(*artifactstest.FakeArtifactService)
		fake.DesignFingerprintAtFunc = func(context.Context, string, string, string) (string, error) {
			return "was", nil
		}
		svc.SetSpecTurnSource(lastPrototype("abc"))
		st, err := svc.GetProjectStatus(context.Background(), "acme", "web")
		if err != nil {
			t.Fatalf("GetProjectStatus: %v", err)
		}
		return st.Spec.PrototypeOutdated
	}

	if !status(t, spec.StatusSnapshot{HasDesign: true, HasPrototype: true, DesignFingerprint: "now"}) {
		t.Fatal("a design change after the prototype run did not mark it outdated")
	}
	if status(t, spec.StatusSnapshot{HasDesign: true, HasPrototype: true, DesignFingerprint: "was"}) {
		t.Fatal("an unchanged design marked the prototype outdated")
	}
	if status(t, spec.StatusSnapshot{HasDesign: true, HasPrototype: false, DesignFingerprint: "now"}) {
		t.Fatal("a project with no prototype read as having an outdated one")
	}
}
