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

// Package httpx provides shared HTTP plumbing for outbound clients.
package httpx

import (
	"net/http"

	"github.com/wso2/asdlc/asdlc-service/middleware"
)

// CorrelationTransport propagates the X-Correlation-ID header from the
// request context onto every outbound request. Wrap an existing transport
// (or pass nil to wrap http.DefaultTransport) and assign to http.Client.Transport.
type CorrelationTransport struct {
	Base http.RoundTripper
}

// RoundTrip implements http.RoundTripper. Per the contract it must not
// mutate the caller's request, so we clone before adding the header.
func (t *CorrelationTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	id := middleware.GetCorrelationID(req.Context())
	if id == "" || req.Header.Get(middleware.CorrelationIDHeader) != "" {
		return base.RoundTrip(req)
	}
	cloned := req.Clone(req.Context())
	cloned.Header.Set(middleware.CorrelationIDHeader, id)
	return base.RoundTrip(cloned)
}

// WrapTransport returns t wrapped with correlation-ID propagation. nil is
// treated as http.DefaultTransport.
func WrapTransport(t http.RoundTripper) http.RoundTripper {
	return &CorrelationTransport{Base: t}
}
