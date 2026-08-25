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
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/wso2/aep/aep-api/internal/delivery"

	"github.com/wso2/aep/aep-api/internal/gen"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/platform/async"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// Error sentinels for the project feature. ErrProjectNotFound is owned here.
// ErrUnauthorized / ErrForbidden are feature-local sentinels the controllers
// branch on.
var (
	ErrProjectNotFound = errors.New("project not found")
	ErrUnauthorized    = errors.New("unauthorized")
	ErrForbidden       = errors.New("forbidden")
)

// Service handles business logic for project operations. edge.Deps holds it as a
// concrete *project.Service (there is one implementation; the old ProjectService
// interface existed only to be faked, and the component tier fakes at the HTTP
// edge instead).
type Service struct {
	client         openchoreo.ProjectClient
	repoSvc        sourcecontrol.RepoService
	webhookSvc     sourcecontrol.WebhookService
	artifactSvc    spec.ArtifactService
	execs          delivery.ExecutionRepository
	skillsProv     skillsProvisioner
	descriptors    descriptorWriter      // project descriptor stamp; may be nil
	skillMirrorSvc skillMirror           // seeds .claude/skills into the new repo; may be nil
	deprovisioner  resourceDeprovisioner // dependency provisioning teardown; may be nil
	runReader      milestoneRunRows      // build/deploy stage reads + delete purge (status_stages.go)
	bindingsReader bindingsReader        // deploy stage: OC release bindings (status_stages.go)
	specTurns      specTurnRows          // spec stage: newest agent turn (status_stages.go); may be nil
	runAbandoner   runAbandoner          // run-supervisor teardown on delete; may be nil
	kickoff        kickoffStarter        // fires `/start` on create (#562); may be nil
}

// runAbandoner is project_service's narrow consumer port for the run-supervisor
// half of the delete cascade: it ends the workflows supervising the project's
// live runs. An app-root adapter over the milestone-run index and the run
// supervisor satisfies it. Wired via SetRunAbandoner; nil is a documented no-op.
//
// It is a SEPARATE port from milestoneRunRows, whose purge deletes rows. Deleting
// the rows is not what stops a supervisor — nothing reads them back — so the two
// halves are two different writes to two different systems, and folding the
// workflow teardown behind a name that says "delete rows" would hide a Temporal
// call inside a database one.
type runAbandoner interface {
	// AbandonProjectRuns ends the supervisor over every live run of the project.
	// Already-settled runs and milestones that were never supervised are no-ops.
	AbandonProjectRuns(ctx context.Context, orgID, projectID string) error
}

// SetRunAbandoner wires the run-supervisor teardown so a project delete stops
// the workflows supervising its runs. A nil abandoner is a documented no-op.
func (s *Service) SetRunAbandoner(a runAbandoner) { s.runAbandoner = a }

// resourceDeprovisioner is project_service's narrow consumer port for the
// dependency-provisioning teardown: on project delete it deprovisions the
// project's OC Resource model (external + platform resources), which the OC
// Project delete does not cascade. *provisioning.Service satisfies it. Wired via
// SetResourceDeprovisioner at the composition root; nil is a no-op.
type resourceDeprovisioner interface {
	DeprovisionProject(ctx context.Context, orgID, projectID string) error
}

// skillsProvisioner is the narrow port for eagerly provisioning the org's
// skills repo on project creation. The full skills.SkillService satisfies it.
// Defined here so project doesn't import the skills package. §6.3/§10.2.
type skillsProvisioner interface {
	EnsureProvisioned(ctx context.Context, orgID string) error
}

// descriptorWriter stamps the project descriptor (specs/.agentic-engineer.toml)
// into a freshly-provisioned repo: the marker identifying the repo as an
// Agentic Engineer project, carrying the idea the user typed at create. Narrow
// port declared here (like skillsProvisioner) so projects keeps no spec edge;
// *spec.DescriptorWriter satisfies it. Wired at the composition root; nil is a
// documented no-op.
type descriptorWriter interface {
	WriteDescriptor(ctx context.Context, orgID, projectID, name, idea string) error
}

