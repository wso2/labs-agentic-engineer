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

package codingagent

// cycle_log_source.go — the LIVE half of a cycle's log.
//
// While a cycle's Component exists, its pod's stdout is served straight from
// OpenChoreo. Nothing is stored on the way past: this read exists to answer one
// console poll, and the same bytes are re-read on the next one.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
)

// ErrComponentGone means the cycle's Component (or its release binding) no
// longer exists — deleted by retention or by a cancel. It is a FACT about the
// world, not a failure: the caller falls back to the archive, and failing that
// tells the user the logs are gone.
var ErrComponentGone = errors.New("codingagent: component no longer exists")

// logPageBytes bounds how much live pod stdout is kept AFTER the OpenChoreo
// read returns — the last 64KiB, so old lines scroll off and the console gets
// fresh content on every poll. The OC call itself is unbounded (sinceSeconds=0).
const logPageBytes = 64 * 1024

// OCLogSource reads a cycle pod's log through the OpenChoreo API.
type OCLogSource struct {
	runtime openchoreo.RuntimeClient
}

// NewOCLogSource wires the live source.
func NewOCLogSource(runtime openchoreo.RuntimeClient) *OCLogSource {
	return &OCLogSource{runtime: runtime}
}

// Tail reads at most maxBytes of the newest pod output for a cycle Component.
// It asks for the WHOLE log the platform still holds and cuts bytes off the end
// of it, which is a viewer's bargain: fresh content now, and whatever scrolled
// off comes back on the next poll.
func (s *OCLogSource) Tail(ctx context.Context, orgName, projectName, componentName, environment string, maxBytes int) (LiveTail, error) {
	binding, err := s.Binding(ctx, orgName, projectName, componentName, environment)
	if err != nil {
		return LiveTail{}, err
	}
	return s.read(ctx, orgName, binding, maxBytes)
}

// Binding resolves a cycle Component's release binding in the environment its
// Job was bound into. An empty environment is refused rather than looked up:
// it is a caller that lost the cycle's environment, and "" names no binding.
func (s *OCLogSource) Binding(ctx context.Context, orgName, projectName, componentName, environment string) (string, error) {
	if s == nil || s.runtime == nil {
		return "", fmt.Errorf("codingagent: live log source not configured")
	}
	if environment == "" {
		return "", fmt.Errorf("codingagent: no environment to read %s's release binding in", componentName)
	}
	binding, err := s.runtime.ReleaseBindingName(ctx, orgName, projectName, componentName, environment)
	if err != nil {
		if errors.Is(err, openchoreo.ErrNotFound) {
			return "", fmt.Errorf("%w: %s", ErrComponentGone, componentName)
		}
		return "", fmt.Errorf("codingagent: resolve release binding for %s: %w", componentName, err)
	}
	return binding, nil
}

// read snapshots the pod and renders its whole log, keeping the newest
// maxBytes.
func (s *OCLogSource) read(ctx context.Context, orgName, releaseBindingName string, maxBytes int) (LiveTail, error) {
	if s == nil || s.runtime == nil {
		return LiveTail{}, fmt.Errorf("codingagent: live log source not configured")
	}
	pod, err := s.runtime.PodSnapshot(ctx, orgName, releaseBindingName)
	if err != nil {
		if errors.Is(err, openchoreo.ErrNotFound) {
			return LiveTail{}, fmt.Errorf("%w: %s", ErrComponentGone, releaseBindingName)
		}
		return LiveTail{}, fmt.Errorf("codingagent: read resource tree for %s: %w", releaseBindingName, err)
	}
	if !pod.Found {
		// The Job is applied but nothing is scheduled yet. The caller narrates
		// the dark zone from the pod state; there is no text to read.
		return LiveTail{Pod: pod}, nil
	}
	lines, err := s.runtime.PodLogs(ctx, orgName, releaseBindingName, pod.Name, 0)
	if err != nil {
		if errors.Is(err, openchoreo.ErrNotFound) {
			// The binding is there but the pod's log is not — a container that
			// has not started, or one already reaped. Not gone; just silent.
			return LiveTail{Pod: pod}, nil
		}
		return LiveTail{}, fmt.Errorf("codingagent: read pod log for %s: %w", pod.Name, err)
	}
	return LiveTail{Text: tailText(lines, maxBytes), Pod: pod}, nil
}

// tailText renders log lines in the `timestamps=true` shape the progress parser
// already understands (`RFC3339Nano <line>`), keeping at most maxBytes from the
// END so a long-running agent's newest output is what survives the cap.
// maxBytes <= 0 keeps everything.
func tailText(lines []openchoreo.PodLogLine, maxBytes int) string {
	var b strings.Builder
	for i := range lines {
		if !lines[i].Timestamp.IsZero() {
			b.WriteString(lines[i].Timestamp.UTC().Format(time.RFC3339Nano))
			b.WriteByte(' ')
		}
		b.WriteString(lines[i].Log)
		b.WriteByte('\n')
	}
	text := b.String()
	if maxBytes <= 0 || len(text) <= maxBytes {
		return text
	}
	// Cut on a line boundary so the oldest surviving line is not half a line.
	cut := text[len(text)-maxBytes:]
	if nl := strings.IndexByte(cut, '\n'); nl >= 0 && nl+1 < len(cut) {
		cut = cut[nl+1:]
	}
	return cut
}
