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

package io.ballerina.library.model;

import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import java.util.function.UnaryOperator;
import java.util.regex.Matcher;
import java.util.regex.Pattern;
import java.util.stream.Collectors;

/**
 * Per-package corrections, applied after the IR is built.
 *
 * <p>Every one of these exists because Central OMITS something the package publishes, and an agent that
 * trusts the docs writes code that fails to build. They are deliberately narrow — keyed on the exact library
 * name, no pattern matching — so a package nobody has had trouble with passes through untouched.
 *
 * <p>"Omits" is the whole admission criterion, and it is narrower than what used to be here. Five of the
 * eight corrections this file once held were re-derived against the payload and the packages' own sources
 * and did not survive it. Two were provably inert: the reader had grown to handle the shape, and the
 * correction went on matching nothing while its test asserted the outcome rather than the mechanism. One
 * injected a name no oracle declares. One rewrote 179 field types to a spelling the package never uses, on a
 * style judgement about someone else's API. One replaced seven real service types with three comment lines,
 * on the premise that they did not exist.
 *
 * <p>So a correction has to clear a specific bar: name the fact Central drops, name the oracle that has it,
 * and be pinned in both directions — what it must change AND what it must leave alone. A correction that is
 * merely an improvement on what the package chose to publish does not belong here, and neither does one whose
 * failure mode is silence.
 *
 * <p>Each patch takes a {@link Library} and returns one; nothing here mutates.
 *
 * @since 0.1.0
 */
public final class Patches {

    /**
     * The intersection member Central publishes in place of {@code error<Something>}: a bare {@code error},
     * last in the intersection, with or without the enclosing parentheses.
     *
     * <p>Both spellings occur and the difference is Central's, not a rendering artefact:
     * {@code isParenthesisedType} is true for the three http declarations filed under {@code errors} and
     * false for the two filed under {@code intersectionTypes}. Matching only the parenthesised form is
     * what hid eight of the eleven sites.
     *
     * <p>The spacing around {@code &} is {@link FromCentral}'s — this matches a RENDERED intersection, so
     * the two move together. When they last diverged this matched nothing and the type argument went
     * missing with no failure anywhere, which is why {@code PatchesTest} now pins every row of the table
     * below rather than a sample of it.
     */
    private static final Pattern TRAILING_BARE_ERROR = Pattern.compile(" & error(\\))?$");

    /**
     * The type argument Central drops, keyed by the declaration that carries it.
     *
     * <p>Central publishes the intersection and omits the argument, and nothing in the payload can recover
     * it: an {@code errors[]} item has no key for it, and the detail records appear only as declarations of
     * their own with no link back. So this is a hand-maintained table, and its shape is dictated by the
     * facts rather than chosen — keyed on {@code (library, declaration)} because one row per package is
     * wrong: {@code ballerina/http} needs three different arguments, and two of them belong to declarations
     * Central files under {@code intersectionTypes}, which the reader renders as aliases rather than errors.
     *
     * <p>Every row is read off the SAME package version the corresponding fixture records — http 2.16.6,
     * kafka 4.6.5, graphql 1.17.0 — so no row is transcribed from a version the corpus cannot check. That
     * is the whole guarantee available here: the argument is a fact about the package, and a table of facts
     * about someone else's package rots when they release. What it must never do is rot silently, which is
     * what the two-direction pins in {@code PatchesTest} are for.
     *
     * <p>What is deliberately absent matters as much: http's 51 errors whose {@code detailType} is a plain
     * named base carry no detail record at all, and attaching one would make every {@code SslError}
     * advertise an {@code int statusCode} it does not have.
     */
    private record DetailArgument(String library, String declaration, String argument) { }

