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

import io.ballerina.library.LoadedPackage;
import io.ballerina.library.Texts;
import io.ballerina.library.model.Fn;
import io.ballerina.library.model.Library;
import io.ballerina.library.model.TypeDef;
import io.ballerina.library.render.Report;
import io.ballerina.library.symbols.Filter;
import io.ballerina.library.symbols.Surface;

import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.stream.Collectors;

/**
 * {@code overview <pkg> [-s <q>]} — a bounded MAP of the package, and the entry document.
 *
 * <p><b>NO GENERATED SIGNATURES.</b> This is the largest behavioural change in the redesign and it is chosen
 * deliberately over a byte cap. A cap still lets {@code ballerina/crypto} emit 20,000 bytes before it degrades; a
 * map is bounded BY CONSTRUCTION, so crypto goes from 1,177 lines to roughly twenty and the failure where an eval
 * harness silently substituted a 2.2KB stub disappears rather than becoming rarer.
 *
 * <p>The cost is honest and worth naming: {@code googleapis.sheets} goes from one call to two, because its
 * 208-line overview carried all 43 remote signatures. That is why the 20,000-byte signature budget moved onto the
 * container verbs — the second call returns all 43 in full.
 *
 * <p>What remains is what a map is for: the counts, the readme's quickstart, the guide's chunk index, where to go
 * next, and a roster whose every row ends in the command that opens it. A roster row that dead-ends is the one
 * shape this document never ships (ADR-0019) — {@code overview}'s old unconditional {@code ops <pkg> <path>}
 * pointer answered "none in any client" on {@code ballerinax/aws.s3} and pointed back here, a two-call loop
 * carrying no information.
 *
 * <p><b>ORDERED TO SURVIVE A PIPE</b> (ADR-0017, unchanged and strengthened). 80% of recorded lookups are piped
 * through a filter, and putting {@code ## Next} before the bulk took {@code head -100} reaching it from 0 of 11
 * packages to 11 of 11.
 *
 * <p>{@code -s} makes this the CROSS-KIND search — the "which symbol owns this capability?" question that no
 * kind-specific verb can answer, since the caller does not yet know the kind.
 *
 * @since 0.1.0
 */
public final class Overview {

    /**
     * How many roster rows the map prints before it prints a count instead.
     *
     * <p>{@code ballerina/http} declares 91 classes and {@code ballerinax/postgresql} 126. A roster is read to
     * CHOOSE one, and a list nobody can hold is not a choice — the search line beside it is.
     */
    public static final int MAX_ROSTER_ROWS = 20;

    /** How many results a cross-kind search lists before it names the count and the narrowing. */
    public static final int MAX_SEARCH_ROWS = 30;

    /** The one qualifier a type alias may carry only if it is an error type. */
    private static final String DISTINCT = "distinct ";

    /** How much of a facts row the error names may take before the command replaces them. */
    private static final int MAX_ERROR_NAMES_CHARS = 300;

    private Overview() {
    }

    /** @param search the {@code -s} query, or {@code null} for the map */
    public record Options(String search) {

        public static final Options MAP = new Options(null);

        public boolean filtered() {
            return search != null && !search.isBlank();
        }
    }

    public static String render(LoadedPackage loaded) {
        return render(loaded, Options.MAP);
    }

    public static String render(LoadedPackage loaded, Options options) {
        return options.filtered() ? searched(loaded, options) : map(loaded);
    }

    // -----------------------------------------------------------------------
    // The map
    // -----------------------------------------------------------------------

    private static String map(LoadedPackage loaded) {
        String pkg = loaded.qualified().qualified();
        Library library = loaded.library();

        Report report = new Report("overview");
        report.heading(1, pkg + " " + loaded.version().text());
        report.facts(factsOf(loaded, library, pkg));

        chunkIndex(report, loaded, pkg);

        report.heading(2, "Next");
        report.bullets(nextBullets(library, pkg));

        roster(report, "Clients", Surface.Scope.CLIENT, library, pkg);
        roster(report, "Classes and object types", Surface.Scope.CLASS, library, pkg);
        roster(report, "Module-level functions", Surface.Scope.MODULE, library, pkg);
        // LAST, and that is the whole reason the section can be unbounded. ADR-0024 took the cap off the quoted
        // code, so this is the one part of the map whose length the package decides; everything a reader
        // navigates by — the facts, the chunk index, `## Next`, the rosters — is already behind them by here, so
        // a `head -100` loses trailing examples rather than the map.
        quickstart(report, loaded, pkg);
        return report.toString();
    }

