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

package io.ballerina.library.symbols;

import io.ballerina.library.model.Fn;
import io.ballerina.library.model.Param;
import io.ballerina.library.model.RecordField;
import io.ballerina.library.model.TypeDef;

import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;
import java.util.Locale;
import java.util.function.Function;

/**
 * {@code -s} — a linear scan over the package that is already in memory.
 *
 * <p>NO INDEX IS BUILT AND NO CACHE TIER IS ADDED. {@code Loader.loadPackage} holds the whole package, and
 * {@link Declarations}, {@link Names} and {@link PathTree} are already constructed per load;
 * {@code ballerinax/github} is about 900 operations and 700 declarations, so a scan over them is free beside the
 * fetch it rides on. The payload cache — 8.0s cold against 1.2s warm — is the entire performance story, and a
 * second cache would be a second thing to invalidate for no measured gain.
 *
 * <p><b>TWO TIERS, AND THE SPLIT IS THE WHOLE DESIGN.</b> Measured on {@code ballerinax/github}: {@code upload} is
 * 7 matches by name or type against 12 in documentation alone, and {@code pagination} is 0 against 14. Ranking
 * documentation hits below surface hits is not enough — 19 results push a 7-result query past a tier threshold, so
 * the agent receives counts where it should receive seven signatures. Dropping documentation instead loses
 * {@code pagination} entirely, and that is the query shape a caller uses when they know the capability and not the
 * vocabulary. So a documentation-only match is NEVER RENDERED and ALWAYS NAMED, which costs one line and leaves
 * every one of them promotable by name in a single follow-up call.
 *
 * <p>That split is also what removes the need for a cross-kind relevance score — the part of a search design
 * hardest to test and easiest to rot silently, since nothing fails when a weight drifts.
 *
 * @since 0.1.0
 */
public final class Filter {

    private Filter() {
    }

    /**
     * What a query selected: the entries whose SURFACE matched, and the ones only their prose did.
     *
     * @param surface rendered at whatever tier the budget allows
     * @param documented named, never rendered
     */
    public record Split<T>(List<T> surface, List<T> documented) {

        public boolean isEmpty() {
            return surface.isEmpty() && documented.isEmpty();
        }

        public int total() {
            return surface.size() + documented.size();
        }
    }

    /**
     * Partition a list against a query.
     *
     * <p>Every whitespace-separated token of the query has to appear, so {@code -s "list repo"} narrows rather
     * than widening the way an OR would. Case-insensitive substring rather than word matching, because the
     * identifiers being searched are camelCase: {@code repo} has to reach {@code listRepositories}, and a
     * word-boundary match would not.
     */
    public static <T> Split<T> apply(
            String query, List<T> items, Function<T, String> surfaceText, Function<T, String> docText) {
        List<String> tokens = tokens(query);
        if (tokens.isEmpty()) {
            return new Split<>(List.copyOf(items), List.of());
        }
        List<T> surface = new ArrayList<>();
        List<T> documented = new ArrayList<>();
        for (T item : items) {
            if (containsAll(surfaceText.apply(item), tokens)) {
                surface.add(item);
            } else if (containsAll(docText.apply(item), tokens)) {
                documented.add(item);
            }
        }
        return new Split<>(List.copyOf(surface), List.copyOf(documented));
    }

    /**
     * A query as the tokens that all have to appear — split on {@code /} as well as on whitespace.
     *
     * <p>THE SLASH IS WHY {@code -s} DID NOT MATCH PATHS. A resource function's searchable text joins its
     * segments with spaces ({@code get repos &#123;owner&#125; &#123;repo&#125; issues}), so a query containing
     * {@code /} could never be a substring of it. Measured: {@code -s "repos/&#123;owner&#125;/&#123;repo&#125;/issues"}
     * matched nothing on a client declaring 903 resource functions, while the identical string resolved as a
     * positional selector — and the flag's own description had claimed path matching all along.
     *
     * <p>Splitting keeps the flag's existing meaning rather than bolting a second one on: every token must
     * appear, so a path narrows exactly as {@code -s "list repo"} already did. It is deliberately an UNORDERED
     * and unanchored AND — {@code -s} is a filter, and the anchored, ordered walk is what the positional
     * selector is for. A caller who needs "this path and not one that merely shares its segments" wants that
     * slot, not this flag.
     *
     * <p>Normalised through {@link PathTree#readableSelector} for the same reason the selector is: the escaped
     * spelling ({@code chat\.postMessage}, {@code code\-scanning}, {@code 'import}) is what the documents
     * print, so it is what a caller copies back.
     */
    private static List<String> tokens(String query) {
        if (query == null || query.isBlank()) {
            return List.of();
        }
        return Arrays.stream(PathTree.readableSelector(query).trim().toLowerCase(Locale.ROOT).split("[\\s/]+"))
                .filter(token -> !token.isEmpty())
                .toList();
    }

