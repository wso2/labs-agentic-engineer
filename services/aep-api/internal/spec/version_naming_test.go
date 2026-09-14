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
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

func at(min int) time.Time {
	return time.Date(2026, 9, 1, 10, min, 0, 0, time.UTC)
}

// version builds a tag as the platform cuts one: the `Spec <name>` subject is
// what makes it a version, whatever it is called.
func version(name string, created time.Time) sourcecontrol.TagInfo {
	return sourcecontrol.TagInfo{
		Name:       name,
		CommitHash: "sha-" + name,
		Message:    specTagSubject + name,
		CreatedAt:  created,
	}
}

func TestVersionTagsOrderByCreation(t *testing.T) {
	// Named out of any numeric order, on purpose: the name carries no sequence.
	tags := []sourcecontrol.TagInfo{
		version("v1", at(1)),
		version("payments-v2", at(30)),
		version("v9", at(10)),
	}

	got := versionTags(tags)

	if len(got) != 3 {
		t.Fatalf("versionTags = %d tags, want 3", len(got))
	}
	if got[0].Name != "payments-v2" || got[1].Name != "v9" || got[2].Name != "v1" {
		t.Fatalf("order = %q/%q/%q, want newest-first payments-v2/v9/v1 — order is creation time, never the number",
			got[0].Name, got[1].Name, got[2].Name)
	}
	latest, ok := latestVersionTag(tags)
	if !ok || latest.Name != "payments-v2" {
		t.Fatalf("latestVersionTag = %q (%v), want payments-v2", latest.Name, ok)
	}
}

func TestVersionTagsIgnoreTagsThisPlatformDidNotCut(t *testing.T) {
	tags := []sourcecontrol.TagInfo{
		version("v1", at(1)),
		// A release tag somebody pushed, newer than every version. Naming a
		// version freely means the NAME can no longer select them, so the
		// annotation subject does — and this one is not a version.
		{Name: "release-2026-09", CommitHash: "sha-r", Message: "ship it", CreatedAt: at(50)},
		// The legacy per-artifact design tag: its own subject, never a version.
		{Name: "v1-2", CommitHash: "sha-d", Message: "Design v1-2", CreatedAt: at(40)},
	}

	got := versionTags(tags)

	if len(got) != 1 || got[0].Name != "v1" {
		t.Fatalf("versionTags = %+v, want only v1", got)
	}
}

// A tag with NO annotation is the only one a name may speak for — a lightweight
// tag from an old mirror.
func TestVersionTagsAcceptAnUnannotatedNumberedName(t *testing.T) {
	tags := []sourcecontrol.TagInfo{{Name: "v3", CommitHash: "sha3", CreatedAt: at(5)}}

	if got := versionTags(tags); len(got) != 1 {
		t.Fatalf("versionTags = %+v, want the bare v3 recognised", got)
	}
}

// The versions the retired per-artifact save cut are this platform's too.
func TestVersionTagsAcceptTheLegacyRequirementsSubject(t *testing.T) {
	tags := []sourcecontrol.TagInfo{
		{Name: "v2", CommitHash: "sha2", Message: "Requirements v2", CreatedAt: at(5)},
	}

	if got := versionTags(tags); len(got) != 1 {
		t.Fatalf("versionTags = %+v, want the legacy requirements tag recognised", got)
	}
}

// …but an ANNOTATED tag says what it is, and a repo may well call a release
// `v2`. Reading that as a version would hand the change list the wrong baseline
// and tell a build the spec had moved when it had not.
func TestVersionTagsRefuseAnAnnotatedForeignTagNamedLikeAVersion(t *testing.T) {
	tags := []sourcecontrol.TagInfo{
		version("v1", at(1)),
		{Name: "v2", CommitHash: "sha2", Message: "ship it", CreatedAt: at(50)},
	}

	got := versionTags(tags)

	if len(got) != 1 || got[0].Name != "v1" {
		t.Fatalf("versionTags = %+v, want only v1 — `v2` is somebody's release tag", got)
	}
	// It is not a version, but its NAME is still taken — git will refuse it just
	// the same — so the suggestion counts one version, offers `v2`, finds it
	// claimed, and steps past.
	if s := suggestedVersionName(tags); s != "v3" {
		t.Errorf("suggestedVersionName = %q, want v3", s)
	}
}

func TestSuggestedVersionNameCountsVersions(t *testing.T) {
	tags := []sourcecontrol.TagInfo{
		version("v1", at(1)),
		version("payments-v2", at(2)),
	}

	if got := suggestedVersionName(tags); got != "v3" {
		t.Fatalf("suggestedVersionName = %q, want v3 — two versions exist, so this is the third", got)
	}
}

func TestSuggestedVersionNameStepsPastATakenName(t *testing.T) {
	// Counting lands on a name somebody already used by hand, so the suggestion
	// walks past it rather than offering a name the cut would refuse.
	tags := []sourcecontrol.TagInfo{
		version("v1", at(1)),
		version("v3", at(2)),
	}

	if got := suggestedVersionName(tags); got != "v4" {
		t.Fatalf("suggestedVersionName = %q, want v4", got)
	}
}

func TestSuggestedVersionNameOnAFreshProject(t *testing.T) {
	if got := suggestedVersionName(nil); got != "v1" {
		t.Fatalf("suggestedVersionName = %q, want v1", got)
	}
}

func TestValidateVersionName(t *testing.T) {
	for _, name := range []string{"v3", "payments-v3", "beta_launch", "1.4.0", "hotfix"} {
		if err := ValidateVersionName(name); err != nil {
			t.Errorf("ValidateVersionName(%q) = %v, want nil", name, err)
		}
	}
	for _, name := range []string{"", "my version", "feature/x", "v3~1", "-v3", ".v3", "v3.", "v3.lock", "v3..1"} {
		if err := ValidateVersionName(name); err == nil {
			t.Errorf("ValidateVersionName(%q) = nil, want a refusal", name)
		}
	}
}
