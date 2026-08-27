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

import java.util.HashSet;
import java.util.List;
import java.util.Set;
import java.util.regex.MatchResult;
import java.util.regex.Pattern;

/**
 * Which printed defaults the caller can actually write.
 *
 * <p>Central publishes a default as the SOURCE EXPRESSION, verbatim, and some of those expressions name
 * module-private declarations: {@code string serviceUrl = BASE_URL}, where {@code BASE_URL} is a private
 * constant three lines above a public one. Spliced into a signature that reads as copyable syntax, it asserts
 * something false — and the natural way to override a later parameter from a positional list is to repeat the
 * printed default for the earlier one, which is exactly the call that fails.
 *
 * <p>Central's own page presents the same fact without the trap: the constructor is printed WITHOUT defaults
 * and the default appears in a separate parameter row, as documentation about a default rather than as syntax.
 * Dropping the default here would be worse than either, because a defaultable parameter printed without its
 * default reads as required.
 *
 * <p>So the expression stays and the line says it cannot be written. Deciding that needs every declaration in
 * the document at once — which is why this is a whole-library pass and not a rule inside the renderer — and it
 * runs after {@link Patches}, because a patch can inject the very declaration that resolves a name.
 *
 * <p>Measured: 66 sites across the nine recorded packages. Two are the sheets constructor parameters the
 * register filed; 63 are http record fields whose default is a private {@code STATUS_*_OBJ} response object,
 * and one is graphql's {@code contextInit}, which defaults to a private function.
 *
 * @since 0.1.0
 */
public final class Defaults {

    /**
     * A module-qualified reference — {@code http:HTTP_2_0}, {@code time:utcNow()}.
     *
     * <p>Removed before the scan rather than resolved: the prefix names an import, the trailing
     * {@code // Special Agent Note} on the same line already says which package to import, and the tool does
     * not fetch a foreign package to check one identifier.
     */
    private static final Pattern QUALIFIED = Pattern.compile("'?[A-Za-z_][\\w.']*\\s*:\\s*[A-Za-z_]\\w*");

    /** A string or backtick-template literal, whose contents are text and not identifiers. */
    private static final Pattern LITERAL = Pattern.compile("\"(?:[^\"\\\\]|\\\\.)*\"|`(?:[^`\\\\]|\\\\.)*`");

    private static final Pattern IDENTIFIER = Pattern.compile("[A-Za-z_]\\w*");

    /**
     * Words in a default expression that are the language's, not the package's.
     *
     * <p>The type names are here because a default can be a cast — postgresql's array constructors all default
     * to {@code <string?[]>[]} — and {@code string} inside one is the basic type, which no package declares.
     */
    private static final Set<String> LANGUAGE = Set.of(
            "string", "int", "float", "decimal", "boolean", "byte", "json", "xml", "anydata", "any", "error",
            "never", "readonly", "handle", "future", "typedesc", "map", "table", "stream", "function",
            "object", "record", "true", "false", "new", "null", "check", "checkpanic", "from", "in", "is",
            "let", "isolated", "transactional");

    private Defaults() {
    }

    /**
     * Mark every default the caller cannot name, in a parameter or in a field.
     *
     * <p>{@code alsoPublished} is the names Central publishes that the document does not render — today its
     * {@code variables} and {@code configurables}. They belong in the set because the claim on the line is
     * that the package does not EXPORT the name, and the payload is the authority on that: http defaults eight
     * fields to public configurables such as {@code waitTime}, and calling those unexported would be false.
     * That they are also invisible in the document is a different gap, and it has its own finding.
     */
    public static Library markUnwritable(Library library, Set<String> alsoPublished) {
        Set<String> declared = declaredNames(library);
        declared.addAll(alsoPublished);
        List<TypeDef> typeDefs = library.typeDefs().stream()
                .map(typeDef -> markTypeDef(typeDef, declared))
                .toList();
        List<TypeDef.ObjectDef> listeners = library.listeners().stream()
                .map(listener -> (TypeDef.ObjectDef) markTypeDef(listener, declared))
                .toList();
        List<ClientClass> clients = library.clients().stream()
                .map(client -> client.withFunctions(markAll(client.functions(), declared)))
                .toList();
        List<Fn.Standalone> functions = library.functions().stream()
                .map(fn -> (Fn.Standalone) markFn(fn, declared))
                .toList();
        return new Library(
                library.name(),
                library.description(),
                typeDefs,
                clients,
                functions,
                listeners,
                library.services(),
                library.annotations(),
                library.configurables());
    }

