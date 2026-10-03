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

// Package turns runs the turns aep-api starts in the studio (kickoff and
// plan): the Turn socket client (ae-design-agent serves it, AE_TURN_SOCKET)
// and the relay that streams a turn's NDJSON frames back to aep-api.
package turns

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/wso2/aep/ae-studio-tools/internal/gen/turnsock"
)

// startHeaderTimeout bounds the wait for the agent's answer to a start: it
// answers the status at once and streams the turn after.
const startHeaderTimeout = 30 * time.Second

// ErrSocketUnavailable means the Turn socket could not be reached or broke
// before it answered (the agent is not up, or went away).
var ErrSocketUnavailable = errors.New("turn socket unavailable")

// Body is a TurnRequest as JSON, already validated against the contract. It
// is forwarded byte for byte: re-encoding the generated model would send an
// empty scope object on every start turn.
type Body []byte

// Client calls the Turn socket through the generated turnsock client.
type Client struct {
	api *turnsock.Client
}

// NewClient dials the Turn socket at socketPath for every request.
func NewClient(socketPath string) *Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socketPath)
		},
		ResponseHeaderTimeout: startHeaderTimeout,
	}
	// The host is a placeholder: every connection is the socket.
	api, err := turnsock.NewClient("http://turn-socket", turnsock.WithHTTPClient(&http.Client{Transport: transport}))
	if err != nil {
		// Only a failing option errors, and none is passed that can.
		panic(fmt.Sprintf("turn socket client: %v", err))
	}
	return &Client{api: api}
}

// Start starts (or reattaches to) the turn body names and answers the
// agent's status and body: the NDJSON stream on 200, else its refusal. The
// caller closes the body. ctx bounds the whole stream, not just the start.
func (c *Client) Start(ctx context.Context, body Body) (io.ReadCloser, int, error) {
	resp, err := c.api.StartTurnWithBody(ctx, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %w", ErrSocketUnavailable, err)
	}
	return resp.Body, resp.StatusCode, nil
}
