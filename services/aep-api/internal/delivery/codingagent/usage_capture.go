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

import (
	"bufio"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/wso2/aep/aep-api/internal/contracts"
	"github.com/wso2/aep/aep-api/internal/gen"
)

// terminalReport is what the runner's TERMINAL line says about a finished run,
// read in one pass over the captured pod log: the tokens it spent, and — when
// its model provider stopped it — the provider limit. Both are the runner's
// last word, so both come off the same line family, and reading the log twice
// for them would be two scans that could disagree about which line was last.
type terminalReport struct {
	// usage is the run's token spend (#249): the last TERMINAL runner line
	// carrying a usage object wins.
	//
	// Both envelope versions are read: a v2 runner emits no `result` kind; its
	// usage rides `run_settled`.
	//
	// LAST, never summed. The runtime reports usage CUMULATIVELY across a
	// session, so the terminal line already contains the whole run; folding the
	// `turn_ended` lines in on the way past would multiply the bill by roughly
	// the number of turns. That is why the read looks at exactly two kinds and
	// ignores every other line that happens to carry a usage object.
	//
	// The capture keeps the per-model split (#291) so RecordUsage can price
	// each model's slice against its own rate row. nil when the log carries no
	// terminal usage — pre-capture runners, or a run that died before its last
	// message.
	usage *contracts.CapturedUsage
	// providerLimit is set when the run's `run_settled` carried
	// `code: provider_limit` — the runner stopped because its provider refused
	// every call (delivery/provider_limit.go).
	providerLimit *providerLimit
}

// providerLimit is what a provider-limit settle carries beside its code.
type providerLimit struct {
	// host is the model endpoint's host, as the run's connection named it.
	host string
	// resetAt is when the provider said the limit resets; nil when it said
	// nothing.
	resetAt *time.Time
	// detail is the provider's own words on the last 429, capped and scrubbed
	// by the runner. Evidence for the operator's log only — it is a third
	// party's text, so the platform neither stores nor shows it.
	detail string
}

// terminalFromLog reads the terminal report off the captured pod log.
//
// Usage follows its own rule (terminalReport.usage). The provider limit is read off the last v2 `run_settled`
// whatever it carries: a runner settles exactly once per attempt, and a settle
// without the code (or without usage — a run stopped before its first reply
// has none) is still the one that decides.
func terminalFromLog(text string) terminalReport {
	var report terminalReport
	scanner := bufio.NewScanner(strings.NewReader(text))
	// The runner's terminal line carries the full usage JSON and can be large;
	// size the buffer generously (16 MiB) so a long line is scanned, not
	// silently dropped mid-log — the token-too-long default (64 KiB) would stop
	// the scan before the terminal line and lose the usage.
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		// Cheap pre-filter before the JSON parse: terminal lines are rare, and
		// one of the two kind names has to appear verbatim in the raw bytes.
		if !strings.Contains(line, `"result"`) && !strings.Contains(line, `"run_settled"`) {
			continue
		}
		_, msg := splitTimestampPrefix(line)
		if u, ok := terminalUsage(msg); ok {
			report.usage = u
		}
		if limit, ok := settledProviderLimit(msg); ok {
			report.providerLimit = limit
		}
	}
	// A scan error (e.g. a line still over the raised cap) stops the loop
	// early and could hide the terminal result — surface it rather than
	// returning a silently-incomplete report.
	if err := scanner.Err(); err != nil {
		slog.Warn("codingagent.terminalFromLog: log scan stopped early — captured usage may be incomplete", "error", err)
	}
	return report
}

// settledProviderLimit reads ONE line as a v2 `run_settled`: ok when it is
// one, with the provider limit when its code says so (nil otherwise). A v1
// runner predates the code and never reports one.
func settledProviderLimit(raw string) (*providerLimit, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed[0] != '{' {
		return nil, false
	}
	var ev struct {
		V              int        `json:"v"`
		Kind           string     `json:"kind"`
		Code           string     `json:"code"`
		Host           string     `json:"host"`
		ResetAt        *time.Time `json:"resetAt"`
		ProviderDetail string     `json:"providerDetail"`
	}
	if err := json.Unmarshal([]byte(trimmed), &ev); err != nil ||
		gen.RunEventV(ev.V) != gen.RunEventV2 ||
		gen.RunEventKind(ev.Kind) != gen.RunEventKindRunSettled {
		return nil, false
	}
	if gen.RunEventCode(ev.Code) != gen.RunEventCodeProviderLimit {
		return nil, true
	}
	return &providerLimit{
		host:    ev.Host,
		resetAt: ev.ResetAt,
		detail:  capText(ev.ProviderDetail, capProviderDetail),
	}, true
}

// capProviderDetail is the contract's cap on RunEvent.providerDetail, enforced
// again here because the line is a producer's and the log line is ours.
const capProviderDetail = 300

// terminalUsage reads the usage off ONE line, in whichever envelope version
// wrote it: v2's `run_settled` or v1's `result`.
//
// The v2 decode goes straight into contracts.CapturedUsage because the
// contract's TurnUsage and this shape agree field-for-field on the wire
// (`inputTokens`, …, `model`, `models[]`) — the split the platform prices is
// exactly the split the producer reports, and re-mapping it here would be a
// second place for the two to drift apart. `costUsd` on the producer's object
// is ignored by construction: CapturedUsage has nowhere to put it, because USD
// is stamped at capture from the rates then in force (ADR-0011) and a runner
// that priced its own run would be a second answer to what it cost.
func terminalUsage(raw string) (*contracts.CapturedUsage, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed[0] != '{' {
		return nil, false
	}
	var v2 struct {
		V     int                      `json:"v"`
		Kind  string                   `json:"kind"`
		Usage *contracts.CapturedUsage `json:"usage"`
	}
	if err := json.Unmarshal([]byte(trimmed), &v2); err == nil &&
		gen.RunEventV(v2.V) == gen.RunEventV2 &&
		gen.RunEventKind(v2.Kind) == gen.RunEventKindRunSettled &&
		v2.Usage != nil {
		u := *v2.Usage
		return &u, true
	}
	ln := parseProgressLine(raw)
	if ln.Kind == v1KindResult && ln.Usage != nil {
		u := *ln.Usage
		return &u, true
	}
	return nil, false
}
