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

package io.ballerina.library.central.schema;

import java.util.List;
import java.util.Optional;

/**
 * The ONLY description of what Ballerina Central sends.
 *
 * <p>Everything downstream of {@link Schema#parse} takes typed values; {@link Schema} is the one place
 * that touches untyped JSON. That matters because the payload is a deeply nested, undocumented JSON
 * tree: hand-walking it — which is what the language-server reader did, across 500 lines — turns a
 * Central field being renamed into subtly wrong signatures that nobody notices until an agent writes
 * Ballerina that will not compile. A schema turns the same event into a located parse error.
 *
 * <p>The strictness split is deliberate:
 *
 * <ul>
 *   <li>fields we read are REQUIRED — a rename, a removal or a type change is a loud
 *       {@code schema-drift} failure, because those are the changes that would make us render the
 *       wrong thing;
 *   <li>unknown keys are STRIPPED rather than rejected — Central adding a field is harmless to a
 *       reader that does not read it, and failing the command over one would take the capability away
 *       for a cosmetic upstream change.
 * </ul>
 *
 * <p>Additions are still visible: {@code KeySpaceTest} snapshots the payload's whole key space per
 * fixture, so a new or vanished Central field shows up as a reviewable diff without costing anything
 * at run time.
 *
 * @param modules every module the payload describes, at least one
 * @since 0.1.0
 */
public record CentralDocs(List<Module> modules) {

    /**
     * A type expression, as Central models it: a recursive node whose meaning comes from which
     * combination of flags is set. Every field is optional because the same node type stands in for a
     * builtin, an external record reference, an inline record's field, a union member and an array
     * element.
     */
    public record TypeNode(
            Optional<String> name,
            Optional<String> category,
            Optional<String> orgName,
            Optional<String> moduleName,
            Optional<String> version,
            Optional<String> description,
            boolean isArrayType,
            boolean isNullable,
            boolean isOptional,
            boolean isAnonymousUnionType,
            boolean isIntersectionType,
            boolean isParenthesisedType,
            boolean isTypeDesc,
            boolean isTuple,
            boolean isRestParam,
            boolean isInclusion,
            boolean isReadOnly,
            boolean isDeprecated,
            boolean isIsolated,
            boolean isDistinct,
            boolean isLambda,
            int arrayDimensions,
            Optional<TypeNode> constraint,
            Optional<TypeNode> elementType,
            Optional<TypeNode> returnType,
            Optional<List<TypeNode>> memberTypes,
            Optional<List<TypeNode>> paramTypes,
            Optional<List<TypeNode>> functionTypes) {

        public List<TypeNode> members() {
            return memberTypes.orElse(List.of());
        }

        /** A lambda's parameter types. Central spells them here and nowhere else. */
        public List<TypeNode> params() {
            return paramTypes.orElse(List.of());
        }

        /**
         * The methods of an ANONYMOUS object type, each itself a function-type node.
         *
         * <p>Central inlines an object type's structure here when the source's own name is module-private —
         * {@code http:createHttpSecureClient} returns {@code HttpClient|ClientError} and {@code HttpClient}
         * is not exported, so this is all a caller gets. A node carrying these is an object, which is why
         * calling it {@code record {}} was not a vague answer but a wrong one.
         */
        public List<TypeNode> objectMethods() {
            return functionTypes.orElse(List.of());
        }
    }

    /**
     * An annotation ATTACHED to something, as opposed to one declared by the module.
     *
     * <p>Central sends the declaration's coordinates and nothing about the arguments, which is why a
     * rendered attachment can only be {@code @module:Name} — see the note on {@link Annotation}.
     */
    public record AnnotationRef(
            String name,
            Optional<String> orgName,
            Optional<String> moduleName,
            Optional<String> version,
            Optional<String> description) { }

    public record Parameter(
            String name,
            Optional<String> description,
            Optional<String> defaultValue,
            boolean isDeprecated,
            TypeNode type) { }

    public record ReturnParameter(Optional<String> description, TypeNode type) { }

    /**
     * A function, in every position Central uses one: a client method, a module function, a listener's
     * {@code init}, a service type's remote contract. Which of the four it is comes from
     * {@code name.equals("init")}, {@code isResource} and {@code isRemote} — the same discrimination
     * {@code FromCentral} performs once, so the rest of the codebase never sees these flags.
     */
    public record Method(
            String name,
            Optional<String> description,
            boolean isRemote,
            boolean isResource,
            boolean isDeprecated,
            boolean isIsolated,
            boolean isExtern,
            Optional<String> accessor,
            Optional<String> resourcePath,
            Optional<List<Parameter>> parameters,
            Optional<List<ReturnParameter>> returnParameters,
            Optional<List<AnnotationRef>> annotations) {

        public List<Parameter> params() {
            return parameters.orElse(List.of());
        }

        public List<ReturnParameter> returns() {
            return returnParameters.orElse(List.of());
        }

        public List<AnnotationRef> attachedAnnotations() {
            return annotations.orElse(List.of());
        }
    }

