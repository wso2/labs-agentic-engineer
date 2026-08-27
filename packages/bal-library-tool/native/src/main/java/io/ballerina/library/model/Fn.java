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

/**
 * A function on a client, or at module scope.
 *
 * <p>A sealed hierarchy rather than one record with nullable fields, and both halves of that are
 * load-bearing. A resource function's accessor and its path are SEPARATE fields, so the mistake the
 * language-server reader warns about in prose ("never merge them into one string") has no field to live
 * in; and the renderer switches over the cases with no {@code default}, so a callable shape nobody
 * renders is a compile error rather than a silently dropped function.
 *
 * <p>{@code accessor} is a plain string rather than a closed set of HTTP methods: Ballerina's resource
 * accessor is an identifier, and {@code subscribe} (websub, graphql) is as legal as {@code get}.
 * Constraining it would reject real packages without buying anything.
 *
 * @since 0.1.0
 */
public sealed interface Fn {

    String description();

    List<Param> params();

    ReturnDef returns();

    /**
     * Whether calling this is discouraged.
     *
     * <p>On the interface rather than on each case, because a caller asking "should I use this?" asks it of
     * every callable form. Central publishes {@code isDeprecated} on all of them; the renderer was wired to
     * service methods only, so github's 37 deprecated operations read as live API.
     */
    boolean isDeprecated();

    /**
     * Whether the callable is {@code isolated}.
     *
     * <p>On the interface for the same reason {@code isDeprecated} is: Central publishes {@code isIsolated} on
     * every callable form, and the fact belongs to the declaration rather than to one of its shapes. It matters
     * to a caller that is itself {@code isolated} — an isolated function may only call isolated ones — and it is
     * load-bearing when a service contract has to be matched exactly, because the compiler's
     * {@code mismatched function signatures} message does not print the qualifier: it reports an expected and a
     * found signature that are textually identical.
     */
    boolean isIsolated();

    record Constructor(
            String description,
            List<Param> params,
            ReturnDef returns,
            boolean isDeprecated,
            boolean isIsolated)
            implements Fn {

        public Constructor(String description, List<Param> params, ReturnDef returns) {
            this(description, params, returns, false, false);
        }
    }

    record Remote(
            String name,
            String description,
            List<Param> params,
            ReturnDef returns,
            boolean isDeprecated,
            boolean isIsolated)
            implements Fn, Standalone {

        public Remote(String name, String description, List<Param> params, ReturnDef returns) {
            this(name, description, params, returns, false, false);
        }
    }

    record Normal(
            String name,
            String description,
            List<Param> params,
            ReturnDef returns,
            boolean isDeprecated,
            boolean isIsolated)
            implements Fn, Standalone {

        public Normal(String name, String description, List<Param> params, ReturnDef returns) {
            this(name, description, params, returns, false, false);
        }
    }

    record Resource(
            String accessor,
            List<PathSegment> paths,
            String description,
            List<Param> params,
            ReturnDef returns,
            boolean isDeprecated,
            boolean isIsolated)
            implements Fn {

        public Resource(String accessor, List<PathSegment> paths, String description, List<Param> params,
                ReturnDef returns) {
            this(accessor, paths, description, params, returns, false, false);
        }
    }

    /**
     * A function at module scope. A constructor belongs to a class and a resource function to a client,
     * so neither can appear there — which is why {@link Library} names this type rather than {@code Fn}.
     */
    sealed interface Standalone extends Fn permits Remote, Normal {

        String name();
    }

    /**
     * One segment of a resource path. {@code Literal} carries the raw segment — including the odd
     * bracketed form Central emits without a type — while {@code Parameter} keeps the declared type and
     * name apart.
     */
    sealed interface PathSegment {

        record Literal(String text) implements PathSegment { }

        record Parameter(String type, String name) implements PathSegment { }
    }
}
