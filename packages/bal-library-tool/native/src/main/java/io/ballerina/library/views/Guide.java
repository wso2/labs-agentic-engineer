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

import io.ballerina.library.Failure;
import io.ballerina.library.LoadedPackage;
import io.ballerina.library.Result;
import io.ballerina.library.Texts;
import io.ballerina.library.render.Report;
import io.ballerina.library.symbols.Names;

import java.util.ArrayList;
import java.util.List;
import java.util.Locale;
import java.util.Optional;
import java.util.stream.Collectors;

/**
 * {@code guide <pkg> [n | "title"] [-s <q>] [--module <name>]} — the package's own readme, verbatim.
 *
 * <p>A VERB rather than a flag on {@code overview}: it is a distinct document that Central publishes, on the
 * package author's release clock, and everything in it is a quotation. ADR-0002 is verb-first with no implicit
 * default, and ADR-0017 split it out of the entry document for size — 44% of the entry document's bytes, 29% of
 * which was account setup a coding agent cannot act on.
 *
 * <p><b>CHUNK ADDRESSING, NOT A CODE-ONLY EXTRACT.</b> A design revision proposed an {@code examples} verb that
 * pulled out the fenced {@code ballerina} blocks alone, on the grounds that readme prose is setup narration.
 * Measured on {@code googleapis.sheets}, the readme is 178 lines with 4 code blocks, so code-only extraction
 * discards about 85% of it — and among what it discards is "If you intend to use the {@code deleteSpreadsheet}
 * operation, you must also enable the Google Drive API in the same project", which is not inferable from any
 * signature and is the difference between a connector that works and one that 403s on a single operation.
 *
 * <p>So a CHUNK is a readme section WITH its prose, addressed by number or by title. The whole readme is still one
 * argument away, and {@code overview} carries the chunk index so an agent knows a recipe exists before paying for
 * the document that holds it.
 *
 * @since 0.1.0
 */
public final class Guide {

    private Guide() {
    }

    /**
     * @param chunk a chunk number or a title fragment, or {@code null} for the whole readme
     * @param search the {@code -s} query over readme text, or {@code null}
     * @param module one module's readme (ADR-0014), or {@code null} for all of them
     */
    public record Options(String chunk, String search, String module) {

        public static final Options ALL = new Options(null, null, null);

        public Options(String module) {
            this(null, null, module);
        }

        public boolean filtered() {
            return search != null && !search.isBlank();
        }
    }

    /**
     * One addressable section of a readme.
     *
     * @param number the 1-based address a caller types
     * @param title the heading it sits under, or a generated one when the readme opens with prose
     * @param markdown the section verbatim, prose and code together
     */
    public record Chunk(int number, String title, String module, String markdown) {

        int lines() {
            return markdown.split("\n", -1).length;
        }
    }

    public static Result<String> render(LoadedPackage loaded, Options options) {
        String pkg = loaded.qualified().qualified();
        List<Readmes.ModuleReadme> readmes = options.module() == null
                ? loaded.readmes()
                : loaded.readmes().stream()
                        .filter(readme -> readme.module().equals(options.module()))
                        .toList();

        if (options.module() != null && readmes.isEmpty()) {
            return Result.err(new Failure.SymbolNotFound(
                    loaded.label(),
                    List.of(options.module()),
                    loaded.readmes().stream().map(Readmes.ModuleReadme::module).toList(),
                    "No module of this package publishes a guide under that name. The candidates are the "
                            + "modules that publish one; drop --module to read them all."));
        }

        List<Chunk> chunks = chunksOf(readmes);
        if (options.chunk() != null) {
            return oneChunk(loaded, chunks, options);
        }
        if (options.filtered()) {
            return Result.ok(searched(loaded, chunks, options));
        }
        return Result.ok(whole(loaded, pkg, readmes, chunks));
    }

    // -----------------------------------------------------------------------
    // Chunks
    // -----------------------------------------------------------------------

    /** Every addressable chunk of every module's readme, in Central's order. */
    public static List<Chunk> chunksOf(LoadedPackage loaded) {
        return chunksOf(loaded.readmes());
    }

