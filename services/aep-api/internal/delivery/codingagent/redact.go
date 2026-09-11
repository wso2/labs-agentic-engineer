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

import "regexp"

// Second line of defense for credentials in agent-pod output.
//
// The runner scrubs its own stdout (runners/remote-worker progress/scrubber.ts),
// which is the only place a token's exact value is known and can be redacted by
// literal match. This package is where raw pod stdout/stderr crosses into
// user-visible surfaces — the console build-log feed and the persisted final
// log — so it redacts by SHAPE as well: a runner regression, a line authored by
// a library rather than by us, or a git subprocess message must not be able to
// turn pod output into a credential disclosure.
//
// Shape-based matching is deliberately narrow. It cannot catch an opaque token
// (that is the runner's job); it does catch the families that have actually
// leaked here, and it never depends on knowing a secret's value.

const redactedPlaceholder = "[REDACTED]"

// tokenPatterns are the GitHub credential families the platform hands to
// runners: installation tokens (ghs_), user/app/refresh/OAuth tokens
// (ghu_/gho_/ghr_/ghp_) and fine-grained PATs (github_pat_).
var tokenPatterns = []*regexp.Regexp{
	regexp.MustCompile(`gh[psour]_[A-Za-z0-9]{20,}`),
	regexp.MustCompile(`github_pat_[A-Za-z0-9_]{20,}`),
}

// urlUserinfo matches the password half of URL credentials, e.g. the
// `https://x-access-token:<secret>@github.com/...` shape that a clone command
// echoed in a child_process error message produces. Only the secret is
// replaced, so the scheme, username and host stay readable — that context is
// what makes the log line useful. Requires the `@`, so an ordinary
// `http://host:8080/path` is not touched.
var urlUserinfo = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.\-]*://[^/\s:@]+:)[^@/\s]+(@)`)

// authHeader matches a credential following an Authorization / x-api-key header
// key, with or without a `Bearer` scheme word. The key is preserved.
var authHeader = regexp.MustCompile(`(?i)((?:authorization|x-api-key)\s*:\s*(?:bearer\s+)?)[A-Za-z0-9._~+/=\-]{16,}`)

// ghOAuthToken matches `gh`'s own config shape. On the credhelper git path the
// runner writes the GitHub token at rest as `oauth_token: <token>` into
// <workspace>/.gh-config/hosts.yml — inside the tree the agent works in, so a
// `cat` of that file reaches this layer as pod output. That token is minted
// inside bash and never enters the runner process, so the runner cannot enroll
// its literal either: shape is the only layer either side has for it, which is
// exactly why it is worth having on both.
var ghOAuthToken = regexp.MustCompile(`(?i)(oauth_token\s*:\s*)\S+`)

// redactSecrets replaces credential-shaped substrings in raw pod output.
// Returns s unchanged when there is nothing to redact.
func redactSecrets(s string) string {
	if s == "" {
		return s
	}
	// URL userinfo first: it consumes the whole secret between `:` and `@`,
	// which keeps a shaped token inside a clone URL from being replaced
	// piecemeal and leaving the surrounding userinfo behind.
	out := urlUserinfo.ReplaceAllString(s, "${1}"+redactedPlaceholder+"${2}")
	out = authHeader.ReplaceAllString(out, "${1}"+redactedPlaceholder)
	out = ghOAuthToken.ReplaceAllString(out, "${1}"+redactedPlaceholder)
	for _, re := range tokenPatterns {
		out = re.ReplaceAllString(out, redactedPlaceholder)
	}
	return out
}
