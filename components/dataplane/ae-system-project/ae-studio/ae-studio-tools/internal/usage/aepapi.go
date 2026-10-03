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

package usage

import (
	"context"
	"fmt"
	"net/http"

	"github.com/wso2/aep/ae-studio-tools/internal/gen/aepapi"
)

// turnUsageRecorder is the generated aep-api client's record-turn-usage
// operation (POST /internal/v1/ae-studio/turn-usage).
type turnUsageRecorder interface {
	RecordTurnUsageWithResponse(ctx context.Context, body aepapi.RecordTurnUsageJSONRequestBody, reqEditors ...aepapi.RequestEditorFn) (*aepapi.RecordTurnUsageResponse, error)
}

// NewAEPAPIPost is the Sender's Post over the aep-api client, which carries
// the org's publisher token (platform.NewAEPAPI: one retry after a 401);
// aep-api binds the org from that token. A 2xx is delivered. A permanent
// refusal is a *RejectedError, so the batch is dropped rather than block the
// records behind it: 404 (a record names a project that is not the org's,
// the whole batch refused), 400, 413 and 422 (a batch aep-api will never
// take). Anything else (401, 403, 409, 429, 5xx) or no answer is a failure to
// retry, naming the status and never the body.
func NewAEPAPIPost(c turnUsageRecorder) func(ctx context.Context, records []TurnRecord) error {
	return func(ctx context.Context, records []TurnRecord) error {
		resp, err := c.RecordTurnUsageWithResponse(ctx, aepapi.RecordTurnUsageJSONRequestBody{Records: records})
		if err != nil {
			return fmt.Errorf("record-turn-usage: %w", err)
		}
		switch status := resp.StatusCode(); {
		case status >= 200 && status < 300:
			return nil
		case permanentRefusal(status):
			return &RejectedError{Status: status}
		default:
			return fmt.Errorf("record-turn-usage: aep-api answered %d", status)
		}
	}
}

// permanentRefusal reports whether aep-api's answer means resending the same
// batch can never succeed.
func permanentRefusal(status int) bool {
	switch status {
	case http.StatusNotFound, http.StatusBadRequest, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity:
		return true
	}
	return false
}
