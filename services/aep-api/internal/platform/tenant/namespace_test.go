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

package tenant

import "testing"

func TestOrgBaseNamespace_Deterministic(t *testing.T) {
	a := OrgBaseNamespace("d3adbeef-1234-4321-abcd-c0ffee123456")
	b := OrgBaseNamespace("d3adbeef-1234-4321-abcd-c0ffee123456")
	if a != b {
		t.Fatalf("non-deterministic: %s vs %s", a, b)
	}
}

func TestOrgBaseNamespace_Format(t *testing.T) {
	got := OrgBaseNamespace("d3adbeef-1234-4321-abcd-c0ffee123456")
	const want = "wc-d3adbeef-5f7c983f"
	if got != want {
		t.Fatalf("got %q; want %q", got, want)
	}
}

func TestOrgBaseNamespace_ShortUUID(t *testing.T) {
	got := OrgBaseNamespace("short")
	if got == "" || len(got) > 63 {
		t.Fatalf("unexpected NS shape: %q (len %d)", got, len(got))
	}
}
