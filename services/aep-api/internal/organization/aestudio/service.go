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

package aestudio

// service.go — the Service: its dependencies, and the single-flight
// converge per org that Trigger starts and Status reports on.

import (
	"context"
	"sync"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/clients/secretmanagersvc"
	"github.com/wso2/aep/aep-api/internal/config"
	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/auth"
	"github.com/wso2/aep/aep-api/internal/platform/modelconn"
)

// The OpenChoreo object names (R18): every org's AE Studio is the Resource
// ae-studio of the Project ae-system, in the org's own namespace.
const (
	ProjectName  = "ae-system"
	ResourceName = "ae-studio"
)

// State, URLs and Status are the organization root's AE Studio types: the
// root declares them so its slices answer GET /ae-studio without importing
// this package.
type (
	State  = organization.AEStudioState
	URLs   = organization.AEStudioURLs
	Status = organization.AEStudioStatus
)

const (
	StateAbsent       = organization.AEStudioAbsent
	StateProvisioning = organization.AEStudioProvisioning
	StateReady        = organization.AEStudioReady
	StateFailed       = organization.AEStudioFailed
)

var (
	_ organization.StudioConverger      = (*Service)(nil)
	_ organization.AEStudioStatusReader = (*Service)(nil)
)

const (
	// convergeTimeout bounds one converge, detached from the request.
	convergeTimeout = 3 * time.Minute
	// releaseWaitInterval and releaseWaitTimeout bound the wait for the
	// ResourceRelease a parameters change cuts.
	releaseWaitInterval = 2 * time.Second
	releaseWaitTimeout  = 2 * time.Minute
	// failureBackoff is how long a failed converge answers failed (so the
	// console's Try again retries after it) unless what it was asked to
	// install changes.
	failureBackoff = 30 * time.Second
	// notReadyBound is how long a binding may stay not Ready, counted from
	// the later of its Ready condition's last transition and this replica's
	// last successful converge, before Status answers failed. It is above
	// the ResourceType's startup budget (the slowest container's
	// startupProbe allows 40 x 5 s = 200 s) and equals the Deployment's
	// default progress deadline, so a pod still pulling or starting is
	// never called failed.
	notReadyBound = 10 * time.Minute
	// settleGrace is how long after a successful converge a terminal Ready
	// reason is still taken as the previous release's, which OC has not yet
	// re-evaluated against the new pin.
	settleGrace = time.Minute
)

// OC is the OpenChoreo surface the Service reads and writes through.
type OC struct {
	Projects interface {
		GetProject(ctx context.Context, org, project string) (*gen.Project, error)
		CreateProject(ctx context.Context, org string, req *gen.CreateProjectRequest) (*gen.Project, error)
	}
	Cells interface {
		EnsureProjectReleaseBinding(ctx context.Context, namespace, project, environment string) error
		ProjectReleaseBindingReadiness(ctx context.Context, namespace, project, environment string) (openchoreo.Readiness, error)
	}
	Targets interface {
		Resolve(ctx context.Context, org, project string) (string, error)
	}
	Resources  openchoreo.ResourceClient
	SecretRefs interface {
		GetSecretReference(ctx context.Context, namespace, name string) (*secretmanagersvc.SecretReference, error)
	}
}

// Deps is what the Service reads the desired state from, and the
// OpenChoreo clients it reaches OpenChoreo through.
type Deps struct {
	Config     config.AEStudioConfig
	OrgSecrets interface {
		List(ctx context.Context, ocOrgID string) ([]organization.OrgSecretRef, error)
	}
	Orgs interface {
		GetByName(ctx context.Context, name string) (*organization.Organization, error)
	}
	Profiles interface {
		GetProfileByOrgID(ctx context.Context, orgID string) (*organization.OrganizationIDPProfile, error)
	}
	Connections interface {
		Connection(ctx context.Context, ocOrgID string) (modelconn.Connection, bool, error)
	}
	GitHub interface {
		Status(ctx context.Context, ocOrgID string) (*organization.Projection, error)
	}
	// OC is every OpenChoreo read and write, Status's and the converge's
	// alike, as aep-api's own identity (M2M + X-Impersonate-Org) wherever
	// one is configured: it never passes the caller's JWT through. The
	// org-membership gate in front of GET /ae-studio is the only check on
	// the caller.
	OC OC
}

