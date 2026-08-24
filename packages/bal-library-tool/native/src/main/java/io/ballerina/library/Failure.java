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

package io.ballerina.library;

import com.google.gson.JsonArray;
import com.google.gson.JsonObject;

import java.util.List;

/**
 * Failures are values, not thrown classes.
 *
 * <p>The question every failure has to answer is "what does the agent reading this do next". The
 * answer is carried by {@link #kind()} and {@link #describe()}, not by the exit code: a failing run
 * is exit 1 whatever went wrong (ADR-0015), so the discriminator has to be in the JSON. A sealed
 * hierarchy makes both switches exhaustive with no {@code default}, so a new failure mode fails the
 * build until someone names it and decides what it tells the caller.
 *
 * @since 0.1.0
 */
public sealed interface Failure {

    /** One place a payload stopped matching the schema. */
    record SchemaIssue(String path, String message) { }

    /** The caller's arguments are wrong — nothing upstream was contacted. */
    record Validation(String message, String suggestion) implements Failure { }

    /** Central has no such package, or no such version of it. */
    record PackageNotFound(String qualified, String suggestion) implements Failure { }

    /**
     * Central answered, but not usefully — a 4xx/5xx, a network error, a bad body. {@code status} is
     * {@code null} when the failure happened before a status line existed.
     */
    record Upstream(String url, int attempts, String message, String suggestion, Integer status)
            implements Failure { }

    /** Central did not answer inside the budget. */
    record Timeout(String url, long budgetMs, String suggestion) implements Failure { }

    /** Central answered with a shape this reader does not understand. */
    record SchemaDrift(String qualified, List<SchemaIssue> issues, String suggestion) implements Failure { }

    /**
     * The package parsed, but no declaration matched the name the caller asked for. {@code
     * candidates} is what the index does hold — either near-misses of the requested name, or the
     * whole roster when there were none.
     */
    record SymbolNotFound(String qualified, List<String> requested, List<String> candidates, String suggestion)
            implements Failure { }

    /**
     * The one route left when Central itself is the problem, and the reason the three failures below
     * can all end the same way: the {@code .bala} of a resolved version carries the same signatures
     * Central serves, so a lookup blocked at the network is not a lookup without an answer. It follows
     * a retry rather than replacing one because the tree only exists for a package some build already
     * pulled. Every suggestion that admits defeat has to close the door a blocked agent otherwise
     * walks through — writing the call from a remembered API — because that is the failure this tool
     * exists to prevent, and it is measurably what happens when nothing forbids it.
     */
    String OFFLINE_FALLBACK = "read the resolved version's sources under "
            + "`~/.ballerina/repositories/central.ballerina.io/bala/<org>/<name>/`, which exist if a "
            + "build already pulled the package — those are the signatures Central publishes. Never "
            + "fall back to a remembered signature.";

    /**
     * The suggestions for the three failures that fire when the outside world is the problem, kept
     * here rather than at each construction site because more than one module raises each of them and
     * an agent branching on {@code kind} should never see two different instructions for the same
     * condition.
     */
    String UPSTREAM_SUGGESTION = "Central answered badly. Run the same command once more; if it "
            + "persists, " + OFFLINE_FALLBACK;

    String TIMEOUT_SUGGESTION = "Central did not answer in time. Run the same command once more — a "
            + "large package is slow on a cold fetch. If it persists, " + OFFLINE_FALLBACK;

    /**
     * Addressed to a human on purpose: no argument the agent can change will make a payload this
     * reader cannot parse. The fallback still applies — a package Central mis-serves is intact on
     * disk — so reporting the drift and getting on with the work are not alternatives.
     */
    String SCHEMA_DRIFT_SUGGESTION = "Central's payload no longer matches this reader, so no change "
            + "of arguments will help. Report the `issues` paths, then " + OFFLINE_FALLBACK;

    /** The discriminator an agent branches on, and the JSON field of the same name. */
    default String kind() {
        return switch (this) {
            case Validation ignored -> "validation";
            case PackageNotFound ignored -> "package-not-found";
            case Upstream ignored -> "upstream";
            case Timeout ignored -> "timeout";
            case SchemaDrift ignored -> "schema-drift";
            case SymbolNotFound ignored -> "symbol-not-found";
        };
    }

    /**
     * The one line a failing run writes to stderr. Kept as a single JSON object so an agent can read
     * it without a parser and a human can read it without tools; {@code kind} is the field worth
     * branching on.
     */
    default String describe() {
        JsonObject json = new JsonObject();
        json.addProperty("kind", kind());
        switch (this) {
            case Validation f -> {
                json.addProperty("message", f.message());
                json.addProperty("suggestion", f.suggestion());
            }
            case PackageNotFound f -> {
                json.addProperty("qualified", f.qualified());
                json.addProperty("suggestion", f.suggestion());
            }
            case Upstream f -> {
                json.addProperty("url", f.url());
                json.addProperty("attempts", f.attempts());
                json.addProperty("message", f.message());
                json.addProperty("suggestion", f.suggestion());
                if (f.status() != null) {
                    json.addProperty("status", f.status());
                }
            }
            case Timeout f -> {
                json.addProperty("url", f.url());
                json.addProperty("budgetMs", f.budgetMs());
                json.addProperty("suggestion", f.suggestion());
            }
            case SchemaDrift f -> {
                json.addProperty("qualified", f.qualified());
                JsonArray issues = new JsonArray();
                for (SchemaIssue issue : f.issues()) {
                    JsonObject entry = new JsonObject();
                    entry.addProperty("path", issue.path());
                    entry.addProperty("message", issue.message());
                    issues.add(entry);
                }
                json.add("issues", issues);
                json.addProperty("suggestion", f.suggestion());
            }
            case SymbolNotFound f -> {
                json.addProperty("qualified", f.qualified());
                json.add("requested", strings(f.requested()));
                json.add("candidates", strings(f.candidates()));
                json.addProperty("suggestion", f.suggestion());
            }
        }
        return json.toString();
    }

    private static JsonArray strings(List<String> values) {
        JsonArray array = new JsonArray();
        values.forEach(array::add);
        return array;
    }
}