// kickoffStarter fires the new project's opening `/start` turn (#562) — the
// journey starts itself rather than waiting for a Generate-spec click. Narrow
// port declared here (like the three around it) so projects keeps no genai
// edge; *spec.Service satisfies it. Wired at the composition root; nil is a
// documented no-op.
//
// Deliberately returns nothing. It runs INLINE — the create answers only once
// the turn exists, so the console can paint the kickoff on arrival and
// `spec.agent == ""` means one thing rather than two — but it still cannot
// fail the creation the user already committed to, so it swallows its own
// errors and bounds its own time.
type kickoffStarter interface {
	Kickoff(ctx context.Context, orgID, projectID string)
}

// skillMirror seeds the new repo's `.claude/skills/` copies from the org
// library, so a clone carries the coding guidance before any design or task
// exists. Narrow port declared here (like the two above) so projects keeps no
// spec edge; *spec.SkillService satisfies it. Wired at the composition root;
// nil is a documented no-op.
type skillMirror interface {
	SyncProjectSkills(ctx context.Context, orgID, projectID string) error
}

func (s *Service) SetSkillsProvisioner(p skillsProvisioner) { s.skillsProv = p }

func (s *Service) SetDescriptorWriter(w descriptorWriter) { s.descriptors = w }

func (s *Service) SetSkillMirror(m skillMirror) { s.skillMirrorSvc = m }

// SetKickoffStarter wires the create-time `/start` dispatch (#562). A nil
// starter is a documented no-op — the project is still perfectly usable, and
// its spec card offers the kickoff as a CTA.
func (s *Service) SetKickoffStarter(k kickoffStarter) { s.kickoff = k }

func NewProjectService(
	client openchoreo.ProjectClient,
	repoSvc sourcecontrol.RepoService,
	webhookSvc sourcecontrol.WebhookService,
	artifactSvc spec.ArtifactService,
	execs delivery.ExecutionRepository,
) *Service {
	return &Service{
		client:      client,
		repoSvc:     repoSvc,
		webhookSvc:  webhookSvc,
		artifactSvc: artifactSvc,
		execs:       execs,
	}
}

func (s *Service) ListProjects(ctx context.Context, orgName string, limit int, cursor, search string) (*gen.ProjectList, error) {
	list, err := s.client.ListProjects(ctx, orgName, limit, cursor)
	if err != nil {
		return nil, translateHTTPError(err)
	}
	// Annotate each project with its repo URL from the BFF's own rows (#108
	// — the delete dialog names the repo the cascade destroys). Best-effort:
	// a failing join degrades to a repoUrl-less list, never a 500.
	if s.repoSvc != nil && len(list.Items) > 0 {
		if repos, repoErr := s.repoSvc.ListByOrg(ctx, orgName); repoErr != nil {
			slog.WarnContext(ctx, "repoUrl annotation skipped", "org", orgName, "error", repoErr)
		} else {
			byProject := make(map[string]string, len(repos))
			for _, r := range repos {
				byProject[r.ProjectID] = r.RepoURL
			}
			for i := range list.Items {
				list.Items[i].RepoURL = byProject[list.Items[i].Name]
			}
		}
	}
	// The search filter is applied Go-side over the FETCHED PAGE only — the
	// OpenChoreo list API takes just (limit, cursor), so a match on a later
	// page is not pulled forward; the caller pages via nextCursor and filters
	// each page. Acceptable for the console's org-sized project counts.
	if search != "" {
		needle := strings.ToLower(search)
		filtered := make([]gen.Project, 0, len(list.Items))
		for _, p := range list.Items {
			if strings.Contains(strings.ToLower(p.Name), needle) ||
				strings.Contains(strings.ToLower(p.DisplayName), needle) {
				filtered = append(filtered, p)
			}
		}
		list.Items = filtered
	}
	return list, nil
}

func (s *Service) GetProject(ctx context.Context, orgName, projectName string) (*gen.Project, error) {
	project, err := s.client.GetProject(ctx, orgName, projectName)
	if err != nil {
		return nil, translateHTTPError(err)
	}
	return project, nil
}

