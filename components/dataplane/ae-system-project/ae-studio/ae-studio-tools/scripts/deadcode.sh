#!/usr/bin/env bash
#
# Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
#
# WSO2 LLC. licenses this file to you under the Apache License,
# Version 2.0 (the "License"); you may not use this file except
# in compliance with the License.
# You may obtain a copy of the License at
#
# http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing,
# software distributed under the License is distributed on an
# "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
# KIND, either express or implied.  See the License for the
# specific language governing permissions and limitations
# under the License.
#
# Dead-code gate for ae-studio-tools (copied from services/aep-api/scripts/deadcode.sh;
# keep the two in step).
#
# Runs golang.org/x/tools/cmd/deadcode — a whole-program Rapid Type Analysis
# that builds the call graph reachable from the cmd/ae-studio-tools main and reports
# every function/method NOTHING live reaches. It is run WITHOUT -test on
# purpose: tests do NOT count as consumers, so a function only a test calls is
# dead (delete it and its orphaned test). generated files (`// Code generated
# … DO NOT EDIT.`) are excluded by deadcode itself.
#
# Two categories are filtered out of the result — they are unreachable BY
# DESIGN, not dead:
#
#   1. Test-support packages ($EXCLUDE_RE): the edgetest harness the suite
#      runs on and the projectstest fake resolver. They exist only to be
#      consumed by tests. Identified by dir name
#      or the *_fortest.go suffix.
#   2. Functions carrying a `//deadcode:keep <reason>` marker in their doc
#      comment: intentional test seams that let tests drive live code, and
#      infrastructure retained but not yet wired.
#
# Usage:
#   scripts/deadcode.sh report   # human audit — prints findings, always exit 0
#   scripts/deadcode.sh check    # CI gate    — exit 1 if any finding survives
set -euo pipefail

cd "$(dirname "$0")/.."

DEADCODE_VERSION="v0.44.0"
DEADCODE="go run golang.org/x/tools/cmd/deadcode@${DEADCODE_VERSION}"

# deadcode must be BUILT with a Go toolchain >= this module's `go` directive: a
# binary built with an older toolchain refuses to load packages targeting a
# newer one ("package requires newer Go version go1.26"). Left to itself,
# `go run …/deadcode@vX` picks the toolchain from x/tools' OWN go directive and
# switches DOWN (v0.44.0 → go1.25.12), so with a go 1.26 module EVERY package
# fails to load. Same failure mode, same remedy as GO_TOOLCHAIN in the root
# Makefile (which pins the toolchain used to build golangci-lint) — pinning
# GOTOOLCHAIN to an explicit name disables the switch entirely.
#
# The version is derived from go.mod rather than hardcoded so it cannot drift
# from the module it analyses. An explicit pin is used instead of
# GOTOOLCHAIN=local because `local` would break a contributor whose installed
# Go predates the directive, where an explicit name just downloads it — the
# same auto-download a normal `go build` of this module already performs.
GO_DIRECTIVE="$(awk '$1 == "go" { print $2; exit }' go.mod)"
case "$GO_DIRECTIVE" in
# A two-component directive ("go 1.26") is legal in go.mod but is a language
# version, not a toolchain name; go1.26.0 is the toolchain that provides it.
[0-9]*.[0-9]*.[0-9]*) ;;
[0-9]*.[0-9]*) GO_DIRECTIVE="${GO_DIRECTIVE}.0" ;;
*)
	echo "FAIL: could not read the 'go' directive from $(pwd)/go.mod" >&2
	exit 1
	;;
esac
GO_TOOLCHAIN="go${GO_DIRECTIVE}"

# Test-support packages/files — unreachable by design, never deletion targets.
EXCLUDE_RE='/edgetest/|/projectstest/|_fortest\.go'

mode="${1:-check}"

# has_marker FILE LINE — true if the func at LINE carries a //deadcode:keep
# marker on its own line or anywhere in the contiguous comment block directly
# above it (the Go doc comment). Stops at the first non-comment line so a
# marker on a PRECEDING function never leaks onto this one.
has_marker() {
	awk -v t="$2" '
		{ L[NR] = $0 }
		END {
			if (index(L[t], "deadcode:keep")) { print "y"; exit }
			for (i = t - 1; i >= 1; i--) {
				c = L[i]; sub(/^[ \t]+/, "", c)
				if (c ~ /^\/\//) {
					if (index(c, "deadcode:keep")) { print "y"; exit }
					continue
				}
				break
			}
		}' "$1"
}

# Findings go to stdout and do not affect the exit status: deadcode exits 0
# whether it reports a hundred functions or none. A non-zero exit therefore
# means the TOOL failed (packages that would not load, a bad flag, a failed
# module download), which is never a pass. The previous `2>/dev/null || true`
# converted exactly that into an empty finding set, i.e. a green gate that had
# analysed nothing.
# stderr is deliberately left attached to the terminal so a failure can never be
# reported without the reason for it.
raw=""
status=0
# ./... (not just ./cmd/ae-studio-tools): the main is still the only root, but
# loading every package is what lets a package nothing imports show up as dead.
raw="$(GOTOOLCHAIN="$GO_TOOLCHAIN" $DEADCODE ./...)" || status=$?
if [ "$status" -ne 0 ]; then
	echo "" >&2
	echo "FAIL: deadcode did not run (exit ${status}); see the error above." >&2
	echo "The gate reports nothing rather than passing: an analysis that did not" >&2
	echo "happen is not a clean one." >&2
	exit 1
fi

survivors=""
while IFS= read -r line; do
	[ -z "$line" ] && continue
	case "$line" in *"unreachable func"*) ;; *) continue ;; esac
	path="${line%%:*}"
	printf '%s\n' "$path" | grep -qE "$EXCLUDE_RE" && continue
	lno="$(printf '%s\n' "$line" | cut -d: -f2)"
	[ -n "$(has_marker "$path" "$lno")" ] && continue
	survivors+="${line}"$'\n'
done <<< "$raw"

survivors="$(printf '%s' "$survivors" | sed '/^$/d')"

if [ "$mode" = "report" ]; then
	if [ -z "$survivors" ]; then
		# The filtered count is printed even when nothing survives: a clean report
		# that also says "0 filtered" is the signature of an analysis that never
		# looked at the tree, and the human auditor should be able to see the
		# difference without re-running the tool by hand.
		filtered="$(printf '%s\n' "$raw" | grep -c 'unreachable func' || true)"
		echo "deadcode: no dead functions (${filtered} finding(s) filtered as test-support / //deadcode:keep)."
	else
		count="$(printf '%s\n' "$survivors" | wc -l | tr -d ' ')"
		echo "deadcode: ${count} unreachable function(s) from cmd/ae-studio-tools (tests do not count):"
		echo ""
		printf '%s\n' "$survivors" | sed 's/^/  /'
	fi
	exit 0
fi

# check mode (gate)
if [ -n "$survivors" ]; then
	count="$(printf '%s\n' "$survivors" | wc -l | tr -d ' ')"
	echo "FAIL: ${count} dead function(s) unreachable from cmd/ae-studio-tools (tests do not count):"
	echo ""
	printf '%s\n' "$survivors" | sed 's/^/  /'
	echo ""
	echo "Each must be resolved:"
	echo "  - delete it (and its orphaned test), OR"
	echo "  - if it is a test seam / intentionally-retained infra, add a"
	echo "    '//deadcode:keep <reason>' line to its doc comment, OR"
	echo "  - if it belongs in a test-support package, move it there."
	echo "See the dead-code note in this module's AGENTS.md."
	exit 1
fi

echo "deadcode: OK — no dead functions."
