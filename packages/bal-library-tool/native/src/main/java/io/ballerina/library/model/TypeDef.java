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

import java.util.List;
import java.util.Optional;

/**
 * A module-level type declaration.
 *
 * <p>The renderer switches over these cases with no {@code default}, so a Ballerina shape nobody
 * renders is a compile error rather than a silently dropped declaration.
 *
 * <p>{@link Alias} carries every one of Central's seventeen alias categories, because a Ballerina type
 * alias is one shape however Central files it, and {@link ObjectDef} carries its four object categories for
 * the same reason. A name whose descriptor the reader could not encode is an {@code Alias} with an empty
 * type — it renders as a comment naming itself. There used to be a separate {@code Other} case for that,
 * produced by one patch injecting one name no oracle declares; when the injection went, so did the case,
 * since two spellings of "no descriptor" is one more than the renderer needs.
 *
 * @since 0.1.0
 */
public sealed interface TypeDef {

    String name();

    String description();

    /**
     * A record. {@code isClosed} is {@code record {| |}} — the form that REJECTS extra fields, and the one
     * fact a caller building a value needs, since an open record accepts anything and a closed one does not.
     */
    record Rec(
            String name,
            String description,
            boolean isClosed,
            boolean isDeprecated,
            List<RecordField> fields) implements TypeDef {

        public Rec(String name, String description, List<RecordField> fields) {
            this(name, description, false, false, fields);
        }

        /**
         * The same record with different members.
         *
         * <p>Exists so a patch cannot lose {@code isClosed} by rebuilding: two of them did, and an inclusive
         * record descriptor holding a rest field is a syntax error, so the loss showed up as a document that
         * stopped compiling at exactly the declaration a patch had rewritten.
         */
        public Rec withFields(List<RecordField> replacement) {
            return new Rec(name, description, isClosed, isDeprecated, replacement);
        }
    }

    /**
     * An enum, whose members carry a description each.
     *
     * <p>A member is a name and a description rather than a bare name, because Central publishes both and 13 of
     * the corpus's 65 members are described — including the two that say which PostgreSQL logical-decoding
     * plugin is the standard one. What Central publishes for NO member is its VALUE: 10 of postgresql's 14 have
     * an explicit one and 6 of those differ from the member name in more than case, so
     * {@code enum SSLMode { … VERIFY_CA }} is a legal declaration that is not quite the real one. The
     * description is the only part of that gap either page can close.
     */
    record Enumeration(String name, String description, List<Member> members) implements TypeDef {

        public record Member(String name, String description) { }

        public List<String> memberNames() {
            return members.stream().map(Member::name).toList();
        }
    }

    /**
     * A type alias: a name bound to a type descriptor.
     *
     * <p>ONE case for all seventeen categories Central models separately — string, integer, decimal, boolean,
     * simple-name-reference, array, union, intersection, anydata, any, tuple, function, typedesc, map,
     * stream, table and xml. They differ in nothing a declaration shows: {@code type Id int|string;} and
     * {@code type TsDef string;} are the same shape with different right-hand sides, and Central already
     * publishes each one's descriptor in full.
     *
     * <p>{@code type.name()} empty means the reader could not encode the descriptor. That still renders the
     * name, as a comment, because the declaration exists and a caller may need to know it does.
     */
    record Alias(String name, String description, TypeRef type) implements TypeDef { }

    record Constant(String name, String description, String value, TypeRef varType) implements TypeDef { }

    /**
     * A module-level {@code public final} variable: a value the package computes once and exports.
     *
     * <p>Distinct from {@link Constant}, which is {@code const} — a compile-time constant whose value is part
     * of its type. These are not: {@code public final readonly & Continue CONTINUE = {};} has a value the
     * package builds, so its initialiser is not something a caller can be given. Central publishes 64 of them
     * across the corpus under {@code variables}, and they were parsed and rendered nowhere, so
     * {@code http:CONTINUE} — which compiles from another module, measured — appeared in no verb.
     *
     * @param initialiser the default Central publishes, which for most of these is {@code {}} and is therefore
     *     not the value; {@link Defaults} decides whether it is writable
     */
    record Variable(String name, String description, TypeRef varType, String initialiser, boolean isReadOnly)
            implements TypeDef { }

    /**
     * A class, an object type, a service type or a listener — the four categories Central publishes with one
     * shape, and one IR case, because a Ballerina object declaration is one shape too.
     *
     * <p>{@link Form} and {@link Role} are orthogonal and both load-bearing. {@code Form} is whether the
     * declaration is instantiable: a {@code class} is, an {@code object} type is a contract that is not, and
     * printing the one as the other told a caller to {@code new} an abstract type. {@code Role} is whether its
     * methods are reached with {@code ->} or with {@code .}, which is the difference between
     * {@code db->query(q)} compiling and not.
     *
     * <p>Neither is a flag Central sets. {@code isClient} does not exist in the payload and {@code isService}
     * exists but is false on all 230 objects in the corpus, so both are DERIVED, and derived from the grammar
     * rather than guessed: a {@code remote} method is only legal in a client or service object, and a service
     * type is a service object by definition — which is what {@code http:Service} and {@code kafka:Service}
     * are declared as in their own sources.
     *
     * <p>{@code methods} is Central's {@code methods} array and nothing else. Its {@code otherMethods},
     * {@code lifeCycleMethods} and {@code initMethod} are all SUBSETS of it — measured across every object in
     * the corpus, with no exception — so they are groupings of one list rather than four lists, and reading
     * them all would print {@code init} twice.
     */
    record ObjectDef(
            String name,
            String description,
            Form form,
            Role role,
            boolean isDistinct,
            boolean isReadOnly,
            boolean isIsolated,
            boolean isDeprecated,
            List<RecordField> fields,
            List<Fn> methods) implements TypeDef {

        /** {@code class X { }} versus {@code type X object { };} — instantiable versus a contract. */
        public enum Form { CLASS, OBJECT_TYPE }

        /** How the object's methods are called: plain, {@code ->} on a client, or attached as a service. */
        public enum Role { PLAIN, CLIENT, SERVICE }

        public ObjectDef(String name, String description) {
            this(name, description, Form.CLASS, Role.PLAIN, false, false, false, false, List.of(), List.of());
        }
    }

    /**
     * An error declaration, with the facts that make error handling learnable from the document instead
     * of guessable.
     *
     * <p>Central publishes one key, {@code detailType}, for two different things, and which one it is
     * decides the syntax:
     *
     * <pre>
     *   category "errors"   supertype       type SslError distinct ClientError;
     *   category "records"  detail record   type FHIRServerError distinct error&lt;FHIRServerErrorDetails&gt;;
     * </pre>
     *
     * <p>{@code base} therefore cannot be named for the wire, and {@code detailRecord} is the flag that
     * says which reading applies. This used to be a single field documented as "never a detail record",
     * generalised from nine fixtures in which every {@code detailType} happens to be an error. It is not
     * a safe generalisation: {@code ballerinax/health.clients.fhir} publishes records, and the document
     * printed {@code distinct FHIRServerErrorDetails} — which is not an error type, does not compile as
     * one, and quietly contradicts the promise that a signature from here is the source for what does.
     */
    record ErrorDef(
            String name, String description, boolean isDistinct, Optional<TypeRef> base, boolean detailRecord)
            implements TypeDef {

        /** An error whose {@code base}, if any, is a supertype rather than a detail record. */
        public ErrorDef(String name, String description, boolean isDistinct, Optional<TypeRef> base) {
            this(name, description, isDistinct, base, false);
        }
    }
}
