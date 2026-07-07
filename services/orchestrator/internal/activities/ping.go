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

package activities

import "context"

// PingActivityName is the registered activity type for Ping. Referencing the
// name as a constant keeps the workflow package from importing this one.
const PingActivityName = "Ping"

// Ping is the O1 smoke-test activity: proves the worker executes activities.
func (*Activities) Ping(_ context.Context, msg string) (string, error) {
	return "pong:" + msg, nil
}