    private static List<Report.Fact> factsOf(LoadedPackage loaded, Library library, String pkg) {
        List<Report.Fact> facts = new ArrayList<>(Report.warning(loaded.warning()));
        facts.add(new Report.Fact("Clients", describeScope(library, Surface.Scope.CLIENT, pkg)));
        facts.add(new Report.Fact("Classes", describeScope(library, Surface.Scope.CLASS, pkg)));
        facts.add(new Report.Fact("Module functions", describeScope(library, Surface.Scope.MODULE, pkg)));
        facts.add(new Report.Fact("Errors", describeErrors(errorsOf(library), pkg)));
        facts.add(new Report.Fact("Types", describeTypes(library.declarations())));
        facts.add(new Report.Fact("Guide", describeGuide(loaded, pkg)));
        return facts;
    }

    /**
     * One scope, counted and pointed at.
     *
     * <p>Read off {@link Surface} rather than off Central's {@code clients} array, which is not the callable
     * surface: SQL-03 recorded the entry document asserting "none — the callable surface is module-level
     * functions" for {@code ballerina/sql}, which has two clients and whose own readme quotes
     * {@code dbClient->query(query)} a few lines further down. An assertion ends a search where an omission would
     * only have invited a follow-up.
     */
    private static String describeScope(Library library, Surface.Scope scope, String pkg) {
        List<Surface.Container> containers = Surface.of(library, scope);
        if (containers.isEmpty()) {
            return "none";
        }
        if (scope == Surface.Scope.MODULE) {
            return Texts.count(containers.get(0).functions().size()) + " — "
                    + Texts.code("bal library funcs " + pkg);
        }
        String named = containers.stream().map(Surface.Container::name).map(Texts::code)
                .collect(Collectors.joining(", "));
        String command = Texts.code("bal library " + scope.verb() + " " + pkg);
        return named.length() <= MAX_ERROR_NAMES_CHARS
                ? Texts.count(containers.size()) + " — " + named + " — " + command
                : Texts.count(containers.size()) + ", too many to name here — " + command;
    }

    /**
     * The error declarations, NAMED on the row rather than listed in a section.
     *
     * <p>The names could not leave with the section: {@code BucketAlreadyOwnedByYouError} and
     * {@code InvalidRangeError} are not guessable, and a row reading "6, listed below" with nothing below names a
     * fact and withholds the one token needed to reach it. {@code ballerina/http}'s 65 are the only case in the
     * corpus that does not fit a line.
     */
    private static String describeErrors(List<TypeDef> errors, String pkg) {
        if (errors.isEmpty()) {
            return "none declared here; each operation names its error type in its `returns` clause";
        }
        String read = "read one with " + Texts.code("bal library type " + pkg + " <Name>");
        String named = errors.stream().map(TypeDef::name).map(Texts::code)
                .collect(Collectors.joining(", "));
        return named.length() <= MAX_ERROR_NAMES_CHARS
                ? Texts.count(errors.size()) + " — " + named + " — " + read
                : Texts.count(errors.size()) + ", too many to name here — " + read;
    }

    private static String describeGuide(LoadedPackage loaded, String pkg) {
        int lines = loaded.readmes().stream()
                .mapToInt(readme -> readme.markdown().split("\n", -1).length)
                .sum();
        return lines == 0
                ? "none published"
                : Texts.count(lines) + " lines — " + Texts.code("bal library guide " + pkg);
    }

    /**
     * How the declarations break down, for the one line that says they are not here.
     *
     * <p>Errors are excluded from the total because they ARE named, on their own row; counting them under "not
     * listed here" would make the line false by exactly the number of errors. SHEETS-04 and GMAIL-05: enums,
     * constants and objects each get their own count — folded into "other" they were invisible, so the entry
     * document never said a package HAS enums, and an enum is the one declaration whose members cannot be
     * guessed.
     */
    private static String describeTypes(List<TypeDef> typeDefs) {
        List<TypeDef> kinds = typeDefs.stream()
                .filter(typeDef -> !isErrorDeclaration(typeDef))
                .toList();
        if (kinds.isEmpty()) {
            return "none declared";
        }
        Map<String, Long> counts = new LinkedHashMap<>();
        counts.put("records", kinds.stream().filter(TypeDef.Rec.class::isInstance).count());
        // "type aliases" and not "unions": the count covers all seventeen of Central's alias categories, and a
        // union is one of them.
        counts.put("type aliases", kinds.stream().filter(TypeDef.Alias.class::isInstance).count());
        counts.put("enums", kinds.stream().filter(TypeDef.Enumeration.class::isInstance).count());
        counts.put("constants", kinds.stream().filter(TypeDef.Constant.class::isInstance).count());
        counts.put("classes and object types", kinds.stream().filter(TypeDef.ObjectDef.class::isInstance)
                .count());
        counts.put("module-level variables", kinds.stream().filter(TypeDef.Variable.class::isInstance).count());
        long other = kinds.size() - counts.values().stream().mapToLong(Long::longValue).sum();
        counts.put("other", other);

        List<String> parts = new ArrayList<>();
        counts.forEach((label, count) -> {
            if (count > 0) {
                parts.add(Texts.count(count) + " " + label);
            }
        });
        return Texts.count(kinds.size()) + " declarations (" + String.join(", ", parts)
                + "), not listed here — read one with `type`";
    }