    private static List<Chunk> chunksOf(List<Readmes.ModuleReadme> readmes) {
        List<Chunk> chunks = new ArrayList<>();
        for (Readmes.ModuleReadme readme : readmes) {
            for (Section section : sectionsOf(readme.markdown())) {
                chunks.add(new Chunk(chunks.size() + 1, section.title(), readme.module(), section.body()));
            }
        }
        return List.copyOf(chunks);
    }

    /** A readme section: the heading it sits under, and everything down to the next heading of that depth. */
    private record Section(String title, String body) { }

    /**
     * Split a readme on its headings, keeping only the sections that hold code.
     *
     * <p>A section with no fenced block is prose about the project — a badge row, a licence, a "report an issue"
     * paragraph — and addressing it would spend a number on something no caller will ask for. A section WITH one
     * keeps all of its prose, which is the point (see the class comment).
     *
     * <p>Fences are tracked so a {@code #} inside a code block cannot start a section: it is a shell comment or a
     * Ballerina doc comment, and treating it as a heading would cut a snippet in half.
     */
    private static List<Section> sectionsOf(String markdown) {
        List<Section> sections = new ArrayList<>();
        String title = null;
        List<String> body = new ArrayList<>();
        boolean inFence = false;

        for (String line : markdown.split("\n", -1)) {
            String stripped = line.stripLeading();
            if (stripped.startsWith("```") || stripped.startsWith("~~~")) {
                inFence = !inFence;
                body.add(line);
                continue;
            }
            if (!inFence && stripped.startsWith("#")) {
                flush(sections, title, body);
                title = stripped.replaceAll("^#+\\s*", "").trim();
                body = new ArrayList<>();
                body.add(line);
                continue;
            }
            body.add(line);
        }
        flush(sections, title, body);
        return List.copyOf(sections);
    }

    private static void flush(List<Section> sections, String title, List<String> body) {
        String text = String.join("\n", body).strip();
        if (text.isEmpty() || !text.contains("```")) {
            return;
        }
        sections.add(new Section(title == null ? "Introduction" : title, text));
    }

    /**
     * One chunk, by number or by title.
     *
     * <p>A number because that is what {@code overview}'s index prints, and a title because that is what an agent
     * reading the index will type when it means one thing rather than "the third one". Title matching goes through
     * {@link Names}' normalisation, so punctuation and casing in a heading are not a trap.
     */
    private static Result<String> oneChunk(LoadedPackage loaded, List<Chunk> chunks, Options options) {
        String pkg = loaded.qualified().qualified();
        if (chunks.isEmpty()) {
            return Result.err(new Failure.Validation(
                    loaded.label() + " publishes no guide chunk — no readme section carries code.",
                    "Read the readme whole: `bal library guide " + pkg + "`."));
        }
        Optional<Chunk> found = byNumber(chunks, options.chunk())
                .or(() -> byTitle(chunks, options.chunk()));
        if (found.isEmpty()) {
            return Result.err(new Failure.SymbolNotFound(
                    loaded.label(),
                    List.of(options.chunk()),
                    chunks.stream().map(chunk -> chunk.number() + ". " + chunk.title()).toList(),
                    "No chunk answers to that. The candidates are every chunk this guide publishes; pass one "
                            + "of the numbers, or read the readme whole with `bal library guide " + pkg + "`."));
        }

        Chunk chunk = found.get();
        Report report = new Report("guide");
        report.heading(1, pkg + " " + loaded.version().text() + " — guide chunk " + chunk.number());

        List<Report.Fact> facts = new ArrayList<>(Report.warning(loaded.warning()));
        facts.add(new Report.Fact("Chunk", chunk.number() + " of " + Texts.count(chunks.size()) + " — "
                + Texts.code(chunk.title())));
        facts.add(new Report.Fact("Size", Texts.count(chunk.lines()) + " lines, quoted verbatim"));
        report.facts(facts);
        report.paragraph(rule(pkg));

        report.heading(2, "Next");
        report.bullets(chunkNext(pkg, chunks, chunk));

        report.heading(2, "Chunk " + chunk.number() + " — " + chunk.title());
        report.embedded(pkg + " readme chunk " + chunk.number(),
                Readmes.demoteHeadings(chunk.markdown(), 2));
        return Result.ok(report.toString());
    }

