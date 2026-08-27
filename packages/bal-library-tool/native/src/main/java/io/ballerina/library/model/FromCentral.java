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

import io.ballerina.library.Failure;
import io.ballerina.library.QualifiedName;
import io.ballerina.library.Result;
import io.ballerina.library.central.schema.CentralDocs;

import java.util.ArrayList;
import java.util.Arrays;
import java.util.LinkedHashMap;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import java.util.Set;
import java.util.stream.Collectors;

/**
 * Central's docs payload → the {@link Library} IR.
 *
 * <p>This is where Central's flag-bag encoding is decided once and for all: a type node's meaning comes
 * from which of a dozen booleans are set, a function's from {@code isResource}/{@code isRemote}/
 * {@code name.equals("init")}, and a record field's from whether {@code inclusionType} is present.
 * Everything downstream sees a sealed hierarchy, and never a flag.
 *
 * <p>Central files a type alias under one of SEVENTEEN categories by the shape of its right-hand side, and
 * publishes the same object in each: a name, a description, and a type node. They all become
 * {@link TypeDef.Alias}, so a string alias and a tuple alias travel the same path and the renderer has one
 * form to get right.
 *
 * @since 0.1.0
 */
public final class FromCentral {

    /** Central's own placeholder for "no owning module", which counts as absent. */
    private static final String NO_ORG = "UNK_ORG";

    private FromCentral() {
    }

    /** The module a name belongs to, for deciding whether it needs a prefix. */
    private record Scope(String moduleId, String orgName) { }

    /**
     * A scope no module can equal, so every name it qualifies comes out prefixed.
     *
     * <p>For the one section written from the caller's side rather than the package's: a service template is
     * code someone writes in their OWN module, where the package's own names are all foreign. Central never
     * publishes an empty {@code orgName}/{@code moduleName} pair for a reference, so this matches nothing.
     */
    private static final Scope FOREIGN = new Scope("", "");

    /** Which of Central's three type encodings a node uses. */
    private enum Encoding {
        EXTERNAL,
        BASIC,
        REF
    }

    // -----------------------------------------------------------------------
    // Type expressions
    // -----------------------------------------------------------------------

    private static Encoding classify(CentralDocs.TypeNode type) {
        boolean owned = type.orgName().filter(org -> !org.isEmpty() && !NO_ORG.equals(org)).isPresent()
                && type.moduleName().filter(module -> !module.isEmpty()).isPresent();
        if (owned) {
            return Encoding.EXTERNAL;
        }
        boolean named = type.name().filter(name -> !name.isEmpty()).isPresent()
                && type.category().filter(category -> !category.isEmpty()).isPresent();
        return named ? Encoding.BASIC : Encoding.REF;
    }

    /** {@code googleapis.gmail} → {@code gmail}: the alias an import statement puts in scope. */
    private static String moduleNameSuffix(String moduleName) {
        String[] parts = moduleName.split("\\.", -1);
        return parts.length == 0 ? moduleName : parts[parts.length - 1];
    }

    /**
     * A type node's name, trimmed.
     *
     * <p>Central pads some singleton and literal type names with surrounding whitespace — 21 of slack's
     * nodes, all of category {@code other}. Its own page gets away with it because HTML collapses the
     * padding; a plain-text declaration does not, and the padding lands either in front of a field's
     * indentation or in the middle of a union. Every other string the reader takes from Central already
     * goes through {@link #trimmed}; type names were the exception.
     */
    private static String typeName(CentralDocs.TypeNode type) {
        return trimmed(type.name());
    }

    private static TypeRef ref(String name, List<TypeRef.Link> links) {
        return links.isEmpty() ? new TypeRef(name) : new TypeRef(name, List.copyOf(links));
    }

    private static TypeRef optional(TypeRef type, boolean isNullable) {
        return isNullable ? ref(type.name() + "?", type.links()) : type;
    }

    private static TypeRef mergeMembers(List<TypeRef> types, String separator) {
        List<TypeRef.Link> links = types.stream().flatMap(type -> type.links().stream()).toList();
        return ref(types.stream().map(TypeRef::name).collect(Collectors.joining(separator)), links);
    }

    /**
     * How many {@code []} pairs a node carries.
     *
     * <p>{@code arrayDimensions} is the count and {@code isArrayType} is the same fact as a boolean;
     * Central sets both, and reading only the boolean is what made a 2-D array print as 1-D. The boolean
     * is still the fallback, because it is the older of the two keys and a payload that dropped the count
     * should degrade to one dimension rather than to none.
     */
    private static int arrayDepth(CentralDocs.TypeNode type) {
        if (type.arrayDimensions() > 0) {
            return type.arrayDimensions();
        }
        return type.isArrayType() ? 1 : 0;
    }

    /**
     * {@code []} per dimension, then {@code ?} — the order Ballerina writes them, and the order that makes
     * {@code T[]?} "a nullable array" rather than "an array of nullables".
     */
    private static TypeRef suffixed(TypeRef type, CentralDocs.TypeNode node) {
        String name = type.name() + "[]".repeat(arrayDepth(node));
        return ref(node.isNullable() ? name + "?" : name, type.links());
    }

