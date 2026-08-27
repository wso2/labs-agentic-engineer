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

import io.ballerina.library.model.Fn;
import io.ballerina.library.model.ModuleRef;
import io.ballerina.library.model.Param;
import io.ballerina.library.model.ReturnDef;
import io.ballerina.library.model.TypeRef;

import java.util.ArrayList;
import java.util.Arrays;
import java.util.LinkedHashMap;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.regex.Matcher;
import java.util.regex.Pattern;
import java.util.stream.Collectors;

/**
 * How one callable is written, and how a foreign name inside it is qualified.
 *
 * <p>This is the module the views share with the API document, and sharing it is what makes their
 * agreement structural rather than tested: {@code overview} and {@code ops --sigs} quote
 * {@link #renderMemberFunction}, so a signature they show cannot differ from the one {@code api} shows.
 * {@code ViewsAgreeTest} still asserts it, because the cheap way to break the guarantee is for a view to
 * hand-roll a line rather than call this.
 *
 * <p>Two conventions carry meaning beyond syntax: a name owned by another package is rendered with that
 * package's module alias ({@code gmail:Message}), and the declaration it came from is repeated in a
 * trailing {@code // Special Agent Note:} so the caller knows which import to add.
 *
 * @since 0.1.0
 */
public final class Signatures {

    private Signatures() {
    }

    /**
     * One external name and the module it came from.
     *
     * <p>The alias and the import path are derived from the module rather than passed alongside it, so the
     * prefix a signature is printed with and the coordinate a footer offers cannot disagree.
     */
    public record ExternalLink(String recordName, ModuleRef module) {

        public String modulePrefix() {
            return module.prefix();
        }
    }

    /**
     * How much of what a callable documents to print.
     *
     * <p>The declaration itself is identical either way — the same bytes, from the same code path — and that is
     * what {@code ViewsAgreeTest} pins. What differs is the documentation around it, and the split follows what
     * each caller is for: {@code api} and {@code type} are declaration registers, where a parameter's description
     * is the point; {@code overview} and {@code ops} answer a question inside a byte budget, and already abbreviate
     * prose for that reason — {@code overview} prints a client's description as its first line only. Wiring the
     * {@code # +} rows into the quoted form instead would have pushed {@code googleapis.gmail} past
     * {@code overview}'s 20,000-byte signature budget, replacing 32 real signatures with a path summary: a change
     * that takes information away from the view it was meant to enrich.
     */
    public enum Detail {

        /** Everything the declaration documents, including a {@code # + name - description} row per parameter. */
        FULL,

        /** The description and the declaration. What a compact view quotes. */
        SIGNATURE
    }

    /**
     * The foreign names inside a type expression that a caller has to import to use it.
     *
     * <p>A PRE-DECLARED module is skipped, which is the one place this collection is not simply "every
     * external link". {@code int:Signed32} is a foreign name by Central's encoding and needs no import by the
     * language's, so a note about it is advice with nothing to advise — and the advice was worse than absent:
     * the literal spelling of {@code import ballerina/lang.int;} is three compiler errors, and the twelve
     * corpus lines carrying it match published sources that import nothing of the kind.
     */
    public static List<ExternalLink> collectExternalLinks(TypeRef type) {
        if (type == null) {
            return List.of();
        }
        List<ExternalLink> links = new ArrayList<>();
        for (TypeRef.Link link : type.links()) {
            if (link instanceof TypeRef.Link.External external
                    && !external.module().coordinate().isEmpty()
                    && !external.module().isPredeclared()) {
                links.add(new ExternalLink(external.recordName(), external.module()));
            }
        }
        return List.copyOf(links);
    }

    /**
     * Qualify every foreign name inside a type expression.
     *
     * <p>Done textually because the expression is already a string by this point — a union or an array of
     * a foreign record has no structure left to walk. The "preceded by a colon" guard keeps an
     * already-qualified name from gaining a second prefix when two links resolve to the same word.
     */
    public static String applyPrefixToTypeName(String typeName, List<ExternalLink> links) {
        String result = typeName;
        for (ExternalLink link : links) {
            Pattern pattern = Pattern.compile("\\b" + Pattern.quote(link.recordName()) + "\\b");
            Matcher matcher = pattern.matcher(result);
            StringBuilder rewritten = new StringBuilder();
            while (matcher.find()) {
                boolean alreadyQualified = matcher.start() > 0 && result.charAt(matcher.start() - 1) == ':';
                String replacement = alreadyQualified
                        ? matcher.group()
                        : link.modulePrefix() + ":" + matcher.group();
                matcher.appendReplacement(rewritten, Matcher.quoteReplacement(replacement));
            }
            matcher.appendTail(rewritten);
            result = rewritten.toString();
        }
        return result;
    }

