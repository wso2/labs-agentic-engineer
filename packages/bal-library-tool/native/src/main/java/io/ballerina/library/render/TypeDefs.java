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
import io.ballerina.library.model.RecordField;
import io.ballerina.library.model.TypeDef;
import io.ballerina.library.model.TypeRef;

import java.util.ArrayList;
import java.util.List;
import java.util.regex.Pattern;
import java.util.stream.Collectors;

/**
 * One module-level declaration, as Ballerina.
 *
 * <p>{@link #renderTypeDef} switches over every case with no {@code default}: adding a Ballerina shape to
 * the IR fails the build here until it has a rendering. It is also the whole of the {@code type} verb's
 * output — that verb selects declarations and prints them, so a declaration read by name is
 * byte-identical to the same declaration inside the API document.
 *
 * <p>Spacing is uniform across kinds and that is deliberate: a doc comment is adjacent to the declaration it
 * documents, and a declaration without one starts at its first line. Every renderer therefore concatenates
 * {@link Signatures#renderDescription}'s output instead of joining it, because that output already ends in a
 * newline. Records did not, which is what put a blank line inside nine of email's seventeen declarations.
 *
 * @since 0.1.0
 */
public final class TypeDefs {

    /**
     * A string constant's value as Central sends it: WITH its quotes.
     *
     * <p>Adding another pair produced {@code const string AUTH_SASL_PLAIN = ""PLAIN"";}, which is not
     * valid Ballerina. It affected all 108 string constants in the corpus, with zero counter-examples.
     * The quotes are still added when they are absent rather than dropped outright, because "already
     * quoted" is an observation about today's payload and an unquoted value would otherwise render as a
     * bare identifier — a worse failure, since it would look like a reference to something rather than
     * like a typo.
     */
    private static final Pattern QUOTED = Pattern.compile("^\".*\"$", Pattern.DOTALL);

    /**
     * Every declaration in this document is {@code public}, and that is measured rather than assumed.
     *
     * <p>Central's docs payload carries no declaration-level visibility at all — the one {@code isPublic} key it
     * sets lives on TYPE REFERENCE nodes, where it describes the referent — so the fact has to come from what
     * Central chooses to publish. Across the seven fixtures whose sources are on disk it publishes 2,085 public
     * type declarations, 229 classes, 178 constants, 13 enums, 10 annotations and 9 module functions, and withholds
     * all 708 module-private declarations: 42 classes, 118 constants, 5 enums, 459 functions and 84 types. Zero
     * counter-examples. The one published category that is NOT public in the source is {@code configurable}
     * (12 of 14), and configurables are not rendered.
     *
     * <p>Without it the document is not a declaration a caller can copy: a type lifted into their own module comes
     * out module-private, which compiles until something outside the module needs it.
     */
    private static final String PUBLIC = "public ";

    /**
     * Which body a field is being written into, because the language spells the two differently.
     *
     * <p>A class or object-type field takes {@code public}; a RECORD field cannot — {@code public} there is
     * {@code invalid token 'public'}, checked by compiling it. Central publishes only the public fields of an
     * object (185 of 185 across the corpus, with all 61 private ones withheld), so every field that reaches this
     * renderer from an object body is one, and omitting the qualifier is not cosmetic: an including class must
     * repeat the visibility of the field it overrides, so the 82 fields postgresql's value classes take from
     * {@code *sql:TypedValue} each produced {@code mismatched visibility qualifiers for field 'value' with object
     * type inclusion}.
     */
    private enum Owner { RECORD, OBJECT }

    private TypeDefs() {
    }

    public static String renderTypeDef(TypeDef typeDef) {
        return switch (typeDef) {
            case TypeDef.Rec record -> renderRecord(record);
            case TypeDef.Enumeration enumeration -> renderEnum(enumeration);
            case TypeDef.Alias alias -> renderAlias(alias);
            case TypeDef.Constant constant -> renderConstant(constant);
            case TypeDef.Variable variable -> renderVariable(variable);
            case TypeDef.ObjectDef object -> renderObject(object);
            case TypeDef.ErrorDef error -> renderError(error);
        };
    }