    /**
     * Does a suffix on this node need the type it wraps in parentheses first?
     *
     * <p>{@code string|()[]} is an array of nil unioned with a string; {@code (string|())[]} is an array of
     * either. Ballerina reads the postfix {@code []} as binding tighter than {@code |}, so a union that
     * takes an array suffix without parentheses is a different type — not a cosmetic difference.
     */
    private static boolean needsParentheses(CentralDocs.TypeNode inner, CentralDocs.TypeNode outer) {
        return arrayDepth(outer) > 0 && (inner.isAnonymousUnionType() || inner.isIntersectionType());
    }

    /** The same names, wrapped in brackets or parentheses. */
    private static TypeRef wrapped(TypeRef type, String open, String close) {
        return ref(open + type.name() + close, type.links());
    }

    /** A type the reader cannot encode. Named, because {@code ""} at a use site reads as a bug. */
    private static final TypeRef UNKNOWN = new TypeRef("");

    /**
     * A function type: {@code isolated function (A, B) returns C}.
     *
     * <p>Central models it as a member node with {@code isLambda}, its parameters in {@code paramTypes}
     * and its result in {@code returnType} — the only place either key appears. Parameter NAMES are not
     * published, so the rendered form is the type-only spelling, which is what a variable of this type is
     * declared with anyway.
     */
    private static TypeRef lambda(CentralDocs.TypeNode type, Scope scope) {
        TypeRef params = mergeMembers(transformAll(type.params(), scope), ", ");
        TypeRef returns = type.returnType().map(node -> transformType(node, scope)).orElse(UNKNOWN);
        String qualifier = type.isIsolated() ? "isolated " : "";
        String signature = qualifier + "function (" + params.name() + ")";
        if (returns.name().isEmpty()) {
            return ref(signature, params.links());
        }
        List<TypeRef.Link> links = new ArrayList<>(params.links());
        links.addAll(returns.links());
        return ref(signature + " returns " + returns.name(), links);
    }

    /** The two categories whose members are an anonymous record's own fields. */
    private static boolean isInlineRecord(String category) {
        return "inline_record".equals(category) || "inline_closed_record".equals(category);
    }

    private static TypeRef transformExternal(CentralDocs.TypeNode type, Scope scope) {
        String moduleName = type.moduleName().orElse("");
        String orgName = type.orgName().orElse("");
        String recordName = typeName(type);
        boolean sameModule = moduleName.equals(scope.moduleId()) && orgName.equals(scope.orgName());

        String name = recordName;
        TypeRef.Link link;
        if (sameModule) {
            link = new TypeRef.Link.Internal(recordName);
        } else {
            // The three facts Central publishes, kept as three facts. Quoting a reserved-word path segment,
            // deciding whether an import is needed at all, and pinning a follow-up lookup to the version this
            // package was documented against are all `ModuleRef`'s job now.
            link = new TypeRef.Link.External(
                    new ModuleRef(orgName, moduleName, type.version().orElse("")), recordName);
            name = moduleNameSuffix(moduleName) + ":" + name;
        }

        return suffixed(ref(name, List.of(link)), type);
    }

    /**
     * The three generics Central models with a type argument rather than with members.
     *
     * <p>{@code map} and {@code table} put their argument in {@code constraint} and {@code stream} puts
     * both of its in {@code memberTypes}. A bare {@code table} or {@code map} is not a Ballerina type — the
     * argument is part of the type descriptor — so the fallback is {@code any}, not nothing.
     */
    private static final Map<String, String> CONSTRAINED = Map.of("map", "any", "table", "any");

    private static TypeRef transformBasic(CentralDocs.TypeNode type, Scope scope) {
        String name = typeName(type);

        if ("stream".equals(name)) {
            List<TypeRef> members = type.members().stream().map(member -> transformType(member, scope)).toList();
            // `", "`, not `","`: every `stream<>` in Ballerina's own spec and docs is written with the space,
            // and this is the one place an agent copies a type argument list verbatim into a declaration.
            TypeRef inner = mergeMembers(members, ", ");
            return suffixed(ref("stream<" + inner.name() + ">", inner.links()), type);
        }

        String fallback = CONSTRAINED.get(name);
        if (fallback != null) {
            TypeRef argument = type.constraint()
                    .map(constraint -> transformType(constraint, scope))
                    .orElseGet(() -> new TypeRef(fallback));
            // Suffixed like any other type: the `?` and the `[]` belong OUTSIDE the argument list, and
            // returning early from this branch is what dropped them from 61 of http's parameters.
            return suffixed(ref(name + "<" + argument.name() + ">", argument.links()), type);
        }

        return suffixed(new TypeRef(name), type);
    }

