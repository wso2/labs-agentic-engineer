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

package io.ballerina.library.constructs;

import com.google.gson.JsonArray;
import com.google.gson.JsonObject;

/**
 * One of Central's type nodes, spelled the way Central spells it.
 *
 * <p>A type node carries no tag saying what it is. Its meaning comes from which of a dozen booleans are
 * set and which of {@code constraint} / {@code elementType} / {@code memberTypes} is populated, and the
 * reader infers one of three encodings from that: owned by another module ({@code orgName} +
 * {@code moduleName} present), a plain named type ({@code name} + {@code category} present), or a
 * structural reference (neither). The three factories below are those three encodings, so a test case
 * states which one it is testing instead of assembling booleans and hoping.
 *
 * <p>Fluent because the construct matrix reads as a list of one-line claims, and a matrix built from
 * {@code json.addProperty("isNullable", true)} would bury the claim in the assembly.
 *
 * @since 0.1.0
 */
public final class Node {

    private final JsonObject json = new JsonObject();

    private Node() {
    }

    /**
     * A language-level type — {@code string}, {@code int}, {@code json}. Central gives these a
     * {@code category} of {@code builtin}, which is what makes them a BASIC node rather than a
     * structural one.
     */
    public static Node builtin(String name) {
        return named(name, "builtin");
    }

    /** A type this module declares itself: a record, an error, an enum. */
    public static Node named(String name, String category) {
        Node node = new Node();
        node.json.addProperty("name", name);
        node.json.addProperty("category", category);
        return node;
    }

    /**
     * A type owned by another package. This is the only encoding that carries a version, and the only one
     * whose rendered name gains a module prefix.
     */
    public static Node external(String org, String module, String name) {
        Node node = named(name, "records");
        node.json.addProperty("orgName", org);
        node.json.addProperty("moduleName", module);
        return node;
    }

    /**
     * A structural type with no name of its own: a union, an intersection, an array of something, an
     * inline record. What it is depends entirely on the flags and members set on it.
     */
    public static Node structural() {
        return new Node();
    }

    /** Set one of Central's booleans. */
    public Node on(String flag) {
        json.addProperty(flag, true);
        return this;
    }

    /** Set one of Central's strings — {@code version}, {@code description}, {@code category}. */
    public Node with(String key, String value) {
        json.addProperty(key, value);
        return this;
    }

    /** Set one of Central's numbers — {@code arrayDimensions}. */
    public Node with(String key, int value) {
        json.addProperty(key, value);
        return this;
    }

    /** {@code map<T>}'s T. */
    public Node constraint(Node constraint) {
        json.add("constraint", constraint.json());
        return this;
    }

    /** The thing an array is an array of, or a parenthesised type's content. */
    public Node elementType(Node elementType) {
        json.add("elementType", elementType.json());
        return this;
    }

    /** A union's or intersection's members, a tuple's element types, an inline record's fields. */
    public Node members(Node... members) {
        JsonArray array = new JsonArray();
        for (Node member : members) {
            array.add(member.json());
        }
        json.add("memberTypes", array);
        return this;
    }

    public JsonObject json() {
        return json.deepCopy();
    }
}