func (s *Service) CreateProject(ctx context.Context, orgName string, req *gen.CreateProjectRequest) (*gen.Project, error) {
	project, err := s.client.CreateProject(ctx, orgName, req)
	if err != nil {
		return nil, translateHTTPError(err)
	}

	// Eagerly provision the org's shared skills repo (+ seed built-ins) so the
	// first design run doesn't pay repo-creation latency. Async + best-effort;
	// reads self-heal via ensureSkillsRepo if this never ran. §6.3/§10.3.
	if s.skillsProv != nil {
		async.Go(context.Background(), "skills provisioning", func(context.Context) {
			bg, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			if perr := s.skillsProv.EnsureProvisioned(bg, orgName); perr != nil {
				slog.WarnContext(bg, "skills repo provisioning failed (will self-heal on read)",
					"org", orgName, "error", perr)
			}
		})
	}

	// Provision + clone the platform-owned git repo (async — polling via GetRepoStatus).
	if s.repoSvc != nil {
		repoInfo, createErr := s.repoSvc.CreateRepo(ctx, orgName, project.Name, req.Name, req.RepoName)
		if createErr != nil {
			// A repo name that already exists — user-chosen or derived from
			// the project name — can never succeed on retry: compensate the
			// OC project away and fail the create so the user picks another
			// name. Every other repo failure stays best-effort (clone happens
			// async and can be retried).
			if sourcecontrol.IsRepoNameConflict(createErr) {
				if delErr := s.client.DeleteProject(ctx, orgName, project.Name); delErr != nil {
					slog.ErrorContext(ctx, "failed to compensate project after repo name conflict",
						"project", project.Name, "error", delErr)
				}
				return nil, createErr
			}
			slog.ErrorContext(ctx, "failed to provision repo", "project", project.Name, "error", createErr)
			// Don't fail project creation — clone happens async and can be retried.
		} else {
			// Build credentials are now pre-staged per WorkflowRun as a K8s
			// Secret named `<workflowRunName>-git-secret` in
			// workflows-<orgID> immediately before each dispatch — see
			// docs/design/build-credential-injection.md. Project creation
			// no longer participates in any secret provisioning;
			// OcSecretRefName is unused on new flows.
			if repoInfo == nil {
				slog.ErrorContext(ctx, "nil repoInfo on CreateRepo", "project", project.Name)
			}
			// Register the per-repo webhook so the BFF starts receiving events
			// (pull_request, push, issue_comment) on this repo. Best-effort.
			if s.webhookSvc != nil {
				if _, hookErr := s.webhookSvc.Register(ctx, orgName, project.Name); hookErr != nil {
					slog.ErrorContext(ctx, "failed to register webhook on repo",
						"project", project.Name, "error", hookErr)
				}
			}
			// Stamp the project descriptor. This is the ONLY durable copy of
			// the idea the user typed — it is what the /start flow reads back
			// to generate requirements from, on any device and any client.
			// Written even with an empty prompt: the file is also the marker
			// that says "an Agentic Engineer project lives here".
			//
			// Best-effort, like every other post-create step above: a write
			// failure must not destroy a creation the user already committed
			// to, and /start degrades by asking for the idea instead.
			if s.descriptors != nil {
				if derr := s.descriptors.WriteDescriptor(ctx, orgName, project.Name, project.Name, req.Prompt); derr != nil {
					slog.ErrorContext(ctx, "failed to write project descriptor (project usable; /start will ask for the idea)",
						"project", project.Name, "error", derr)
				}
			}

			// Seed `.claude/skills/` so a clone carries the org's coding
			// guidance before any design or task exists. ASYNC for the same
			// reason the skills-repo provisioning above is: this may have to
			// create the org repo on first touch (its read path provisions
			// lazily), and GitHub repo creation must not sit in the create
			// latency the user waits on. Best-effort — every later refresh at
			// design save and at dispatch is diff-first, so a project that
			// misses this seed heals on its first build.
			if s.skillMirrorSvc != nil {
				mirror, projectName := s.skillMirrorSvc, project.Name
				async.Go(context.Background(), "project skills seed", func(context.Context) {
					bg, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
					defer cancel()
					if merr := mirror.SyncProjectSkills(bg, orgName, projectName); merr != nil {
						slog.WarnContext(bg, "skills: project mirror seed failed (heals on the next refresh)",
							"org", orgName, "project", projectName, "error", merr)
					}
				})
			}

			// The journey starts itself (#562): fire `/start` rather than
			// land the user on a dashboard asking them to press a button for
			// work the platform can already do. AFTER the descriptor commit
			// above — that file is where the turn reads the idea from — and
			// BEFORE this call returns, so the client arrives at a project
			// whose turn already exists rather than one that looks unstarted
			// for the couple of seconds the dispatch takes.
			//
			// HELD when the caller says reference documents are still coming:
			// they are the primary brief, and a kickoff dispatched before the
			// upload would interview the user about a document the agent
			// never saw. The references call fires it instead. An abandoned
			// upload therefore leaves the project un-started, which the spec
			// card offers as a CTA — the honest outcome, and better than an
			// interview conducted blind.
			if s.kickoff != nil && !req.ReferencesPending {
				s.kickoff.Kickoff(ctx, orgName, project.Name)
			}
		}
	}

	return project, nil
}

