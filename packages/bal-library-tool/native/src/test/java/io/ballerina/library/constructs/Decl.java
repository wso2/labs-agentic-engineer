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
 * One module-level declaration, spelled the way Central spells it.
 *
 * <p>These are deliberately thin: every factory writes only the keys Central actually sends for that
 * declaration, so a construct case that omits a key is testing what the reader does when Central omits
 * it. Filling in defaults here would hide exactly the cases worth testing.
 *
 * @since 0.1.0
 */
public final class Decl {

    private final JsonObject json = new JsonObject();

    private Decl() {
    }

    private static Decl of(String name) {
        Decl decl = new Decl();
        decl.json.addProperty("name", name);
        return decl;
    }

    /** A record. Fields are {@link #field} or {@link #inclusion} entries. */
    public static Decl record(String name, Decl... fields) {
        Decl decl = of(name);
        JsonArray array = new JsonArray();
        for (Decl field : fields) {
            array.add(field.json());
        }
        decl.json.add("fields", array);
        return decl;
    }

    /** A declared record field. */
    public static Decl field(String name, Node type) {
        Decl decl = of(name);
        decl.json.add("type", type.json());
        return decl;
    }

    /**
     * An included record — Ballerina's {@code *Other;}. Central spells it as a field with no name whose
     * {@code inclusionType} carries the included record's own fields as members.
     */
    public static Decl inclusion(Node inclusionType) {
        Decl decl = new Decl();
        decl.json.add("inclusionType", inclusionType.json());
        return decl;
    }

    /**
     * One member of an inline record or an inclusion: a name plus the member's type under
     * {@code elementType}, which is how Central distinguishes "this is a field" from "this is a type".
     */
    public static Node member(String name, Node type) {
        return Node.structural().with("name", name).elementType(type);
    }

    /** An error. {@code isDistinct} is required by the schema, so it is always written. */
    public static Decl error(String name, boolean isDistinct) {
        Decl decl = of(name);
        decl.json.addProperty("isDistinct", isDistinct);
        return decl;
    }

    /**
     * An error's {@code detailType}, which is the one key Central uses for two different facts: for most
     * errors it holds the BASE the error narrows ({@code distinct ClientError}), and for an error that
     * carries a detail record it holds a parenthesised intersection whose {@code error} member has had its
     * type argument stripped. Both shapes appear in the corpus and both are tested.
     */
    public Decl detail(Node detailType) {
        json.add("detailType", detailType.json());
        return this;
    }

    /**
     * The intersection Central publishes for an error that carries a detail record:
     * {@code (Base & error)}, with the {@code <Detail>} argument dropped on the way out.
     */
    public static Node detailIntersection(Node base) {
        return Node.structural()
                .on("isParenthesisedType")
                .elementType(Node.structural()
                        .on("isIntersectionType")
                        .members(base, Node.builtin("error")));
    }

    /** A constant. Central sends a string constant's value WITH its quotes. */
    public static Decl constant(String name, String value, Node type) {
        Decl decl = of(name);
        decl.json.addProperty("value", value);
        decl.json.add("type", type.json());
        return decl;
    }

    /** An enum. Members are {@link #named} entries. */
    public static Decl enumeration(String name, Decl... members) {
        Decl decl = of(name);
        JsonArray array = new JsonArray();
        for (Decl member : members) {
            array.add(member.json());
        }
        decl.json.add("members", array);
        return decl;
    }

    /**
     * A name and, optionally, a description — all the reader takes from a class, an object type, an enum
     * member, or any of the six alias categories.
     */
    public static Decl named(String name) {
        return of(name);
    }

    /** A client class. Methods are {@link #method} entries. */
    public static Decl client(String name, Decl... methods) {
        Decl decl = of(name);
        decl.json.add("methods", methodArray(methods));
        return decl;
    }

    /** A callable. Bare, it is a public function; the qualifier flags make it remote or resource. */
    public static Decl method(String name, Decl... params) {
        Decl decl = of(name);
        JsonArray array = new JsonArray();
        for (Decl param : params) {
            array.add(param.json());
        }
        decl.json.add("parameters", array);
        return decl;
    }

    /** A parameter. Same shape as a record field, and read by the same schema function. */
    public static Decl param(String name, Node type) {
        return field(name, type);
    }