    private static Optional<Chunk> byNumber(List<Chunk> chunks, String requested) {
        try {
            int number = Integer.parseInt(requested.trim());
            return chunks.stream().filter(chunk -> chunk.number() == number).findFirst();
        } catch (NumberFormatException ignored) {
            return Optional.empty();
        }
    }

    private static Optional<Chunk> byTitle(List<Chunk> chunks, String requested) {
        String wanted = Names.normalise(requested);
        if (wanted.isEmpty()) {
            return Optional.empty();
        }
        List<Chunk> exact = chunks.stream()
                .filter(chunk -> Names.normalise(chunk.title()).equals(wanted))
                .toList();
        if (exact.size() == 1) {
            return Optional.of(exact.get(0));
        }
        List<Chunk> partial = chunks.stream()
                .filter(chunk -> Names.normalise(chunk.title()).contains(wanted))
                .toList();
        return partial.size() == 1 ? Optional.of(partial.get(0)) : Optional.empty();
    }

    private static List<String> chunkNext(String pkg, List<Chunk> chunks, Chunk current) {
        List<String> next = new ArrayList<>();
        chunks.stream()
                .filter(chunk -> chunk.number() == current.number() + 1)
                .findFirst()
                .ifPresent(chunk -> next.add("the next chunk: "
                        + Texts.code("bal library guide " + pkg + " " + chunk.number())
                        + " — " + chunk.title()));
        next.add("the whole readme: " + Texts.code("bal library guide " + pkg) + " — "
                + Texts.count(chunks.size()) + " chunks and the prose between them");
        next.add("the generated signatures, which win where the two disagree: "
                + Texts.code("bal library overview " + pkg));
        return next;
    }

    // -----------------------------------------------------------------------
    // The whole readme
    // -----------------------------------------------------------------------

    private static String whole(
            LoadedPackage loaded, String pkg, List<Readmes.ModuleReadme> readmes, List<Chunk> chunks) {
        Report report = new Report("guide");
        report.heading(1, pkg + " " + loaded.version().text() + " — guide");

        List<Report.Fact> facts = new ArrayList<>(Report.warning(loaded.warning()));
        facts.add(new Report.Fact("Modules", readmes.isEmpty()
                ? "none — this package publishes no guide"
                : readmes.stream().map(readme -> Texts.code(readme.module()))
                        .collect(Collectors.joining(", "))));
        facts.add(new Report.Fact("Size", Texts.count(lineCount(readmes)) + " lines, quoted verbatim"));
        facts.add(new Report.Fact("Chunks", chunks.isEmpty()
                ? "none — no section of this readme carries code"
                : Texts.count(chunks.size()) + ", addressable one at a time"));
        report.facts(facts);

        // ADR-0013. The rule that a readme can be stale where a signature cannot is the sentence directly above
        // the readme, rather than a line of `--help` read some lookups before it mattered.
        report.paragraph("*The package's own readme, verbatim, with its headings demoted two levels. It is "
                + "Central's prose and can be out of date; the signatures the container verbs print are "
                + "generated from the package. Where the two disagree, the signature is what compiles.*");

        report.heading(2, "Next");
        List<String> next = new ArrayList<>();
        if (!chunks.isEmpty()) {
            next.add("one chunk at a time: " + Texts.code("bal library guide " + pkg + " <n>") + " — "
                    + chunks.stream().limit(4)
                            .map(chunk -> chunk.number() + ". " + chunk.title())
                            .collect(Collectors.joining(", "))
                    + (chunks.size() > 4 ? ", …" : ""));
        }
        next.add("search this text: " + Texts.code("bal library guide " + pkg + " -s \"<what you need>\""));
        next.add("the generated signatures: " + Texts.code("bal library overview " + pkg));
        report.bullets(next);

        if (readmes.isEmpty()) {
            report.heading(2, "No guide");
            report.paragraph("This package publishes none. " + Texts.code("bal library overview " + pkg)
                    + " is all Central holds.");
            return report.toString();
        }

        boolean single = readmes.size() == 1;
        for (Readmes.ModuleReadme readme : readmes) {
            report.heading(2, single ? "Guide" : "Guide — " + readme.module());
            String label = (single ? pkg : readme.module()) + " readme";
            report.embedded(label, Readmes.demoteHeadings(readme.markdown(), 2));
        }
        return report.toString();
    }