    private static final List<DetailArgument> ERROR_DETAIL_ARGUMENTS = List.of(
            new DetailArgument("ballerina/http", "ApplicationResponseError", "Detail"),
            new DetailArgument("ballerina/http", "ClientRequestError", "Detail"),
            new DetailArgument("ballerina/http", "RemoteServerError", "Detail"),
            new DetailArgument("ballerina/http", "LoadBalanceActionError", "LoadBalanceActionErrorData"),
            new DetailArgument(
                    "ballerina/http", "StatusCodeResponseBindingError", "StatusCodeBindingErrorDetail"),
            new DetailArgument("ballerinax/kafka", "PayloadBindingError", "PartitionOffset"),
            new DetailArgument("ballerinax/kafka", "PayloadValidationError", "PartitionOffset"),
            // graphql's four are anonymous record descriptors rather than names, which is why the table
            // holds the argument's SPELLING and not a type name to look up.
            new DetailArgument("ballerina/graphql", "HttpError", "record {| anydata body; |}"),
            new DetailArgument(
                    "ballerina/graphql", "InvalidDocumentError", "record {| ErrorDetail[]? errors; |}"),
            new DetailArgument(
                    "ballerina/graphql", "PayloadBindingError", "record {| ErrorDetail[]? errors; |}"),
            new DetailArgument("ballerina/graphql", "ServerError",
                    "record {| json? data?; ErrorDetail[] errors; map<json>? extensions?; |}"));

    /**
     * Order matters in one place: every other correction keys on the library name, so
     * {@link #changeClientConfigName} — which CHANGES that name — runs last.
     */
    private static final List<UnaryOperator<Library>> PATCHES = List.of(
            Patches::restoreErrorDetailArguments,
            Patches::declareSlackOkTrue,
            Patches::changeClientConfigName);

    private Patches() {
    }

    public static Library applyPatches(Library library) {
        Library current = library;
        for (UnaryOperator<Library> patch : PATCHES) {
            current = patch.apply(current);
        }
        return current;
    }

    /**
     * The error detail type Central drops on the way out — eleven declarations across three packages.
     *
     * <p>The real declaration is {@code distinct (ClientError & error<Detail>)}, and Central publishes the
     * intersection while dropping the type ARGUMENT. Restoring it is what lets an agent reach
     * {@code e.detail().statusCode}, which is the most-traced lookup in the recorded corpus. Without it the
     * detail is not merely undocumented but unreachable: field access on the erased shape fails to compile
     * with {@code type 'map<(…Cloneable & readonly)> & readonly' does not support field access}.
     *
     * <p>Two things this reaches that its {@code ballerina/http}-only predecessor did not. Kafka's and
     * graphql's six were excluded by the library-name gate alone — same category, same IR shape, same
     * rendered spelling. Http's other two were excluded twice over: Central files them under
     * {@code intersectionTypes}, so they arrive as {@link TypeDef.Alias} rather than
     * {@link TypeDef.ErrorDef}, and it publishes them unparenthesised, so a pattern anchored on brackets
     * missed them even where the guard let them through.
     *
     * <p>The table gates on the declaration name, so the pattern is a shape CHECK on eleven known rows
     * rather than a search. That is what keeps the widening from over-reaching: http's
     * {@code StatusCodeBindingClientRequestError} is also an unparenthesised intersection alias, and its
     * members are two named errors rather than a bare {@code error}, so a rule keyed on shape alone would
     * hand it an argument its source never gave it.
     */
    private static Library restoreErrorDetailArguments(Library library) {
        Map<String, String> arguments = ERROR_DETAIL_ARGUMENTS.stream()
                .filter(row -> row.library().equals(library.name()))
                .collect(Collectors.toMap(DetailArgument::declaration, DetailArgument::argument));
        if (arguments.isEmpty()) {
            return library;
        }
        return library.withTypeDefs(library.typeDefs().stream()
                .map(typeDef -> restoreDetail(typeDef, arguments.get(typeDef.name())))
                .toList());
    }

