#!/usr/bin/env bash
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

# The runner's process ancestor. It sets the resource limits that every process
# a task spawns inherits, then hands over to the real entrypoint (the image's
# CMD). It is deliberately the only thing in this file: an entrypoint that grows
# provisioning logic becomes a second, shell-shaped half of `oneshot.ts` that no
# test covers.
#
# WHY A WRAPPER AT ALL — because nothing else in the stack can set RLIMIT_CORE:
#
#   - not the pod spec: Kubernetes has no `ulimit` field, so `docker run
#     --ulimit core=0` has no cluster equivalent. The coding-agent Job renders
#     from aep-api's `coding-agent` ComponentType, whose container carries
#     image/env/resources and nothing that reaches rlimits.
#   - not Node: there is no `setrlimit` binding, and a limit raised after the
#     process starts would not cover a child already spawned.
#   - not the agent's own Bash tool: a limit is only as good as the process that
#     cannot forget it, and a limit the agent has to remember to set is the
#     defect, not the fix.
#
# So the image is the only layer that can, and a shell is the only thing in the
# image that can call setrlimit. That makes this file image configuration, not
# runner source — which is why it lives beside the Dockerfile rather than under
# `src/`.
set -euo pipefail

# `ulimit -c 0` sets the SOFT AND HARD limit, and the hard limit is what makes
# this hold: it is inherited by every descendant and no descendant can raise it
# again. A task's children are a JVM (`bal build`), a headless chromium (under
# `agent-browser`), `go build`, `npm`, and a dev server — all grandchildren of
# the agent's Bash tool. A crash in any of them drops a `core` of tens of
# megabytes into the CLONED REPOSITORY's working tree, where it is untracked,
# invisible to a `git status` an agent skims, and one `git add -A` away from a
# customer's pull request. One live run left a 26MB `core` there and only the
# lead's habit of staging by path kept it out of the PR.
#
# Suppressing the dump costs no diagnosis: nothing in this platform reads a core
# file, the pod is one-shot and its workspace is an emptyDir that is discarded
# with it, so a core has never reached anywhere a human could open it. A crash
# still surfaces the way every other failure does — the command's exit code, its
# stderr in the progress feed, and for a JVM the `hs_err_pid*.log` it writes
# itself, which is text and is what would actually be read.
#
# Not `|| true`: lowering a limit is always permitted, so a failure here means
# the shell is not the one this file was written for, and a runner that silently
# stopped enforcing this is worse than one that fails to start.
ulimit -c 0

# `exec` so this shell is replaced rather than left as a PID-1 middleman that
# would have to forward signals itself: the Job's activeDeadlineSeconds and the
# BFF's cancel both work by signalling the container's process, and a wrapper
# that swallowed SIGTERM would turn a cancel into a kill.
exec "$@"