    /**
     * A record, in the form that says whether it accepts fields it does not declare.
     *
     * <p>{@code record {| |}} is closed and {@code record { }} is open, and the difference decides whether
     * a value carrying an extra field type-checks. Central publishes it as {@code isClosed} and the reader
     * used to drop it, so 418 closed records across the corpus read as open.
     */
    private static String renderRecord(TypeDef.Rec typeDef) {
        String open = typeDef.isClosed() ? " record {|" : " record {";
        String close = typeDef.isClosed() ? "|};" : "};";
        List<String> lines = new ArrayList<>();
        if (typeDef.isDeprecated()) {
            lines.add("@deprecated");
        }
        lines.add(PUBLIC + "type " + typeDef.name() + open);
        for (RecordField field : typeDef.fields()) {
            lines.add(renderRecordField(field, Owner.RECORD));
        }
        lines.add(close);
        // Concatenated rather than newline-joined, for the reason `renderEnum` gives below: joining a
        // description that already ends in a newline put a blank line between a record's doc comment and the
        // declaration it documents, and a leading blank line in front of an undescribed one.
        return Signatures.renderDescription(typeDef.description()) + String.join("\n", lines);
    }

    /**
     * One record member, in the form it is written.
     *
     * <p>{@code readonly} sits inside the type and {@code @deprecated} sits above the member, which is
     * where Ballerina puts each of them: the first is part of what the field IS, the second is a note about
     * using it.
     */
    private static String renderRecordField(RecordField field, Owner owner) {
        List<Signatures.ExternalLink> links = Signatures.collectExternalLinks(field.type());
        String typeName = Signatures.applyPrefixToTypeName(field.type().name(), links);
        String note = Signatures.buildSpecialAgentNote(links);
        String description = Signatures.renderDocComment(field.description(), "    ");
        return switch (field.form()) {
            // An inclusion takes no visibility qualifier in either body: `*sql:TypedValue;` is a statement about
            // where the members come from, not a member of its own.
            case INCLUSION -> description + "    *" + typeName + ";" + note;
            // No doc comment: Central's description for a rest field is the literal string "Rest field",
            // which is its label for the FORM and not documentation. The source writes none either.
            case REST -> "    " + typeName + "...;" + note;
            case DECLARED -> {
                String deprecated = field.deprecated() ? "    @deprecated\n" : "";
                String visibility = owner == Owner.OBJECT ? PUBLIC : "";
                String readonly = field.readonly() ? "readonly " : "";
                String optional = field.optional() ? "?" : "";
                String defaultValue = field.hasDefault() ? " = " + field.defaultValue() : "";
                // The note is rebuilt here rather than reused, because an unwritable default and a foreign
                // type name must end up in ONE trailing comment.
                String caveat = Signatures.buildSpecialAgentNote(
                        links, field.unwritableDefault() ? List.of(field.defaultValue()) : List.of());
                yield description + deprecated + "    " + visibility + readonly + typeName + " "
                        + Identifiers.write(field.name())
                        + optional + defaultValue + ";" + caveat;
            }
        };
    }

