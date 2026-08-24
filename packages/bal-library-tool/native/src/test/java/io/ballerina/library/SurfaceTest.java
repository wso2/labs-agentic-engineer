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

package io.ballerina.library;

import io.ballerina.library.model.ClientClass;
import io.ballerina.library.model.Fn;
import io.ballerina.library.model.Library;
import io.ballerina.library.model.TypeDef;
import io.ballerina.library.symbols.Surface;
import org.testng.Assert;
import org.testng.annotations.DataProvider;
import org.testng.annotations.Test;

import java.util.HashSet;
import java.util.List;
import java.util.Optional;
import java.util.Set;

/**
 * The cut the three container verbs address, and the one property that makes it safe.
 *
 * <p>{@code client}, {@code class} and {@code funcs} are only worth splitting if the split is EXHAUSTIVE and
 * DISJOINT: every callable declaration is addressed by exactly one of them. A declaration in neither is
 * unreachable, and one in both is a document that says two different things about the same name.
 *
 * @since 0.1.0
 */
public class SurfaceTest {

    @DataProvider(name = "fixtures")
    public Object[][] fixtures() {
        return FixtureCorpus.fixtureRows();
    }

    @Test(dataProvider = "fixtures")
    public void everyObjectIsAddressedByExactlyOneVerb(String slug) {
        Library library = FixtureCorpus.libraryFor(slug);
        Set<String> objects = new HashSet<>();
        library.addressable().stream()
                .filter(TypeDef.ObjectDef.class::isInstance)
                .forEach(typeDef -> objects.add(typeDef.name()));

        Set<String> clients = names(Surface.of(library, Surface.Scope.CLIENT));
        Set<String> classes = names(Surface.of(library, Surface.Scope.CLASS));

        Set<String> both = new HashSet<>(clients);
        both.retainAll(classes);
        Assert.assertTrue(both.isEmpty(), slug + " addresses these under two verbs: " + both);

        Set<String> covered = new HashSet<>(clients);
        covered.addAll(classes);
        Assert.assertEquals(covered, objects, slug + " left an object addressable by no verb");
    }

    /**
     * The rule Central's own filing cannot express.
     *
     * <p>SQL-03's shape, and it is present in this corpus too: {@code ballerina/http} declares
     * {@code ClientObject} and {@code StatusCodeClientObject} as {@code client object} types, so Central files
     * them among the ordinary declarations and NOT under {@code clients}. A verb reading that array would leave
     * both unaddressable while reporting the package has eight clients.
     */
    @Test
    public void aClientObjectTypeIsAClientEvenThoughCentralDoesNotFileItAsOne() {
        Library library = FixtureCorpus.libraryFor("ballerina__http");
        Set<String> bucket = new HashSet<>(library.clients().stream().map(ClientClass::name).toList());
        Set<String> derived = names(Surface.of(library, Surface.Scope.CLIENT));

        Assert.assertTrue(derived.containsAll(bucket), "every filed client is still a client");
        Set<String> extra = new HashSet<>(derived);
        extra.removeAll(bucket);
        Assert.assertEquals(extra, Set.of("ClientObject", "StatusCodeClientObject"), "derived, not filed");
        // And the other direction: they are not ALSO offered by `class`, which is what would make the split a lie.
        Assert.assertFalse(names(Surface.of(library, Surface.Scope.CLASS)).contains("ClientObject"));
    }

    @Test
    public void theModuleScopeIsOneAnonymousContainerOrNoneAtAll() {
        // `funcs` shares every line of resolution and rendering with the other two verbs, which is only possible
        // because a module's functions arrive wearing the same shape as a client's.
        List<Surface.Container> uuidLike =
                Surface.of(FixtureCorpus.libraryFor("ballerina__http"), Surface.Scope.MODULE);
        Assert.assertEquals(uuidLike.size(), 1);
        Assert.assertTrue(uuidLike.get(0).isModule());
        Assert.assertEquals(uuidLike.get(0).name(), "");
        Assert.assertEquals(uuidLike.get(0).functions().size(), 7);

        // A package with none gets an empty list rather than an empty container, so the verb can say where the
        // callable surface actually is instead of printing a heading with nothing under it.
        Assert.assertTrue(Surface.of(FixtureCorpus.libraryFor("ballerinax__github"), Surface.Scope.MODULE)
                .isEmpty());
    }

    @Test
    public void aConstructorIsAMemberNamedInitAndUsedToBeReachableFromNoVerb() {
        // T14. `ops` could not address one, and `overview --client <Name>` was the only document that carried it.
        Surface.Container client = client("ballerinax__github", "Client");
        Assert.assertTrue(client.constructor().isPresent());
        Assert.assertTrue(client.memberNames().contains("init"));
        Assert.assertTrue(client.member("init").orElseThrow() instanceof Fn.Constructor);
    }

    @Test
    public void whetherAContainerDeclaresResourceFunctionsIsWhatChoosesTheSelectorGrammar() {
        // Not a per-verb property: `ballerina/http`'s Client is a legal argument to BOTH `client` and `class`,
        // and it declares seven resource functions either way.
        Assert.assertTrue(client("ballerinax__github", "Client").hasPaths());
        Assert.assertTrue(client("ballerina__http", "Client").hasPaths());
        Assert.assertFalse(client("ballerinax__twilio", "Client").hasPaths(), "twilio is 199 remote functions");
        Assert.assertFalse(container("ballerina__http", Surface.Scope.CLASS, "Cookie").hasPaths());
    }

    @Test
    public void aNameResolvesExactlyOrThroughNormalisationAndNeverAmbiguously() {
        List<Surface.Container> clients =
                Surface.of(FixtureCorpus.libraryFor("ballerina__http"), Surface.Scope.CLIENT);
        Assert.assertEquals(Surface.byName(clients, "Client").orElseThrow().name(), "Client");
        // No casing heuristic anywhere: the normalisation `type` already uses is what makes this work.
        Assert.assertEquals(Surface.byName(clients, "failoverclient").orElseThrow().name(), "FailoverClient");
        Assert.assertTrue(Surface.byName(clients, "NoSuchClient").isEmpty());
    }

    @Test
    public void aResourceFunctionCarriesNoNameSoItIsNeverAMember() {
        // The structural wrinkle the whole addressing scheme is built around, pinned so a later refactor cannot
        // quietly start listing 903 nameless operations as member names.
        Surface.Container github = client("ballerinax__github", "Client");
        Assert.assertFalse(github.operations().isEmpty());
        Assert.assertEquals(github.memberNames(), List.of("init"));
    }

    private static Set<String> names(List<Surface.Container> containers) {
        return new HashSet<>(containers.stream().map(Surface.Container::name).toList());
    }

    private static Surface.Container client(String slug, String name) {
        return container(slug, Surface.Scope.CLIENT, name);
    }

    private static Surface.Container container(String slug, Surface.Scope scope, String name) {
        Optional<Surface.Container> found =
                Surface.byName(Surface.of(FixtureCorpus.libraryFor(slug), scope), name);
        Assert.assertTrue(found.isPresent(), slug + " has no " + scope + " named " + name);
        return found.get();
    }
}