    private static TypeRef transformRef(CentralDocs.TypeNode type, Scope scope) {
        List<CentralDocs.TypeNode> members = type.members();

        if (type.isLambda()) {
            return suffixed(lambda(type, scope), type);
        }
        if (type.isTuple()) {
            // `[A, B]` is already bracketed, so it takes a suffix without parentheses.
            return suffixed(wrapped(mergeMembers(transformAll(members, scope), ", "), "[", "]"), type);
        }
        if (type.isAnonymousUnionType()) {
            return suffixed(mergeMembers(transformAll(members, scope), "|"), type);
        }
        if (type.isIntersectionType()) {
            // Spaced, where the union above is not: `A & B` is how the language spells an intersection, and
            // `A|B` is how it spells a union. Patches.restoreErrorDetailArguments matches on this spelling.
            return suffixed(mergeMembers(transformAll(members, scope), " & "), type);
        }

        String category = type.category().orElse("");
        if (isInlineRecord(category)) {
            CentralDocs.TypeNode first = members.isEmpty() ? null : members.get(0);
            // Named-field form: each member is a field, not a type. The alternative form carries bare
            // types and collapses to an opaque `record {}`.
            if (first != null && first.name().isPresent() && first.elementType().isPresent()) {
                StringBuilder body = new StringBuilder("record {");
                for (CentralDocs.TypeNode member : members) {
                    if (member.elementType().isEmpty()) {
                        continue;
                    }
                    TypeRef fieldType = transformType(member.elementType().get(), scope);
                    body.append(fieldType.name())
                            .append(member.isOptional() ? "?" : "")
                            .append(' ')
                            .append(member.name().orElse(""))
                            .append("; ");
                }
                body.append('}');
                return optional(new TypeRef(body.toString()), type.isNullable());
            }
            return optional(inlineRecord(members, scope), type.isNullable());
        }

        if (type.elementType().isPresent()) {
            CentralDocs.TypeNode inner = type.elementType().get();
            TypeRef result = transformType(inner, scope);
            // Parenthesised either because Central said so, or because the suffix about to be appended
            // would otherwise bind to the union's last member instead of to the whole union. Central sets
            // the flag on 23 of the corpus's nodes and omits it on the ones SQL-04 measured, so the reader
            // cannot rely on it alone.
            if (type.isParenthesisedType() || needsParentheses(inner, type)) {
                result = wrapped(result, "(", ")");
            }
            if (type.isTypeDesc()) {
                // `typedesc<T[]>` would be a descriptor OF an array; the suffix goes outside.
                return suffixed(wrapped(result, "typedesc<", ">"), type);
            }
            return suffixed(result, type);
        }

        if (!type.objectMethods().isEmpty()) {
            // An anonymous object type. `object {}` is the widest true statement available — the methods are
            // there in the payload but rendering them belongs with the rest of the object surface. Measured
            // against the real signature: `object {}|http:ClientError` typechecks where `record {}|…` does
            // not, which is the whole of HTTP-09's cost.
            return suffixed(new TypeRef("object {}"), type);
        }

        if (!members.isEmpty()) {
            return optional(inlineRecord(members, scope), type.isNullable());
        }

        // Nothing to encode: no name, no members, no element type. `record {}` here was the reader's value
        // for "I could not tell" printed as though it were a fact, which is worse than saying nothing —
        // sql's two constants came out typed as records for comparison against an `int?`.
        return UNKNOWN;
    }

    private static List<TypeRef> transformAll(List<CentralDocs.TypeNode> types, Scope scope) {
        return types.stream().map(type -> transformType(type, scope)).toList();
    }

    /** An anonymous record whose fields Central did not describe; only its links survive. */
    private static TypeRef inlineRecord(List<CentralDocs.TypeNode> members, Scope scope) {
        List<TypeRef.Link> links = members.stream()
                .flatMap(member -> transformType(member, scope).links().stream())
                .toList();
        return ref("record {}", links);
    }

    private static TypeRef transformType(CentralDocs.TypeNode type, Scope scope) {
        return switch (classify(type)) {
            case EXTERNAL -> transformExternal(type, scope);
            case BASIC -> transformBasic(type, scope);
            case REF -> transformRef(type, scope);
        };
    }

    // -----------------------------------------------------------------------
    // Functions
    // -----------------------------------------------------------------------

    private static String trimmed(Optional<String> value) {
        return value.orElse("").trim();
    }

    private static String orNull(String value) {
        return value.isEmpty() ? null : value;
    }

    /**
     * A parameter's form, from the two flags Central sets on its TYPE node rather than on the parameter.
     *
     * <p>A rest parameter is published the same way a record's rest field is: {@code isRestParam} on the type
     * node, the parameter's own name repeated as that node's {@code name}, and the real type one level down in
     * {@code elementType}. Reading the node's name instead produced
     * {@code function removeCookiesFromRemoteStore(cookiesToRemove cookiesToRemove);} — the parameter's name
     * standing in as its type, with {@code Cookie} dropped.
     */
    private static Param.Form paramForm(CentralDocs.TypeNode type) {
        if (type.isRestParam()) {
            return Param.Form.REST;
        }
        return type.isInclusion() ? Param.Form.INCLUSION : Param.Form.NORMAL;
    }

    private static List<Param> transformParams(List<CentralDocs.Parameter> parameters, Scope scope) {
        List<Param> params = new ArrayList<>(parameters.size());
        for (CentralDocs.Parameter parameter : parameters) {
            Param.Form form = paramForm(parameter.type());
            CentralDocs.TypeNode node = form == Param.Form.REST
                    ? parameter.type().elementType().orElse(parameter.type())
                    : parameter.type();
            params.add(new Param(
                    parameter.name(),
                    trimmed(parameter.description()),
                    transformType(node, scope),
                    orNull(trimmed(parameter.defaultValue())),
                    form));
        }
        return List.copyOf(params);
    }