    /**
     * The error declarations, whichever array Central filed them in.
     *
     * <p>HTTP-11. Filtering {@link TypeDef.ErrorDef} alone found 56 of {@code ballerina/http}'s 65 public error
     * declarations, and the nine it missed were disproportionately the LISTENER-side ones, published under an
     * alias category rather than under {@code errors}. The signal is {@code distinct}, and it is exact rather
     * than a heuristic: a type alias may be {@code distinct} only if it is an error type or an object type, and
     * an object type is an {@link TypeDef.ObjectDef} by the time it reaches here.
     */
    private static boolean isErrorDeclaration(TypeDef typeDef) {
        return typeDef instanceof TypeDef.ErrorDef
                || (typeDef instanceof TypeDef.Alias alias && alias.type().name().startsWith(DISTINCT));
    }

    private static List<TypeDef> errorsOf(Library library) {
        return library.declarations().stream().filter(Overview::isErrorDeclaration).toList();
    }

    // -----------------------------------------------------------------------
    // Worked code
    // -----------------------------------------------------------------------

    /**
     * The readme's Ballerina code, quoted whole. {@link Snippets} decides what counts as Ballerina.
     *
     * <p>Marked as a quotation with begin/end markers because it is one: these are the package author's bytes,
     * not this document's claim, and the oracle that checks every signature this view prints has to cut them out
     * by structure rather than by recognising a heading.
     */
    private static void quickstart(Report report, LoadedPackage loaded, String pkg) {
        List<String> blocks = Snippets.select(loaded);
        if (blocks.isEmpty()) {
            return;
        }
        report.heading(2, "Quickstart");
        // ADR-0013 asked for the rules a reader needs at the moment they read quoted code. ADR-0024 cut that to
        // two, because the third — what a mark on a line means — described a check that no longer runs.
        report.paragraph("*Every Ballerina block in the package's own readme, quoted verbatim and in its "
                + "order. It is Central's text and can be out of date; the signatures the container verbs "
                + "generate win wherever the two disagree.*");
        report.embedded(pkg + " readme usage", blocks.stream()
                .map(block -> "```ballerina\n" + block + "\n```")
                .collect(Collectors.joining("\n\n")));
    }

    /**
     * The guide's chunks, one line.
     *
     * <p>A chunk index rather than the chunks: this document is a map, and the map's job here is to say that a
     * recipe for the thing you want exists and what to type to read it.
     */
    private static void chunkIndex(Report report, LoadedPackage loaded, String pkg) {
        List<Guide.Chunk> chunks = Guide.chunksOf(loaded);
        if (chunks.isEmpty()) {
            return;
        }
        report.paragraph("Guide chunks (" + Texts.count(chunks.size()) + "): "
                + chunks.stream()
                        .map(chunk -> chunk.number() + ". " + Texts.code(chunk.title()))
                        .collect(Collectors.joining("  "))
                + " — " + Texts.code("bal library guide " + pkg + " <n>"));
    }

    // -----------------------------------------------------------------------
    // Next, and the rosters
    // -----------------------------------------------------------------------