    /** The trailing note naming which package each foreign type came from. */
    public static String buildSpecialAgentNote(List<ExternalLink> links) {
        return buildSpecialAgentNote(links, List.of());
    }

    /**
     * The one trailing note a line carries, however many things it has to say.
     *
     * <p>Both facts go in the same comment because a second {@code //} would sit INSIDE the first comment,
     * so two notes on a line are one note that reads as though the author lost track.
     *
     * <p>Deduplicated HERE, so every caller gets it. A type expression can mention one foreign name twice —
     * {@code mime:Entity|Attachment|(mime:Entity|Attachment)[]} does, once bare and once as the array's element —
     * and the note came out as {@code Entity, Entity FROM ballerina/mime package}, which reads either as a mangle
     * or as two things needing two imports. The dedup used to live in {@link #collectSignatureLinks} alone, so
     * signature notes had it and the record-field notes that call this directly did not.
     */
    public static String buildSpecialAgentNote(List<ExternalLink> links, List<String> unwritableDefaults) {
        if (links.isEmpty() && unwritableDefaults.isEmpty()) {
            return "";
        }
        List<String> clauses = new ArrayList<>();
        Map<String, List<String>> grouped = new LinkedHashMap<>();
        for (ExternalLink link : deduplicated(links)) {
            grouped.computeIfAbsent(link.module().importPath(), key -> new ArrayList<>())
                    .add(link.recordName());
        }
        // "module", not "package", and the import path rather than the coordinate: this clause IS the import
        // advice, an `import` statement names a module, and 15 of the corpus's notes name a module that is not
        // its package at all (`ballerina/http.httpscerr`, `ballerina/graphql.parser`).
        grouped.forEach((module, names) ->
                clauses.add(String.join(", ", names) + " FROM " + module + " module"));
        if (!unwritableDefaults.isEmpty()) {
            clauses.add(unwritableClause(unwritableDefaults));
        }
        return " // Special Agent Note: " + String.join(", ", clauses);
    }

    /** Ballerina doc comments, one {@code #} per line, with the trailing newline callers splice in. */
    public static String renderDescription(String description) {
        if (description.trim().isEmpty()) {
            return "";
        }
        // `split("\n", -1)` rather than `lines()`: a trailing newline or a stray carriage return has to
        // survive verbatim, because these bytes are compared against a committed snapshot.
        return Arrays.stream(description.split("\n", -1))
                .map(line -> "# " + line)
                .collect(Collectors.joining("\n")) + "\n";
    }

    /** One entry per distinct foreign name, keyed on the name AND its owning module, first occurrence winning. */
    private static List<ExternalLink> deduplicated(List<ExternalLink> links) {
        Set<String> seen = new LinkedHashSet<>();
        List<ExternalLink> unique = new ArrayList<>();
        for (ExternalLink link : links) {
            if (seen.add(link.recordName() + "::" + link.module().coordinate())) {
                unique.add(link);
            }
        }
        return List.copyOf(unique);
    }

    /**
     * Every foreign name a signature mentions, params before return.
     *
     * <p>Not deduplicated: {@link #buildSpecialAgentNote} does that now, and {@link #applyPrefixToTypeName} is
     * idempotent by its own "preceded by a colon" guard, so a repeated link qualifies a name once.
     */
    private static List<ExternalLink> collectSignatureLinks(List<Param> params, TypeRef returns) {
        List<ExternalLink> links = new ArrayList<>();
        for (Param param : params) {
            links.addAll(collectExternalLinks(param.type()));
        }
        links.addAll(collectExternalLinks(returns));
        return List.copyOf(links);
    }

    /**
     * One parameter, in the form it is written: {@code T p}, {@code *T p} or {@code T... p}.
     *
     * <p>Neither of the two special forms takes a default. An inclusion's fields carry their own and
     * {@code *T x = {}} is not a form the language has; a rest parameter's default is "none passed".
     */
    private static String renderParam(Param param) {
        List<ExternalLink> links = collectExternalLinks(param.type());
        String type = applyPrefixToTypeName(param.type().name(), links);
        String name = Identifiers.write(param.name());
        return switch (param.form()) {
            case INCLUSION -> "*" + type + " " + name;
            case REST -> type + "... " + name;
            case NORMAL -> type + " " + name
                    + (param.hasDefault() ? " = " + param.defaultValue() : "");
        };
    }

    /**
     * The {@code returns} clause, or nothing at all.
     *
     * <p>A callable Central publishes no return parameter for returns nothing, and a Ballerina function that
     * returns nothing has no clause. Twelve of http's declarations used to end in {@code returns nil}, which
     * names a type the compiler does not have, and three of those are constructors — where the wrong clause
     * also erased the one fact a caller needs from it, whether {@code new} can fail.
     */
    private static String renderReturns(ReturnDef returns, List<ExternalLink> links) {
        return returns.hasType()
                ? " returns " + applyPrefixToTypeName(returns.type().name(), links)
                : "";
    }

