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

package requests

import "net/http"

// HttpClient is the minimal interface oapi-codegen's generated clients ask
// for via `gen.WithHTTPClient`. Lets us swap a plain *http.Client for a
// RetryableHTTPClient transparently. Matches agent-manager's `clients/requests`
// shape.
type HttpClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// Compile-time assertion that *http.Client satisfies the interface.
var _ HttpClient = (*http.Client)(nil)