    /**
     * Where to go, DERIVED from what the package declares rather than asserted.
     *
     * <p>ADR-0019: a pointer that cannot answer is worse than no pointer. The old bullet said {@code <path>}
     * unconditionally, and an agent following it on {@code ballerinax/aws.s3} spent a call to arrive back here.
     */
    private static List<String> nextBullets(Library library, String pkg) {
        List<String> next = new ArrayList<>();
        for (Surface.Scope scope : Surface.Scope.values()) {
            List<Surface.Container> containers = Surface.of(library, scope);
            if (containers.isEmpty()) {
                continue;
            }
            String argument = scope == Surface.Scope.MODULE || containers.size() > 1
                    ? ""
                    : " " + containers.get(0).name();
            next.add(Texts.code("bal library " + scope.verb() + " " + pkg + argument) + " — "
                    + prose(scope, containers));
        }
        next.add(Texts.code("bal library overview " + pkg + " -s \"<what you need>\"")
                + " — search every kind at once when you do not know which verb holds it");
        next.add(Texts.code("bal library type " + pkg + " <Name> [-r]")
                + " — a declaration whole, with the types it names");
        return next;
    }

    private static String prose(Surface.Scope scope, List<Surface.Container> containers) {
        return switch (scope) {
            case CLIENT -> containers.size() == 1
                    ? "the client's whole callable surface"
                    : Texts.count(containers.size()) + " clients, called with `->`";
            case CLASS -> Texts.count(containers.size()) + " classes and object types, called with `.`";
            case MODULE -> Texts.count(containers.get(0).functions().size())
                    + " functions callable without a client";
        };
    }

    private static void roster(
            Report report, String label, Surface.Scope scope, Library library, String pkg) {
        List<Surface.Container> containers = Surface.of(library, scope);
        if (containers.isEmpty()) {
            return;
        }
        if (scope == Surface.Scope.MODULE) {
            report.heading(2, label + " — " + Texts.count(containers.get(0).functions().size())
                    + ", call with " + Texts.code("."));
            report.literal(Report.columns(containers.get(0).standalone().stream()
                    .map(Fn.Standalone::name).toList()));
            return;
        }
        report.heading(2, label + " — " + Texts.count(containers.size()));
        List<Surface.Container> shown = containers.subList(0, Math.min(MAX_ROSTER_ROWS, containers.size()));
        report.bullets(shown.stream()
                .map(container -> Texts.code(container.name()) + " — " + counts(container) + " · "
                        + Texts.code("bal library " + scope.verb() + " " + pkg + " " + container.name()))
                .toList());
        if (shown.size() < containers.size()) {
            report.paragraph(Texts.count(containers.size() - shown.size()) + " more, not listed — "
                    + Texts.code("bal library " + scope.verb() + " " + pkg + " -s \"<what it does>\"")
                    + " searches all of them at once.");
        }
    }

    private static String counts(Surface.Container container) {
        List<String> parts = new ArrayList<>();
        int resources = container.operations().size();
        long remote = container.standalone().stream().filter(Fn.Remote.class::isInstance).count();
        long normal = container.standalone().stream().filter(Fn.Normal.class::isInstance).count();
        if (resources > 0) {
            parts.add(Texts.count(resources) + " resource");
        }
        if (remote > 0) {
            parts.add(Texts.count(remote) + " remote");
        }
        if (normal > 0) {
            parts.add(Texts.count(normal) + " normal");
        }
        return parts.isEmpty() ? "nothing callable" : String.join(", ", parts);
    }

    // -----------------------------------------------------------------------
    // The cross-kind search
    // -----------------------------------------------------------------------

    /** One thing a cross-kind search found: what it is, who owns it, and the command that opens it. */
    private record Hit(String kind, String label, String owner, String command) {

        String row() {
            return Texts.code(label) + " — " + kind + (owner.isEmpty() ? "" : " on " + Texts.code(owner))
                    + " · " + Texts.code(command);
        }
    }

