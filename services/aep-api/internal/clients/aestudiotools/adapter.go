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

package aestudiotools

// adapter.go — one unary /internal/v1 call: bounded by the call timeout, sent
// through send (token, OU impersonation, error map), its 2xx reply decoded.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// replyBodyLimit bounds a decoded 2xx reply: a read-bundle of a whole tree is
// the largest answer (the pod caps a commit at 16 MiB of content).
const replyBodyLimit = 64 << 20

// do runs one unary op of org and decodes its 2xx reply into out; a nil out
// reads no body.
func (a *Adapter) do(ctx context.Context, org, op string, out any, fn call) error {
	ctx, cancel := a.unary(ctx)
	defer cancel()
	resp, err := a.send(ctx, org, op, fn, always)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if out == nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, problemBodyLimit))
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, replyBodyLimit)).Decode(out); err != nil {
		return fmt.Errorf("ae studio: decode %s: %w", op, err)
	}
	return nil
}

// optional is v as an optional query parameter: nil, left out of the query,
// when v is its zero value (the generated client sends every non-nil one).
func optional[T comparable](v T) *T {
	var zero T
	if v == zero {
		return nil
	}
	return &v
}

// literalSlashes sends a trailing file path parameter with its slashes as
// path separators (the pod's read-file route takes the path's segments), not
// the generated client's %2F.
func literalSlashes(_ context.Context, req *http.Request) error {
	req.URL.RawPath = ""
	return nil
}
