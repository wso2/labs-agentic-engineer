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

// Package webhook holds the GitHub webhook route's two halves: the HMAC check
// on a delivery and the Forwarder that hands a verified delivery to aep-api.
package webhook

import (
	"context"
	"errors"
)

// Forwarder hands a verified delivery to aep-api (flow 6, ticket 04 §8).
type Forwarder interface {
	Forward(ctx context.Context, delivery, event string, body []byte) error
}

// ErrUpstreamUnavailable is a Forward failure GitHub should see as 503:
// aep-api answered 5xx or could not be reached.
var ErrUpstreamUnavailable = errors.New("aep-api unavailable")

// Unwired is the forwarder until aep-api's ingest-webhook-event exists
// (phase 4 replaces it with the generated-client forwarder and deletes this).
func Unwired() Forwarder { return unwired{} }

type unwired struct{}

func (unwired) Forward(context.Context, string, string, []byte) error { return ErrUpstreamUnavailable }
