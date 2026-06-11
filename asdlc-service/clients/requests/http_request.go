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

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
)

// HttpRequest is a fluent builder for HTTP requests.
type HttpRequest struct {
	Name    string
	URL     string
	Method  string
	query   url.Values
	headers http.Header
	body    []byte
	host    string
}

func NewRequest(name, method, rawURL string) *HttpRequest {
	return &HttpRequest{
		Name:    name,
		URL:     rawURL,
		Method:  method,
		query:   url.Values{},
		headers: http.Header{},
	}
}

func (r *HttpRequest) SetHeader(key, value string) *HttpRequest {
	r.headers.Set(key, value)
	return r
}

// SetHost overrides the Host header sent to the server.
func (r *HttpRequest) SetHost(host string) *HttpRequest {
	r.host = host
	return r
}

func (r *HttpRequest) SetQuery(key, value string) *HttpRequest {
	r.query.Set(key, value)
	return r
}

func (r *HttpRequest) SetJSON(body any) *HttpRequest {
	data, err := json.Marshal(body)
	if err == nil {
		r.body = data
		r.headers.Set("Content-Type", "application/json")
	}
	return r
}

// Build constructs the *http.Request ready for execution.
func (r *HttpRequest) Build(ctx context.Context) (*http.Request, error) {
	rawURL := r.URL
	if len(r.query) > 0 {
		rawURL += "?" + r.query.Encode()
	}

	var bodyReader *bytes.Reader
	if r.body != nil {
		bodyReader = bytes.NewReader(r.body)
	} else {
		bodyReader = bytes.NewReader([]byte{})
	}

	req, err := http.NewRequestWithContext(ctx, r.Method, rawURL, bodyReader)
	if err != nil {
		return nil, err
	}

	req.Header = r.headers.Clone()
	req.Header.Set("Accept", "application/json")

	if r.host != "" {
		req.Host = r.host
	}

	return req, nil
}