    private static ReturnDef transformReturn(CentralDocs.Method method, Scope scope) {
        List<CentralDocs.ReturnParameter> returns = method.returns();
        // A function Central gives no return parameters for returns nothing, and a function that returns
        // nothing has no `returns` clause. The previous stand-in was the word `nil`, which is the English
        // name of the basic type and not a type name the compiler knows.
        if (returns.isEmpty()) {
            return ReturnDef.none();
        }
        CentralDocs.ReturnParameter first = returns.get(0);
        return new ReturnDef(transformType(first.type(), scope), orNull(trimmed(first.description())));
    }

    /**
     * Split a resource path into its segments, keeping a path parameter's type and name apart. Central
     * writes them as {@code [string owner]}; a bracketed segment with no space inside is not a parameter
     * and stays a literal.
     */
    public static List<Fn.PathSegment> createPaths(Optional<String> resourcePath) {
        String path = resourcePath.orElse("");
        if (path.isEmpty()) {
            return List.of();
        }
        List<Fn.PathSegment> segments = new ArrayList<>();
        for (String segment : path.split("/", -1)) {
            if (segment.startsWith("[") && segment.endsWith("]") && segment.length() >= 2) {
                String inner = segment.substring(1, segment.length() - 1);
                int space = inner.indexOf(' ');
                if (space != -1) {
                    segments.add(new Fn.PathSegment.Parameter(
                            inner.substring(0, space), inner.substring(space + 1)));
                    continue;
                }
            }
            segments.add(new Fn.PathSegment.Literal(segment));
        }
        return List.copyOf(segments);
    }

    private static Fn transformMethod(CentralDocs.Method method, Scope scope) {
        List<Param> params = transformParams(method.params(), scope);
        ReturnDef returns = transformReturn(method, scope);
        String description = trimmed(method.description());

        boolean deprecated = method.isDeprecated();
        boolean isolated = method.isIsolated();

        if ("init".equals(method.name())) {
            return new Fn.Constructor(description, params, returns, deprecated, isolated);
        }
        if (method.isResource()) {
            return new Fn.Resource(
                    method.accessor().orElse(""),
                    createPaths(method.resourcePath()),
                    description,
                    params,
                    returns,
                    deprecated,
                    isolated);
        }
        if (method.isRemote()) {
            return new Fn.Remote(method.name(), description, params, returns, deprecated, isolated);
        }
        return new Fn.Normal(method.name(), description, params, returns, deprecated, isolated);
    }

    // -----------------------------------------------------------------------
    // Records
    // -----------------------------------------------------------------------

    /**
     * Fields of one record, with inclusions ({@code *Other;}) spliced in.
     *
     * <p>Keyed by name as it goes: an included field that the record also declares itself must resolve
     * to the declaration, and Central lists them in that order.
     */
    private static List<RecordField> transformRecordFields(List<CentralDocs.Field> declared, Scope scope) {
        List<RecordField> members = new ArrayList<>();
        Set<String> declaredNames = new LinkedHashSet<>();
        for (CentralDocs.Field field : declared) {
            if (field instanceof CentralDocs.Field.Declared entry) {
                declaredNames.add(entry.name());
            }
        }

        for (int index = 0; index < declared.size(); index++) {
            CentralDocs.Field field = declared.get(index);
            switch (field) {
                case CentralDocs.Field.Inclusion inclusion -> {
                    TypeRef included = transformType(inclusion.inclusionType(), scope);
                    if (!included.name().isEmpty()) {
                        members.add(RecordField.inclusion(included));
                        continue;
                    }
                    // Central named no included record, so `*;` is not available. Splicing its members is
                    // the fallback — with a name the record declares ITSELF winning, because the source's
                    // own declaration is the one that holds (PSQL-01).
                    for (CentralDocs.TypeNode member : inclusion.inclusionType().members()) {
                        if (member.name().isEmpty() || member.elementType().isEmpty()
                                || declaredNames.contains(member.name().get())) {
                            continue;
                        }
                        members.add(new RecordField(
                                member.name().get(),
                                trimmed(member.description()),
                                transformType(member.elementType().get(), scope)));
                    }
                }
                case CentralDocs.Field.Declared entry -> {
                    if (isStrandedRest(entry, index == declared.size() - 1)) {
                        continue;
                    }
                    members.add(declaredField(entry, scope));
                }
            }
        }
        return List.copyOf(members);
    }

    /**
     * A rest field somewhere other than the end, which the grammar cannot express.
     *
     * <p>{@code T...;} is only legal as a record's LAST member, so a rest field with declarations after it is
     * not something a source ever wrote — it is a flattening artefact. When Central splices an inclusion in
     * ({@code time:Civil} is {@code *Date; *TimeOfDay;}) it copies each included record's implicit
     * {@code anydata...} along with the members, so {@code Civil} arrives carrying TWO rest fields, at
     * positions 3 and 7 of 11.
     *
     * <p>Rendering those emitted {@code anydata...;} twice mid-record, which the compiler rejects four times
     * over with {@code more record fields after rest field}. Dropping them costs nothing: the record stays
     * inclusive, and {@code record { … }} already means {@code anydata...}. Every genuine rest field in the
     * corpus is the final member, so this leaves all four untouched.
     */
    private static boolean isStrandedRest(CentralDocs.Field.Declared entry, boolean isLast) {
        return entry.type().isRestParam() && !isLast;
    }

