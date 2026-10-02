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

package repo

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Credential injection (design D7/§8): a fresh token is minted in-process
// immediately before every remote op via the engine's Credential and handed to
// git through a static GIT_ASKPASS shim reading the child's environment. The
// token is NEVER in argv (invisible to ps), NEVER written to any .git/config
// (no secret at rest on the shared RWO volume), and no credential helper is
// configured. A mid-op auth failure re-mints once and retries that op.

// askpassFile is the shim's name under <root>/tmp.
const askpassFile = "askpass.sh"

// tokenEnvVar carries the freshly minted token into the git child process,
// where only the shim reads it.
const tokenEnvVar = "REPO_TOKEN"

// askpassScript answers git's credential prompts: the username prompt gets
// the fixed GitHub token username, everything else (the password prompt)
// gets the token from the environment.
const askpassScript = `#!/bin/sh
# repo credential shim — answers git's GIT_ASKPASS prompts from the child
# environment so the token never appears in argv or on-disk git config.
case "$1" in
*sername*) echo x-access-token ;;
*) printf %s "$REPO_TOKEN" ;;
esac
`

// writeAskpassShim writes the static shim (0700) into <root>/tmp at engine
// init and returns its path. Re-writing an identical shim is harmless, so
// concurrent engines on one root need no coordination here.
func writeAskpassShim(root string) (string, error) {
	path := filepath.Join(TmpDir(root), askpassFile)
	if err := os.WriteFile(path, []byte(askpassScript), 0o700); err != nil {
		return "", fmt.Errorf("repo: write askpass shim: %w", err)
	}
	return path, nil
}

// credEnv mints a token for ref and returns the env overlay for one
// remote git op. A nil credential (file:// origins in tests) yields nil —
// askpass injection is skipped entirely.
func (e *Engine) credEnv(ctx context.Context, ref RepoRef) (map[string]string, error) {
	if e.cred == nil {
		return nil, nil
	}
	token, err := e.cred.Token(ctx)
	if err != nil {
		return nil, fmt.Errorf("repo: mint token for %s: %w", ref.RepoSlug, err)
	}
	return map[string]string{
		"GIT_ASKPASS": e.askpass,
		tokenEnvVar:   token,
	}, nil
}

// authFailurePatterns are the stderr shapes of a rejected credential across
// git's http transport. Matched case-insensitively.
var authFailurePatterns = []string{
	"authentication failed",
	"invalid username or password",
	"the requested url returned error: 401",
	"the requested url returned error: 403",
}

// isAuthFailure reports whether a remote-op error looks like a credential
// rejection (token expired/revoked mid-op) — the re-mint-once trigger.
func isAuthFailure(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, p := range authFailurePatterns {
		if strings.Contains(msg, p) {
			return true
		}
	}
	return false
}
