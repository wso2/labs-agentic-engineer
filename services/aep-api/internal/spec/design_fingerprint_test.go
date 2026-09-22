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

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// The prototype staleness check is "did the design move", so what the design
// fingerprint must catch and what it must ignore are its whole specification.
func TestDesignFingerprint(t *testing.T) {
	t.Parallel()

	base := []sourcecontrol.Entry{
		entry("specs/design/design.cell", "cell"),
		entry("specs/design/security.json", "sec"),
		entry("specs/design/components/web/design.json", "web"),
		entry("specs/design/components/web/prototype.json", "proto1"),
		entry("specs/requirements/prd.md", "prd"),
	}
	with := func(path, sha string) []sourcecontrol.Entry {
		out := make([]sourcecontrol.Entry, 0, len(base))
		for _, e := range base {
			if e.Path == path {
				e.SHA = sha
			}
			out = append(out, e)
		}
		return out
	}

	t.Run("a design file change moves it", func(t *testing.T) {
		t.Parallel()
		if DesignFingerprint(base) == DesignFingerprint(with("specs/design/components/web/design.json", "web2")) {
			t.Fatal("an edited design.json left the design fingerprint unchanged")
		}
		if DesignFingerprint(base) == DesignFingerprint(with("specs/design/security.json", "sec2")) {
			t.Fatal("an edited security.json left the design fingerprint unchanged")
		}
	})

	// A feedback turn rewrites the prototype alone; that must never read as the
	// design having moved under it.
	t.Run("a prototype-only rewrite does not move it", func(t *testing.T) {
		t.Parallel()
		if DesignFingerprint(base) != DesignFingerprint(with("specs/design/components/web/prototype.json", "proto2")) {
			t.Fatal("a prototype rewrite moved the design fingerprint")
		}
		added := append(append([]sourcecontrol.Entry{}, base...),
			entry("specs/design/components/admin/prototype.json", "proto3"))
		if DesignFingerprint(base) != DesignFingerprint(added) {
			t.Fatal("a new prototype moved the design fingerprint")
		}
	})

	t.Run("requirements are not the design", func(t *testing.T) {
		t.Parallel()
		if DesignFingerprint(base) != DesignFingerprint(with("specs/requirements/prd.md", "prd2")) {
			t.Fatal("a requirements edit moved the design fingerprint")
		}
	})

	// Only the slot rule is excluded: a prototype.json anywhere else is an
	// ordinary design file.
	t.Run("a prototype.json outside a component slot still counts", func(t *testing.T) {
		t.Parallel()
		stray := append(append([]sourcecontrol.Entry{}, base...),
			entry("specs/design/prototype.json", "stray"))
		if DesignFingerprint(base) == DesignFingerprint(stray) {
			t.Fatal("a non-slot prototype.json was excluded from the design fingerprint")
		}
	})

	t.Run("order does not matter", func(t *testing.T) {
		t.Parallel()
		reversed := make([]sourcecontrol.Entry, len(base))
		for i, e := range base {
			reversed[len(base)-1-i] = e
		}
		if DesignFingerprint(base) != DesignFingerprint(reversed) {
			t.Fatal("listing order changed the design fingerprint")
		}
	})
}

func TestHasPrototype(t *testing.T) {
	t.Parallel()
	if hasPrototype([]sourcecontrol.Entry{entry("specs/design/components/web/design.json", "a")}) {
		t.Fatal("a design with no prototype reported one")
	}
	if !hasPrototype([]sourcecontrol.Entry{entry("specs/design/components/web/prototype.json", "a")}) {
		t.Fatal("a component prototype was not seen")
	}
	if hasPrototype([]sourcecontrol.Entry{entry("specs/design/prototype.json", "a")}) {
		t.Fatal("a non-slot prototype.json counted as a component prototype")
	}
}
