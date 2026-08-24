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
 * One member of a record body. {@code defaultValue} is {@code null} when there is none.
 *
 * <p>{@link Form} is what the member is WRITTEN as, and the three forms are not interchangeable: an
 * inclusion states that another record's fields are part of this one, and a rest field states what an open
 * record's extra fields may hold. Both were previously flattened into declared fields — an inclusion by
 * copying the included record's members in, which loses the link and lets a member overwrite a local
 * declaration, and a rest field by printing its type followed by an empty name.
 *
 * <p>One record with a form rather than a sealed triple, because all three genuinely carry a type and the
 * renderer's job is to spell that type differently. {@code name} is empty for the two forms that have none.
 *
 * <p>A field is also what a class or object type declares, and this is the type for both. Central publishes
 * one shape for the two, the language writes them the same way, and a second record differing only in its
 * name would have to be kept in step with this one for no gain.
 *
 * <p>{@code unwritableDefault} is set by {@link Defaults} when the default expression names something the
 * document does not declare, so the renderer can say so instead of presenting it as copyable syntax.
 *
 * @since 0.1.0
 */
public record RecordField(
        String name,
        String description,
        TypeRef type,
        String defaultValue,
        boolean optional,
        boolean readonly,
        boolean deprecated,
        boolean unwritableDefault,
        Form form) {

    /** How a record member is written: {@code T f;}, {@code *T;} or {@code T...;}. */
    public enum Form { DECLARED, INCLUSION, REST }

    public RecordField(String name, String description, TypeRef type) {
        this(name, description, type, null, false, false, false, false, Form.DECLARED);
    }

    public RecordField(String name, String description, TypeRef type, String defaultValue, boolean optional) {
        this(name, description, type, defaultValue, optional, false, false, false, Form.DECLARED);
    }

    public RecordField(String name, String description, TypeRef type, String defaultValue, boolean optional,
            boolean readonly, boolean deprecated, Form form) {
        this(name, description, type, defaultValue, optional, readonly, deprecated, false, form);
    }

    /** {@code *Other;} — the included record, by name. */
    public static RecordField inclusion(TypeRef type) {
        return new RecordField("", "", type, null, false, false, false, false, Form.INCLUSION);
    }

    /** {@code T...;} — what an open record's undeclared fields may hold. */
    public static RecordField rest(String description, TypeRef type) {
        return new RecordField("", description, type, null, false, false, false, false, Form.REST);
    }

    public boolean hasDefault() {
        return defaultValue != null;
    }

    /** The same field, with its default marked as one the document cannot name. */
    public RecordField withUnwritableDefault() {
        return new RecordField(
                name, description, type, defaultValue, optional, readonly, deprecated, true, form);
    }
}
