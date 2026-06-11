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

package services

import "strings"

// NormalizeExternalURL ensures a URL or path ends with a single trailing
// slash. The AP router 404s on bare context paths without one — see
// gotcha C3 in deployments/POC-API-PLATFORM.md. ALL code that reads
// `ReleaseBinding.status.endpoints[].externalURLs.http.path` (or the full
// composed URL) MUST pass it through this helper before handing it to a
// caller or rendering it in the console.
//
// Empty input returns "" unchanged (callers handle that as "no URL yet").
func NormalizeExternalURL(raw string) string {
	if raw == "" {
		return ""
	}
	// Strip any trailing slashes then append exactly one. Cheap, allocation-
	// free for the common already-normalised case.
	trimmed := strings.TrimRight(raw, "/")
	return trimmed + "/"
}
