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
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestScrubFailureDetail_RedactsCredentialShapes: Detail is the one free-text
// field on the record and it reaches a user-visible card, so the shapes a
// provisioner's error could carry are redacted with their keys kept legible.
func TestScrubFailureDetail_RedactsCredentialShapes(t *testing.T) {
	cases := map[string]struct{ in, keep, drop string }{
		"bearer header":  {"sm-api: Authorization: Bearer abcdef0123456789XYZ rejected", "Authorization: Bearer [REDACTED]", "abcdef0123456789XYZ"},
		"token kv":       {"clone failed: token=ghp_abcdefghijklmnopqrstuvwxyz0123 denied", "token=[REDACTED]", "ghp_"},
		"url userinfo":   {"fetch https://x-access-token:s3cr3tvalue@github.com/o/r failed", "https://x-access-token:[REDACTED]@github.com/o/r", "s3cr3tvalue"},
		"github pat":     {"got github_pat_11ABCDEFG0123456789abcdefghij back", "got [REDACTED] back", "github_pat_11"},
		"plain sentence": {`external resourcetype "sendgrid": at least one config key required`, `external resourcetype "sendgrid": at least one config key required`, ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got := ScrubFailureDetail(c.in)
			require.Contains(t, got, c.keep)
			if c.drop != "" {
				require.NotContains(t, got, c.drop)
			}
		})
	}
}

// TestScrubFailureDetail_Caps: a stack of wrapped causes is cut to the cap.
func TestScrubFailureDetail_Caps(t *testing.T) {
	got := ScrubFailureDetail(strings.Repeat("x", failureDetailMax*2))
	require.Len(t, []rune(got), failureDetailMax+1)
	require.True(t, strings.HasSuffix(got, "…"))
}

// TestRunFailure_JSONBRoundTrip: the column encodes itself, so the map write
// path the run repository uses cannot hand the driver a Go struct.
func TestRunFailure_JSONBRoundTrip(t *testing.T) {
	at := time.Date(2026, 9, 11, 7, 35, 54, 0, time.UTC)
	in := &RunFailure{
		Code: RunFailureCodeDependencyUnprovisionable, Phase: RunPhasePlanning,
		Component: "allocation-api", Dependency: "sendgrid", Permanent: true,
		Attempts: 1, MaxAttempts: 3, FirstAt: at, LastAt: at, Detail: "no keys",
	}
	v, err := in.Value()
	require.NoError(t, err)
	var out RunFailure
	require.NoError(t, out.Scan(v))
	require.Equal(t, *in, out)

	var nilFailure *RunFailure
	v, err = nilFailure.Value()
	require.NoError(t, err)
	require.Nil(t, v, "a nil record is SQL NULL, not the string \"null\"")
	require.NoError(t, out.Scan(nil))
}
