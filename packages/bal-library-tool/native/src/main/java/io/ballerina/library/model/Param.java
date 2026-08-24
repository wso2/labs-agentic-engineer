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
 * One parameter of a callable. {@code defaultValue} is {@code null} when the parameter is required —
 * distinct from an empty default, which Central never publishes.
 *
 * <p>{@link Form} is what the parameter is WRITTEN as, and the three forms are the same three a record
 * member has, for the same reason: {@code *Queries queries} means the caller passes the included record's
 * fields as named arguments rather than the record itself, and {@code Cookie... cookies} means they pass any
 * number of them. Neither is decoration — both change how the call is written.
 *
 * <p>{@code unwritableDefault} is set by {@link Defaults} when the default expression names something the
 * document does not declare — {@code string serviceUrl = BASE_URL}, where {@code BASE_URL} is module-private.
 *
 * @since 0.1.0
 */
public record Param(
        String name,
        String description,
        TypeRef type,
        String defaultValue,
        Form form,
        boolean unwritableDefault) {

    /** How a parameter is written: {@code T p}, {@code *T p} or {@code T... p}. */
    public enum Form { NORMAL, INCLUSION, REST }

    public Param(String name, String description, TypeRef type) {
        this(name, description, type, null, Form.NORMAL, false);
    }

    public Param(String name, String description, TypeRef type, String defaultValue) {
        this(name, description, type, defaultValue, Form.NORMAL, false);
    }

    public Param(String name, String description, TypeRef type, String defaultValue, Form form) {
        this(name, description, type, defaultValue, form, false);
    }

    public boolean hasDefault() {
        return defaultValue != null;
    }

    public boolean inclusion() {
        return form == Form.INCLUSION;
    }

    /** The same parameter, with its default marked as one the document cannot name. */
    public Param withUnwritableDefault() {
        return new Param(name, description, type, defaultValue, form, true);
    }
}