    private static boolean containsAll(String haystack, List<String> tokens) {
        String lowered = haystack.toLowerCase(Locale.ROOT);
        return tokens.stream().allMatch(lowered::contains);
    }

    // -----------------------------------------------------------------------
    // What counts as surface
    // -----------------------------------------------------------------------

    /**
     * A callable's addressable text: how it is named, and every name inside its declaration.
     *
     * <p>Parameter and TYPE names are included deliberately. An agent that knows it holds an
     * {@code ActionsCacheList} and wants the call that returns one has no other way to ask, and the type name is
     * on the line either way — so matching it costs nothing and answers a question the name alone cannot.
     */
    public static String surfaceOf(Fn fn) {
        StringBuilder text = new StringBuilder();
        switch (fn) {
            case Fn.Resource resource -> {
                text.append(resource.accessor()).append(' ');
                resource.paths().forEach(segment ->
                        text.append(PathTree.displaySegment(segment)).append(' '));
            }
            case Fn.Standalone named -> text.append(named.name()).append(' ');
            case Fn.Constructor ignored -> text.append("init ");
        }
        for (Param param : fn.params()) {
            text.append(param.name()).append(' ').append(param.type().name()).append(' ');
        }
        if (fn.returns().hasType()) {
            text.append(fn.returns().type().name());
        }
        return text.toString();
    }

    /**
     * Everything a callable DOCUMENTS: its own prose plus every parameter's and the return's.
     *
     * <p>A return with no description is {@code null} rather than empty, so it is guarded here: the alternative
     * is the four literal characters {@code null} in the haystack, which would make {@code -s "null"} match every
     * undocumented callable in the package.
     */
    public static String docsOf(Fn fn) {
        StringBuilder text = new StringBuilder(fn.description()).append(' ');
        fn.params().forEach(param -> text.append(param.description()).append(' '));
        if (fn.returns().hasDescription()) {
            text.append(fn.returns().description());
        }
        return text.toString();
    }

    /** A declaration's addressable text: its name, and the names of whatever it is made of. */
    public static String surfaceOf(TypeDef typeDef) {
        StringBuilder text = new StringBuilder(typeDef.name()).append(' ');
        switch (typeDef) {
            case TypeDef.Rec record -> appendFields(text, record.fields());
            case TypeDef.Enumeration enumeration ->
                    enumeration.members().forEach(member -> text.append(member.name()).append(' '));
            case TypeDef.Alias alias -> text.append(alias.type().name());
            case TypeDef.Constant constant -> text.append(constant.varType().name());
            case TypeDef.Variable variable -> text.append(variable.varType().name());
            case TypeDef.ErrorDef error -> error.base().ifPresent(base -> text.append(base.name()));
            // FIELDS ONLY, and no methods at all. A class's methods are the container verbs' business, and this
            // function is what `type -s` searches: folding them in makes that verb useless, because
            // `ballerinax/kafka`'s 5KB `Consumer` matches `TopicPartition` on the strength of one method called
            // `getTopicPartitions` — so a query that should return four records returned 14,603 bytes and was
            // refused by the budget. The callables are searched directly, by their own signatures, in the scope
            // that addresses them.
            case TypeDef.ObjectDef object -> appendFields(text, object.fields());
        }
        return text.toString();
    }

    /** Everything a declaration DOCUMENTS: its own prose plus its fields' or members'. */
    public static String docsOf(TypeDef typeDef) {
        StringBuilder text = new StringBuilder(typeDef.description()).append(' ');
        switch (typeDef) {
            case TypeDef.Rec record ->
                    record.fields().forEach(field -> text.append(field.description()).append(' '));
            case TypeDef.Enumeration enumeration ->
                    enumeration.members().forEach(member -> text.append(member.description()).append(' '));
            case TypeDef.ObjectDef object ->
                    object.fields().forEach(field -> text.append(field.description()).append(' '));
            default -> {
                // An alias, a constant, a variable and an error carry their own description and nothing else
                // that documents anything separately.
            }
        }
        return text.toString();
    }

    private static void appendFields(StringBuilder text, List<RecordField> fields) {
        fields.forEach(field -> text.append(field.name()).append(' ')
                .append(field.type().name()).append(' '));
    }
}
