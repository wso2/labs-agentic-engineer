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

package delivery

import "testing"

// THE DEDUPE-KEY RATCHET.
//
// Every key the delivery domain mints with is frozen here against its literal.
// The literals are deliberately copied out rather than computed: a test that
// re-derived them from the same fmt template would agree with any edit, which
// is the one thing this must not do.
//
// It exists because a changed key is the quietest regression this domain can
// have. The host turns a key into a `dedupe:<key>` label and files nothing when
// an OPEN issue already carries it, so editing a template does not fail, log or
// error — the platform simply starts filing a SECOND issue every time a webhook
// is redelivered or an activity is retried, and the milestone fills with
// duplicates of work that is already open. No test that reads titles, bodies or
// labels would notice.
//
// If a key here genuinely has to change, change the literal too — and know that
// every issue already open under the old key stops deduping the moment you do.
func TestIssueDedupeKeysAreFrozen(t *testing.T) {
	const (
		component = "order-service"
		shortSHA  = "abc123def456" // ShortSHA's 12 chars
	)
	for _, tc := range []struct {
		name string
		got  string
		want string
	}{
		// eventcore: a build that stayed red through its automatic re-trigger.
		{"fix", DedupeKeyFix(component, shortSHA), "aep fix order-service abc123def456"},
		// eventcore: built fine, never became ready. Reached from the supervisor
		// through a port, because a deployment produces no webhook.
		{"deploy", DedupeKeyDeploy(component, shortSHA), "aep deploy order-service abc123def456"},
		// eventcore: a pull request that would not merge. Keyed to the PR, so
		// every synchronize on that branch finds the same issue.
		{"conflict", DedupeKeyConflict(42), "aep conflict pr-42"},
		// eventcore: main went red outside any run — the deployed version
		// regressing.
		{"red main", DedupeKeyRedMain(component, shortSHA), "aep red-main order-service abc123def456"},
		// eventcore: the wiring-conformance halves. The missing set is part of
		// each key, and the two prefixes differ so neither swallows the other.
		{"unwired resources",
			DedupeKeyUnwiredResources(component, []string{"order-db", "order-cache"}),
			"aep unwired order-service order-db,order-cache"},
		{"unwired endpoints",
			DedupeKeyUnwiredEndpoints(component, []string{"todo-api99-todo-api"}),
			"aep unwired-endpoints order-service todo-api99-todo-api"},
		// validation: one repair issue per failed criterion, keyed to the ATTEMPT
		// so the next attempt's failures are fresh work.
		{"validation fix",
			DedupeKeyValidationFix("AC-001-a", "cycle-abc"),
			"aep validation-fix AC-001-a cycle-abc"},
		// validation: the version's own validation issue. Colon-delimited, unlike
		// every other key — it mirrors the provision gate's `gate:<project>:…`.
		{"validation issue",
			DedupeKeyValidationIssue("proj", 5),
			"validation:proj:5"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("dedupe key = %q; want %q\n"+
					"A changed key does not fail — it silently RE-FILES issues instead of "+
					"deduping onto the open one. Change the literal only if that is the intent.",
					tc.got, tc.want)
			}
		})
	}
}

// The keys of the two conformance halves must never collide for one component:
// they are independent defects, fixed in different blocks of the same file, and
// a shared key would let whichever was detected first swallow the other.
func TestIssueDedupeKeys_ConformanceHalvesNeverCollide(t *testing.T) {
	missing := []string{"order-db"}
	if DedupeKeyUnwiredResources("svc", missing) == DedupeKeyUnwiredEndpoints("svc", missing) {
		t.Error("the resource and endpoint halves share a dedupe key — one would swallow the other")
	}
}

// Scoping is the other half of the contract, and it is per-key on purpose. A
// key that dropped one of these dimensions would suppress genuinely new work:
// the next version's build failure, the next attempt's repair, the next
// version's validation issue.
func TestIssueDedupeKeys_ScopeTheirWork(t *testing.T) {
	if DedupeKeyFix("svc", "aaaaaaaaaaaa") == DedupeKeyFix("svc", "bbbbbbbbbbbb") {
		t.Error("a fix key ignores the commit — the next version's failure would dedupe onto the last")
	}
	if DedupeKeyValidationFix("AC-1", "cycle-1") == DedupeKeyValidationFix("AC-1", "cycle-2") {
		t.Error("a repair key ignores the attempt — a criterion failing again would file nothing")
	}
	if DedupeKeyValidationIssue("proj", 5) == DedupeKeyValidationIssue("proj", 6) {
		t.Error("a validation key ignores the version — v6 would dedupe onto v5's oracle")
	}
}
