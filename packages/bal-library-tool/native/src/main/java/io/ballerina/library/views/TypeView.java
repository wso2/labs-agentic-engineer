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
import io.ballerina.library.model.TypeDef;
import io.ballerina.library.render.Documents;
import io.ballerina.library.render.TypeDefs;
import io.ballerina.library.symbols.Declarations;
import io.ballerina.library.symbols.Filter;
import io.ballerina.library.symbols.Names;

import java.util.ArrayList;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Set;

/**
 * {@code type <pkg> [<Name>...] [-s <q>] [-r]} — declarations, addressed by name or by search.
 *
 * <p>The CODE register: nothing but real declarations and the tool's own {@code //} annotations. This is the verb
 * that replaces the grep-then-sed loop, and it replaces it by removing the thing being navigated rather than by
 * navigating better — a record is read whole, so there is no closing brace to probe for and no extent to get
 * wrong.
 *
 * <p>A large response is the right answer here. The agent needs the whole shape, and the alternative
 * (single-field addressing) needs an elision convention that is itself a small lie about the declaration.
 *
 * <p><b>ALL-OR-NOTHING across names, kept deliberately.</b> If any one name fails, stdout gets nothing: "exit 0
 * means stdout is complete" is what every redirecting caller relies on, and a partial document under exit 0
 * breaks that silently rather than loudly.
 *
 * @since 0.1.0
 */
public final class TypeView {

    /**
     * How many bytes a SEARCHED response may print.
     *
     * <p>Smaller than the closure budget on purpose. A caller who names a declaration asked for that declaration
     * whole; a caller who searched asked "which of these is it", and the answer to that is a set of names long
     * before it is 20KB of records.
     */
    public static final int MAX_SEARCH_BYTES = 6_000;

    private TypeView() {
    }

    /**
     * @param names declarations asked for by name, possibly empty when {@code search} is set
     * @param search the {@code -s} query, or {@code null}
     * @param resolve whether to append the transitive closure
     * @param note a {@code //} line to print above the answer, set when another verb routed a caller here rather
     *     than failing on a kind guess (§3.7)
     */
    public record Options(List<String> names, String search, boolean resolve, String note) {

        public Options(List<String> names, boolean resolve) {
            this(names, null, resolve, null);
        }

        public Options(List<String> names, String search, boolean resolve) {
            this(names, search, resolve, null);
        }
    }

    public static Result<String> render(LoadedPackage loaded, Options options) {
        Declarations index = Declarations.index(loaded.library().addressable());
        return options.search() != null && !options.search().isBlank()
                ? searched(loaded, index, options)
                : named(loaded, index, options);
    }

    // -----------------------------------------------------------------------
    // Addressed by name
    // -----------------------------------------------------------------------

    private static Result<String> named(LoadedPackage loaded, Declarations index, Options options) {
        String label = loaded.label();
        List<String> roots = new ArrayList<>();
        List<String> unresolved = new ArrayList<>();
        Set<String> candidates = new LinkedHashSet<>();
        // Whether any name COLLIDED, as opposed to merely missing. The two need different advice and the
        // difference is not recoverable from the counts: eight near-misses of a name that matched nothing look
        // exactly like eight colliding declarations, and telling the caller "several declarations match" when
        // none did sends them to re-run with a candidate they never asked for.
        boolean ambiguous = false;
        for (String requested : options.names()) {
            Names.Match match = Names.match(requested, index.names());
            if (match instanceof Names.Match.Found found) {
                roots.add(found.name());
                continue;
            }
            ambiguous |= match instanceof Names.Match.Ambiguous;
            unresolved.add(requested);
            candidates.addAll(Names.candidatesOf(match));
        }

        if (!unresolved.isEmpty()) {
            return Result.err(new Failure.SymbolNotFound(
                    label, List.copyOf(unresolved), List.copyOf(candidates),
                    missSuggestion(loaded, candidates, ambiguous)));
        }
        return Result.ok(document(loaded, index, roots, options));
    }

    private static String missSuggestion(LoadedPackage loaded, Set<String> candidates, boolean ambiguous) {
        String pkg = loaded.qualified().qualified();
        if (candidates.isEmpty()) {
            return "No declaration matched, and nothing in " + loaded.label() + " is close. Search for it "
                    + "instead: `bal library type " + pkg + " -s \"<what it does>\"`, or check the roster with "
                    + "`bal library overview " + pkg + "`; add --refresh if you believe it was published since.";
        }
        return (ambiguous
                ? "Several declarations normalise to the same name, so this reader will not choose between "
                        + "them. Re-run `type` with one of the candidates exactly as spelled"
                : "No declaration matched. The candidates are the closest names in the package; re-run `type` "
                        + "with one of them if it is what you meant")
                + ". Add --refresh if you believe the name exists and is newer than the cached copy.";
    }

    // -----------------------------------------------------------------------
    // Addressed by search
    // -----------------------------------------------------------------------

