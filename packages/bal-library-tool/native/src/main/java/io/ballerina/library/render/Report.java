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

package io.ballerina.library.render;

import java.util.ArrayList;
import java.util.List;
import java.util.stream.Collectors;

/**
 * The report register: Markdown that DESCRIBES a package.
 *
 * <p>A document either IS Ballerina or it describes a package, and blending the two produces things like a
 * {@code client class} shell whose body is {@code // WARNING: 903 resource functions} — a document that
 * looks like a declaration, is not one, and invites a reader to transcribe from it or to reason about what
 * "the file" contains. An agent that mistakes a summary for source writes code against it.
 *
 * <p>So the rules are mechanical, and {@code RegisterTest} enforces them:
 *
 * <ul>
 *   <li>Ballerina appears ONLY inside a fenced {@code ```ballerina} block. A fence is unambiguously a
 *       quotation, so a signature stays copyable truth while the surrounding document cannot be mistaken
 *       for source.
 *   <li>{@code //} annotations are illegal here. In the code register they annotate real declarations,
 *       which is what {@code // Special Agent Note:} does; here they were the thing doing the
 *       impersonating, and Markdown prose replaces them.
 *   <li>Structure is headings, so {@code grep '^## '} returns the document's sections.
 *   <li>Ballerina doc comments ({@code #}) stay INSIDE the fences, because they are the language's own doc
 *       syntax and belong to the quoted declaration.
 * </ul>
 *
 * <p>Every view builds its output through this class rather than hand-rolling a heading, which is the only
 * reason those rules hold for documents nobody has written yet.
 *
 * @since 0.1.0
 */
public final class Report {

    /**
     * The on-disk format generation of a report document, and a skew detector.
     *
     * <p>New verbs and flags fail loudly under skew, but flipping the DEFAULT document is silent in both
     * directions: a caller expecting Markdown that greps {@code client class} in a Ballerina document
     * matches nothing and then reads an empty range at exit 0. An HTML comment on the first line makes
     * that detectable rather than silent: it survives {@code grep}, every Markdown reader renders it as
     * nothing, and it cannot appear in the Ballerina document the code register prints.
     */
    private static final String REPORT_FORMAT = "v1";

    private final List<String> blocks = new ArrayList<>();

    public Report(String verb) {
        blocks.add("<!-- bal library " + verb + " " + REPORT_FORMAT + " -->");
    }

    /** A left-hand label and its value, for {@link #facts}. */
    public record Fact(String label, String value) { }

    /**
     * A load warning as the FIRST fact of a table, or no rows at all when there is nothing wrong.
     *
     * <p>Every report opens with a facts table and every one of them can be built from an unverified version,
     * so the null rule and the row's position live here rather than six times over. First, because a warning
     * a reader meets after the answer has already been read is not a warning.
     */
    public static List<Fact> warning(String warning) {
        return warning == null ? List.of() : List.of(new Fact("Warning", warning));
    }

    public Report heading(int level, String text) {
        blocks.add("#".repeat(Math.min(6, Math.max(1, level))) + " " + text);
        return this;
    }

    public Report paragraph(String text) {
        if (!text.trim().isEmpty()) {
            blocks.add(text);
        }
        return this;
    }

    /**
     * A two-column facts table. Headerless on purpose — the left column IS the label, and a {@code | | |}
     * header row is how GitHub-flavoured Markdown spells that.
     */
    public Report facts(List<Fact> rows) {
        if (rows.isEmpty()) {
            return this;
        }
        List<String> lines = new ArrayList<>(List.of("| | |", "|---|---|"));
        for (Fact row : rows) {
            lines.add("| " + row.label() + " | " + row.value() + " |");
        }
        blocks.add(String.join("\n", lines));
        return this;
    }

    public Report bullets(List<String> items) {
        if (!items.isEmpty()) {
            blocks.add(items.stream().map(item -> "- " + item).collect(Collectors.joining("\n")));
        }
        return this;
    }

    /**
     * Quoted Ballerina. The ONLY way this class emits the language, and the reason a report can carry
     * copyable signatures without looking like source.
     */
    public Report ballerina(List<String> declarations) {
        String body = fenceBody(declarations);
        if (body.trim().isEmpty()) {
            return this;
        }
        blocks.add("```ballerina\n" + body + "\n```");
        return this;
    }

