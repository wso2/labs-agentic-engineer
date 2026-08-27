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

package io.ballerina.library.symbols;

import io.ballerina.library.model.TypeDef;

import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * Declarations by name.
 *
 * <p>A duplicate name keeps the FIRST declaration, because {@code Patches} prepends its injections and
 * those are corrections of what Central got wrong. There should be no duplicates left — the sap
 * {@code ClientError} collision was the only one and it is fixed — but a silent last-wins would make the
 * next one impossible to notice.
 *
 * @since 0.1.0
 */
public final class Declarations {

    private final List<String> names;
    private final Map<String, TypeDef> byName;

    private Declarations(List<String> names, Map<String, TypeDef> byName) {
        this.names = names;
        this.byName = byName;
    }

    public static Declarations index(List<TypeDef> typeDefs) {
        Map<String, TypeDef> byName = new LinkedHashMap<>();
        List<String> names = new ArrayList<>();
        for (TypeDef typeDef : typeDefs) {
            if (byName.containsKey(typeDef.name())) {
                continue;
            }
            byName.put(typeDef.name(), typeDef);
            names.add(typeDef.name());
        }
        return new Declarations(List.copyOf(names), byName);
    }

    /** Every declaration name, in the package's own order. */
    public List<String> names() {
        return names;
    }

    /** The declaration, or {@code null} when the name is not one. */
    public TypeDef get(String name) {
        return byName.get(name);
    }
}