    /**
     * A record field is one of two things and they have nothing in common: an inclusion
     * ({@code *OtherRecord;}, whose members get spliced in) or a declared field. Modelling it as a
     * sealed pair means {@code FromCentral} cannot read a declared field's {@code type} off an
     * inclusion by accident.
     */
    public sealed interface Field {

        record Inclusion(TypeNode inclusionType, boolean isReadOnly, boolean isDeprecated)
                implements Field { }

        record Declared(
                String name,
                Optional<String> description,
                Optional<String> defaultValue,
                boolean isReadOnly,
                boolean isDeprecated,
                Optional<List<AnnotationRef>> annotations,
                TypeNode type) implements Field {

            public List<AnnotationRef> attachedAnnotations() {
                return annotations.orElse(List.of());
            }
        }
    }

    /** A name and a description: an enum's member, and nothing else in the payload. */
    public record Named(String name, Optional<String> description, boolean isDeprecated) { }

    /**
     * A declared type alias, which is the ONE shape Central uses for fourteen categories.
     *
     * <p>Verified rather than assumed: across the nine fixtures every item of {@code stringTypes},
     * {@code integerTypes}, {@code decimalTypes}, {@code booleanTypes}, {@code simpleNameReferenceTypes},
     * {@code arrayTypes}, {@code unionTypes}, {@code intersectionTypes}, {@code anyDataTypes},
     * {@code tupleTypes}, {@code functionTypes} and {@code typeDescriptorTypes} has exactly the same key
     * set — a name, a description, and the whole of a {@link TypeNode}. The four remaining categories
     * ({@code anyTypes}, {@code mapTypes}, {@code streamTypes}, {@code tableTypes}, {@code xmlTypes}) are
     * empty in every fixture and are read the same way, because they are siblings in Central's model and
     * a wrong guess surfaces as a located {@code schema-drift} failure rather than as silence.
     *
     * <p>{@code type} is the declaration object read AS a type node, which is not a trick — a Ballerina
     * type alias IS a name bound to a type descriptor, and that is how Central publishes it. Reading it
     * this way is what makes {@code simpleNameReferenceTypes}' resolved target available at all.
     */
    public record AliasDecl(String name, Optional<String> description, TypeNode type) { }

    /**
     * A class, an object type, a service type or a listener — one shape, four categories.
     *
     * <p>Also verified: {@code classes} and {@code listeners} carry {@code initMethod}/{@code isService}/
     * {@code isIsolated}, {@code objectTypes} and {@code serviceTypes} carry {@code isDistinct}, and every
     * key beyond the name is optional in at least one of them, so one lenient shape covers all four
     * without pretending the differences do not exist.
     *
     * <p>{@code otherMethods} is Central's name for the methods that are neither remote nor resource;
     * {@code methods} holds the ones that are. A caller needs both, and the previous reader took the name
     * and discarded every one of them.
     */
    public record ObjectDecl(
            String name,
            Optional<String> description,
            boolean isDeprecated,
            boolean isIsolated,
            boolean isReadOnly,
            boolean isService,
            boolean isDistinct,
            Optional<Method> initMethod,
            Optional<List<Field>> fields,
            Optional<List<Method>> methods,
            Optional<List<Method>> otherMethods) {

        public List<Field> fieldList() {
            return fields.orElse(List.of());
        }

        public List<Method> methodList() {
            return methods.orElse(List.of());
        }

        public List<Method> otherMethodList() {
            return otherMethods.orElse(List.of());
        }
    }

    /**
     * A module-level {@code public} variable or {@code configurable}.
     *
     * <p>One shape for both, again by measurement: http's 61 variables and 13 configurables and graphql's
     * 3 and 1 all carry the same six keys. Neither was read at all, so a configurable — which is the one
     * declaration a deployer must set — appeared in no verb.
     */
    public record VariableDecl(
            String name,
            Optional<String> description,
            Optional<String> defaultValue,
            boolean isReadOnly,
            boolean isDeprecated,
            TypeNode type) { }

    /**
     * An error declaration, which carries two facts beyond a name.
     *
     * <p>{@code isDistinct} is required under this package's usual rule — verified present on all 74
     * error declarations across the nine fixtures, so its absence means the payload changed shape
     * rather than that a package is unusual.
     *
     * <p>{@code detailType} is optional because six of the nine module roots genuinely publish none: an
     * error at the top of its own hierarchy ({@code http:Error}, {@code kafka:Error}) narrows nothing.
     * Despite the name it holds the distinct SUPERTYPE, and the reader calls it {@code base} from here
     * on.
     */
    public record ErrorDecl(
            String name, Optional<String> description, boolean isDistinct, Optional<TypeNode> detailType) { }