    /**
     * Whether a record is closed, which Central's own flag does not always answer.
     *
     * <p>{@code isClosed} is the flag, but a record carrying an explicit rest field is closed by the
     * GRAMMAR: {@code T...;} is only legal in an exclusive descriptor, because an inclusive one already has
     * an implicit {@code anydata...} rest field. Central publishes {@code isClosed: false} for all four
     * rest-field records in the corpus while their sources are all {@code record {| … |}} — so trusting the
     * flag alone emits {@code record { … QueryParamType...; }}, which does not compile.
     */
    private static boolean isClosed(CentralDocs.RecordDecl record, List<RecordField> fields) {
        return record.isClosed()
                || fields.stream().anyMatch(field -> field.form() == RecordField.Form.REST);
    }

    private static RecordField declaredField(CentralDocs.Field.Declared entry, Scope scope) {
        // A rest field is a declared field with no name whose type node carries `isRestParam`; the element
        // type under it is what the extra fields may hold.
        if (entry.type().isRestParam()) {
            return RecordField.rest(trimmed(entry.description()), transformType(entry.type(), scope));
        }
        return new RecordField(
                entry.name(),
                trimmed(entry.description()),
                transformType(entry.type(), scope),
                orNull(trimmed(entry.defaultValue())),
                entry.type().isOptional(),
                entry.isReadOnly(),
                entry.isDeprecated(),
                RecordField.Form.DECLARED);
    }

    // -----------------------------------------------------------------------
    // Objects: classes, object types, service types and listeners
    // -----------------------------------------------------------------------

    /**
     * One object declaration, from the shape Central publishes for all four of its object categories.
     *
     * <p>Only {@code methods} is read. {@code otherMethods}, {@code lifeCycleMethods} and {@code initMethod}
     * are all subsets of it — verified on every object in the corpus, 103 of 103 {@code initMethod}s
     * byte-identical to the {@code init} entry in {@code methods} — so reading them too would print the
     * constructor twice and the lifecycle methods three times.
     */
    private static TypeDef.ObjectDef objectDef(
            CentralDocs.ObjectDecl decl, TypeDef.ObjectDef.Form form, Scope scope) {
        List<Fn> methods = distinctMethods(decl.methodList()).stream()
                .map(method -> transformMethod(method, scope))
                .toList();
        return new TypeDef.ObjectDef(
                decl.name(),
                trimmed(decl.description()),
                form,
                roleOf(decl, methods),
                decl.isDistinct(),
                decl.isReadOnly(),
                decl.isIsolated(),
                decl.isDeprecated(),
                transformRecordFields(decl.fieldList(), scope),
                methods);
    }

    /**
     * One entry per method the object actually declares.
     *
     * <p>{@code 'start} and {@code start} are the SAME identifier — the quote is an escape, not part of the
     * name — and {@code postgresql:CdcListener} publishes both: its own {@code start} and the {@code 'start}
     * it includes from {@code *cdc:Listener}, with different descriptions and the same signature spelled two
     * ways. Emitting both is a redeclared symbol, which the compiler reports and no test would have.
     *
     * <p>First wins, as it does in the declaration index, and for the same reason: Central lists an object's
     * own declaration before the ones it inherits, so the entry that survives is the one the class itself
     * wrote.
     */
    private static List<CentralDocs.Method> distinctMethods(List<CentralDocs.Method> methods) {
        Map<String, CentralDocs.Method> byIdentity = new LinkedHashMap<>();
        for (CentralDocs.Method method : methods) {
            String name = method.name().startsWith("'") ? method.name().substring(1) : method.name();
            String identity = method.isResource()
                    ? "resource:" + method.accessor().orElse("") + " " + method.resourcePath().orElse("")
                    : name;
            byIdentity.putIfAbsent(identity, method);
        }
        return List.copyOf(byIdentity.values());
    }

    /**
     * Whether an object is a client, a service, or neither.
     *
     * <p>Derived, because Central publishes neither fact: there is no {@code isClient} key at all, and
     * {@code isService} is present but false on all 230 objects in the corpus, including the seven
     * {@code http} service types whose own sources say {@code distinct service object}. The grammar settles
     * both — a {@code remote} method is legal only inside a client or service object, and a service type is
     * by definition the object a {@code service … on …} declaration implements — and getting it wrong is not
     * cosmetic: {@code class Client {}} tells a caller to construct an abstract type, and a client whose
     * methods are not marked {@code remote} tells them to call {@code db.query()} instead of
     * {@code db->query()}.
     */
    private static TypeDef.ObjectDef.Role roleOf(CentralDocs.ObjectDecl decl, List<Fn> methods) {
        if (decl.isService()) {
            return TypeDef.ObjectDef.Role.SERVICE;
        }
        boolean remote = methods.stream().anyMatch(Fn.Remote.class::isInstance);
        return remote ? TypeDef.ObjectDef.Role.CLIENT : TypeDef.ObjectDef.Role.PLAIN;
    }

    /** The same object declared as the service object a {@code serviceTypes} entry is. */
    private static TypeDef.ObjectDef asService(TypeDef.ObjectDef object) {
        return new TypeDef.ObjectDef(
                object.name(),
                object.description(),
                object.form(),
                TypeDef.ObjectDef.Role.SERVICE,
                object.isDistinct(),
                object.isReadOnly(),
                object.isIsolated(),
                object.isDeprecated(),
                object.fields(),
                object.methods());
    }