    /**
     * A listener, with the constructor its {@code init} declares.
     *
     * <p>The {@code initMethod} is named, because Central names it: it is a method like any other, and
     * {@code init} is how every reader tells a constructor from a method.
     *
     * <p>It is written TWICE, under {@code initMethod} and again in {@code methods}, because that is what
     * Central sends: across the corpus all 103 {@code initMethod}s are byte-identical to the {@code init}
     * entry in the same object's {@code methods}. A payload carrying only one of the two would test a shape
     * Central never publishes, and would make a reader that correctly reads one array look broken.
     */
    public static Decl listener(String name, Decl... initParams) {
        return listenerAttaching("Service", name, initParams);
    }

    /**
     * A listener whose {@code attach} takes a named service type.
     *
     * <p>{@code attach} is what decides whether a {@code service X on new Listener(…)} template is written at
     * all (HTTP-14), and Central files it under {@code lifeCycleMethods} rather than with the listener's own
     * methods — so a synthetic listener without it exercises a shape no real payload has.
     */
    public static Decl listenerAttaching(String attachedType, String name, Decl... initParams) {
        Decl decl = of(name);
        JsonObject init = method("init", initParams).json();
        decl.json.add("initMethod", init);
        JsonArray methods = new JsonArray();
        methods.add(init);
        decl.json.add("methods", methods);
        JsonArray lifecycle = new JsonArray();
        lifecycle.add(method("attach",
                param("s", Node.named(attachedType, "types")),
                param("name", Node.builtin("string[]|string?"))).json());
        decl.json.add("lifeCycleMethods", lifecycle);
        return decl;
    }

    /** A service type — the remote contract a service written against a listener must implement. */
    public static Decl serviceType(String name, Decl... methods) {
        Decl decl = of(name);
        decl.json.add("methods", methodArray(methods));
        return decl;
    }

    /** An annotation. {@code attachmentPoints} is a comma-separated string, not an array. */
    public static Decl annotation(String name, String attachmentPoints) {
        Decl decl = of(name);
        decl.json.addProperty("attachmentPoints", attachmentPoints);
        return decl;
    }

    /**
     * Set the declaration's {@code type} node.
     *
     * <p>For an annotation, this is the record an attachment's argument must be — the key Central sends for
     * eleven of the corpus's twelve annotations and the reader never read.
     */
    public Decl typed(Node type) {
        json.add("type", type.json());
        return this;
    }

    /**
     * A type alias: a name, the members it resolves to, and the flag that says how they combine.
     *
     * <p>{@code isAnonymousUnionType} is not incidental — Central sets it on EVERY item of every alias
     * category, including the single-member ones. {@code type TsDef string;} arrives as a one-member
     * anonymous union, which is why a helper that wrote only {@code memberTypes} produced a shape Central
     * never sends.
     */
    public static Decl alias(String name, Node... members) {
        return aliasOf(name, "isAnonymousUnionType", members);
    }

    /** An intersection alias, which Central flags differently: {@code type Frozen any & readonly;}. */
    public static Decl intersectionAlias(String name, Node... members) {
        return aliasOf(name, "isIntersectionType", members);
    }

    /** A tuple alias: {@code type Pair [A, B];}. */
    public static Decl tupleAlias(String name, Node... members) {
        return aliasOf(name, "isTuple", members);
    }

    private static Decl aliasOf(String name, String flag, Node... members) {
        Decl decl = of(name);
        decl.json.addProperty(flag, true);
        JsonArray array = new JsonArray();
        for (Node member : members) {
            array.add(member.json());
        }
        decl.json.add("memberTypes", array);
        return decl;
    }

    /** The declaration's own return type. Central sends returns as a list, and the reader takes the first. */
    public Decl returns(Node type) {
        JsonObject returnParameter = new JsonObject();
        returnParameter.add("type", type.json());
        JsonArray array = new JsonArray();
        array.add(returnParameter);
        json.add("returnParameters", array);
        return this;
    }

    /** A return type plus the description of what comes back. */
    public Decl returns(Node type, String description) {
        returns(type);
        json.getAsJsonArray("returnParameters").get(0).getAsJsonObject()
                .addProperty("description", description);
        return this;
    }

    /** Set one of Central's booleans — {@code isRemote}, {@code isResource}, {@code isDeprecated}. */
    public Decl on(String flag) {
        json.addProperty(flag, true);
        return this;
    }

    /** Set one of Central's strings — {@code description}, {@code defaultValue}, {@code accessor}. */
    public Decl with(String key, String value) {
        json.addProperty(key, value);
        return this;
    }

    private static JsonArray methodArray(Decl... methods) {
        JsonArray array = new JsonArray();
        for (Decl method : methods) {
            array.add(method.json());
        }
        return array;
    }

    public JsonObject json() {
        return json.deepCopy();
    }
}
