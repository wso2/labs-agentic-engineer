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
 * A client class and everything callable on it.
 *
 * <p>{@code isIsolated} is Central's, and true on all 18 clients in the corpus. It is part of the declaration
 * rather than decoration: a caller whose own function is {@code isolated} can only construct and call into an
 * isolated one, and the client class is the first thing they construct.
 *
 * @since 0.1.0
 */
public record ClientClass(String name, String description, boolean isIsolated, List<Fn> functions) {

    public ClientClass(String name, String description, List<Fn> functions) {
        this(name, description, false, functions);
    }

    public ClientClass withFunctions(List<Fn> replacement) {
        return new ClientClass(name, description, isIsolated, replacement);
    }

    /**
     * The same declaration as a {@link TypeDef}, which is what makes it addressable.
     *
     * <p>{@code type <pkg> Client} used to fail on the one declaration an agent asks for first, because the
     * name index is built from {@code TypeDef}s and a client was not one. It renders identically either way —
     * {@code TypeDefs.renderObject} on a {@code CLIENT}-role class emits the same bytes the client renderer
     * did, which is why that renderer is gone and this conversion is the whole of the fix.
     *
     * <p>The two shapes still both exist because they carry different amounts: a client has no fields and no
     * {@code distinct}/{@code readonly} qualifiers to lose, and Central publishes it under its own key. This is
     * the widening, done at the one boundary that needs it rather than by flattening the IR.
     */
    public TypeDef.ObjectDef asObjectDef() {
        return new TypeDef.ObjectDef(
                name,
                description,
                TypeDef.ObjectDef.Form.CLASS,
                TypeDef.ObjectDef.Role.CLIENT,
                false,
                false,
                isIsolated,
                false,
                List.of(),
                functions);
    }
}
