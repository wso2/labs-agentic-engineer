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

package main

import (
	"log/slog"
	"os"

	"github.com/wso2/aep/aep-api/app"
)

// main is the OSS process entry point: NewOSSOptions wires Options (M2M
// AuthProvider when configured, a request-auth strategy selected by
// OC_FORWARD_USER_JWT — DirectOCStrategy by default, UserJWTStrategy when
// set — no impersonation), then hand process lifecycle to app.Run (which owns
// config load). All service-graph wiring lives in internal/app.Assemble so it
// is reachable from a test with faked deps.
func main() {
	opts, err := app.NewOSSOptions()
	if err != nil {
		slog.Error("failed to build OSS options", "error", err)
		os.Exit(1)
	}

	if err := app.Run(opts); err != nil {
		slog.Error("aep-api exited", "error", err)
		os.Exit(1)
	}
}
