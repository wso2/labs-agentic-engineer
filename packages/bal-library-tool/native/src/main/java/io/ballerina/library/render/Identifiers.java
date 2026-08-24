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

import java.util.Set;

/**
 * An identifier, as Ballerina spells it.
 *
 * <p>A declared name that collides with a keyword is written with a leading quote — {@code 'start}. Central
 * mostly does this itself: the kafka listener's parameter arrives as {@code 'service} and postgresql's own
 * lifecycle method as {@code 'start}. But it is not consistent, and the one place it is not is a real one —
 * {@code postgresql:CdcListener} publishes the method it includes from {@code cdc:Listener} as a bare
 * {@code start}, which renders as {@code function start()} and is a syntax error.
 *
 * <p>Quoting is applied where a name Central chose is written into a declaration: method names, parameter
 * names and field names. Not to type names, which the payload capitalises, and not blindly to everything,
 * because a name Central already quoted must not gain a second quote.
 *
 * @since 0.1.0
 */
public final class Identifiers {

    /**
     * The words the compiler rejects as a declared name.
     *
     * <p>DERIVED, not recalled. Each of 94 candidates was compiled on its own as
     * {@code public type T record {| int <word>; |};} and kept only if {@code bal build} rejected it. The list
     * written from memory first was wrong in both directions: it included {@code channel}, {@code catch},
     * {@code finally} and {@code try}, which are legal identifiers — quoting {@code channel} moved 60 lines of
     * {@code ballerinax/slack}, and the corpus caught it — and it omitted the query keywords
     * {@code from}, {@code where}, {@code select}, {@code order}, {@code by}, {@code ascending},
     * {@code descending}, {@code equals}, {@code outer} and {@code conflict}, every one of which the compiler
     * does reject. The seven candidates it accepts are {@code catch}, {@code channel}, {@code collect},
     * {@code finally}, {@code group}, {@code key} and {@code try}.
     */
    private static final Set<String> RESERVED = Set.of(
            "annotation", "any", "anydata", "as", "ascending", "base16", "base64", "boolean", "break", "by",
            "byte", "check", "checkpanic", "class", "client", "commit", "conflict", "const", "continue",
            "decimal", "descending", "distinct", "do", "else", "enum", "equals", "error", "external", "fail",
            "false", "final", "float", "flush", "fork", "from", "function", "future", "handle", "if", "import",
            "in", "int", "is", "isolated", "join", "json", "let", "limit", "lock", "map", "match", "new",
            "null", "object", "on", "order", "outer", "panic", "private", "public", "readonly", "record",
            "remote", "resource", "retry", "return", "returns", "rollback", "select", "service", "source",
            "start", "stream", "string", "table", "transaction", "transactional", "true", "type", "typedesc",
            "var", "wait", "where", "while", "worker", "xml", "xmlns");

    private Identifiers() {
    }

    /** The name, quoted if the language would otherwise read it as a keyword. */
    public static String write(String name) {
        return RESERVED.contains(name) ? "'" + name : name;
    }
}