    // -----------------------------------------------------------------------
    // Services
    // -----------------------------------------------------------------------

    /**
     * Pair every listener the module declares with every service type, producing the
     * {@code service X on new Y(...)} template plus the contract the service must implement.
     *
     * <p>Every listener, not the first: {@code ballerina/email} publishes an IMAP listener and a POP one,
     * the readme's examples all use the POP one, and taking {@code listeners().get(0)} named the other and
     * discarded this one with no diagnostic. A module with service types but no listener (or the reverse)
     * describes no service worth templating.
     *
     * <p>Built under {@link #FOREIGN} rather than the module's own scope, which is the one place in this
     * reader where that is right. Every other section declares the package's API and so writes its own names
     * bare; a service template is code the CALLER writes, in the caller's module, where every one of the
     * package's names — the service type, the listener AND the contract's parameter and return types — is
     * foreign. Under the module's own scope the block came out as
     * {@code service http:Service on new http:Listener(…)} whose method returned an unqualified
     * {@code Interceptor}, so the two halves of one line disagreed about whose module they were in and the
     * template did not resolve.
     */
    /**
     * The service type this listener's {@code attach} takes, or {@code null}.
     *
     * <p>HTTP-14. This is the only attachability signal the payload carries, and the reason it is only a
     * partial one is worth stating: {@code attach} names {@code Service}, and a {@code distinct service object}
     * type is a subtype of it only by INCLUDING it — {@code ballerina/http}'s {@code ServiceContract} and
     * {@code InterceptableService} both write {@code *Service;} where its four interceptor types write nothing.
     * Central publishes neither: measured on the recorded payload, {@code ServiceContract} arrives carrying
     * {@code description}, {@code isDistinct} and {@code name} and no fields, no methods and no
     * {@code inclusionType}, so it is byte-for-byte indistinguishable from {@code RequestInterceptor}.
     *
     * <p>So the equality test is deliberately narrow rather than a guess at the subtype relation. What it costs
     * is two templates that WOULD have compiled; what it buys is that no template is printed which does not.
     *
     * <p>{@code null} when the payload publishes no {@code attach} at all, and the caller then treats every
     * service type as attachable — the behaviour before this narrowing. Absent evidence is not evidence of
     * absence, and suppressing every template for a package whose payload merely omits the lifecycle methods
     * would lose the section wholesale. All three of the corpus's service packages publish it.
     */
    private static String attachedTypeName(CentralDocs.Listener listener) {
        // `attach` is a LIFECYCLE method — Central files it apart from the listener's own methods — and its
        // FIRST parameter is the service; the second is the optional attachment path.
        return listener.lifeCycleMethodList().stream()
                .filter(method -> "attach".equals(method.name()))
                .flatMap(method -> method.params().stream().limit(1))
                .map(param -> param.type().name().orElse(""))
                .filter(name -> !name.isEmpty())
                .findFirst()
                .orElse(null);
    }

    private static boolean attachable(CentralDocs.ObjectDecl serviceType, CentralDocs.Listener listener) {
        String attached = attachedTypeName(listener);
        return attached == null || serviceType.name().equals(attached);
    }

    private static List<Service> buildServices(CentralDocs.Module module) {
        if (module.listeners().isEmpty() || module.serviceTypes().isEmpty()) {
            return List.of();
        }
        List<Service> services = new ArrayList<>();
        for (CentralDocs.ObjectDecl serviceType : module.serviceTypes()) {
            // Every method, whatever its form. A service type's contract is usually remote methods, but
            // graphql's is resource functions, and dropping the ones that were not `remote function <name>`
            // dropped those.
            List<Fn> methods = serviceType.methodList().stream()
                    .map(method -> transformMethod(method, FOREIGN))
                    .toList();
            for (CentralDocs.Listener listener : module.listeners()) {
                services.add(new Service(
                        serviceType.name(),
                        serviceType.isDeprecated(),
                        new Service.Listener(
                                module.id() + ":" + listener.name(),
                                transformParams(listener.initParameterList(), FOREIGN)),
                        methods,
                        attachable(serviceType, listener)));
            }
        }
        return List.copyOf(services);
    }

    /**
     * Every annotation the module declares, with its config record and the whole of its {@code on} clause.
     *
     * <p>All of them, and Central's own clause verbatim. The reader used to keep the two attachment points a
     * service author writes and drop the rest, on the argument that an annotation attaching to a parameter or a
     * record field "is not something the agent reaches for from a service". For {@code ballerina/http} that
     * argument fails on its own document: {@code @http:Payload}, {@code @http:Header} and {@code @http:Query} are
     * the three most common annotations in Ballerina HTTP code, two of them are named in the doc comments of
     * {@code getHeaderMap} and {@code getQueryMap} and shown in their fenced examples, and all three were deleted
     * a few lines below. Nine of the corpus's twelve went that way.
     *
     * <p>The clause is passed through rather than mapped onto a closed set. Checked against the published
     * sources, Central's string matches the {@code on} clause exactly for all twelve — including the order — so
     * mapping it could only lose information, and the mapping that existed invented {@code service_function},
     * which is not a token the language has.
     */
    private static List<Library.AnnotationDef> buildAnnotations(CentralDocs.Module module, Scope scope) {
        List<Library.AnnotationDef> annotations = new ArrayList<>();
        for (CentralDocs.Annotation annotation : module.annotations()) {
            String points = Arrays.stream(annotation.attachmentPoints().orElse("").split(",", -1))
                    .map(String::trim)
                    .filter(point -> !point.isEmpty())
                    .collect(Collectors.joining(", "));
            annotations.add(new Library.AnnotationDef(
                    annotation.name(),
                    trimmed(annotation.description()),
                    annotation.type().map(type -> transformType(type, scope)),
                    points));
        }
        return List.copyOf(annotations);
    }