    /**
     * The clause that says a printed default is not one the caller can write.
     *
     * <p>The expression stays on the line because dropping it would make a defaultable parameter read as
     * required, which is a worse claim than an unwritable default. Naming it and saying so leaves the caller
     * with the one move that works: omit the argument, or pass the ones after it by name.
     */
    private static String unwritableClause(List<String> expressions) {
        String names = String.join(", ", expressions);
        return expressions.size() == 1
                ? "the default " + names + " is not exported by this package; omit the argument rather than "
                        + "repeating it"
                : "the defaults " + names + " are not exported by this package; omit the arguments rather "
                        + "than repeating them";
    }

    /** Every unwritable default in a parameter list, in the order the parameters are declared. */
    private static List<String> unwritableDefaults(List<Param> params) {
        return params.stream()
                .filter(Param::unwritableDefault)
                .map(Param::defaultValue)
                .toList();
    }

    /**
     * The indented sibling of {@link #renderDescription}, for a doc comment inside a block.
     *
     * <p>Public because a continuation line that does not carry its own {@code #} is not a comment — it is
     * source, and the compiler reads it as a declaration. Every renderer that prefixes a description by
     * hand has emitted that, so the splitting lives in one place and the block renderers call it.
     */
    public static String renderDocComment(String description, String indent) {
        if (description.isEmpty()) {
            return "";
        }
        return indent + "# " + String.join("\n" + indent + "# ", description.split("\n", -1)) + "\n";
    }

    /**
     * A {@code # + name - description} doc row, continued as bare {@code #} lines.
     *
     * <p>Ballerina's own convention continues a {@code +} row without repeating the marker, so a
     * description that spans lines becomes one row and then plain comment lines. The alternative —
     * repeating {@code # + name -} per line — would claim the parameter is documented twice.
     */
    private static void addDocRow(List<String> lines, String indent, String label, String description) {
        String[] parts = description.split("\n", -1);
        lines.add(indent + "# + " + label + " - " + parts[0]);
        for (int index = 1; index < parts.length; index++) {
            lines.add(indent + "# " + parts[index]);
        }
    }

    /**
     * A callable's whole doc comment: its own description, then a {@code # +} row per documented parameter and
     * one for the return.
     *
     * <p>Central publishes 2,755 parameter and 1,532 return descriptions across the nine fixtures and the reader
     * rendered them for module-level functions only, so the same fact appeared in one section of a document and
     * not in another. Little of it is prose — 282 distinct parameter descriptions cover all 2,755 uses, and
     * {@code ballerinax/github} has 1,168 drawn from five strings — but which parameter carries which is exactly
     * what a caller cannot infer, and the source states it on every one.
     */
    private static String renderCallableDocs(Fn fn, String indent, Detail detail) {
        String description = renderDocComment(fn.description(), indent);
        if (detail == Detail.SIGNATURE) {
            return description;
        }
        List<String> rows = new ArrayList<>();
        for (Param param : fn.params()) {
            if (!param.description().isEmpty()) {
                addDocRow(rows, indent, param.name(), param.description());
            }
        }
        if (fn.returns().hasDescription()) {
            addDocRow(rows, indent, "return", fn.returns().description());
        }
        return rows.isEmpty() ? description : description + String.join("\n", rows) + "\n";
    }

    /**
     * The qualifiers that precede {@code function}, in the order the language writes them.
     *
     * <p>{@code isolated} sits between visibility and {@code remote}/{@code resource} — {@code isolated remote
     * function}, {@code public isolated function} — verified by compiling each form rather than recalled.
     * Central sets {@code isIsolated} on 1,615 of the corpus's 1,617 methods and the reader emitted it on none,
     * which is invisible until a service contract has to be matched exactly: {@code graphql:Interceptor.execute}
     * is declared {@code isolated remote function}, and the compiler's {@code mismatched function signatures}
     * message prints an expected and a found signature that are textually identical because it omits the
     * qualifier from both.
     */
    private static String isolatedQualifier(Fn fn) {
        return fn.isIsolated() ? "isolated " : "";
    }

    /**
     * A resource function's path, as the caller types it.
     *
     * <p>Path parameters keep the {@code [string owner]} spelling rather than a display form like
     * {@code {owner}}, because this string is a declaration and that is what goes in the source.
     * {@code ops} prints the display form in its Markdown prose and this form inside its fenced blocks,
     * which is the whole reason the two registers are separate.
     */
    private static String renderResourcePath(Fn.Resource fn) {
        return fn.paths().stream()
                .map(segment -> switch (segment) {
                    case Fn.PathSegment.Literal literal -> literal.text();
                    case Fn.PathSegment.Parameter parameter ->
                            "[" + parameter.type() + " " + parameter.name() + "]";
                })
                .collect(Collectors.joining("/"));
    }

