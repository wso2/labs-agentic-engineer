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

package aestudio

import (
	"os"
	"testing"
)

// TestVendoredResourceTypeMatchesSource guards the go:embed copy, as
// designspec/schema_vendor_test.go does for its schemas.
func TestVendoredResourceTypeMatchesSource(t *testing.T) {
	// aestudio → organization → internal → aep-api → services → repo root
	src, err := os.ReadFile("../../../../../components/dataplane/ae-system-project/ae-studio/resourcetype.yaml")
	if err != nil {
		t.Fatalf("read source RT: %v", err)
	}
	got, err := os.ReadFile("resourcetype.yaml")
	if err != nil {
		t.Fatalf("read vendored RT: %v", err)
	}
	if string(got) != string(src) {
		t.Fatal("vendored resourcetype.yaml differs from components/…/ae-studio/resourcetype.yaml — run `go generate ./internal/organization/aestudio/`")
	}
}