    // -----------------------------------------------------------------------
    // Entry point
    // -----------------------------------------------------------------------

    /**
     * The module of the payload that the caller actually asked for.
     *
     * <p>Reading the first module instead is untested by construction, because every fixture in the
     * corpus is single-module: a multi-module package would render whichever module Central happened to
     * put first, under the name the caller typed. It is also what makes the cache's coordinate check
     * meaningful; verifying one module and then rendering another verifies nothing.
     *
     * <p>{@code id.equals(name)} is the package's default module — the one {@code import org/name;} puts
     * in scope. {@code id.startsWith(name + ".")} catches the submodule form ({@code googleapis.gmail}
     * under {@code googleapis}), which Central names the same way.
     */
    public static Result<CentralDocs.Module> selectModule(CentralDocs docs, QualifiedName qualified) {
        for (CentralDocs.Module module : docs.modules()) {
            boolean named = module.id().equals(qualified.name())
                    || module.id().startsWith(qualified.name() + ".");
            if (module.orgName().equals(qualified.org()) && named) {
                return Result.ok(module);
            }
        }
        String returned = docs.modules().stream()
                .map(module -> module.orgName() + "/" + module.id())
                .collect(Collectors.joining(", "));
        return Result.err(new Failure.SchemaDrift(
                qualified.qualified(),
                List.of(new Failure.SchemaIssue(
                        "docsData.modules", "no module matches; Central returned " + returned)),
                Failure.SCHEMA_DRIFT_SUGGESTION));
    }

    public static Library fromCentral(CentralDocs.Module module) {
        Scope scope = new Scope(module.id(), module.orgName());
        List<TypeDef> typeDefs = new ArrayList<>();

        for (CentralDocs.RecordDecl record : module.records()) {
            List<RecordField> fields = transformRecordFields(record.declaredFields(), scope);
            typeDefs.add(new TypeDef.Rec(
                    record.name(),
                    trimmed(record.description()),
                    isClosed(record, fields),
                    record.isDeprecated(),
                    fields));
        }

        // Ordering is part of the output contract — a reader greps the API document and the section a
        // name lands in is how they find it. Aliases Central models as dedicated node kinds all render
        // the same way, so they are grouped by the order Central lists them rather than merged.
        addAliases(typeDefs, module.stringTypes(), scope);
        addAliases(typeDefs, module.integerTypes(), scope);
        addAliases(typeDefs, module.decimalTypes(), scope);
        addAliases(typeDefs, module.arrayTypes(), scope);
        for (CentralDocs.ErrorDecl error : module.errors()) {
            typeDefs.add(new TypeDef.ErrorDef(
                    error.name(),
                    trimmed(error.description()),
                    error.isDistinct(),
                    error.detailType().map(detail -> transformType(detail, scope)),
                    isDetailRecord(error.detailType())));
        }

        for (CentralDocs.Constant constant : module.constants()) {
            typeDefs.add(new TypeDef.Constant(
                    constant.name(),
                    trimmed(constant.description()),
                    constant.value(),
                    transformType(constant.type(), scope)));
        }

        for (CentralDocs.EnumDecl enumeration : module.enums()) {
            typeDefs.add(new TypeDef.Enumeration(
                    enumeration.name(),
                    trimmed(enumeration.description()),
                    // The member's description as well as its name. Taking `Named::name` and dropping the rest
                    // was a one-line loss at the IR builder, exactly like `arrayTypes` in SLACK-01.
                    enumeration.memberList().stream()
                            .map(member -> new TypeDef.Enumeration.Member(
                                    member.name(), trimmed(member.description())))
                            .toList()));
        }

        // Grouped, because they are one family: a class, an object type and a service type differ in what
        // their declaration says, not in what Central sends. The previous reader took the name of each and
        // discarded everything else — 340 declarations across the corpus, on the argument that the callable
        // surface lives in the clients section. For `ballerina/sql` that argument fails in both halves:
        // `clients` is empty and every one of its 122 methods is on a class or an object type.
        for (CentralDocs.ObjectDecl cls : module.classes()) {
            typeDefs.add(objectDef(cls, TypeDef.ObjectDef.Form.CLASS, scope));
        }
        for (CentralDocs.ObjectDecl objectType : module.objectTypes()) {
            typeDefs.add(objectDef(objectType, TypeDef.ObjectDef.Form.OBJECT_TYPE, scope));
        }
        for (CentralDocs.ObjectDecl serviceType : module.serviceTypes()) {
            // A service type IS a service object, whatever `isService` says — see `roleOf`.
            typeDefs.add(asService(objectDef(serviceType, TypeDef.ObjectDef.Form.OBJECT_TYPE, scope)));
        }

        addAliases(typeDefs, module.unionTypes(), scope);
        addAliases(typeDefs, module.intersectionTypes(), scope);

        addAliases(typeDefs, module.simpleNameReferenceTypes(), scope);
        addAliases(typeDefs, module.booleanTypes(), scope);

        // The nine categories nothing read until now. Appended rather than interleaved: the order above is
        // the output contract a reader greps by, and moving an existing declaration to make room for a new
        // one would be a second change nobody asked for.
        addAliases(typeDefs, module.anyDataTypes(), scope);
        addAliases(typeDefs, module.anyTypes(), scope);
        addAliases(typeDefs, module.tupleTypes(), scope);
        addAliases(typeDefs, module.functionTypes(), scope);
        addAliases(typeDefs, module.typeDescriptorTypes(), scope);
        addAliases(typeDefs, module.mapTypes(), scope);
        addAliases(typeDefs, module.streamTypes(), scope);
        addAliases(typeDefs, module.tableTypes(), scope);
        addAliases(typeDefs, module.xmlTypes(), scope);
        addVariables(typeDefs, module, scope);

        List<ClientClass> clients = module.clients().stream()
                .map(client -> new ClientClass(
                        client.name(),
                        trimmed(client.description()),
                        client.isIsolated(),
                        client.methodList().stream().map(method -> transformMethod(method, scope)).toList()))
                .toList();

        // Module-level resource functions and constructors are not callable as free functions, so only
        // the two forms that are get listed.
        List<Fn.Standalone> functions = module.functions().stream()
                .map(method -> transformMethod(method, scope))
                .filter(Fn.Standalone.class::isInstance)
                .map(Fn.Standalone.class::cast)
                .toList();

        // A listener is a class, and Central publishes it as one. It gets its own section rather than a place
        // among the type declarations because it is the entry point to a package's service half — postgresql's
        // `CdcListener` printed nowhere at all, and its readme's own example names it.
        List<TypeDef.ObjectDef> listeners = module.listeners().stream()
                .map(listener -> objectDef(listener.object(), TypeDef.ObjectDef.Form.CLASS, scope))
                .toList();

        return new Library(
                module.orgName() + "/" + module.id(),
                trimmed(module.summary()),
                List.copyOf(typeDefs),
                clients,
                functions,
                listeners,
                buildServices(module),
                buildAnnotations(module, scope),
                buildConfigurables(module, scope));
    }

