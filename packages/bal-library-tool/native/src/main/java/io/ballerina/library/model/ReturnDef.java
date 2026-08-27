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

/**
 * What a callable returns. {@code description} is {@code null} when Central published none.
 *
 * <p>An empty type name is "returns nothing", and it renders as no {@code returns} clause at all. The
 * previous stand-in was the word {@code nil}, which is the ENGLISH name of the basic type — Ballerina spells
 * it {@code ()} — so twelve of http's declarations named a type the compiler does not have. Saying nothing is
 * both correct and what the source says: a function with no return has no clause.
 *
 * @since 0.1.0
 */
public record ReturnDef(TypeRef type, String description) {

    private static final ReturnDef NONE = new ReturnDef(new TypeRef(""), null);

    public ReturnDef(TypeRef type) {
        this(type, null);
    }

    /** A callable Central published no return parameter for. */
    public static ReturnDef none() {
        return NONE;
    }

    public boolean hasType() {
        return !type.name().isEmpty();
    }

    public boolean hasDescription() {
        return description != null && !description.isEmpty();
    }
}
