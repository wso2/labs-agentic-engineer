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

package app

import (
	"context"
	"errors"
	"testing"

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/delivery/task"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol/issues"
)

type refusingAdopter struct{ err error }

func (a refusingAdopter) AdoptIssue(context.Context, string, string, int) error { return a.err }

// The event plane's "no deployed version" reaches the issue agent as the
// issues package's own sentinel; any other failure passes through as is.
func TestIssueAgentPromoter_TranslatesNoDeployedVersion(t *testing.T) {
	promote := func(err error) error {
		return issueAgentPromoter{commands: task.NewCommands(nil, refusingAdopter{err: err})}.
			PromoteAndExecute(context.Background(), "acme", "expenses", "api", 7)
	}
	if err := promote(delivery.ErrNoDeployedMilestone); !errors.Is(err, issues.ErrNoDeployedVersion) {
		t.Errorf("no deployed milestone: err = %v, want issues.ErrNoDeployedVersion", err)
	}
	other := errors.New("github down")
	if err := promote(other); !errors.Is(err, other) || errors.Is(err, issues.ErrNoDeployedVersion) {
		t.Errorf("other failure: err = %v, want it passed through", err)
	}
	if err := promote(nil); err != nil {
		t.Errorf("success: err = %v", err)
	}
}
