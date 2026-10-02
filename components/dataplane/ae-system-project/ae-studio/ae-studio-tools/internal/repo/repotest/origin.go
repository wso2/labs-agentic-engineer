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

// Package repotest is the real file:// git origin for tests of the repo
// engine and of the packages that read through it (files, edge). It commits
// with genuine git plumbing in a hermetic environment (no user or system
// config, fixed identity and date, so seeded commit shas are stable).
package repotest

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Branch is the origin's default branch.
const Branch = "main"

const (
	actorName  = "repotest"
	actorEmail = "repotest@aep.test"
	// fixedDate keeps commits deterministic (git raw format); 2026-01-01.
	fixedDate = "1767225600 +0000"
)

// Origin is a bare repository in t.TempDir() serving as a file:// origin. Its
// plumbing is safe for concurrent use: object writes are content-addressed,
// ref updates take lockfiles, and every commit uses its own index file.
type Origin struct {
	dir string
}

// NewOrigin creates a bare origin whose first commit on Branch holds files
// (repo-relative path → content; nil for an empty tree).
func NewOrigin(t *testing.T, files map[string]string) *Origin {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "origin.git")
	if _, err := run(nil, nil, "init", "--bare", "-b", Branch, dir); err != nil {
		t.Fatalf("origin: init: %v", err)
	}
	o := &Origin{dir: dir}
	// Every push into the origin makes receive-pack spawn a detached
	// `gc --auto`, which can outlive the test and fail t.TempDir's cleanup.
	// A fixture never needs maintenance, so switch both paths off.
	o.Git(t, "config", "gc.auto", "0")
	o.Git(t, "config", "maintenance.auto", "false")
	o.Commit(t, files, "seed")
	return o
}

// URL is the origin's file:// clone URL.
func (o *Origin) URL() string { return "file://" + o.dir }

// Dir is the origin's GIT_DIR.
func (o *Origin) Dir() string { return o.dir }

// Commit layers files over the Branch tip (if any) as one commit with msg,
// moves Branch to it and returns its sha.
func (o *Origin) Commit(t *testing.T, files map[string]string, msg string) string {
	t.Helper()
	parent, err := o.exec(nil, nil, "rev-parse", "--verify", "--quiet", "refs/heads/"+Branch)
	if err != nil {
		parent = ""
	}
	parent = strings.TrimSpace(parent)
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
	args := []string{"commit-tree", tree, "-m", msg}
	if parent != "" {
		args = append(args, "-p", parent)
	}
	sha := o.git(t, nil, nil, args...)
	o.git(t, nil, nil, "update-ref", "refs/heads/"+Branch, sha)
	return sha
}

// HeadSHA is the Branch tip.
func (o *Origin) HeadSHA(t *testing.T) string {
	t.Helper()
	return o.Git(t, "rev-parse", "refs/heads/"+Branch)
}

// Tag creates an annotated tag with msg on the Branch tip.
func (o *Origin) Tag(t *testing.T, name, msg string) {
	t.Helper()
	o.Git(t, "tag", "-a", name, o.HeadSHA(t), "-m", msg)
}

// FileAt is the exact content of ref:path, untrimmed.
func (o *Origin) FileAt(t *testing.T, ref, path string) string {
	t.Helper()
	out, err := o.exec(nil, nil, "cat-file", "blob", ref+":"+path)
	if err != nil {
		t.Fatalf("origin: %v", err)
	}
	return out
}

// Git runs any git command against the origin and returns its trimmed
// stdout, failing the test on error.
func (o *Origin) Git(t *testing.T, args ...string) string {
	t.Helper()
	return o.git(t, nil, nil, args...)
}

func (o *Origin) git(t *testing.T, env map[string]string, stdin []byte, args ...string) string {
	t.Helper()
	out, err := o.exec(env, stdin, args...)
	if err != nil {
		t.Fatalf("origin: %v", err)
	}
	return strings.TrimSpace(out)
}

func (o *Origin) exec(env map[string]string, stdin []byte, args ...string) (string, error) {
	full := map[string]string{"GIT_DIR": o.dir}
	for k, v := range env {
		full[k] = v
	}
	return run(full, stdin, args...)
}

// run executes git hermetically with env overlaid and returns raw stdout.
func run(env map[string]string, stdin []byte, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.TempDir(),
		"GIT_CONFIG_GLOBAL=" + os.DevNull,
		"GIT_CONFIG_SYSTEM=" + os.DevNull,
		"GIT_TERMINAL_PROMPT=0",
		"LC_ALL=C",
		"GIT_AUTHOR_NAME=" + actorName, "GIT_AUTHOR_EMAIL=" + actorEmail, "GIT_AUTHOR_DATE=" + fixedDate,
		"GIT_COMMITTER_NAME=" + actorName, "GIT_COMMITTER_EMAIL=" + actorEmail, "GIT_COMMITTER_DATE=" + fixedDate,
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
		return stdout.String(), fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}