// SetResourceDeprovisioner wires the dependency-provisioning teardown so a
// project delete deprovisions its OC Resource model. A nil deprovisioner is a
// documented no-op.
func (s *Service) SetResourceDeprovisioner(d resourceDeprovisioner) {
	s.deprovisioner = d
}

func (s *Service) DeleteProject(ctx context.Context, orgName, projectName string) error {
	// Deprovision the project's OC Resource model FIRST — while its design (the
	// dependency inventory) is still readable and before the OC Project delete,
	// which does not cascade the logically-owned Resources/bindings. Best-effort.
	if s.deprovisioner != nil {
		if err := s.deprovisioner.DeprovisionProject(ctx, orgName, projectName); err != nil {
			slog.ErrorContext(ctx, "failed to deprovision project resources", "org", orgName, "project", projectName, "error", err)
		}
	}

	// An OC Project that is ALREADY GONE is not a failure of this delete — it is
	// the state this delete exists to reach. Everything below outlives the OC
	// Project and has no other exit from the system: the run supervisors, the
	// repository row, the executions and the run ledger. Aborting here would
	// strand all four permanently, because the second attempt takes this same
	// branch and never gets past it — which is exactly how a half-deleted project
	// keeps a supervisor polling a repository nobody can reach any more.
	//
	// Every other OC failure still aborts. A delete that could not REACH
	// OpenChoreo has established nothing, and tearing down the platform's half of
	// a project that still exists there would strand the project instead.
	if err := translateHTTPError(s.client.DeleteProject(ctx, orgName, projectName)); err != nil &&
		!errors.Is(err, ErrProjectNotFound) {
		return err
	}

	// The run SUPERVISORS come down before the things they read do — before the
	// repository they poll and before the rows they write.
	//
	// Nothing else ends a run workflow. Its milestone poll retries forever
	// (Temporal's default policy is unbounded) against a repository that is about
	// to stop existing, and its workflow id is keyed on (org, project, milestone)
	// alone — so a project later created under the SAME NAME collides with it, and
	// that project's first run is refused as AlreadyStarted and never supervised.
	// Best-effort, like the rest of the teardown: the project delete has already
	// been committed upstream and cannot be undone by a failure here.
	if s.runAbandoner != nil {
		if err := s.runAbandoner.AbandonProjectRuns(ctx, orgName, projectName); err != nil {
			slog.ErrorContext(ctx, "failed to abandon run supervisors for project — they will keep polling a deleted repository",
				"org", orgName, "project", projectName, "error", err)
		}
	}

	// Stop the deliveries this project's repo would otherwise keep making.
	//
	// The remote survives a project delete, so its webhook survives with it —
	// and it would go on posting `issues`, `push` and `pull_request` events for a
	// project the platform has already forgotten. This is the last point at which
	// it can be removed at all: the hook ID and the repo identity both live on
	// the repo row that the very next step deletes.
	//
	// Best-effort by construction. A webhook left on a repository is untidy, not
	// dangerous — the event plane resolves every delivery through the repo row
	// and drops what it cannot place — so it must never be the thing that blocks
	// a delete from completing.
	if s.webhookSvc != nil {
		if err := s.webhookSvc.Unregister(ctx, orgName, projectName); err != nil {
			slog.ErrorContext(ctx, "failed to unregister webhook — the repo will keep posting deliveries for a deleted project",
				"org", orgName, "project", projectName, "error", err)
		}
	}

	// Drop the platform's own repository record and its workspace clone.
	//
	// The REMOTE is deliberately not touched, and that is the one piece of this
	// teardown that leaves something behind: the GitHub repository survives a
	// project delete, along with its issues and its milestones. DeleteRepo
	// announces the URL it is orphaning, which is the last moment anything can
	// name it. The consequence worth knowing at this call site is that the repo
	// NAME stays taken — a project recreated under it is refused with
	// ErrRepoNameConflict until a human intervenes.
	if s.repoSvc != nil {
		if err := s.repoSvc.DeleteRepo(ctx, orgName, projectName); err != nil {
			slog.ErrorContext(ctx, "failed to delete git repo for project", "org", orgName, "project", projectName, "error", err)
		}
	}

	// Tasks are GitHub ISSUES, and they survive with the remote above; nothing
	// here deletes them. The platform-owned executions rows for the project ARE
	// purged — they are keyed to the project and would otherwise orphan (no FK to
	// cascade). Best-effort, like the repo cleanup.
	if s.execs != nil {
		if err := s.execs.DeleteByProject(ctx, orgName, projectName); err != nil {
			slog.ErrorContext(ctx, "failed to purge executions for project", "org", orgName, "project", projectName, "error", err)
		}
	}

	// Purge the milestone runs (and their cycle records) too — a recreated
	// same-named project must not resurrect stale runs, or the status poll would
	// chase versions the fresh repo never had.
	if s.runReader != nil {
		if err := s.runReader.DeleteByProject(ctx, orgName, projectName); err != nil {
			slog.ErrorContext(ctx, "failed to purge milestone runs for project", "org", orgName, "project", projectName, "error", err)
		}
	}

	return nil
}