// Service installs and converges each org's AE Studio (ticket 08 §9, §10).
type Service struct {
	cfg         config.AEStudioConfig
	orgSecrets  interface{ List(context.Context, string) ([]organization.OrgSecretRef, error) }
	orgs        interface{ GetByName(context.Context, string) (*organization.Organization, error) }
	profiles    interface{ GetProfileByOrgID(context.Context, string) (*organization.OrganizationIDPProfile, error) }
	connections interface{ Connection(context.Context, string) (modelconn.Connection, bool, error) }
	github      interface{ Status(context.Context, string) (*organization.Projection, error) }
	oc          OC
	now         func() time.Time

	notConfiguredOnce sync.Once

	mu sync.Mutex
	// flights are the orgs with a converge running; a Trigger during one
	// asks it to run once more when it ends, so a save made mid-converge
	// still lands.
	flights map[string]*flight
	// failures are the orgs whose last converge failed.
	failures map[string]failure
	// converged is, per org, when this replica's last converge succeeded
	// (the start of its binding's not-Ready clock).
	converged map[string]time.Time
	// statusFailures is, per org, the desired state Status last logged a
	// failed answer for, so a polling console logs it once.
	statusFailures map[string]string
}

type flight struct{ again bool }

// failure is a failed converge: when, and the desired state it was given
// ("" when the converge failed before it could compute one).
type failure struct {
	at          time.Time
	fingerprint string
}

// New builds the Service.
func New(d Deps) *Service {
	return &Service{
		cfg: d.Config, orgSecrets: d.OrgSecrets, orgs: d.Orgs, profiles: d.Profiles,
		connections: d.Connections, github: d.GitHub,
		oc: d.OC, now: time.Now,
		flights: map[string]*flight{}, failures: map[string]failure{}, converged: map[string]time.Time{},
		statusFailures: map[string]string{},
	}
}

// Trigger starts a converge of org's AE Studio and returns at once. One
// converge runs per org at a time; a Trigger while one runs makes it run
// once more after, rather than starting a second.
//
// The converge keeps ctx's values but not its cancellation or deadline, and
// is marked as aep-api's own call (WithServiceIdentity): it outlives the
// request that started it.
func (s *Service) Trigger(ctx context.Context, org string) {
	s.mu.Lock()
	if f, ok := s.flights[org]; ok {
		f.again = true
		s.mu.Unlock()
		return
	}
	f := &flight{}
	s.flights[org] = f
	s.mu.Unlock()
	go s.fly(auth.WithServiceIdentity(context.WithoutCancel(ctx)), org, f)
}

// fly runs converges of org until no Trigger asked for another.
func (s *Service) fly(ctx context.Context, org string, f *flight) {
	for {
		cctx, cancel := context.WithTimeout(ctx, convergeTimeout)
		fingerprint, err := s.converge(cctx, org)
		cancel()
		s.mu.Lock()
		if err != nil {
			s.failures[org] = failure{at: s.now(), fingerprint: fingerprint}
		} else {
			delete(s.failures, org)
			s.converged[org] = s.now()
		}
		if !f.again {
			delete(s.flights, org)
			s.mu.Unlock()
			return
		}
		f.again = false
		s.mu.Unlock()
	}
}

// convergedAt is when org's last converge on this replica succeeded; zero
// when none has since the process started.
func (s *Service) convergedAt(org string) time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.converged[org]
}

// busy reports whether a converge of org is running.
func (s *Service) busy(org string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.flights[org]
	return ok
}

// recentlyFailed reports whether org's last converge failed within the
// back-off on the same desired state. A converge that failed before it had
// a desired state (a transient failure reading it) matches any, so the
// back-off holds even when Status's own read of it then succeeds.
func (s *Service) recentlyFailed(org, fingerprint string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.failures[org]
	return ok && (f.fingerprint == "" || f.fingerprint == fingerprint) && s.now().Sub(f.at) < failureBackoff
}

// firstStatusFailure reports whether this is the first failed answer Status
// gives org for this desired state in the current failure episode, and
// records it. statusRecovered ends the episode.
func (s *Service) firstStatusFailure(org, fingerprint string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.statusFailures[org] == fingerprint {
		return false
	}
	s.statusFailures[org] = fingerprint
	return true
}

// statusRecovered ends org's failure episode: Status answered other than
// failed, so the next failed answer is logged again.
func (s *Service) statusRecovered(org string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.statusFailures, org)
}
