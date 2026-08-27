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
 * A rendered type expression plus the packages its names came from.
 *
 * @param name the expression as Ballerina writes it
 * @param links where each name in it is declared; empty when everything is in scope
 * @since 0.1.0
 */
public record TypeRef(String name, List<Link> links) {

    public TypeRef(String name) {
        this(name, List.of());
    }

    /**
     * Where a type name came from. {@code External} names are re-prefixed with the owning module at
     * render time and gathered into the trailing agent note; {@code Internal} ones are already in
     * scope.
     *
     * <p>{@code External} carries the module as {@link ModuleRef} rather than as a formatted string,
     * because the import path, the CLI coordinate and "needs no import at all" are three different
     * answers that one string kept conflating.
     */
    public sealed interface Link {

        String recordName();

        record Internal(String recordName) implements Link { }

        record External(ModuleRef module, String recordName) implements Link { }
    }
}