func (s *Service) GetProjectStatus(ctx context.Context, orgName, projectName string) (*gen.ProjectStatus, error) {
	// The nested stages are contract-required: always present, zero-valued
	// (idle build, no deploy) until the repo is ready and the sources read.
	status := &gen.ProjectStatus{}
	status.Build.Status = buildIdle
	status.Deploy.Status = deployNone

	// Check git repo
	if s.repoSvc == nil {
		status.Phase = "no-repo"
		return status, nil
	}

	repo, err := s.repoSvc.GetRepo(ctx, orgName, projectName)
	if err != nil {
		slog.ErrorContext(ctx, "get repo for project status", "error", err, "project", projectName)
		status.Phase = "no-repo"
		return status, nil
	}
	if repo == nil {
		status.Phase = "no-repo"
		return status, nil
	}

	if done := applyRepoToProjectStatus(status, repo); done {
		return status, nil
	}

	// Ready repo: derive the three stage aggregates + the flat artifact
	// fields from the four poll sources (status_stages.go).
	if err := s.populateStages(ctx, orgName, projectName, status); err != nil {
		return nil, err
	}
	return status, nil
}

// applyRepoToProjectStatus maps a provisioned git_repositories row onto the
// status fields that depend on repo lifecycle. Returns true when phase is
// fully determined (no-repo, cloning, or error) and artifact checks can stop.
func applyRepoToProjectStatus(status *gen.ProjectStatus, repo *sourcecontrol.GitRepository) bool {
	if repo == nil {
		status.Phase = "no-repo"
		return true
	}

	status.RepoStatus = repo.Status
	status.RepoURL = repo.RepoURL

	switch repo.Status {
	case "pending", "cloning":
		status.Phase = "repo-cloning"
		return true
	case "error":
		status.Phase = "repo-error"
		status.RepoErrorMessage = repo.ErrorMessage
		return true
	}
	return false
}

// translateHTTPError lifts OC-level sentinel errors (openchoreo.ErrNotFound
// etc.) into the project-service vocabulary the controllers branch on. The
// underlying err is preserved in the chain so deeper layers can still
// errors.Is against openchoreo.* if they want richer context.
func translateHTTPError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, openchoreo.ErrNotFound):
		return fmt.Errorf("%w: %v", ErrProjectNotFound, err)
	case errors.Is(err, openchoreo.ErrUnauthorized):
		return ErrUnauthorized
	case errors.Is(err, openchoreo.ErrForbidden):
		return ErrForbidden
	}
	return err
}