    /**
     * {@code -s} across every kind at once.
     *
     * <p>This is the question no kind-specific verb can answer, because the caller does not yet know the kind —
     * and it is why {@code overview} takes the flag at all. Rows carry the kind, the owning symbol and the
     * runnable command, so one call turns "which symbol does X?" into an addressed follow-up.
     */
    private static String searched(LoadedPackage loaded, Options options) {
        String pkg = loaded.qualified().qualified();
        Library library = loaded.library();
        List<Hit> surface = new ArrayList<>();
        List<String> documented = new ArrayList<>();

        for (Surface.Scope scope : Surface.Scope.values()) {
            for (Surface.Container container : Surface.of(library, scope)) {
                Filter.Split<Fn> split = Filter.apply(options.search(), container.functions(),
                        Filter::surfaceOf, Filter::docsOf);
                split.surface().forEach(fn -> surface.add(callableHit(pkg, scope, container, fn)));
                split.documented().forEach(fn -> documented.add(nameOf(fn)));
            }
        }
        Filter.Split<TypeDef> types = Filter.apply(options.search(), library.declarations(),
                Filter::surfaceOf, Filter::docsOf);
        types.surface().forEach(typeDef -> surface.add(new Hit(kindOf(typeDef), typeDef.name(), "",
                "bal library type " + pkg + " " + typeDef.name())));
        types.documented().forEach(typeDef -> documented.add(typeDef.name()));

        List<Guide.Chunk> chunks = Guide.chunksOf(loaded).stream()
                .filter(chunk -> chunk.title().toLowerCase(Locale.ROOT)
                        .contains(options.search().toLowerCase(Locale.ROOT)))
                .toList();
        chunks.forEach(chunk -> surface.add(new Hit("guide chunk", chunk.title(), "",
                "bal library guide " + pkg + " " + chunk.number())));

        Report report = new Report("overview");
        report.heading(1, pkg + " " + loaded.version().text() + " — " + Texts.code(options.search()));

        List<Report.Fact> facts = new ArrayList<>(Report.warning(loaded.warning()));
        facts.add(new Report.Fact("Filter", Texts.code(options.search()) + " — "
                + Texts.count(surface.size()) + " by name or type, " + Texts.count(documented.size())
                + " more by documentation only"));
        facts.add(new Report.Fact("Scope", "every kind: clients, classes, module functions, declarations and "
                + "guide chunk titles"));
        report.facts(facts);

        report.heading(2, "Next");
        report.bullets(surface.isEmpty()
                ? List.of("read the map instead: " + Texts.code("bal library overview " + pkg),
                        "or widen the query — this one matched nothing, in any kind")
                : List.of("run the command on the row that looks right — each is complete as printed",
                        "add " + Texts.code("-r") + " to any of them for the types it names"));

        if (!surface.isEmpty()) {
            List<Hit> shown = surface.subList(0, Math.min(MAX_SEARCH_ROWS, surface.size()));
            report.heading(2, Texts.count(surface.size()) + " matched by name or type");
            report.bullets(shown.stream().map(Hit::row).toList());
            if (shown.size() < surface.size()) {
                report.paragraph(Texts.count(surface.size() - shown.size())
                        + " more, not listed. Narrow the query.");
            }
        }
        if (!documented.isEmpty()) {
            // NAMED, never rendered. Measured on github, `cache` is 6 surface hits against 33 in doc comments
            // alone; rendering those would bury the six the caller came for, and dropping them would lose the
            // vague query that has no lexical overlap with any identifier.
            report.heading(2, Texts.count(documented.size()) + " matched documentation only");
            report.literal(Report.columns(documented));
        }
        return report.toString();
    }

    private static Hit callableHit(String pkg, Surface.Scope scope, Surface.Container container, Fn fn) {
        String label = fn instanceof Fn.Resource resource
                ? resource.accessor() + " " + resource.paths().stream()
                        .map(io.ballerina.library.symbols.PathTree::displaySegment)
                        .collect(Collectors.joining("/"))
                : nameOf(fn);
        String argument = container.isModule() ? "" : " " + container.name();
        String selector = label.contains(" ") || label.contains("{") ? " '" + label + "'" : " " + label;
        return new Hit(kindOf(fn), label, container.isModule() ? "" : container.name(),
                "bal library " + scope.verb() + " " + pkg + argument + selector);
    }

    private static String nameOf(Fn fn) {
        return switch (fn) {
            case Fn.Standalone named -> named.name();
            case Fn.Constructor ignored -> "init";
            case Fn.Resource resource -> resource.accessor();
        };
    }

    private static String kindOf(Fn fn) {
        return switch (fn) {
            case Fn.Resource ignored -> "resource function, call with `->`";
            case Fn.Remote ignored -> "remote function, call with `->`";
            case Fn.Normal ignored -> "function, call with `.`";
            case Fn.Constructor ignored -> "constructor";
        };
    }

    private static String kindOf(TypeDef typeDef) {
        return switch (typeDef) {
            case TypeDef.Rec ignored -> "record";
            case TypeDef.Enumeration ignored -> "enum";
            case TypeDef.Alias ignored -> "type alias";
            case TypeDef.Constant ignored -> "constant";
            case TypeDef.Variable ignored -> "module-level variable";
            case TypeDef.ErrorDef ignored -> "error";
            case TypeDef.ObjectDef object -> object.form() == TypeDef.ObjectDef.Form.CLASS
                    ? "class" : "object type";
        };
    }
}
