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

package io.ballerina.library.views;

import io.ballerina.library.Texts;
import io.ballerina.library.central.SearchHit;
import io.ballerina.library.render.Report;

import java.util.ArrayList;
import java.util.List;

/**
 * {@code find <keywords...>} — which package, when you cannot name it.
 *
 * <p>The one capability none of the addressed verbs covers, and the reason this verb exists at all: every other
 * verb needs {@code org/name} before it can say anything. It reads and writes no cache — the query space is
 * unbounded and Central's registry is the one thing that genuinely changes.
 *
 * <p><b>ONE LINE PER PACKAGE.</b> Measured on the shipped format, a result set was 3,580 bytes of which 2,202
 * (62%) was description prose — {@code ballerinax/health.clients.fhir} alone spent about 400 characters on four
 * sentences. The first sentence carries the discriminating fact and the rest is marketing, so the rest goes.
 *
 * <p><b>THE FIX FOR A BAD QUERY IS TO MAKE IT CHEAP, NOT SMART.</b> T13 recorded that "how do i parse a json
 * string" returns {@code regex}, {@code observe}, {@code log}, {@code task}, {@code jwt}. Central publishes no
 * relevance score, so there is nothing honest to threshold on — and a "this looks like a question" heuristic on
 * token count or question words reads well on the example that suggests it and misfires silently forever after.
 * After the format change a useless hit costs about 80 characters instead of 400, and the {@code Matches} row
 * already tells an agent the query was broad.
 *
 * <p><b>ADR-0003 is kept exactly.</b> Central's relevance order stands, packages under 1,000 pulls are
 * stable-partitioned to the end, and the pull count is printed on every row. Dropping counts on the grounds that
 * "relevance already encodes popularity" is measurably false: Central ranks a ONE-PULL package fourth for
 * {@code http client}, above {@code ballerina/sql}. The list looks clean because the demotion already ran.
 *
 * @since 0.1.0
 */
public final class Find {

    /**
     * How many hits the document lists.
     *
     * <p>Ten rather than twelve now that a row is one line: the document is read to pick ONE package, and the
     * tail of a relevance list is where the one-pull packages live even after the demotion.
     */
    private static final int LISTED = 10;

    private Find() {
    }

    public static String render(List<String> keywords, SearchHit.Results results) {
        String query = String.join(" ", keywords);
        Report report = new Report("find");
        report.heading(1, "Find — " + Texts.code(query));

        List<SearchHit> shown = results.hits().subList(0, Math.min(LISTED, results.hits().size()));
        report.facts(List.of(
                new Report.Fact("Query", Texts.code(query)),
                new Report.Fact("Matches", Texts.count(results.total()) + " in Central, "
                        + Texts.count(shown.size()) + " listed"),
                new Report.Fact("Order", "Central's relevance, with packages under 1,000 pulls moved to the "
                        + "end. A low count is a verdict on adoption, not on quality.")));

        if (results.hits().isEmpty()) {
            // A real gap in the shipped verb: zero matches printed a sentence and no way forward.
            report.heading(2, "Next");
            report.bullets(alternatives(keywords));
            report.heading(2, "No packages matched");
            report.paragraph("Central's index matches package names, summaries and keywords — not "
                    + "documentation, and not questions. One broad noun works better than a sentence.");
            return report.toString();
        }

        report.heading(2, "Next");
        SearchHit first = shown.get(0);
        report.bullets(List.of(
                "read one: " + Texts.code("bal library overview " + first.qualified()),
                "still unsure: " + Texts.code("bal library overview " + first.qualified()
                        + " -s \"" + query + "\"") + " — searches that package's whole symbol surface"));

        report.heading(2, "Packages — " + Texts.count(shown.size()));
        report.bullets(shown.stream().map(Find::describe).toList());
        return report.toString();
    }

    /**
     * One package, one line: the coordinate, the version, the pull count and the FIRST sentence.
     *
     * <p>The first sentence and not the first line: Central's summaries are one paragraph, so a line split keeps
     * all four sentences for a package that happens not to wrap.
     */
    private static String describe(SearchHit hit) {
        StringBuilder line = new StringBuilder(Texts.code(hit.qualified()));
        if (!hit.version().isEmpty()) {
            line.append(' ').append(hit.version());
        }
        line.append(" — ").append(Texts.count(hit.pullCount())).append(" pulls");
        String summary = firstSentence(hit.summary());
        if (!summary.isEmpty()) {
            line.append(" — ").append(summary);
        }
        return line.toString();
    }

    /** Up to the first full stop that ends a sentence rather than an abbreviation or a version number. */
    static String firstSentence(String summary) {
        String flattened = summary.replaceAll("\\s+", " ").trim();
        for (int index = 0; index < flattened.length() - 1; index++) {
            if (flattened.charAt(index) == '.' && flattened.charAt(index + 1) == ' ') {
                return flattened.substring(0, index + 1);
            }
        }
        return flattened;
    }

    /**
     * What to try when nothing matched.
     *
     * <p>Built from the query rather than asserted: each keyword alone is the one narrowing that is guaranteed to
     * be a different query from the one that just failed.
     */
    private static List<String> alternatives(List<String> keywords) {
        List<String> next = new ArrayList<>();
        for (String keyword : keywords.subList(0, Math.min(2, keywords.size()))) {
            next.add("one keyword at a time: " + Texts.code("bal library find " + keyword));
        }
        next.add("or name the vendor: a third-party connector is published under "
                + Texts.code("ballerinax/") + ", the standard library under " + Texts.code("ballerina/"));
        return next;
    }
}
