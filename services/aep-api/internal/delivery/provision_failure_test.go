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
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestProvisionFailedError_KeepsTheClassificationAndTheText: the typed error
// replaces a flattened string, so what read the string must read the same —
// the joined text, the delivery sentinel when any fault is permanent, and the
// provisioner's own cause through Unwrap.
func TestProvisionFailedError_KeepsTheClassificationAndTheText(t *testing.T) {
	cause := errors.New("provisioner said no")
	permanent := &ProvisionFailedError{Faults: []ProvisionFault{
		{Dependency: "orders-db", Reason: "503", Cause: errors.New("503")},
		{Component: "api", Dependency: "sendgrid", Reason: "no keys", Permanent: true, Cause: cause},
	}}
	require.Equal(t, "provision 2 dependenc(ies) failed: orders-db: 503; sendgrid: no keys", permanent.Error())
	require.True(t, errors.Is(permanent, ErrProvisionPermanent))
	require.True(t, errors.Is(permanent, cause), "the provisioner's own error stays reachable")

	wrapped := fmt.Errorf("activity: %w", permanent)
	var got *ProvisionFailedError
	require.True(t, errors.As(wrapped, &got))
	require.Equal(t, "sendgrid", got.Faults[1].Dependency)

	transient := &ProvisionFailedError{Faults: []ProvisionFault{{Dependency: "orders-db", Reason: "503"}}}
	require.False(t, errors.Is(transient, ErrProvisionPermanent))
	require.False(t, errors.Is(transient, cause))
}
