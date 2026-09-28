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

package runread_test

import (
	"testing"

	"github.com/wso2/aep/aep-api/internal/delivery/validation"
)

// TestValidationReportPathMatchesTheRunnersOwn pins the one duplicated string in
// this package. The read surface may not import `delivery/validation` (a sibling
// slice), so it repeats the report path — and a silent drift would hand the
// console a link to a file that is not there. This external test package can
// import both, so it is where the two are held together.
//
// The literal is repeated a third time here on purpose: reading it back out of
// runread would make the test tautological.
func TestValidationReportPathMatchesTheRunnersOwn(t *testing.T) {
	const path = "tests/acceptance/report.json"
	if validation.ReportFilePath != path {
		t.Fatalf("the validation runner writes %q but the run read surface links %q",
			validation.ReportFilePath, path)
	}
}