    /**
     * A signature at column zero — a report quotes declarations, it does not indent them.
     *
     * <p>Named here rather than repeated in each view, because "the views and {@code api} print the same bytes"
     * is a property of calling one function, and two private copies is where that starts to drift.
     */
    public static String renderSignature(Fn fn) {
        return renderMemberFunction(fn, "", Detail.SIGNATURE);
    }

    public static String renderMemberFunction(Fn fn, String indent, Detail detail) {
        List<ExternalLink> links = collectSignatureLinks(fn.params(), fn.returns().type());
        String params = fn.params().stream().map(Signatures::renderParam).collect(Collectors.joining(", "));
        String note = buildSpecialAgentNote(links, unwritableDefaults(fn.params()));
        String docs = renderCallableDocs(fn, indent, detail);
        // Above the signature and below the doc comment, which is where the language puts it. A caller who
        // reads only the signature line still sees it, because it is on the line before.
        String deprecated = fn.isDeprecated() ? indent + "@deprecated\n" : "";
        String isolated = isolatedQualifier(fn);

        return switch (fn) {
            // The constructor's doc comment, like every other member's. It was the one arm that opened with the
            // signature, and `init` is the one method every caller has to write: sap's says the config record
            // decides "which type of additional behaviours are added to the endpoint (e.g. security, circuit
            // breaking)" and that "Caching is enabled always", neither of which is inferable from its parameters.
            case Fn.Constructor constructor -> docs + deprecated
                    + indent + isolated + "function init(" + params + ")"
                    + renderReturns(constructor.returns(), links) + ";" + note;
            case Fn.Remote remote -> docs + deprecated
                    + indent + isolated + "remote function " + Identifiers.write(remote.name())
                    + "(" + params + ")" + renderReturns(remote.returns(), links) + ";" + note;
            case Fn.Resource resource -> {
                // Path parameters are declared in the path, so repeating them in the parameter list would
                // be a signature no caller can write.
                Set<String> inPath = resource.paths().stream()
                        .filter(Fn.PathSegment.Parameter.class::isInstance)
                        .map(segment -> ((Fn.PathSegment.Parameter) segment).name())
                        .collect(Collectors.toCollection(LinkedHashSet::new));
                String rest = resource.params().stream()
                        .filter(param -> !inPath.contains(param.name()))
                        .map(Signatures::renderParam)
                        .collect(Collectors.joining(", "));
                yield docs + deprecated
                        + indent + isolated + "resource function " + resource.accessor() + " "
                        + renderResourcePath(resource) + "(" + rest + ")"
                        + renderReturns(resource.returns(), links) + ";" + note;
            }
            case Fn.Normal normal -> docs + deprecated
                    + indent + isolated + "function " + Identifiers.write(normal.name()) + "(" + params + ")"
                    + renderReturns(normal.returns(), links) + ";" + note;
        };
    }

    /**
     * A module-level function, documented rather than merely declared: standalone functions are usually
     * utilities whose parameters are not self-describing.
     *
     * <p>Kept separate from {@link #renderMemberFunction} because a module function's declaration differs from a
     * member's in a way a mode cannot express: it carries {@code public}, which no member does. It has always
     * spent lines on {@code # +} rows, and since {@link Detail#FULL} landed the two forms document a parameter
     * the same way — through the same {@link #addDocRow}.
     */
    public static String renderStandaloneFunction(Fn.Standalone fn) {
        List<ExternalLink> links = collectSignatureLinks(fn.params(), fn.returns().type());
        List<String> lines = new ArrayList<>();
        if (!fn.description().isEmpty()) {
            for (String line : fn.description().split("\n", -1)) {
                lines.add("# " + line);
            }
        }
        for (Param param : fn.params()) {
            if (!param.description().isEmpty()) {
                addDocRow(lines, "", param.name(), param.description());
            }
        }
        if (fn.returns().hasDescription()) {
            addDocRow(lines, "", "return", fn.returns().description());
        }
        if (fn.isDeprecated()) {
            lines.add("@deprecated");
        }
        String params = fn.params().stream().map(Signatures::renderParam).collect(Collectors.joining(", "));
        lines.add("public " + isolatedQualifier(fn) + "function " + Identifiers.write(fn.name())
                + "(" + params + ")" + renderReturns(fn.returns(), links) + ";"
                + buildSpecialAgentNote(links, unwritableDefaults(fn.params())));
        return String.join("\n", lines);
    }
}
