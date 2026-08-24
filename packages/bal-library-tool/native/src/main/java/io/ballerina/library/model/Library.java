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
import java.util.Optional;

/**
 * What a Ballerina package's public API is, independent of how Central describes it or how it gets
 * rendered.
 *
 * <p>The pipeline is fetch → parse → transform → render and no stage mutates its input, which is why
 * every collection here is an immutable {@code List}.
 *
 * @param name {@code org/module-id} — the string an {@code import} statement takes
 * @since 0.1.0
 */
public record Library(
        String name,
        String description,
        List<TypeDef> typeDefs,
        List<ClientClass> clients,
        List<Fn.Standalone> functions,
        List<TypeDef.ObjectDef> listeners,
        List<Service> services,
        List<AnnotationDef> annotations,
        List<Configurable> configurables) {

    /**
     * The listener-less, configurable-less shape, for a test that is about something else.
     *
     * <p>Only ONE convenience constructor, and only for tests. There were two, and the wider of them is what
     * silently zeroed {@code configurables} the day the field was added: {@link Defaults} rebuilt the library
     * through it, so a category that parsed correctly and rendered correctly arrived empty in every view. A
     * production site now has to name every component, which is what makes adding one a compile error at each
     * place that has to decide about it.
     */
    public Library(String name, String description, List<TypeDef> typeDefs, List<ClientClass> clients,
            List<Fn.Standalone> functions, List<Service> services, List<AnnotationDef> annotations) {
        this(name, description, typeDefs, clients, functions, List.of(), services, annotations, List.of());
    }

    /**
     * A {@code configurable} the package declares, which a DEPLOYER sets and a caller cannot reference.
     *
     * <p>Held apart from {@link #typeDefs()} because it is not a declaration a caller can write against, and
     * that is measured rather than assumed: {@code http:maxActiveConnections} is
     * {@code attempt to refer to non-accessible symbol} from another module, because a {@code configurable} is
     * module-private — Central publishes it anyway, and it is the one category it publishes that is not part
     * of the public API. So it belongs in the report register, where the fact is "set this in
     * {@code Config.toml}", and NOT in the code register, where a declaration is something to copy.
     *
     * <p>It must never gain the blanket {@code public} the other declarations carry, for the same reason.
     */
    public record Configurable(String name, String description, TypeRef type, String defaultValue) { }

    /**
     * An annotation the module declares, with the two facts that make one usable.
     *
     * <p>{@code attachmentPoints} is Central's own comma-separated clause, carried through rather than mapped:
     * checked against the published sources for all twelve annotations in the corpus, the string IS the source's
     * {@code on} clause, down to the order — {@code "record field, parameter, return"} for {@code graphql:ID},
     * {@code "service, type"} for {@code http:ServiceConfig}. What preceded it was a two-value enum whose
     * {@code OBJECT_METHOD} printed {@code service_function}, a token the compiler rejects, and whose closed set
     * silently dropped the nine annotations that attach anywhere else — {@code @http:Payload},
     * {@code @http:Header} and {@code @http:Query} among them.
     *
     * <p>{@code type} is the record an attachment's argument must be, absent for the marker annotations that
     * take none. Without it {@code @http:ResourceConfig { … }} has no discoverable field set, even though the
     * record is declared in the same document.
     */
    public record AnnotationDef(
            String name, String description, Optional<TypeRef> type, String attachmentPoints) { }

    /**
     * Every declaration the document contains, by name.
     *
     * <p>Listeners are held apart from {@link #typeDefs()} because they print in their own section — a
     * listener is the entry point to a package's service half, and burying it among postgresql's 125 value
     * classes is how it went unnoticed that it printed nowhere at all. They are declarations for every other
     * purpose, though: {@code type} resolves them, {@code --deps} walks them, and {@code overview} counts
     * them.
     */
    public List<TypeDef> declarations() {
        List<TypeDef> all = new java.util.ArrayList<>(typeDefs);
        all.addAll(listeners);
        return List.copyOf(all);
    }

    /**
     * Every declaration a caller can address by name, clients included.
     *
     * <p>Separate from {@link #declarations()} because the two answer different questions and one list cannot.
     * {@code overview} counts declarations to describe the type surface, and it names clients on their own row;
     * folding them in would double-count them there. {@code type} resolves a name, and a client is the name
     * asked for first — {@code type ballerinax/sap Client} asserted the package had no such declaration, in a
     * package where the client is 1 of 4 things published.
     */
    public List<TypeDef> addressable() {
        List<TypeDef> all = new java.util.ArrayList<>(declarations());
        clients.forEach(client -> all.add(client.asObjectDef()));
        return List.copyOf(all);
    }

    public Library withTypeDefs(List<TypeDef> replacement) {
        return new Library(name, description, replacement, clients, functions, listeners, services,
                annotations, configurables);
    }

    public Library withClients(List<ClientClass> replacement) {
        return new Library(name, description, typeDefs, replacement, functions, listeners, services,
                annotations, configurables);
    }

    public Library withServices(List<Service> replacement) {
        return new Library(name, description, typeDefs, clients, functions, listeners, replacement,
                annotations, configurables);
    }

    public Library withName(String replacement) {
        return new Library(replacement, description, typeDefs, clients, functions, listeners, services,
                annotations, configurables);
    }
}
