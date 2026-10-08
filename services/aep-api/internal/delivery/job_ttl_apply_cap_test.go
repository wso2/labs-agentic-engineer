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
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/config"
)

// The longest CODING_AGENT_JOB_TTL boot accepts, plus OpenChoreo's worst
// re-create lag on Cloud, must fit the apply cap.
func TestMaxCodingAgentJobTTL_FitsTheApplyCap(t *testing.T) {
	const worstApplyLag = 13 * time.Minute
	if got := config.MaxCodingAgentJobTTL + worstApplyLag; got > CycleApplyCap {
		t.Fatalf("MaxCodingAgentJobTTL + %v = %v, past CycleApplyCap %v", worstApplyLag, got, CycleApplyCap)
	}
}
