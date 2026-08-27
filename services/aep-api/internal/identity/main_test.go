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

package identity_test

import (
	"testing"

	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
)

// TestMain hands this package's run to the DB harness, which terminates the
// throwaway Postgres it started once the last test is done. Every package whose
// tests reach dbtest.New owns this three-line delegation — `go test ./...` gives
// each package its own process, so a package that omits it strands its own
// container. Enforced by arch's TestDBTestPackagesShutDownTheirContainer.
func TestMain(m *testing.M) { dbtest.Main(m) }
