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

import io.ballerina.library.model.Fn;
import io.ballerina.library.model.Library;
import io.ballerina.library.model.TypeDef;

import java.util.ArrayList;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Optional;
import java.util.Set;

/**
 * The callable surface of a package, cut into the three scopes the three container verbs address.
 *
 * <p>THE CUT IS BY DERIVED ROLE, NOT BY CENTRAL'S BUCKET, and that is the whole reason this class exists.
 * Central publishes no {@code isClient} key at all, and its {@code clients} array is not the callable surface: for
 * {@code ballerina/sql} it is EMPTY while {@code Client} and {@code SchemaClient} — both reached with {@code ->} —
 * are filed under the ordinary declarations, along with the 122 methods that hang off them. A verb that read
 * {@link Library#clients()} would answer "this package has no clients" for a package whose whole point is one.
 *
 * <p>So {@code Role} decides, and {@code Role} is derived from the grammar by {@code FromCentral.roleOf}: an
 * object with a {@code remote} method is a CLIENT, a {@code serviceTypes} entry is a SERVICE, everything else is
 * PLAIN. {@link Library#addressable()} is the one list that already merges both of Central's filings, so it is
 * what this partitions.
 *
 * <p>{@link Scope#MODULE} is the odd one and it is deliberately shaped like the other two. A module's functions
 * have no container to name, so they get an anonymous one — which lets {@code funcs} share every line of
 * resolution, filtering, budgeting and rendering with {@code client} and {@code class} instead of being a fourth
 * code path that drifts from them.
 *
 * @since 0.1.0
 */
public final class Surface {

    private Surface() {
    }

    /**
     * Which slice of the callable surface a verb addresses.
     *
     * <p>The verb name rides on the scope so that a kind-mismatch note can say where a symbol actually lives
     * without a switch at each site that has to say it.
     */
    public enum Scope {

        /** Objects reached with {@code ->}: every derived-CLIENT declaration, from either Central bucket. */
        CLIENT("client"),

        /** Objects reached with {@code .}: classes, object types, service types and listeners. */
        CLASS("class"),

        /** Functions at module scope, which belong to no object. */
        MODULE("funcs");

        private final String verb;

        Scope(String verb) {
            this.verb = verb;
        }

        /** The verb that addresses this scope, for the note that routes a caller to it. */
        public String verb() {
            return verb;
        }
    }

    /**
     * One addressable callable surface.
     *
     * @param name the declaration's name, or empty for the module's own functions
     * @param scope which verb addresses it
     * @param functions everything callable on it, in the package's own order
     */
    public record Container(String name, Scope scope, String description, List<Fn> functions) {

        /** The module's anonymous container has no name to address, so it is never resolved by one. */
        public boolean isModule() {
            return scope == Scope.MODULE;
        }

        /** How this container is named in prose: its declaration name, or "module-level functions". */
        public String label() {
            return isModule() ? "module-level functions" : name;
        }

        /**
         * The resource functions, with their paths in the display spelling the tree navigates by.
         *
         * <p>Whether this is empty is what decides the SELECTOR GRAMMAR (ADR-0019): a container
         * that declares resource functions parses {@code get}/{@code post}/{@code delete} as an accessor, and one
         * that does not parses the same token as a member name.
         */
        public List<PathTree.Operation> operations() {
            List<PathTree.Operation> operations = new ArrayList<>();
            for (Fn fn : functions) {
                if (fn instanceof Fn.Resource resource) {
                    operations.add(new PathTree.Operation(
                            resource, resource.paths().stream().map(PathTree::displaySegment).toList()));
                }
            }
            return List.copyOf(operations);
        }

        public boolean hasPaths() {
            return functions.stream().anyMatch(Fn.Resource.class::isInstance);
        }

        /** The functions that carry a name rather than a path. */
        public List<Fn.Standalone> standalone() {
            return functions.stream()
                    .filter(Fn.Standalone.class::isInstance)
                    .map(Fn.Standalone.class::cast)
                    .toList();
        }

        /**
         * The constructor, which is part of the container and used to be reachable from no verb at all.
         *
         * <p>T14. {@code overview --client <Name>} was the only document that carried one, so an agent that had
         * navigated to the operation it wanted could not then ask how to build the thing it calls.
         */
        public Optional<Fn.Constructor> constructor() {
            return functions.stream()
                    .filter(Fn.Constructor.class::isInstance)
                    .map(Fn.Constructor.class::cast)
                    .findFirst();
        }

        /**
         * Every member a caller can address BY NAME — the constructor as {@code init}, then the named functions.
         *
         * <p>Resource functions are absent by construction: they have no name, which is the structural wrinkle
         * the whole addressing scheme is built around.
         */
        public List<String> memberNames() {
            List<String> names = new ArrayList<>();
            constructor().ifPresent(ignored -> names.add("init"));
            standalone().forEach(fn -> names.add(fn.name()));
            return List.copyOf(names);
        }

        /** One member by exact name, {@code init} included. */
        public Optional<Fn> member(String name) {
            if ("init".equals(name)) {
                return constructor().map(Fn.class::cast);
            }
            return standalone().stream()
                    .filter(fn -> fn.name().equals(name))
                    .map(Fn.class::cast)
                    .findFirst();
        }
    }

    /**
     * The containers a scope addresses, in the package's own order.
     *
     * <p>Deduplicated by name, first winning, for the same reason {@link Declarations} keeps the first: a
     * declaration Central files twice is a filing artefact, and a silent last-wins would make the next one
     * impossible to notice.
     */
    public static List<Container> of(Library library, Scope scope) {
        if (scope == Scope.MODULE) {
            return library.functions().isEmpty()
                    ? List.of()
                    : List.of(new Container("", Scope.MODULE, library.description(),
                            List.copyOf(library.functions())));
        }
        List<Container> containers = new ArrayList<>();
        Set<String> seen = new LinkedHashSet<>();
        for (TypeDef typeDef : library.addressable()) {
            if (!(typeDef instanceof TypeDef.ObjectDef object) || !seen.add(object.name())) {
                continue;
            }
            if (scopeOf(object) == scope) {
                containers.add(new Container(
                        object.name(), scope, object.description(), object.methods()));
            }
        }
        return List.copyOf(containers);
    }

    /** Which verb addresses an object: {@code client} for a {@code ->} surface, {@code class} for the rest. */
    public static Scope scopeOf(TypeDef.ObjectDef object) {
        return object.role() == TypeDef.ObjectDef.Role.CLIENT ? Scope.CLIENT : Scope.CLASS;
    }

    /**
     * Resolve a container by the name a caller typed.
     *
     * <p>Exact first, then {@link Names}' normalisation, so {@code schemaclient} reaches {@code SchemaClient}
     * without any casing heuristic in the dispatcher. A normalised name that reaches SEVERAL containers resolves
     * to none of them — the caller has to choose, and picking one would answer a different question silently.
     */
    public static Optional<Container> byName(List<Container> containers, String requested) {
        for (Container container : containers) {
            if (container.name().equals(requested)) {
                return Optional.of(container);
            }
        }
        String wanted = Names.normalise(requested);
        if (wanted.isEmpty()) {
            return Optional.empty();
        }
        List<Container> normalised = containers.stream()
                .filter(container -> Names.normalise(container.name()).equals(wanted))
                .toList();
        return normalised.size() == 1 ? Optional.of(normalised.get(0)) : Optional.empty();
    }
}