    public record RecordDecl(
            String name,
            Optional<String> description,
            boolean isClosed,
            boolean isReadOnly,
            boolean isDeprecated,
            Optional<List<Field>> fields) {

        public List<Field> declaredFields() {
            return fields.orElse(List.of());
        }
    }

    public record Constant(String name, Optional<String> description, String value, TypeNode type) { }

    public record EnumDecl(
            String name, Optional<String> description, boolean isDeprecated, Optional<List<Named>> members) {

        public List<Named> memberList() {
            return members.orElse(List.of());
        }
    }

    /**
     * A client class.
     *
     * <p>{@code isIsolated} is required under this package's usual rule: it is set on all 18 clients across the
     * nine fixtures, and every one of their sources writes {@code public isolated client class}. Its absence
     * would mean the payload changed shape rather than that a package is unusual.
     */
    public record Client(
            String name,
            Optional<String> description,
            boolean isDeprecated,
            boolean isIsolated,
            Optional<List<Method>> methods) {

        public List<Method> methodList() {
            return methods.orElse(List.of());
        }
    }

    /**
     * A listener: an {@link ObjectDecl} plus the {@code attach}/{@code detach}/{@code start} contract
     * Central files separately under {@code lifeCycleMethods}.
     *
     * <p>Composed rather than flattened because that is what Central publishes, and because the listener's
     * {@code init} — the whole of what a {@code service … on new Listener(…)} template needs — lives in
     * the object half.
     */
    public record Listener(ObjectDecl object, Optional<List<Method>> lifeCycleMethods) {

        public String name() {
            return object.name();
        }

        public List<Parameter> initParameterList() {
            return object.initMethod().map(Method::params).orElse(List.of());
        }

        public List<Method> lifeCycleMethodList() {
            return lifeCycleMethods.orElse(List.of());
        }
    }

    /**
     * An annotation the module DECLARES. {@code attachmentPoints} is comma-separated, e.g.
     * {@code "service, type"}, and {@code type} is the record an attachment's argument must be — absent
     * for the marker annotations that take none.
     */
    public record Annotation(
            String name,
            Optional<String> description,
            Optional<String> attachmentPoints,
            boolean isDeprecated,
            Optional<TypeNode> type) { }

    /**
     * The module. Every array here is read as a BUCKET, not as a required key: Central omits a bucket
     * when the module has none of that kind, so absent and empty describe the same module.
     *
     * <p>This used to say the opposite — that every array is always present, so a missing one means the
     * payload changed shape rather than that a package is unusual. That was measured and it is false.
     * Requiring them cost the tool ~15% of Central: `pinecone.vector`, `weaviate`, `azure_cosmosdb` (at
     * every published version) and `sendgrid` each failed EVERY verb with a wall of "expected an array,
     * received nothing", and an agent left with no readable signatures hand-rolled the connector over
     * {@code http:Client}. A reader that refuses a whole package over an absent empty list is worse than
     * one that reports the list as empty. See {@code Schema.Cursor#bucket}.
     *
     * <p>{@code description} is a different kind of exception, and the one field whose absence must not
     * cost the caller anything else: it is the module's own written guide — the same bytes the published
     * {@code .bala} keeps at {@code docs/README.md} — and a package that never wrote a {@code Module.md}
     * should still render its API.
     */
    public record Module(
            String id,
            String orgName,
            Optional<String> summary,
            Optional<String> description,
            List<RecordDecl> records,
            List<AliasDecl> stringTypes,
            List<AliasDecl> integerTypes,
            List<AliasDecl> decimalTypes,
            List<AliasDecl> booleanTypes,
            List<AliasDecl> simpleNameReferenceTypes,
            List<AliasDecl> arrayTypes,
            List<AliasDecl> unionTypes,
            List<AliasDecl> intersectionTypes,
            List<AliasDecl> anyDataTypes,
            List<AliasDecl> anyTypes,
            List<AliasDecl> tupleTypes,
            List<AliasDecl> functionTypes,
            List<AliasDecl> typeDescriptorTypes,
            List<AliasDecl> mapTypes,
            List<AliasDecl> streamTypes,
            List<AliasDecl> tableTypes,
            List<AliasDecl> xmlTypes,
            List<ErrorDecl> errors,
            List<Constant> constants,
            List<EnumDecl> enums,
            List<ObjectDecl> classes,
            List<ObjectDecl> objectTypes,
            List<Client> clients,
            List<Method> functions,
            List<Listener> listeners,
            List<ObjectDecl> serviceTypes,
            List<Annotation> annotations,
            List<VariableDecl> variables,
            List<VariableDecl> configurables) { }
}