    /**
     * {@code -s} over the declaration roster.
     *
     * <p>The two-tier rule of {@link Filter} applies here in the code register's own vocabulary: surface matches
     * are DECLARATIONS, documentation-only matches are a {@code //} line of names. Over the budget, nothing is
     * rendered and every match is named — which is the honest answer to "which of these is it" and keeps
     * "exit 0 means stdout is complete" true, where a truncated set of records would not.
     */
    private static Result<String> searched(LoadedPackage loaded, Declarations index, Options options) {
        List<TypeDef> roster = index.names().stream().map(index::get).toList();
        Filter.Split<TypeDef> split =
                Filter.apply(options.search(), roster, Filter::surfaceOf, Filter::docsOf);

        if (split.isEmpty()) {
            return Result.err(new Failure.SymbolNotFound(
                    loaded.label(), List.of(options.search()), List.of(),
                    "No declaration's name, fields or documentation mention that. Widen the query, or read "
                            + "the roster with `bal library overview " + loaded.qualified().qualified() + "`."));
        }

        List<String> names = split.surface().stream().map(TypeDef::name).toList();
        int bytes = names.stream().mapToInt(name -> Texts.byteLength(TypeDefs.renderTypeDef(index.get(name))))
                .sum();

        List<String> blocks = new ArrayList<>();
        blocks.add(Documents.headerComment(loaded.label(), loaded.warning()));
        if (options.note() != null) {
            blocks.add(options.note());
        }
        blocks.add(searchComment(options.search(), split, bytes));

        if (!names.isEmpty() && bytes <= MAX_SEARCH_BYTES) {
            return Result.ok(joined(blocks, bodyOf(loaded, index, names, options.resolve())));
        }
        return Result.ok(String.join("\n\n", blocks) + "\n");
    }

    /** What the query selected, and what to do with it — the code register's version of a facts table. */
    private static String searchComment(String query, Filter.Split<TypeDef> split, int bytes) {
        String pkgArgument = "<Name>...";
        List<String> lines = new ArrayList<>();
        lines.add("// Search: \"" + query + "\" — " + Texts.count(split.surface().size())
                + " by name or field, " + Texts.count(split.documented().size())
                + " more by documentation only.");
        if (!split.surface().isEmpty() && bytes > MAX_SEARCH_BYTES) {
            lines.add("// " + Texts.count(bytes) + " bytes of declarations, over the "
                    + Texts.count(MAX_SEARCH_BYTES) + "-byte budget, so none is printed. Name the ones you "
                    + "want: `bal library type <pkg> " + pkgArgument + "`.");
            lines.add("// Matched: " + String.join(", ", split.surface().stream().map(TypeDef::name).toList()));
        }
        if (!split.documented().isEmpty()) {
            // NAMED, never rendered. 33 documentation hits against 6 surface hits is the measured ratio on
            // github, and rendering them would bury the six the caller came for.
            lines.add("// Matched documentation only, not printed: "
                    + String.join(", ", split.documented().stream().map(TypeDef::name).toList()));
        }
        return String.join("\n", lines);
    }

    // -----------------------------------------------------------------------
    // The document
    // -----------------------------------------------------------------------

    private static String document(
            LoadedPackage loaded, Declarations index, List<String> roots, Options options) {
        List<String> blocks = new ArrayList<>();
        blocks.add(Documents.headerComment(loaded.label(), loaded.warning()));
        if (options.note() != null) {
            blocks.add(options.note());
        }
        return joined(blocks, bodyOf(loaded, index, roots, options.resolve()));
    }

    private static String joined(List<String> header, List<String> body) {
        List<String> blocks = new ArrayList<>(header);
        blocks.addAll(body);
        return String.join("\n\n", blocks) + "\n";
    }

    /**
     * The declarations themselves, the rule that reads them, and the cross-package footer.
     *
     * <p>Shared by the named and searched paths so the two cannot drift into printing a declaration differently
     * — which is the failure mode §4.1 of the design exists to prevent.
     */
    private static List<String> bodyOf(
            LoadedPackage loaded, Declarations index, List<String> roots, boolean resolve) {
        Closure.Result closure = resolve
                ? Closure.of(roots, index, Closure.MAX_BYTES, Closure.UNBOUNDED)
                : new Closure.Result(roots, List.of());

        List<String> blocks = new ArrayList<>();
        String errorRule = subtypeChainRule(closure.names(), index);
        if (errorRule != null) {
            blocks.add(errorRule);
        }
        for (String name : closure.names()) {
            TypeDef typeDef = index.get(name);
            if (typeDef != null) {
                blocks.add(TypeDefs.renderTypeDef(typeDef));
            }
        }
        String omission = Closure.omissionComment(closure.omitted());
        if (omission != null) {
            blocks.add(omission);
        }
        // Under `-r` only. The flag is what asks for cross-package edges to be NAMED rather than followed, so
        // the footer IS the feature; printing it unasked turned a note into an unrequested instruction.
        if (resolve) {
            String footer = Closure.externalFooter(
                    Closure.externalRefs(closure.names(), index),
                    loaded,
                    new LinkedHashSet<>(closure.names()));
            if (footer != null) {
                blocks.add(footer);
            }
        }
        return blocks;
    }

    /**
     * What the {@code distinct} chain below is FOR, printed when the answer is error declarations.
     *
     * <p>ADR-0013. Only when the resolved set actually contains one, or it is noise on every other lookup. The
     * test is {@code distinct}, which is exact rather than a heuristic: a type alias may be {@code distinct} only
     * if it is an error type or an object type, and an object type is an {@link TypeDef.ObjectDef} by the time it
     * reaches here.
     */
    private static String subtypeChainRule(List<String> selected, Declarations index) {
        boolean anyError = selected.stream()
                .map(index::get)
                .anyMatch(typeDef -> typeDef instanceof TypeDef.ErrorDef
                        || (typeDef instanceof TypeDef.Alias alias
                                && alias.type().name().startsWith("distinct ")));
        return anyError
                ? "// The subtype chain is what `is` tests against, so `e is <Name>` works off these lines\n"
                        + "// directly. A package's whole error set reads in one call: pass every name at once."
                : null;
    }
}
