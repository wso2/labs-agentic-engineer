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

package middleware

import (
	"context"
	"net/http"

	"github.com/wso2/asdlc/asdlc-service/internal/platform/obs"
)

// These are thin wrappers over the correlation-ID context helpers in
// internal/platform/obs. They share obs's context key so values set via
// either package are readable by the other.

// CorrelationIDHeader is the HTTP header carrying the request correlation ID.
const CorrelationIDHeader = obs.CorrelationIDHeader

// AddCorrelationID returns a middleware that attaches a correlation ID to every request.
func AddCorrelationID() func(http.Handler) http.Handler {
	return obs.AddCorrelationID()
}

// GetCorrelationID retrieves the correlation ID from the context.
func GetCorrelationID(ctx context.Context) string {
	return obs.GetCorrelationID(ctx)
}

// WithCorrelationID returns a derived context carrying the given ID.
func WithCorrelationID(ctx context.Context, id string) context.Context {
	return obs.WithCorrelationID(ctx, id)
}
