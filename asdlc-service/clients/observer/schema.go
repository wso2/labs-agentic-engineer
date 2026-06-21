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

// Package observer holds the BFF's client to the OpenChoreo Observer
// service plus the on-the-wire schema for runner progress events.
//
// schemaVersion=1 mirrors the TS source-of-truth at
// remote-worker/src/lib/progress/schema.ts. A CI gate keeps the two in
// sync against schemas/progress-event.schema.json.
package observer

import (
	"encoding/json"
	"strings"

	"github.com/wso2/asdlc/asdlc-service/internal/contracts"
)

// ProgressSchemaVersion is the only version this BFF supports today.
// Tolerant decoding still wraps unknown shapes as a `log` event so
// out-of-band lines never break the feed.
const ProgressSchemaVersion = 1

// ProgressEvent is the unified progress envelope. The value type lives on the
// dependency-free leaf (contracts) so the task HTTP edge can consume it without
// importing this client; observer owns the wire parser (ParseProgressLine) that
// produces it.
type ProgressEvent = contracts.ProgressEvent

// ParseProgressLine attempts to decode a fluent-bit log line as a versioned
// progress envelope. Lines that are non-JSON or that don't carry a
// recognised schema version are wrapped as a `log` event so the feed
// stays continuous (the runner is not the only thing that can write to
// stdout — fluent-bit may still pick up stray lines from libraries).
func ParseProgressLine(raw string) ProgressEvent {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed[0] != '{' {
		return ProgressEvent{Kind: "log", Summary: raw}
	}
	var ev ProgressEvent
	if err := json.Unmarshal([]byte(trimmed), &ev); err != nil {
		return ProgressEvent{Kind: "log", Summary: raw}
	}
	if ev.SchemaVersion != ProgressSchemaVersion || ev.Kind == "" {
		return ProgressEvent{Kind: "log", Summary: raw}
	}
	return ev
}
