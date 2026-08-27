/*
 *  Copyright (c) 2026, WSO2 LLC. (http://www.wso2.com)
 *
 *  WSO2 LLC. licenses this file to you under the Apache License,
 *  Version 2.0 (the "License"); you may not use this file except
 *  in compliance with the License.
 *  You may obtain a copy of the License at
 *
 *    http://www.apache.org/licenses/LICENSE-2.0
 *
 *  Unless required by applicable law or agreed to in writing,
 *  software distributed under the License is distributed on an
 *  "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 *  KIND, either express or implied.  See the License for the
 *  specific language governing permissions and limitations
 *  under the License.
 */

package io.ballerina.library.cache;

import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;

/**
 * Where the cache lives — decided as a pure function of the environment, so the decision is testable
 * without a filesystem and without setting real environment variables.
 *
 * <p>{@code ~/.cache/bal-library} is the default because it is the conventional location; because
 * {@code $HOME} in a runner is owned by the run user with nothing mounted over it but the workspace; and
 * because it is not world-writable, so the symlink-precreation hazard {@code /tmp} has does not arise.
 *
 * <p>Not beside the installed tool, which lives in the read-only bala repository. Not in the workspace,
 * which is a git clone the platform commits and provisioning scrubs per task.
 *
 * @since 0.1.0
 */
public final class CacheLocation {

    /** The environment variable value that turns the cache off, spelled once. */
    public static final String CACHE_OFF = "off";

    public static final String CACHE_VARIABLE = "BAL_LIBRARY_CACHE";
    public static final String CACHE_DIR_VARIABLE = "BAL_LIBRARY_CACHE_DIR";

    private CacheLocation() {
    }

    /** Everything about the machine the decision depends on. */
    public record Environment(Map<String, String> env, String homedir, String tmpdir, String user) { }

    /** One rung of the preference list. */
    public sealed interface Candidate {

        /** No cache at all: either asked for, or nowhere safe to put one. */
        record Disabled(String reason) implements Candidate { }

        record Directory(String root, int mode) implements Candidate { }
    }

    /**
     * The candidates, in preference order, for the caller to try until one works.
     *
     * <p>A LIST rather than one answer, because the precedence has a fallback in it —
     * "{@code <tmpdir>/bal-library-<user>} when {@code $HOME} is unusable" — and "unusable" is not something
     * a pure function can determine. An empty or relative {@code $HOME} it can see; a {@code $HOME} that
     * exists and is read-only, which is a shape a container genuinely has, it cannot. Returning the ordered
     * candidates keeps this function pure and testable with no filesystem while still letting the process
     * wrapper reach a temp directory in that case.
     *
     * <ol>
     *   <li>{@code BAL_LIBRARY_CACHE=off} — explicit opt-out
     *   <li>{@code BAL_LIBRARY_CACHE_DIR=<dir>} — explicit location
     *   <li>{@code $XDG_CACHE_HOME/bal-library} — when absolute, per the spec
     *   <li>{@code <homedir>/.cache/bal-library} — the default
     *   <li>{@code <tmpdir>/bal-library-<user>} — when the above is unusable, mode 0700
     *   <li>disabled
     * </ol>
     *
     * <p>Rungs 1 and 2 are the caller being EXPLICIT, so they get no fallback: silently caching somewhere
     * other than the directory somebody named would be worse than not caching.
     *
     * <p>The temp fallback is mode 0700 with the user name in the directory because a shared temp directory
     * is world-writable and shared with the agent's own scratch files; a shared name there is a directory
     * another user can pre-create.
     */
    public static List<Candidate> candidates(Environment environment) {
        Map<String, String> env = environment.env();

        if (CACHE_OFF.equals(env.get(CACHE_VARIABLE))) {
            return List.of(new Candidate.Disabled(CACHE_VARIABLE + "=" + CACHE_OFF));
        }

        String explicit = env.get(CACHE_DIR_VARIABLE);
        if (explicit != null && !explicit.trim().isEmpty()) {
            return List.of(new Candidate.Directory(explicit, 0700));
        }

        List<Candidate> candidates = new ArrayList<>();
        String xdg = env.get("XDG_CACHE_HOME");
        if (xdg != null && !xdg.trim().isEmpty() && isAbsolute(xdg)) {
            candidates.add(new Candidate.Directory(join(xdg, "bal-library"), 0700));
        } else if (!environment.homedir().trim().isEmpty() && isAbsolute(environment.homedir())) {
            candidates.add(new Candidate.Directory(join(environment.homedir(), ".cache", "bal-library"), 0700));
        }

        if (!environment.tmpdir().trim().isEmpty() && isAbsolute(environment.tmpdir())) {
            candidates.add(new Candidate.Directory(
                    join(environment.tmpdir(), "bal-library-" + environment.user()), 0700));
        }

        if (candidates.isEmpty()) {
            return List.of(new Candidate.Disabled(
                    "no writable location: neither $HOME nor a temp directory is usable"));
        }
        return List.copyOf(candidates);
    }

    private static boolean isAbsolute(String path) {
        try {
            return Path.of(path).isAbsolute();
        } catch (RuntimeException ignored) {
            // A path the platform cannot even parse is not an absolute one.
            return false;
        }
    }

    private static String join(String first, String... rest) {
        return Path.of(first, rest).toString();
    }
}
