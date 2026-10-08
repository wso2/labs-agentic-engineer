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

import (
	"reflect"
	"testing"
)

func TestServesStoriesStamp_RoundTrip(t *testing.T) {
	body := "## Scope\nDo the thing.\n"
	stamped := StampServesStories(body, []string{"F1.1", "F1.2", "F2.1"})
	if got := ParseServesStories(stamped); !reflect.DeepEqual(got, []string{"F1.1", "F1.2", "F2.1"}) {
		t.Fatalf("parse(stamp) = %v", got)
	}
	// Restamping replaces, never duplicates.
	restamped := StampServesStories(stamped, []string{"F3.2"})
	if got := ParseServesStories(restamped); !reflect.DeepEqual(got, []string{"F3.2"}) {
		t.Fatalf("parse(restamp) = %v", got)
	}
	if n := len(servesStoriesLinePattern.FindAllString(restamped, -1)); n != 1 {
		t.Fatalf("stamp appears %d times, want 1", n)
	}
	// Empty stories strips the stamp.
	if got := ParseServesStories(StampServesStories(stamped, nil)); got != nil {
		t.Fatalf("strip left a stamp: %v", got)
	}
	// No stamp → nil.
	if got := ParseServesStories(body); got != nil {
		t.Fatalf("unstamped body parsed as %v", got)
	}
}

// A stamp reads back in ID order (F2.10 after F2.9), and a token that is not a
// story ID — an old numeric stamp, a stray word — is skipped.
func TestParseServesStories_OrdersIDsAndSkipsOthers(t *testing.T) {
	body := "Do it.\n\n**Serves stories:** F2.10, F2.9, 7, soon, F1.1\n"
	if got := ParseServesStories(body); !reflect.DeepEqual(got, []string{"F1.1", "F2.9", "F2.10"}) {
		t.Fatalf("parse = %v", got)
	}
}