    /**
     * A class, an object type, a service type or a listener, with everything it declares.
     *
     * <p>Two forms, and the difference is not cosmetic: {@code class X { }} is instantiable and
     * {@code type X object { };} is a contract that is not, so printing every object type as a class told a
     * caller to {@code new} an abstract type — for {@code sql:Client}, the front door of every Ballerina
     * database connector.
     *
     * <p>The qualifiers are ordered as the language's own libraries write them:
     * {@code public distinct readonly isolated client class X}, with {@code public} outermost on a class and
     * before the NAME on an object type ({@code public type X distinct isolated client object { }}) — which is
     * where the language puts it, and where compiling both forms confirmed it belongs.
     */
    private static String renderObject(TypeDef.ObjectDef typeDef) {
        boolean isClass = typeDef.form() == TypeDef.ObjectDef.Form.CLASS;
        StringBuilder quals = new StringBuilder();
        if (typeDef.isDistinct()) {
            quals.append("distinct ");
        }
        if (typeDef.isReadOnly()) {
            quals.append("readonly ");
        }
        if (typeDef.isIsolated()) {
            quals.append("isolated ");
        }
        quals.append(switch (typeDef.role()) {
            case CLIENT -> "client ";
            case SERVICE -> "service ";
            case PLAIN -> "";
        });

        List<String> lines = new ArrayList<>();
        if (typeDef.isDeprecated()) {
            lines.add("@deprecated");
        }
        lines.add(isClass
                ? PUBLIC + quals + "class " + typeDef.name() + " {"
                : PUBLIC + "type " + typeDef.name() + " " + quals + "object {");
        lines.addAll(renderMembers(typeDef.fields(), typeDef.methods()));
        lines.add(isClass ? "}" : "};");
        return Signatures.renderDescription(typeDef.description()) + String.join("\n", lines);
    }

    /**
     * The members of an object body: its fields, then its methods.
     *
     * <p>Shared with the client class renderer, because a client class is an object like any other and two
     * copies of "how an object body is laid out" is where the two would drift. Fields are contiguous, as a
     * record's are; a blank line separates each method from what precedes it, so a 20-method client stays
     * scannable.
     *
     * <p>BETWEEN members, not before each one. The old rule exempted the constructor, which amounted to the
     * same thing for the clients that declare one and left a blank line under the header of the ones that do
     * not — {@code http:Caller} among them. That is the spacing rule this file's own header states, applied
     * where it had not been.
     */
    public static List<String> renderMembers(List<RecordField> fields, List<Fn> methods) {
        List<String> lines = new ArrayList<>();
        for (RecordField field : fields) {
            lines.add(renderRecordField(field, Owner.OBJECT));
        }
        for (Fn fn : methods) {
            if (!lines.isEmpty()) {
                lines.add("");
            }
            lines.add(Signatures.renderMemberFunction(fn, "    ", Signatures.Detail.FULL));
        }
        return lines;
    }

    /**
     * An enum, with a doc comment on each member that has one.
     *
     * <p>13 of the corpus's 65 members are described and all 65 printed as bare names. It is the only part of a
     * member Central publishes beyond the name — no member anywhere in the payload carries its VALUE — so where a
     * description exists it is the whole of what distinguishes one member from another.
     */
    private static String renderEnum(TypeDef.Enumeration typeDef) {
        List<String> members = new ArrayList<>();
        for (TypeDef.Enumeration.Member member : typeDef.members()) {
            // The comma belongs to the member's own line, so a described member's doc comment stays above the
            // name it documents rather than after the previous member's separator.
            members.add(Signatures.renderDocComment(member.description(), "    ") + "    " + member.name());
        }
        // Concatenated rather than newline-joined: `renderDescription` already ends in a newline, and an
        // enum with no description must not gain a leading blank line.
        return Signatures.renderDescription(typeDef.description())
                + PUBLIC + "enum " + typeDef.name() + " {\n" + String.join(",\n", members) + "\n}";
    }

    /**
     * A type alias, in the one form all seventeen of Central's alias categories share.
     *
     * <p>{@code // Unknown type:} survives for the case where the descriptor could not be encoded, because
     * the alternatives are both worse: dropping the declaration hides that the name exists, and emitting
     * {@code type X ;} is a syntax error.
     */
    private static String renderAlias(TypeDef.Alias typeDef) {
        String description = Signatures.renderDescription(typeDef.description());
        if (typeDef.type().name().isEmpty()) {
            return description + "// Unknown type: " + typeDef.name();
        }
        List<Signatures.ExternalLink> links = Signatures.collectExternalLinks(typeDef.type());
        String type = Signatures.applyPrefixToTypeName(typeDef.type().name(), links);
        return description + PUBLIC + "type " + typeDef.name() + " " + type + ";"
                + Signatures.buildSpecialAgentNote(links);
    }

