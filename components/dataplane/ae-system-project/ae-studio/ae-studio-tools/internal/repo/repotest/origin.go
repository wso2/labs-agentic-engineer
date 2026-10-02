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

// Package repotest is a real file:// git origin for tests of packages that
// read through the repo engine (files, edge). It commits with genuine git
// plumbing in a hermetic environment, so no user or system config leaks in.
package repotest

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Branch is the origin's default branch.
const Branch = "main"

// Origin is a bare repository in t.TempDir() serving as a file:// origin.
type Origin struct {
	dir string
}

// NewOrigin creates a bare origin whose first commit on Branch holds files
// (repo-relative path → content).
func NewOrigin(t *testing.T, files map[string]string) *Origin {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "origin.git")
	if _, err := run(nil, nil, "init", "--bare", "-b", Branch, dir); err != nil {
		t.Fatalf("origin: init: %v", err)
	}
	o := &Origin{dir: dir}
	// A fixture never needs maintenance, and a detached gc outliving the test
	// would fail t.TempDir's cleanup.
	o.git(t, nil, nil, "config", "gc.auto", "0")
	o.git(t, nil, nil, "config", "maintenance.auto", "false")
	o.Commit(t, files)
	return o
}

// URL is the origin's file:// clone URL.
func (o *Origin) URL() string { return "file://" + o.dir }

// Commit layers files over the Branch tip (if any) as one new commit, moves
// Branch to it and returns its sha.
func (o *Origin) Commit(t *testing.T, files map[string]string) string {
	t.Helper()
	parent, err := o.exec(nil, nil, "rev-parse", "--verify", "--quiet", "refs/heads/"+Branch)
	if err != nil {
		parent = ""
	}
	idx := map[string]string{"GIT_INDEX_FILE": filepath.Join(t.TempDir(), "index")}
	if parent != "" {
		o.git(t, idx, nil, "read-tree", parent)
	}
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		blob := o.git(t, nil, []byte(files[p]), "hash-object", "-w", "--stdin")
		o.git(t, idx, nil, "update-index", "--add", "--cacheinfo", "100644,"+blob+","+p)
	}
	tree := o.git(t, idx, nil, "write-tree")
	args := []string{"commit-tree", tree, "-m", "commit"}
	if parent != "" {
		args = append(args, "-p", parent)
	}
	sha := o.git(t, nil, nil, args...)
	o.git(t, nil, nil, "update-ref", "refs/heads/"+Branch, sha)
	return sha
}

// git runs a command against the origin and fails the test on error.
func (o *Origin) git(t *testing.T, env map[string]string, stdin []byte, args ...string) string {
	t.Helper()
	out, err := o.exec(env, stdin, args...)
	if err != nil {
		t.Fatalf("origin: %v", err)
	}
	return out
}

func (o *Origin) exec(env map[string]string, stdin []byte, args ...string) (string, error) {
	full := map[string]string{"GIT_DIR": o.dir}
	for k, v := range env {
		full[k] = v
	}
	return run(full, stdin, args...)
}

// run executes git hermetically (no user/system config, fixed identity) with
// env overlaid, returning trimmed stdout.
func run(env map[string]string, stdin []byte, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.TempDir(),
		"GIT_CONFIG_GLOBAL=" + os.DevNull,
		"GIT_CONFIG_SYSTEM=" + os.DevNull,
		"GIT_TERMINAL_PROMPT=0",
		"LC_ALL=C",
		"GIT_AUTHOR_NAME=repotest", "GIT_AUTHOR_EMAIL=repotest@aep.test",
		"GIT_COMMITTER_NAME=repotest", "GIT_COMMITTER_EMAIL=repotest@aep.test",
	}
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", &gitError{args: args, err: err, stderr: strings.TrimSpace(stderr.String())}
	}
	return strings.TrimSpace(stdout.String()), nil
}

type gitError struct {
	args   []string
	err    error
	stderr string
}

func (e *gitError) Error() string {
	return "git " + strings.Join(e.args, " ") + ": " + e.err.Error() + ": " + e.stderr
}
