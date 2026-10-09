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

package spec

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools/aestudiotest"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// The idea is free text a user typed: newlines, straight quotes, apostrophes
// and backslashes all have to survive a write→read cycle intact. This is the
// whole reason the descriptor uses a real TOML encoder instead of hand-rolled
// key writing.
func TestDescriptorRoundTripsAwkwardIdeaText(t *testing.T) {
	t.Parallel()
	idea := "A \"claims\" tracker for Ops.\nDon't lose receipts — path C:\\temp matters.\n\n[not-a-section]\nidea = \"not a key\""
	in := Descriptor{
		APIVersion: DescriptorAPIVersion,
		Name:       "expense-tracker",
		CreatedAt:  "2026-07-29T10:14:00Z",
		Idea:       idea,
	}

	raw, err := MarshalDescriptor(in)
	if err != nil {
		t.Fatalf("MarshalDescriptor: %v", err)
	}
	// The design agent reads the file; any TOML decoder must get it back.
	var got Descriptor
	if _, err := toml.Decode(string(raw), &got); err != nil {
		t.Fatalf("decode %q: %v", raw, err)
	}
	if got.Idea != idea {
		t.Fatalf("idea round-trip:\n got %q\nwant %q\nencoded as:\n%s", got.Idea, idea, raw)
	}
	if got.Name != in.Name || got.APIVersion != in.APIVersion || got.CreatedAt != in.CreatedAt {
		t.Fatalf("identity round-trip = %+v, want %+v", got, in)
	}
}

// NewDescriptor stamps the current apiVersion so callers cannot forget it —
// the field is what identifies the file as an Agentic Engineer descriptor.
func TestNewDescriptorStampsAPIVersion(t *testing.T) {
	t.Parallel()
	d := NewDescriptor("expense-tracker", "an expense tracker", "2026-07-29T10:14:00Z")
	if d.APIVersion != DescriptorAPIVersion {
		t.Fatalf("apiVersion = %q, want %q", d.APIVersion, DescriptorAPIVersion)
	}
}

// descriptorRef is the project repository the descriptor tests write to.
var descriptorRef = sourcecontrol.RepoRef{Org: "default", Owner: "acme", Repo: "greeter"}

// newDescriptorRig seeds the project repository with files and answers the
// pod and a writer over it.
func newDescriptorRig(t *testing.T, files map[string]string) (*aestudiotest.Fake, *DescriptorWriter) {
	t.Helper()
	pod := aestudiotest.New()
	pod.SeedRepo(descriptorRef, files)
	w := NewDescriptorWriter(pod, memRepos(t, "default", "p", "https://github.com/acme/greeter"))
	w.now = func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) }
	return pod, w
}

func commitCalls(pod *aestudiotest.Fake) []aestudiotest.Call {
	var out []aestudiotest.Call
	for _, c := range pod.Calls() {
		if c.Op == aestudiotest.OpCommit {
			out = append(out, c)
		}
	}
	return out
}

// A fresh repository gets the descriptor and the ignore file in one commit,
// authored by the pod (no identity sent).
func TestWriteDescriptor_OneCommitOnAFreshRepo(t *testing.T) {
	ctx := context.Background()
	pod, w := newDescriptorRig(t, map[string]string{"README.md": "# hi"})

	if err := w.WriteDescriptor(ctx, "default", "p", "greeter", "a lunch app"); err != nil {
		t.Fatalf("WriteDescriptor: %v", err)
	}
	commits := commitCalls(pod)
	if len(commits) != 1 || commits[0].Author != nil || commits[0].Committer != nil {
		t.Fatalf("commits = %+v, want one with no identity", commits)
	}
	files, _, _ := pod.ReadBundle(ctx, descriptorRef, "", sourcecontrol.BundleFilter{Paths: []string{DescriptorPath, SpecIgnorePath}})
	if !strings.Contains(files[DescriptorPath], `idea = "a lunch app"`) || files[SpecIgnorePath] != SpecIgnoreContent {
		t.Fatalf("tip = %v, want the descriptor and the ignore file", files)
	}
}

// A writer that moves one of the paths between the read and the commit
// makes the commit conflict; the writer re-reads and lands on the retry.
func TestWriteDescriptor_RetriesAfterAConflict(t *testing.T) {
	ctx := context.Background()
	pod, w := newDescriptorRig(t, map[string]string{})
	raced := false
	pod.BeforeCommit(func() {
		if raced {
			return
		}
		raced = true
		_, _ = pod.Commit(ctx, descriptorRef, sourcecontrol.CommitRequest{Message: "theirs", Writes: []sourcecontrol.FileWrite{{Path: SpecIgnorePath, Content: "theirs\n"}}})
	})

	if err := w.WriteDescriptor(ctx, "default", "p", "greeter", "idea"); err != nil {
		t.Fatalf("WriteDescriptor: %v", err)
	}
	if n := len(commitCalls(pod)); n != 3 { // ours (conflict), the racer's, ours again
		t.Fatalf("commits = %d, want 3", n)
	}
	got, _, _ := pod.ReadFile(ctx, descriptorRef, "", SpecIgnorePath)
	if string(got) != SpecIgnoreContent {
		t.Fatalf("ignore file = %q, want ours after the retry", got)
	}
}

// A path that keeps moving is a conflict after CommitAttempts tries.
func TestWriteDescriptor_GivesUpAfterTheLastAttempt(t *testing.T) {
	pod, w := newDescriptorRig(t, map[string]string{})
	pod.FailOp(aestudiotest.OpCommit, &sourcecontrol.CommitConflictError{Conflicts: []sourcecontrol.Conflict{{Path: DescriptorPath}}})

	err := w.WriteDescriptor(context.Background(), "default", "p", "greeter", "idea")
	if !errors.Is(err, sourcecontrol.ErrCommitConflict) {
		t.Fatalf("err = %v, want ErrCommitConflict", err)
	}
	if n := len(commitCalls(pod)); n != sourcecontrol.CommitAttempts {
		t.Fatalf("commits = %d, want %d", n, sourcecontrol.CommitAttempts)
	}
}

// A transient failure is the caller's at once, with no retry.
func TestWriteDescriptor_TransientFailureIsTheCallers(t *testing.T) {
	pod, w := newDescriptorRig(t, map[string]string{})
	pod.FailOp(aestudiotest.OpCommit, sourcecontrol.ErrAEStudioUnavailable)

	err := w.WriteDescriptor(context.Background(), "default", "p", "greeter", "idea")
	if !errors.Is(err, sourcecontrol.ErrAEStudioUnavailable) {
		t.Fatalf("err = %v, want ErrAEStudioUnavailable", err)
	}
	if n := len(commitCalls(pod)); n != 1 {
		t.Fatalf("commits = %d, want 1", n)
	}
}