    /**
     * A constant, with or without a declared type.
     *
     * <p>Central sends a type node with no name for a constant whose source declares no type either —
     * {@code public const EXECUTION_FAILED = -3;}, where Ballerina infers it. Printing a type there is
     * printing a guess: it used to come out as {@code const record {} EXECUTION_FAILED = -3;}, which does
     * not compile and typed an integer as a record. The inferred form is what the source says.
     */
    /**
     * A module-level {@code public final} variable, WITH its initialiser.
     *
     * <p>The initialiser is not optional here the way a record field's default is: {@code public final T X;} is
     * {@code uninitialized variable 'X'}, checked by compiling it. And Central's value is the real one, also
     * checked — all 64 of these in the corpus are written {@code = {}} in their published sources, with zero
     * exceptions, so the empty mapping is the package's own initialiser and not a placeholder standing in for
     * one. That makes the rendered line byte-identical to the source rather than a guess that happens to
     * compile.
     */
    private static String renderVariable(TypeDef.Variable typeDef) {
        List<Signatures.ExternalLink> links = Signatures.collectExternalLinks(typeDef.varType());
        String type = Signatures.applyPrefixToTypeName(typeDef.varType().name(), links);
        String initialiser = typeDef.initialiser().isEmpty() ? "" : " = " + typeDef.initialiser();
        return Signatures.renderDescription(typeDef.description())
                + PUBLIC + "final " + type + " " + typeDef.name() + initialiser + ";"
                + Signatures.buildSpecialAgentNote(links);
    }

    private static String renderConstant(TypeDef.Constant typeDef) {
        String type = typeDef.varType().name();
        boolean needsQuotes = "string".equals(type) && !QUOTED.matcher(typeDef.value()).matches();
        String value = needsQuotes ? "\"" + typeDef.value() + "\"" : typeDef.value();
        String declared = type.isEmpty() ? "" : type + " ";
        return Signatures.renderDescription(typeDef.description())
                + PUBLIC + "const " + declared + typeDef.name() + " = " + value + ";";
    }

    /**
     * An error declaration, as the combinations of the facts Central publishes about it:
     *
     * <pre>
     *   distinct + supertype      type SslError distinct ClientError;
     *   distinct + detail record  type FHIRServerError distinct error&lt;FHIRServerErrorDetails&gt;;
     *   distinct only             type Error distinct error;
     *   base only                 type X A|B|C;
     *   neither                   type X error;
     * </pre>
     *
     * <p>Every one of {@code ballerina/http}'s 56 errors used to render as the last line, which made the
     * subtype hierarchy — and therefore {@code e is http:ClientRequestError} — unlearnable from the
     * document. The absent-base default is {@code error} rather than nothing because an error at the top
     * of its own hierarchy narrows the language's {@code error}, and that is what its declaration says.
     *
     * <p>The second line is the later fix. A detail record is not a supertype, so printing it bare said
     * {@code distinct FHIRServerErrorDetails} — a declaration naming a record where an error type has to
     * be, which does not compile and cannot be {@code is}-tested. {@code error<...>} is the wrapper the
     * language requires and the wrapper the published source carries.
     */
    private static String renderError(TypeDef.ErrorDef typeDef) {
        TypeRef baseRef = typeDef.base().orElse(null);
        List<Signatures.ExternalLink> links = Signatures.collectExternalLinks(baseRef);
        String base = baseRef == null ? "error" : Signatures.applyPrefixToTypeName(baseRef.name(), links);
        String narrowed = baseRef != null && typeDef.detailRecord() ? "error<" + base + ">" : base;
        String distinct = typeDef.isDistinct() ? "distinct " : "";
        return Signatures.renderDescription(typeDef.description())
                + PUBLIC + "type " + typeDef.name() + " " + distinct + narrowed + ";"
                + Signatures.buildSpecialAgentNote(links);
    }
}
