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

package run

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/temporal"
)

type stubGate struct {
	unconfigured []string
	provisioning []string
	err          error
}

func (g stubGate) DeploymentReadiness(context.Context, string, string, string) ([]string, []string, error) {
	return g.unconfigured, g.provisioning, g.err
}

// TestCheckDeployReadiness_UnwiredFailsClosed is the one activity in this
// package that must NOT degrade to "nothing to do". Every other optional
// collaborator's worst case is work not happening; this one's worst case is a
// deploy that publishes an application with empty credentials — the exact
// outcome the gate exists to prevent. Non-retryable, because waiting does not
// wire a port.
func TestCheckDeployReadiness_UnwiredFailsClosed(t *testing.T) {
	acts := NewActivities(Deps{})

	_, err := acts.CheckDeployReadiness(context.Background(), ProjectRef{OrgID: "acme", ProjectID: "shop"})

	require.Error(t, err, "an unwired gate must refuse, never wave the deploy through")
	var appErr *temporal.ApplicationError
	require.True(t, errors.As(err, &appErr))
	require.True(t, appErr.NonRetryable(), "retrying cannot wire a missing port")
}

// TestCheckDeployReadiness_ReportsBothBlockersSeparately: the workflow treats
// the two differently — it polls one and parks on the other — so the activity
// must not collapse them into a single "not ready".
func TestCheckDeployReadiness_ReportsBothBlockersSeparately(t *testing.T) {
	acts := NewActivities(Deps{DeployGate: stubGate{
		unconfigured: []string{"stripe"},
		provisioning: []string{"postgres"},
	}})

	verdict, err := acts.CheckDeployReadiness(context.Background(), ProjectRef{OrgID: "acme", ProjectID: "shop"})

	require.NoError(t, err)
	require.Equal(t, []string{"stripe"}, verdict.Unconfigured)
	require.Equal(t, []string{"postgres"}, verdict.Provisioning)
}
