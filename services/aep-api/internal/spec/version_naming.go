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
	"errors"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// A VERSION IS A NAMED SNAPSHOT (console ADR-0030).
//
// `v<N>` used to be the version's identity: the platform found the newest one
// by taking the maximum of `^v(\d+)$` over the tags, and the same scan decided
// whether HEAD's specs/ tree still matched it. The user could not say what a
// version was for, and `v7` is a serial.
//
// So the name became theirs, and the two jobs the number was doing split:
//
//   - IDENTITY is the tag name itself, whatever the user typed.
//   - ORDER is the tag's creation time (TagInfo.CreatedAt) — which also
//     separates two versions that name the same commit, as a rebuild-with-a-
//     new-name would. No number is parsed to order anything.
//
// What still needs answering is WHICH tags are versions, now that a version
// can be called `payments-v2`. The answer is the tag's own message: every spec
// tag this platform cuts carries the subject `Spec <name>`, so a repo's other
// tags (a release tag, the legacy `v<N>-<M>` design tags, anything a person
// pushed) are not mistaken for versions.

// specTagSubject is the first line of every spec tag's annotation. It is the
// marker that makes a tag a VERSION, so it is written on create and matched on
// read — never inferred from the name, which is the user's.
const specTagSubject = "Spec "

// legacyTagSubject is the subject the retired per-artifact save wrote on a
// requirements tag. Those tags ARE this platform's versions, cut before a
// version could be named, so they are recognised alongside the current marker.
const legacyTagSubject = "Requirements "

// legacyVersionName is the last resort, and it is deliberately narrow: it
// applies ONLY to a tag carrying no annotation at all. An annotated tag says
// what it is, and a repo's own release tag may well be called `v1` — reading
// that as the newest version would hand the change list the wrong baseline and
// tell a build the spec had moved when it had not.
var legacyVersionName = regexp.MustCompile(`^v(\d+)$`)

// versionNamePattern is the shape a version name may take — the same subset the
// console's field enforces, and a deliberate narrowing of what git itself would
// accept. It keeps a name safe in a ref, in a URL path segment, and readable
// back.
var versionNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// ErrVersionNameInvalid is a name a version may not carry.
var ErrVersionNameInvalid = errors.New("invalid version name")

// ErrVersionNameTaken is a name some version already carries. It is terminal:
// a supplied name is never quietly turned into a different one.
var ErrVersionNameTaken = errors.New("version name already exists")

// ValidateVersionName reports why a name cannot be a version's, or nil.
func ValidateVersionName(name string) error {
	switch {
	case name == "":
		return errors.New("name the version")
	case !versionNamePattern.MatchString(name):
		return errors.New("use letters, digits, dot, dash and underscore")
	case strings.HasPrefix(name, "-"), strings.HasPrefix(name, "."):
		return errors.New("start the name with a letter or a digit")
	case strings.HasSuffix(name, "."), strings.HasSuffix(name, ".lock"):
		return errors.New("do not end the name with a dot or with .lock")
	case strings.Contains(name, ".."):
		return errors.New("do not use two dots in a row")
	}
	return nil
}

// isVersionTag reports whether a tag is one of this platform's spec versions.
//
// The annotation decides it. A name only decides when there is no annotation to
// read — a lightweight tag from an old mirror — because any repo may hold a
// release tag called `v1`, and mistaking one for a version is not a cosmetic
// error: it becomes the baseline the change list and the rebuild check are
// computed against.
func isVersionTag(t sourcecontrol.TagInfo) bool {
	if t.Message != "" {
		return strings.HasPrefix(t.Message, specTagSubject) ||
			strings.HasPrefix(t.Message, legacyTagSubject)
	}
	return legacyVersionName.MatchString(t.Name)
}

// versionTags returns the project's versions, NEWEST FIRST by creation time.
// Ties break on the name, descending, so the order is total and stable — two
// tags cut in the same second must not swap places between two reads.
func versionTags(tags []sourcecontrol.TagInfo) []sourcecontrol.TagInfo {
	out := make([]sourcecontrol.TagInfo, 0, len(tags))
	for _, t := range tags {
		if isVersionTag(t) {
			out = append(out, t)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].Name > out[j].Name
	})
	return out
}

// latestVersionTag returns the newest version, or ok=false when the project has
// never been built.
func latestVersionTag(tags []sourcecontrol.TagInfo) (sourcecontrol.TagInfo, bool) {
	versions := versionTags(tags)
	if len(versions) == 0 {
		return sourcecontrol.TagInfo{}, false
	}
	return versions[0], true
}

// suggestedVersionName is what the build dialog prefills: `v<count + 1>`,
// incremented until the name is free.
//
// The count, not the highest number, because the suggestion reads as "my fourth
// version" rather than as a continuation of a sequence the names no longer
// carry. Counting can land on a name that exists (a version somebody called
// `v4` by hand), so it steps past every taken name — including tags that are
// not versions at all, since git will refuse those just the same.
func suggestedVersionName(tags []sourcecontrol.TagInfo) string {
	taken := make(map[string]bool, len(tags))
	for _, t := range tags {
		taken[t.Name] = true
	}
	for n := len(versionTags(tags)) + 1; ; n++ {
		name := "v" + strconv.Itoa(n)
		if !taken[name] {
			return name
		}
	}
}
