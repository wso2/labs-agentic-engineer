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

package devflow

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDevWorkflowID_Format(t *testing.T) {
	// With a run suffix — each re-run gets its own workflow id / timeline.
	require.Equal(t, "devflow-acme-shop-v3-abc", DevWorkflowID("acme", "shop", "v3", "abc"))
}

func TestDevWorkflowID_NoSuffix(t *testing.T) {
	// Empty suffix keeps the legacy format (no trailing dash).
	require.Equal(t, "devflow-acme-shop-v3", DevWorkflowID("acme", "shop", "v3", ""))
}

func TestTaskWorkflowID_Format(t *testing.T) {
	// The child inherits the parent's run suffix so a dev run and its tasks
	// share one lineage: devflow-…-v3-abc → taskflow-…-v3-7-abc.
	require.Equal(t, "taskflow-acme-shop-v3-7-abc", taskWorkflowID("acme", "shop", "v3", 7, "abc"))
}

func TestTaskWorkflowID_NoSuffix(t *testing.T) {
	require.Equal(t, "taskflow-acme-shop-v3-7", taskWorkflowID("acme", "shop", "v3", 7, ""))
}

func TestNewRunSuffix_Base36(t *testing.T) {
	// base36 of the epoch-millis: [0-9a-z], compact, Temporal/DNS-safe.
	require.Equal(t, "0", newRunSuffix(time.UnixMilli(0)))
	require.Equal(t, "z", newRunSuffix(time.UnixMilli(35)))  // 35 → 'z'
	require.Equal(t, "10", newRunSuffix(time.UnixMilli(36))) // 36 → '10'
}

func TestNewRunSuffix_MonotonicDistinct(t *testing.T) {
	// Distinct instants yield distinct suffixes (millisecond resolution).
	a := newRunSuffix(time.UnixMilli(1783587000000))
	b := newRunSuffix(time.UnixMilli(1783587000001))
	require.NotEqual(t, a, b)
	require.NotEmpty(t, a)
}
