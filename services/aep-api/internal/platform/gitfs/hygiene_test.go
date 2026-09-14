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
	"bytes"
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/platform/gitfs"
	"github.com/wso2/aep/aep-api/internal/platform/gitfs/workspacetest"
)

const testToken = "ghs_sekret-token-1234567890"

// TestAskpassShimAnswersPrompts invokes the generated shim exactly as git
// would: username prompt → the fixed token username, password prompt → the
// token from the child environment.
func TestAskpassShimAnswersPrompts(t *testing.T) {
	engine := workspacetest.NewEngine(t)
	shim := gitfs.AskpassPath(engine)

	info, err := os.Stat(shim)
	if err != nil {
		t.Fatalf("askpass shim missing: %v", err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("askpass shim mode = %v, want 0700", info.Mode().Perm())
	}

	run := func(prompt string) string {
		cmd := exec.Command(shim, prompt)
		cmd.Env = append(os.Environ(), "GITFS_TOKEN="+testToken)
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("shim %q: %v", prompt, err)
		}
		return string(out)
	}
	if got := run("Username for 'https://github.com':"); got != "x-access-token\n" {
		t.Fatalf("shim username answer = %q, want x-access-token", got)
	}
	if got := run("Password for 'https://x-access-token@github.com':"); got != testToken {
		t.Fatalf("shim password answer = %q, want the token verbatim", got)
	}
}

// TestTokenNeverInArgvOrOnDisk drives clone/fetch/push/tag with a credential
// attached and asserts the token appears in NO git argv and NOWHERE on the
// mount afterwards (config files, packed refs, anything).
func TestTokenNeverInArgvOrOnDisk(t *testing.T) {
	fx := workspacetest.New(t, seedFiles())
	fx.Ref.Cred = stubCred{token: testToken}
	ctx := context.Background()

	rec := recordCommands(t, fx.Engine)

	// Exercise every remote-op class: clone (via first read), fetch, push,
	// tag push, snapshot.
	mustHead(t, fx, "")
	if _, err := fx.Engine.Mutate(ctx, fx.Ref, func(tx gitfs.Tx) error {
		tx.Write("specs/a.md", []byte("a\n"))
		return nil
	}, gitfs.CommitOpts{Message: "hygiene", Retry: fastRetry}); err != nil {
		t.Fatalf("Mutate: %v", err)
	}
	if err := fx.Engine.Tag(ctx, fx.Ref, gitfs.TagSpec{Name: "v1", Message: "v1"}); err != nil {
		t.Fatalf("Tag: %v", err)
	}
	sha := mustHead(t, fx, "")
	if err := fx.Engine.Ensure(ctx, fx.Ref, sha); err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	sawRemoteOp := false
	for _, c := range rec.all() {
		for _, arg := range c.Args {
			if strings.Contains(arg, testToken) {
				t.Fatalf("token leaked into argv: %v", c.Args)
			}
		}
		// The token may ONLY travel via the GITFS_TOKEN env var, paired with
		// GIT_ASKPASS pointing at the shim.
		hasToken := false
		hasAskpass := false
		for _, kv := range c.Env {
			if strings.Contains(kv, testToken) {
				if kv != "GITFS_TOKEN="+testToken {
					t.Fatalf("token leaked into env var %q", kv)
				}
				hasToken = true
			}
			if strings.HasPrefix(kv, "GIT_ASKPASS=") {
				hasAskpass = true
			}
		}
		if hasToken {
			sawRemoteOp = true
			if !hasAskpass {
				t.Fatalf("GITFS_TOKEN set without GIT_ASKPASS on %v", c.Args)
			}
		}
	}
	if !sawRemoteOp {
		t.Fatal("no remote op carried the credential — the test exercised nothing")
	}

	// Nothing under the workspace root may contain the token (no secret at
	// rest on the shared volume: .git/config, credential files, anything).
	tokenBytes := []byte(testToken)
	err := filepath.WalkDir(fx.Engine.Root(), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		content, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		if bytes.Contains(content, tokenBytes) {
			t.Errorf("token found at rest in %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk workspace root: %v", err)
	}

	// And explicitly: the mirror's config holds no credential/askpass state.
	config, err := os.ReadFile(filepath.Join(mirrorGitDir(t, fx), "config"))
	if err != nil {
		t.Fatalf("read mirror config: %v", err)
	}
	for _, banned := range []string{"askpass", "credential", "extraheader", "x-access-token"} {
		if strings.Contains(strings.ToLower(string(config)), banned) {
			t.Fatalf("mirror config contains %q:\n%s", banned, config)
		}
	}
}

// TestNilCredentialSkipsAskpass: file:// origins run without any credential
// plumbing at all.
func TestNilCredentialSkipsAskpass(t *testing.T) {
	fx := workspacetest.New(t, seedFiles())
	rec := recordCommands(t, fx.Engine)
	mustHead(t, fx, "")
	for _, c := range rec.all() {
		for _, kv := range c.Env {
			if strings.HasPrefix(kv, "GIT_ASKPASS=") || strings.HasPrefix(kv, "GITFS_TOKEN=") {
				t.Fatalf("nil credential still injected %q on %v", kv, c.Args)
			}
		}
	}
}

// TestForcedConfigIsNotACredentialChannel guards the config the engine forces
// into EVERY git child's environment. It is a tempting place to put
// http.extraheader or a credential helper, and doing so would broadcast a
// secret to every invocation and every mirror at once — the exact opposite of
// the askpass design, where the token reaches only remote ops and only via
// GITFS_TOKEN. The same banned words the mirror's config is held to apply
// here, checked on the key AND the value: a rule may configure git's
// behaviour, never its credentials.
func TestForcedConfigIsNotACredentialChannel(t *testing.T) {
	rules := gitfs.ForcedConfigRules()
	if len(rules) == 0 {
		t.Fatal("no forced config rules — this test would prove nothing")
	}
	for _, rule := range rules {
		for _, field := range rule {
			for _, banned := range []string{"askpass", "credential", "extraheader", "x-access-token", "authorization", "token", "password"} {
				if strings.Contains(strings.ToLower(field), banned) {
					t.Errorf("forced config rule %v contains %q — credentials must travel via GITFS_TOKEN + the askpass shim, not the forced config every child inherits", rule, banned)
				}
			}
		}
	}
}