    private static TypeDef restoreDetail(TypeDef typeDef, String argument) {
        if (argument == null) {
            return typeDef;
        }
        if (typeDef instanceof TypeDef.ErrorDef error && error.base().isPresent()) {
            return withArgument(error.base().get(), argument)
                    .map(base -> (TypeDef) new TypeDef.ErrorDef(
                            error.name(), error.description(), error.isDistinct(), Optional.of(base)))
                    .orElse(typeDef);
        }
        if (typeDef instanceof TypeDef.Alias alias) {
            return withArgument(alias.type(), argument)
                    .map(type -> (TypeDef) new TypeDef.Alias(alias.name(), alias.description(), type))
                    .orElse(typeDef);
        }
        return typeDef;
    }

    /**
     * The same intersection with the argument put back, or empty when the descriptor is not the shape this
     * corrects — which is also what makes the correction idempotent, since a restored member no longer ends
     * in a bare {@code error}.
     */
    private static Optional<TypeRef> withArgument(TypeRef base, String argument) {
        Matcher matcher = TRAILING_BARE_ERROR.matcher(base.name());
        if (!matcher.find()) {
            return Optional.empty();
        }
        String restored = base.name().substring(0, matcher.start()) + " & error<" + argument + ">"
                + (matcher.group(1) == null ? "" : ")");
        return Optional.of(new TypeRef(restored, base.links()));
    }

    /**
     * {@code ballerinax/slack} — the one declaration Central omits and 179 of its records reference.
     *
     * <p>{@code OkTrueDef} is how Slack's schema names "this call succeeded", and the package declares it:
     * {@code types.bal:1146}, {@code public type OkTrueDef true;}. Central publishes no declaration for it —
     * it is in none of the module's 33 list-valued categories, because there is no category for a singleton
     * {@code true} alias — while sending it 179 times as a record field's type. So the document referenced a
     * name it never defined, 179 times.
     *
     * <p>This used to be repaired from the other end: rewrite all 179 field types to the literal
     * {@code true}, on the premise that the alias "sends the agent looking for a type that is not worth
     * finding". Two things were wrong with that. It is a style judgement about someone else's public API
     * rather than a correction of Central — the source writes {@code OkTrueDef ok;} 179 times and
     * {@code true ok;} zero times — and it made the name unaddressable, so
     * {@code bal library type ballerinax/slack OkTrueDef} denied a declaration the package really has. Both
     * repairs are hand-maintained facts of the same size; this one matches the source at 180 lines where the
     * other diverged at 179.
     *
     * <p>No description, because the declaration has no doc comment. Inventing one would be the same
     * category of error as the injection this replaces.
     */
    private static Library declareSlackOkTrue(Library library) {
        if (!"ballerinax/slack".equals(library.name())
                || library.typeDefs().stream().anyMatch(typeDef -> "OkTrueDef".equals(typeDef.name()))) {
            return library;
        }
        List<TypeDef> combined = new ArrayList<>();
        combined.add(new TypeDef.Alias("OkTrueDef", "", new TypeRef("true")));
        combined.addAll(library.typeDefs());
        return library.withTypeDefs(List.copyOf(combined));
    }

    /**
     * {@code ballerinax/client.config} — the module path needs quoting in an import, and the unquoted form
     * does not parse.
     *
     * <p>The token the parser rejects is {@code client}, which is a keyword — it prefixes
     * {@code client class} and {@code client object}. {@code config} is an ordinary identifier and needs
     * nothing. Verified with the compiler rather than argued: {@code import ballerinax/client.config;} fails
     * with {@code invalid token 'client'} and {@code import ballerinax/'client.config;} builds.
     *
     * <p>The same fact is encoded once more, for foreign references, in {@code FromCentral}. Both are needed:
     * that one spells the link, this one spells the document's own {@code import} header.
     */
    private static Library changeClientConfigName(Library library) {
        return "ballerinax/client.config".equals(library.name())
                ? library.withName("ballerinax/'client.config")
                : library;
    }

}
