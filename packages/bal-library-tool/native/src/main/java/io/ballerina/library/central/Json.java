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

package io.ballerina.library.central;

import com.google.gson.JsonElement;
import com.google.gson.JsonObject;

/**
 * Reading a string off an untyped object, for the two places that walk raw JSON without the schema.
 *
 * <p>{@link Coordinates} and the registry-search reader both need this, and they need it for different reasons:
 * the coordinate check runs on fields the schema deliberately strips, and search reads a response shape that is
 * not the docs payload at all. Neither is worth a second schema, but two near-identical private copies is where
 * one of them quietly starts accepting a number.
 *
 * <p>Two spellings because the callers differ on what absence means. A missing coordinate must fail the check, so
 * {@link #string} says {@code null}; a missing summary is just a package that wrote none, so {@link #text} says
 * empty.
 *
 * @since 0.1.0
 */
final class Json {

    private Json() {
    }

    /** The value as a string, or {@code null} for absent and for anything that is not a string. */
    static String string(JsonObject owner, String key) {
        JsonElement value = owner.get(key);
        if (value == null || !value.isJsonPrimitive() || !value.getAsJsonPrimitive().isString()) {
            return null;
        }
        return value.getAsString();
    }

    /** The value as a string, or {@code ""} for absent and for anything that is not a string. */
    static String text(JsonObject owner, String key) {
        String value = string(owner, key);
        return value == null ? "" : value;
    }
}
