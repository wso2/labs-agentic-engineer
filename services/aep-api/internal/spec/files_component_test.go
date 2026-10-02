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

// Shared rig for the references component tests: the REAL contract-first
// handler chain (strict server via componenttest) over the production gitrepo
// gateway, with the REAL gitfs Workspace engine mirroring a REAL bare file://
// origin (pure workspacetest fixture). Only the repo row + credential resolver
// are faked. The Files API read/apply endpoints are gone (the pod serves
// them); the spec.FilesService stays for aep-api's in-process adapters.
package spec_test

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/edge"
	"github.com/wso2/aep/aep-api/internal/platform/componenttest"
	"github.com/wso2/aep/aep-api/internal/platform/gitfs"
	"github.com/wso2/aep/aep-api/internal/platform/gitfs/workspacetest"
	"github.com/wso2/aep/aep-api/internal/platform/gittest"
	"github.com/wso2/aep/aep-api/internal/platform/secrets"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
	"github.com/wso2/aep/aep-api/internal/spec"
)

const (
	filesTestOrg  = "acme-org"
	filesTestProj = "widgets"
	testSlug      = "acme-widgets"
)

// ---- faked edges ----

// filesStubRepoResolver hands out the single repo row, keyed by the AUTHENTICATED
// org — a caller resolved to any other org gets ErrRepoNotFound (the 404),
// mirroring the production (org_id, project_id) row lookup.
type filesStubRepoResolver struct{ rec *sourcecontrol.GitRepository }

func (s filesStubRepoResolver) GetRepo(_ context.Context, orgID, _ string) (*sourcecontrol.GitRepository, error) {
	if s.rec == nil || orgID != s.rec.OrgID {
		return nil, sourcecontrol.ErrRepoNotFound
	}
	return s.rec, nil
}

type filesStubCred struct{}

func (filesStubCred) Token(context.Context) (string, time.Time, error) {
	return "test-token", time.Time{}, nil
}
func (filesStubCred) Identity() secrets.Identity {
	return secrets.Identity{Name: "Bot", Email: "bot@aep.dev", Login: "bot"}
}
func (filesStubCred) RepoOwner() string                        { return "acme" }
func (filesStubCred) WebhookStrategy() secrets.WebhookStrategy { return secrets.WebhookPlatform }

type filesStubResolver struct{}

func (filesStubResolver) Resolve(context.Context, string) (secrets.Credential, error) {
	return filesStubCred{}, nil
}

// ---- harness ----

type filesRig struct {
	h      *componenttest.Harness
	remote *gittest.Remote
	engine *gitfs.Engine
}

func newFilesRig(t *testing.T, seed map[string]string) *filesRig {
	t.Helper()
	remote := gittest.NewRemote(t, gittest.WithSeed(seed, "seed"))
	rec := &sourcecontrol.GitRepository{
		OrgID:         filesTestOrg,
		ProjectID:     filesTestProj,
		RepoURL:       remote.URL(),
		RepoSlug:      testSlug, // pinned — SlugForURL can't parse file:// URLs
		DefaultBranch: "main",
		Status:        "ready",
	}
	// The production gateway over the real engine: every read AND the Apply
	// write run through the Workspace port (the REST git-object port is nil —
	// files never touches it).
	engine := workspacetest.NewEngine(t)
	gitOps := sourcecontrol.NewGitOpsService(filesStubResolver{}, engine)
	svc := spec.NewFilesService(filesStubRepoResolver{rec: rec}, gitOps)
	h := componenttest.New(t, componenttest.Options{Deps: edge.Deps{
		Spec: mustSpecHandlers(t, spec.Deps{Files: svc}),
	}})
	return &filesRig{h: h, remote: remote, engine: engine}
}

// mirrorRevParse resolves rev inside the ENGINE's bare mirror (not the origin)
// — the C8 sha-consistency probe.
func (r *filesRig) mirrorRevParse(t *testing.T, rev string) string {
	t.Helper()
	repoDir, err := gitfs.RepoDir(r.engine.Root(), gitfs.RepoRef{
		OrgID: filesTestOrg, ProjectID: filesTestProj, RepoSlug: testSlug,
	})
	if err != nil {
		t.Fatalf("mirror git dir: %v", err)
	}
	gitDir := gitfs.GitSubdir(repoDir)
	cmd := exec.Command("git", "--git-dir", gitDir, "rev-parse", "--verify", rev)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_SYSTEM="+os.DevNull)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("mirror rev-parse %s: %v\n%s", rev, err, out)
	}
	return strings.TrimSpace(string(out))
}

func firstBytes(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
