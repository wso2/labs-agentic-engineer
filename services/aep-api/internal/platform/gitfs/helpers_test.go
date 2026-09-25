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

package gitfs_test

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/platform/gitfs"
	"github.com/wso2/aep/aep-api/internal/platform/gitfs/workspacetest"
	"github.com/wso2/aep/aep-api/internal/platform/secrets"
)

// seedFiles is the default origin content used across tests.
func seedFiles() map[string]string {
	return map[string]string{
		"README.md":                 "hello\n",
		"specs/requirements/prd.md": "req v1\n",
	}
}

// stubCred is a fake secrets.Credential minting a fixed token.
type stubCred struct{ token string }

func (s stubCred) Token(context.Context) (string, time.Time, error) {
	return s.token, time.Time{}, nil
}
func (s stubCred) Identity() secrets.Identity {
	return secrets.Identity{Name: "Stub User", Email: "stub@aep.test", Login: "stub"}
}
func (s stubCred) RepoOwner() string { return "stub-owner" }
func (s stubCred) WebhookStrategy() secrets.WebhookStrategy {
	return secrets.WebhookPerRepo
}

// cmdRecord is one observed git invocation.
type cmdRecord struct {
	Args []string
	Env  []string
}

// recorder captures every git invocation through the exec hook.
type recorder struct {
	mu   sync.Mutex
	cmds []cmdRecord
}

func recordCommands(t *testing.T, e *gitfs.Engine) *recorder {
	t.Helper()
	r := &recorder{}
	gitfs.SetExecHook(e, func(args, env []string) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.cmds = append(r.cmds, cmdRecord{Args: append([]string(nil), args...), Env: append([]string(nil), env...)})
	})
	t.Cleanup(func() { gitfs.SetExecHook(e, nil) })
	return r
}

func (r *recorder) all() []cmdRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]cmdRecord(nil), r.cmds...)
}

func (r *recorder) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cmds = nil
}

// countSubcommand counts invocations whose git subcommand equals name
// (skipping global --git-dir flags).
func (r *recorder) countSubcommand(name string) int {
	n := 0
	for _, c := range r.all() {
		if subcommand(c.Args) == name {
			n++
		}
	}
	return n
}

func subcommand(args []string) string {
	for i := 1; i < len(args); i++ {
		switch {
		case args[i] == "--git-dir" || args[i] == "-C":
			i++ // skip the value
		case strings.HasPrefix(args[i], "-"):
		default:
			return args[i]
		}
	}
	return ""
}

// gitOut runs git against a bare repo dir directly (origin or mirror
// assertions that gittest.Remote does not cover).
func gitOut(t *testing.T, gitDir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"--git-dir", gitDir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CONFIG_SYSTEM="+os.DevNull,
		"GIT_TERMINAL_PROMPT=0",
		// Scrubbing the config above also scrubs the developer's identity, and
		// object-writing commands (`tag -a`) refuse to run without one.
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@aep.local",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@aep.local")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// mustHead is Engine.Head or fail.
func mustHead(t *testing.T, fx *workspacetest.Fixture, at string) string {
	t.Helper()
	sha, err := fx.Engine.Head(context.Background(), fx.Ref, at)
	if err != nil {
		t.Fatalf("Head(%q): %v", at, err)
	}
	return sha
}

// mirrorGitDir derives the fixture repo's mirror path.
func mirrorGitDir(t *testing.T, fx *workspacetest.Fixture) string {
	t.Helper()
	repoDir, err := gitfs.RepoDir(fx.Engine.Root(), fx.Ref)
	if err != nil {
		t.Fatalf("RepoDir: %v", err)
	}
	return gitfs.GitSubdir(repoDir)
}
