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

package agentfold

// sse.go — the minimal `data:` frame reader, matching
// packages/agent-stream/src/sse-client.ts parseSseStream: frames are
// delimited by a blank line, each `data:` payload is one single-line
// StreamPart JSON, `data: [DONE]` terminates a complete stream, comment
// frames (`: keep-alive`) carry no data line and are skipped, and a payload
// that fails to parse is a truncation remnant, silently dropped. Framing is
// byte-level, so multibyte characters split across transport chunks need no
// special handling.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"strings"
)

// StreamPart is the structural subset of the agent stream part this package
// reads (packages/agent-stream/src/stream-types.ts).
type StreamPart struct {
	Type         string          `json:"type"`
	ID           string          `json:"id,omitempty"`
	Delta        string          `json:"delta,omitempty"`
	Text         string          `json:"text,omitempty"`
	ToolName     string          `json:"toolName,omitempty"`
	ToolCallID   string          `json:"toolCallId,omitempty"`
	Input        json.RawMessage `json:"input,omitempty"`
	Output       json.RawMessage `json:"output,omitempty"`
	Error        json.RawMessage `json:"error,omitempty"`
	FinishReason string          `json:"finishReason,omitempty"`
	// Manifest-part fields (type == "manifest", D14).
	Files   map[string]string `json:"files,omitempty"`
	Deleted []string          `json:"deleted,omitempty"`
	Usage   *TurnUsage        `json:"usage,omitempty"`
}

// TurnUsage is the manifest part's token-usage payload (#249) — the turn's
// spend as reported by the agents service. Field names match the wire.
type TurnUsage struct {
	InputTokens         int64  `json:"inputTokens"`
	OutputTokens        int64  `json:"outputTokens"`
	CacheReadTokens     int64  `json:"cacheReadTokens"`
	CacheCreationTokens int64  `json:"cacheCreationTokens"`
	Model               string `json:"model,omitempty"`
}

// SSEDone is the terminal sentinel payload: `data: [DONE]`.
const SSEDone = "[DONE]"

// StreamEnd reports how a stream ended: Done — the server's [DONE] sentinel
// arrived (complete); EOF — the bytes ran out first (severed mid-turn;
// whatever was folded is incomplete and, per D14, must not be committed).
type StreamEnd string

const (
	StreamDone StreamEnd = "done"
	StreamEOF  StreamEnd = "eof"
)

// ForEachDataFrame reads SSE frames from r and calls fn with every raw
// `data:` payload (the verbatim single-line JSON, trimmed) until [DONE] or
// EOF. Comment/keep-alive frames are skipped. A non-nil error from fn aborts
// the read and is returned verbatim; a transport read error is returned as-is
// (callers distinguish their own fn errors via errors.Is/As or a captured
// sentinel). Consumers that must forward payload bytes untouched (the turn
// broker) use this directly — StreamPart is a structural subset, so a
// re-marshal would drop unknown fields.
func ForEachDataFrame(r io.Reader, fn func(data []byte) error) (StreamEnd, error) {
	br := bufio.NewReader(r)
	var buf []byte
	chunk := make([]byte, 32*1024)
	for {
		n, readErr := br.Read(chunk)
		buf = append(buf, chunk[:n]...)
		for {
			sep := bytes.Index(buf, []byte("\n\n"))
			if sep < 0 {
				break
			}
			frame := strings.TrimSpace(string(buf[:sep]))
			buf = buf[sep+2:]
			if !strings.HasPrefix(frame, "data:") {
				continue // comment / keep-alive frame
			}
			data := strings.TrimSpace(frame[len("data:"):])
			if data == SSEDone {
				return StreamDone, nil
			}
			if err := fn([]byte(data)); err != nil {
				return "", err
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				return StreamEOF, nil
			}
			return "", readErr
		}
	}
}