    /**
     * The exact bytes {@link #ballerina} will put inside the fence.
     *
     * <p>Exists so a header can size a block it has not built yet without re-deriving the separator rule. A
     * view that computed its own total was short by one byte per declaration, because it counted one newline
     * each where this class writes a blank line between multi-line entries.
     */
    public static int ballerinaBytes(List<String> declarations) {
        return fenceBody(declarations).getBytes(java.nio.charset.StandardCharsets.UTF_8).length;
    }

    private static String fenceBody(List<String> declarations) {
        // A blank line between entries as soon as any of them is multi-line, which is what a doc comment
        // makes them: `# Get a repository` followed straight by the next declaration's comment reads as one
        // four-line comment on one function. The API document separates declarations the same way for the
        // same reason.
        String separator = declarations.stream().anyMatch(entry -> entry.contains("\n")) ? "\n\n" : "\n";
        return String.join(separator, declarations);
    }

    /**
     * Quoted plain text — a path tree, a segment listing. Unfenced it would be prose; fenced as
     * {@code ballerina} it would be a lie about what it is; so it is a bare fence, which is neither.
     */
    public Report literal(List<String> lines) {
        String body = String.join("\n", lines);
        if (body.trim().isEmpty()) {
            return this;
        }
        blocks.add("```\n" + body + "\n```");
        return this;
    }

    /** Opens the quotation. The label names whose Markdown follows. */
    public static final String EMBED_BEGIN = "<!-- guide: begin ";

    /** Closes it, carrying the same label, so the two can be matched rather than counted. */
    public static final String EMBED_END = "<!-- guide: end ";

    /**
     * Markdown from somewhere else, embedded verbatim between a begin and an end marker.
     *
     * <p>A paragraph is this document's claim; an embedding is a quotation whose blank-line runs and heading
     * depth are its author's business. Demoting the quoted headings keeps ONE outline, but demotion alone is
     * invisible: a reader landing mid-document meets Markdown that looks like this document's and has no way
     * to tell where the tool stopped speaking. The markers say it — and they say it in the same vocabulary as
     * the format marker on line one, so they survive {@code grep}, render as nothing, and leave the quotation
     * byte-for-byte copyable, which a fence around a document that carries its own fences would not.
     *
     * <p>{@code RegisterTest} cuts on them when it stops checking this document's structure at the guide, so
     * the boundary the reader sees and the boundary the tests trust are the same one.
     */
    public Report embedded(String label, String markdown) {
        if (markdown.trim().isEmpty()) {
            return this;
        }
        blocks.add(EMBED_BEGIN + label + " -->");
        blocks.add(markdown);
        blocks.add(EMBED_END + label + " -->");
        return this;
    }

    /**
     * Blocks separated by one blank line, with exactly one trailing newline. The format marker is joined
     * directly to the title instead, so the document opens on its heading for a human reader.
     */
    @Override
    public String toString() {
        String marker = blocks.isEmpty() ? "" : blocks.get(0);
        String rest = String.join("\n\n", blocks.subList(Math.min(1, blocks.size()), blocks.size()));
        return marker + "\n" + rest + "\n";
    }

    /**
     * Lay names out in aligned columns.
     *
     * <p>A 36-entry path tree is 445 bytes in four columns and about 1.3KB one per line, and the
     * four-column form is also the one a reader can scan for the segment they want. Column width comes
     * from the longest entry so nothing is truncated — truncating a name would make it unusable as the
     * next command's argument.
     */
    public static List<String> columns(List<String> entries) {
        return columns(entries, 4);
    }

    private static List<String> columns(List<String> entries, int perRow) {
        if (entries.isEmpty()) {
            return List.of();
        }
        int width = entries.stream().mapToInt(String::length).max().orElse(0);
        List<String> rows = new ArrayList<>();
        for (int start = 0; start < entries.size(); start += perRow) {
            List<String> row = entries.subList(start, Math.min(start + perRow, entries.size()));
            StringBuilder line = new StringBuilder();
            for (int index = 0; index < row.size(); index++) {
                String entry = row.get(index);
                line.append(index == row.size() - 1 ? entry : pad(entry, width + 1));
                if (index < row.size() - 1) {
                    line.append(' ');
                }
            }
            rows.add(stripTrailing(line.toString()));
        }
        return List.copyOf(rows);
    }

    private static String pad(String value, int width) {
        return value.length() >= width ? value : value + " ".repeat(width - value.length());
    }

    /** {@code String::stripTrailing} also strips Unicode spaces; this matches JS {@code trimEnd}. */
    private static String stripTrailing(String value) {
        int end = value.length();
        while (end > 0 && Character.isWhitespace(value.charAt(end - 1))) {
            end--;
        }
        return value.substring(0, end);
    }
}
