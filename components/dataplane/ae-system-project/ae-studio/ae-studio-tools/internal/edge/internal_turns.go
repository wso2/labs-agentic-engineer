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

package edge

import (
	"context"
	"net/http"

	"github.com/wso2/aep/ae-studio-tools/internal/gen"
)

// The turns op the contract gained for phase 3. Its behaviour lands with
// Task 3.6; until then the route answers 503 so the contract is served in
// full and nothing is silently accepted.

// StartRepoTurn is replaced by Task 3.6.
func (s internalServer) StartRepoTurn(_ context.Context, _ gen.StartRepoTurnRequestObject) (gen.StartRepoTurnResponseObject, error) {
	return gen.StartRepoTurn503ApplicationProblemPlusJSONResponse(
		newProblem(http.StatusServiceUnavailable, "not_implemented", "turns are not served yet")), nil
}