    // -----------------------------------------------------------------------
    // Search over the prose
    // -----------------------------------------------------------------------

    /**
     * {@code -s} over readme text.
     *
     * <p>Chunk-granular rather than line-granular, because a matching line out of its section is the failure the
     * whole design is against: "enable the Google Drive API in the same project" is only actionable with the
     * heading and the steps around it.
     */
    private static String searched(LoadedPackage loaded, List<Chunk> chunks, Options options) {
        String pkg = loaded.qualified().qualified();
        String needle = options.search().toLowerCase(Locale.ROOT);
        List<Chunk> matched = chunks.stream()
                .filter(chunk -> chunk.markdown().toLowerCase(Locale.ROOT).contains(needle)
                        || chunk.title().toLowerCase(Locale.ROOT).contains(needle))
                .toList();

        Report report = new Report("guide");
        report.heading(1, pkg + " " + loaded.version().text() + " — guide, "
                + Texts.code(options.search()));

        List<Report.Fact> facts = new ArrayList<>(Report.warning(loaded.warning()));
        facts.add(new Report.Fact("Filter", Texts.code(options.search()) + " — "
                + Texts.count(matched.size()) + " of " + Texts.count(chunks.size()) + " chunks"));
        report.facts(facts);

        report.heading(2, "Next");
        report.bullets(matched.isEmpty()
                ? List.of("read the readme whole: " + Texts.code("bal library guide " + pkg),
                        "or search the symbols instead: "
                                + Texts.code("bal library overview " + pkg + " -s \"" + options.search()
                                        + "\""))
                : List.of("open one: " + Texts.code("bal library guide " + pkg + " "
                        + matched.get(0).number()),
                        "read the readme whole: " + Texts.code("bal library guide " + pkg)));

        if (matched.isEmpty()) {
            report.heading(2, "No chunk mentions that");
            report.paragraph("Nothing in this readme's code-carrying sections contains "
                    + Texts.code(options.search()) + ".");
            return report.toString();
        }

        report.heading(2, Texts.count(matched.size()) + " matching chunks");
        report.bullets(matched.stream()
                .map(chunk -> chunk.number() + ". " + Texts.code(chunk.title()) + " — "
                        + Texts.count(chunk.lines()) + " lines · "
                        + Texts.code("bal library guide " + pkg + " " + chunk.number()))
                .toList());
        return report.toString();
    }

    /**
     * Where the quotation came from and which half wins, as one paragraph.
     *
     * <p>It used to carry a second paragraph naming the readme's own identifiers that this version no longer
     * declares. ADR-0024 removed the check behind it: {@code guide} reproduces the package's document, and a
     * name check is an analysis of its contents rather than a property of the reproduction. The sentence that
     * remains says the same thing at the altitude {@code guide} works at — Central's prose can be stale, and the
     * generated signatures win.
     */
    private static String rule(String pkg) {
        return "*Quoted verbatim from the package's own readme, headings demoted two levels. It is "
                + "Central's prose and can be out of date; " + Texts.code("bal library overview " + pkg)
                + " generates its signatures from the package, and those win where the two disagree.*";
    }

    private static int lineCount(List<Readmes.ModuleReadme> readmes) {
        return readmes.stream()
                .mapToInt(readme -> readme.markdown().split("\n", -1).length)
                .sum();
    }
}