    /**
     * The module's {@code configurable} declarations, which are settings rather than API.
     *
     * <p>Kept out of the declaration list on purpose. A {@code configurable} is module-private — referencing
     * {@code http:maxActiveConnections} from another module is {@code attempt to refer to non-accessible
     * symbol} — so printing one among the declarations would offer a caller a name they cannot write, and
     * printing it with the blanket {@code public} the others carry would be a second error on the same line.
     * What a caller CAN do with it is set it in {@code Config.toml}, which is a report-register fact.
     */
    private static List<Library.Configurable> buildConfigurables(CentralDocs.Module module, Scope scope) {
        return module.configurables().stream()
                .map(configurable -> new Library.Configurable(
                        configurable.name(),
                        trimmed(configurable.description()),
                        transformType(configurable.type(), scope),
                        trimmed(configurable.defaultValue())))
                .toList();
    }

    /**
     * The module's {@code public final} variables, as declarations.
     *
     * <p>These ARE public API: {@code http:CONTINUE} compiles from another module. All 64 in the corpus were
     * dropped, so a caller had no way to learn from this tool that the package exports 61 pre-built status
     * values — the names its own signatures and readme use.
     */
    private static void addVariables(List<TypeDef> typeDefs, CentralDocs.Module module, Scope scope) {
        for (CentralDocs.VariableDecl variable : module.variables()) {
            typeDefs.add(new TypeDef.Variable(
                    variable.name(),
                    trimmed(variable.description()),
                    transformType(variable.type(), scope),
                    trimmed(variable.defaultValue()),
                    variable.isReadOnly()));
        }
    }

    /**
     * Whether an error's {@code detailType} is its DETAIL RECORD rather than its supertype.
     *
     * <p>Central overloads the one key, and only {@code category} separates the two: {@code "errors"} is
     * a supertype ({@code distinct ClientError}), anything else is the detail record, which the language
     * writes as {@code distinct error<Detail>}. Reading category-less as a supertype keeps every fixture
     * rendering as before — all nine publish {@code "errors"} — while a record-detail package like
     * {@code ballerinax/health.clients.fhir} stops printing a declaration that is not an error type.
     */
    private static boolean isDetailRecord(Optional<CentralDocs.TypeNode> detailType) {
        return detailType.flatMap(CentralDocs.TypeNode::category).filter(c -> !"errors".equals(c)).isPresent();
    }

    private static void addAliases(List<TypeDef> typeDefs, List<CentralDocs.AliasDecl> aliases, Scope scope) {
        for (CentralDocs.AliasDecl alias : aliases) {
            typeDefs.add(new TypeDef.Alias(
                    alias.name(), trimmed(alias.description()), transformType(alias.type(), scope)));
        }
    }

}
