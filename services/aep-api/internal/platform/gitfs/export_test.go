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

package gitfs

import (
	"context"
	"time"
)

// Test-only accessors compiled solely into the gitfs test binary, so the
// external gitfs_test package can observe internals without widening the
// production API.

// SleepJittered exposes the CAS-retry backoff wait so the base<=0 guard can be
// exercised directly (a 0/negative Backoff element must not panic rand.N).
func SleepJittered(ctx context.Context, backoff []time.Duration, i int) error {
	return sleepJittered(ctx, backoff, i)
}

// SetExecHook installs an observer invoked with every git invocation's argv
// + flattened env immediately before it runs — the seam for the
// credential-hygiene assertions and crash injection. Pass nil to remove.
func SetExecHook(e *Engine, hook func(args []string, env []string)) {
	e.execHook = hook
}

// AskpassPath exposes the shim location for direct shim invocation tests.
func AskpassPath(e *Engine) string { return e.askpass }

// NewLocker exposes the production flock implementation for contention tests.
func NewLocker() Locker { return flockLocker{} }

// IsENOSPC exposes isENOSPC for ENOSPC-detection unit tests.
func IsENOSPC(err error) bool { return isENOSPC(err) }

// MapDiskErr exposes Engine.mapDiskErr for emergency-sweep wiring tests.
func MapDiskErr(e *Engine, err error) error { return e.mapDiskErr(err) }

// RunGitWithEnv runs one git command through the engine's own child-process
// path (buildCmd → hermetic base env → overlay), returning stdout. It is the
// seam for asserting what git ACTUALLY does under the engine's environment:
// a test can hand the invocation a GIT_TRACE2_EVENT sink and read back which
// children git spawned, or ask git to resolve a config key and see which
// layer won. The overlay travels through execOpts, the same channel the
// credential plumbing uses, so what runs is the production path rather than a
// re-creation of it.
func RunGitWithEnv(e *Engine, ctx context.Context, env map[string]string, args ...string) ([]byte, error) {
	return e.git(ctx, execOpts{env: env}, args...)
}

// ForcedConfigRules exposes the config rules the engine imposes on every git
// child, so a test can assert that git resolves EVERY one of them rather than
// only the rule a named test happens to mention — the coverage grows with the
// list instead of needing a new test per rule.
func ForcedConfigRules() [][2]string {
	rules := make([][2]string, 0, len(forcedConfig))
	for _, c := range forcedConfig {
		rules = append(rules, [2]string{c.key, c.value})
	}
	return rules
}