    /**
     * Every name the document declares, including enum members.
     *
     * <p>Enum members are module-level symbols in Ballerina — {@code sql:COLUMNS_ONLY} is written that way —
     * so a default naming one is resolvable even though the member is not a declaration of its own.
     */
    private static Set<String> declaredNames(Library library) {
        Set<String> names = new HashSet<>();
        for (TypeDef typeDef : library.declarations()) {
            names.add(typeDef.name());
            if (typeDef instanceof TypeDef.Enumeration enumeration) {
                names.addAll(enumeration.memberNames());
            }
        }
        library.clients().forEach(client -> names.add(client.name()));
        library.functions().forEach(fn -> names.add(fn.name()));
        return names;
    }

    private static TypeDef markTypeDef(TypeDef typeDef, Set<String> declared) {
        return switch (typeDef) {
            case TypeDef.Rec record -> record.withFields(markFields(record.fields(), declared));
            case TypeDef.ObjectDef object -> new TypeDef.ObjectDef(
                    object.name(),
                    object.description(),
                    object.form(),
                    object.role(),
                    object.isDistinct(),
                    object.isReadOnly(),
                    object.isIsolated(),
                    object.isDeprecated(),
                    markFields(object.fields(), declared),
                    markAll(object.methods(), declared));
            // A variable prints no initialiser, so it has no default to resolve.
            case TypeDef.Variable ignored -> typeDef;
            case TypeDef.Alias ignored -> typeDef;
            case TypeDef.Constant ignored -> typeDef;
            case TypeDef.Enumeration ignored -> typeDef;
            case TypeDef.ErrorDef ignored -> typeDef;
        };
    }

    private static List<RecordField> markFields(List<RecordField> fields, Set<String> declared) {
        return fields.stream()
                .map(field -> field.hasDefault() && !isWritable(field.defaultValue(), declared)
                        ? field.withUnwritableDefault()
                        : field)
                .toList();
    }

    private static List<Fn> markAll(List<Fn> functions, Set<String> declared) {
        return functions.stream().map(fn -> markFn(fn, declared)).toList();
    }

    private static Fn markFn(Fn fn, Set<String> declared) {
        List<Param> params = fn.params().stream()
                .map(param -> param.hasDefault() && !isWritable(param.defaultValue(), declared)
                        ? param.withUnwritableDefault()
                        : param)
                .toList();
        if (params.equals(fn.params())) {
            return fn;
        }
        return switch (fn) {
            case Fn.Constructor f -> new Fn.Constructor(
                    f.description(), params, f.returns(), f.isDeprecated(), f.isIsolated());
            case Fn.Remote f -> new Fn.Remote(
                    f.name(), f.description(), params, f.returns(), f.isDeprecated(), f.isIsolated());
            case Fn.Normal f -> new Fn.Normal(
                    f.name(), f.description(), params, f.returns(), f.isDeprecated(), f.isIsolated());
            case Fn.Resource f -> new Fn.Resource(
                    f.accessor(), f.paths(), f.description(), params, f.returns(), f.isDeprecated(),
                    f.isIsolated());
        };
    }

    /**
     * Whether every identifier in a default expression is one the caller can write.
     *
     * <p>Textual because the expression arrives as text and there is no expression IR to walk. The three
     * exclusions are what a survey of all 184 distinct default expressions in the corpus turned up: literals,
     * module-qualified references, and the language's own words.
     */
    public static boolean isWritable(String expression, Set<String> declared) {
        String scanned = QUALIFIED.matcher(LITERAL.matcher(expression).replaceAll(" ")).replaceAll(" ");
        return IDENTIFIER.matcher(scanned).results()
                .map(MatchResult::group)
                .allMatch(word -> LANGUAGE.contains(word) || declared.contains(word));
    }
}
