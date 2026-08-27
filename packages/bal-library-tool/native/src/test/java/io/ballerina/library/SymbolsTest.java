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
import io.ballerina.library.model.TypeDef;
import io.ballerina.library.model.TypeRef;
import io.ballerina.library.symbols.Declarations;
import io.ballerina.library.symbols.Names;
import io.ballerina.library.symbols.PathTree;
import org.testng.Assert;
import org.testng.annotations.DataProvider;
import org.testng.annotations.Test;

import java.util.List;
import java.util.Optional;

/**
 * The two indexes, and the discovery corpus.
 *
 * <p>The path corpus at the bottom is built from the lookup terms the nine recorded playground runs actually
 * used, each pinned with its hit count. That makes a regression a reviewable diff — and, more importantly, makes
 * a ZERO-hit pin impossible to mistake for a working index, which is how the earlier grep loop failed silently
 * for five runs in a row.
 *
 * @since 0.1.0
 */
public class SymbolsTest {

    /** Every distinct lookup the nine recorded runs made, as this design addresses it. */
    @DataProvider(name = "recordedLookups")
    public Object[][] recordedLookups() {
        return new Object[][] {
                // The golden run: a repository's star count.
                {"ballerinax__github", "repos", 421},
                {"ballerinax__github", "repos/*/*", 420},
                {"ballerinax__github", "repos/*/*/issues", 30},
                {"ballerinax__github", "repos/*/*/pulls", 31},
                {"ballerinax__github", "repos/*/*/stargazers", 1},
                {"ballerinax__github", "user", 93},
                {"ballerinax__github", "orgs", 200},
                {"ballerinax__github", "search", 7},
                // Connectors the other runs reached for.
                {"ballerinax__googleapis.gmail", "users", 32},
                {"ballerinax__slack", "chat.postMessage", 1},
                {"ballerinax__slack", "conversations.list", 1},
        };
    }

    private static ClientClass clientOf(String slug) {
        List<ClientClass> clients = FixtureCorpus.libraryFor(slug).clients();
        Assert.assertFalse(clients.isEmpty(), slug + " has no clients");
        return clients.get(0);
    }

    private static ClientClass clientOf(String slug, String name) {
        return FixtureCorpus.libraryFor(slug).clients().stream()
                .filter(candidate -> candidate.name().equals(name))
                .findFirst()
                .orElseThrow(() -> new AssertionError(slug + " has no client " + name));
    }

    private static PathTree github() {
        return PathTree.build(PathTree.operationsOf(clientOf("ballerinax__github")));
    }

    // -----------------------------------------------------------------------
    // Ballerina's identifier escaping
    // -----------------------------------------------------------------------

    @Test
    public void aSegmentReadsInProseWithoutTheEscapingItNeedsInSource() {
        // Central publishes github's paths as Ballerina writes them. Those spellings are right inside a fence
        // and unusable as a shell argument, which is where the agent has to type them.
        Assert.assertEquals(PathTree.readableSegment("code\\-scanning"), "code-scanning");
        Assert.assertEquals(PathTree.readableSegment("'import"), "import");
        Assert.assertEquals(PathTree.readableSegment("app\\-manifests"), "app-manifests");
        Assert.assertEquals(PathTree.readableSegment("rate_limit"), "rate_limit",
                "an underscore needs no escaping and keeps none");
        Assert.assertEquals(PathTree.readableSegment("repos"), "repos");
    }

    @Test
    public void bothSpellingsAddressTheSameNode() {
        // Because an agent may copy either.
        PathTree tree = github();
        for (String spelling : new String[] {"repos/*/*/code-scanning", "repos/*/*/code\\-scanning"}) {
            PathTree.Resolution resolution = PathTree.resolve(tree, PathTree.splitPath(spelling));
            Assert.assertTrue(resolution instanceof PathTree.Resolution.Found, spelling);
        }
    }

    // -----------------------------------------------------------------------
    // The tree
    // -----------------------------------------------------------------------

    @Test
    public void theTreeAccountsForEveryOperationExactlyOnce() {
        for (String slug : new String[] {
                "ballerinax__github", "ballerinax__slack", "ballerinax__googleapis.gmail"}) {
            for (ClientClass client : FixtureCorpus.libraryFor(slug).clients()) {
                List<PathTree.Operation> operations = PathTree.operationsOf(client);
                if (operations.isEmpty()) {
                    continue;
                }
                PathTree tree = PathTree.build(operations);
                Assert.assertEquals(tree.total(), operations.size(), slug + " " + client.name() + ": total");
                Assert.assertEquals(PathTree.operationsUnder(tree).size(), operations.size(),
                        slug + " " + client.name() + ": walk");
            }
        }
    }

    @Test
    public void githubs903OperationsReduceTo36TopLevelSegments() {
        // The measurement the whole discovery argument rests on: a substring search for `repos` returns 484
        // hits, and this is 36 lines.
        PathTree tree = github();
        Assert.assertEquals(tree.total(), 903);
        Assert.assertEquals(tree.children().size(), 36);
        Assert.assertEquals(tree.children().get(0).segment(), "repos");
        Assert.assertEquals(tree.children().get(0).total(), 421,
                "busiest subtree first, because the tree is read to choose");
    }

    @Test
    public void slacksPathsDoNotNestAndTheTreeSaysSo() {
        // 174 operations across 174 distinct top segments: its paths are RPC-style dotted names, so level 1 IS
        // the complete list. An agent told to descend a flat list spends a turn discovering there is nowhere to
        // go.
        PathTree tree = PathTree.build(PathTree.operationsOf(clientOf("ballerinax__slack")));
        Assert.assertEquals(tree.total(), 174);
        Assert.assertEquals(tree.children().size(), 174);
        Assert.assertTrue(tree.children().stream().allMatch(child -> child.children().isEmpty()));
    }

    @Test
    public void childrenAreOrderedByOperationCountThenAlphabetically() {
        PathTree tree = github();
        for (int index = 1; index < tree.children().size(); index++) {
            PathTree previous = tree.children().get(index - 1);
            PathTree current = tree.children().get(index);
            boolean ordered = previous.total() > current.total()
                    || (previous.total() == current.total()
                            && Texts.compareLocale(previous.segment(), current.segment()) <= 0);
            Assert.assertTrue(ordered, previous.segment() + "(" + previous.total() + ") before "
                    + current.segment() + "(" + current.total() + ")");
        }
    }

    // -----------------------------------------------------------------------
    // Anchoring
    // -----------------------------------------------------------------------

    @Test
    public void aPathIsMatchedFromTheFirstSegmentNeverAsASuffix() {
        PathTree tree = github();
        PathTree.Resolution.Found anchored =
                (PathTree.Resolution.Found) PathTree.resolve(tree, PathTree.splitPath("repos/*/*"));
        Assert.assertEquals(anchored.node().operations().size(), 3);

        // The counter-measurements, which are why this walks the tree instead of filtering strings. Two
        // unrelated subtrees END in the same three segments, so a SUFFIX match returns nine operations for a
        // path that has three — and a SUBSTRING match returns 426.
        List<PathTree.Operation> all = PathTree.operationsUnder(tree);
        String target = "repos/{owner}/{repo}";
        List<String> bySuffix = all.stream()
                .map(operation -> String.join("/", operation.segments()))
                .filter(path -> path.endsWith(target))
                .toList();
        long bySubstring = all.stream()
                .map(operation -> String.join("/", operation.segments()))
                .filter(path -> path.contains(target))
                .count();
        Assert.assertEquals(bySuffix.size(), 9);
        Assert.assertEquals(bySubstring, 426);

        List<String> strangers = bySuffix.stream()
                .filter(path -> !path.startsWith(target))
                .distinct()
                .sorted()
                .toList();
        Assert.assertEquals(strangers, List.of(
                "orgs/{org}/teams/{teamSlug}/repos/{owner}/{repo}",
                "teams/{teamId}/repos/{owner}/{repo}"),
                "these two are what an unanchored match mixes in, and they are about team access");
    }

    @Test
    public void aPathThatRunsOutNamesWhatWasThere() {
        // Instead of failing blankly.
        PathTree.Resolution resolution =
                PathTree.resolve(github(), PathTree.splitPath("repos/*/*/nonesuch"));
        PathTree.Resolution.Missing missing = (PathTree.Resolution.Missing) resolution;
        Assert.assertEquals(missing.matched(), List.of("repos", "{owner}", "{repo}"));
        Assert.assertEquals(missing.token(), "nonesuch");
        Assert.assertEquals(missing.available().size(), 63);
    }

    @Test
    public void aFirstSegmentThatIsWrongReportsNothingMatched() {
        // Not a partial path.
        PathTree.Resolution resolution = PathTree.resolve(github(), PathTree.splitPath("nonesuch/repos"));
        Assert.assertEquals(((PathTree.Resolution.Missing) resolution).matched(), List.of());
    }

    @Test
    public void anEmptyPathIsTheRootWhichIsEveryOperation() {
        PathTree.Resolution resolution = PathTree.resolve(github(), PathTree.splitPath(""));
        Assert.assertEquals(((PathTree.Resolution.Found) resolution).node().total(), 903);
    }

    @Test
    public void aWildcardAddressesAParameterLevelAndTheAnchoredMatchIsNotASuffixMatch() {
        PathTree tree = github();
        PathTree.Resolution.Found anchored =
                (PathTree.Resolution.Found) PathTree.resolve(tree, PathTree.splitPath("repos/*/*"));
        Assert.assertEquals(anchored.node().operations().size(), 3);
        Assert.assertEquals(anchored.path(), List.of("repos", "{owner}", "{repo}"));

        // The same three are reachable by naming the parameters, with or without braces.
        for (String spelling : new String[] {"repos/{owner}/{repo}", "repos/owner/repo", "repos/*/repo"}) {
            PathTree.Resolution resolution = PathTree.resolve(tree, PathTree.splitPath(spelling));
            Assert.assertTrue(resolution instanceof PathTree.Resolution.Found, spelling);
            Assert.assertEquals(
                    ((PathTree.Resolution.Found) resolution).node().operations().size(), 3, spelling);
        }
    }

    // -----------------------------------------------------------------------
    // Auto-descent
    // -----------------------------------------------------------------------

    /**
     * Locating a trailing segment deeper than it was asked for, which is the ONE relaxation of anchoring.
     *
     * <p>`repos/owner/repo/caches` is a real request against github: `caches` exists, at
     * `repos/{owner}/{repo}/actions/caches`. The anchored answer is honest and costs a round trip, so the trailing
     * segment is located under the prefix that DID match — and only when it is unambiguous, because picking one of
     * several is exactly the failure anchoring exists to prevent.
     */
    @Test
    public void aTrailingSegmentIsLocatedUnderTheMatchedPrefixWhenThereIsExactlyOne() {
        PathTree tree = github();
        PathTree.Located located =
                PathTree.locate(tree, PathTree.splitPath("repos/owner/repo/caches"));
        Assert.assertTrue(located.relocated(), "one occurrence, so it is answered");
        Assert.assertTrue(located.resolution() instanceof PathTree.Resolution.Found);
        Assert.assertEquals(located.alternatives().size(), 1);
        Assert.assertEquals(String.join("/", located.alternatives().get(0)),
                "repos/{owner}/{repo}/actions/caches");
    }

    @Test
    public void aTrailingSegmentFoundInSeveralPlacesIsListedAndNotChosen() {
        PathTree tree = github();
        // `secrets` exists under `actions`, `codespaces` AND `dependabot`, and they are three different APIs.
        PathTree.Located located =
                PathTree.locate(tree, PathTree.splitPath("repos/owner/repo/secrets"));
        Assert.assertFalse(located.relocated(), "several occurrences, so none is chosen");
        Assert.assertTrue(located.resolution() instanceof PathTree.Resolution.Missing);
        Assert.assertTrue(located.alternatives().size() > 1, located.alternatives().toString());
        for (List<String> path : located.alternatives()) {
            Assert.assertTrue(PathTree.resolve(tree, path) instanceof PathTree.Resolution.Found,
                    "every path offered has to be one the tree holds: " + String.join("/", path));
        }
    }

    @Test
    public void locationOnlyAppliesToTheLastSegmentAndNeverRelaxesAnchoringItself() {
        PathTree tree = github();
        // A segment that misses in the MIDDLE is still a miss: there are tokens after it, so there is no single
        // trailing segment to locate and guessing where the rest attaches would be exactly the suffix matching
        // that returns nine operations for a three-operation path.
        PathTree.Located middle = PathTree.locate(tree, PathTree.splitPath("repos/nonesuch/actions/caches"));
        Assert.assertFalse(middle.relocated());
        Assert.assertTrue(middle.resolution() instanceof PathTree.Resolution.Missing);
        Assert.assertTrue(middle.alternatives().isEmpty(), middle.alternatives().toString());

        // And a path that resolves anchored is untouched — no search runs at all.
        PathTree.Located exact = PathTree.locate(tree, PathTree.splitPath("repos/owner/repo"));
        Assert.assertFalse(exact.relocated());
        Assert.assertTrue(exact.resolution() instanceof PathTree.Resolution.Found);
        Assert.assertTrue(exact.alternatives().isEmpty());
    }

    @Test
    public void descentStepsThroughParameterOnlyLevelsAndNamesTheBranchItDidNotTake() {
        PathTree tree = github();
        PathTree.Resolution.Found repos =
                (PathTree.Resolution.Found) PathTree.resolve(tree, List.of("repos"));

        PathTree.Descent descent = PathTree.autoDescend(repos.node(), repos.path());
        Assert.assertEquals(descent.path(), List.of("repos", "{owner}", "{repo}"));
        Assert.assertEquals(descent.node().operations().size(), 3);

        // Naming the sibling is not cosmetic: `repos` really has two parameter children with different
        // spellings, and collapsing silently to the dominant one hides an operation permanently, because
        // nothing downstream would mention it again.
        Assert.assertEquals(descent.skipped().size(), 1);
        Assert.assertEquals(descent.skipped().get(0).path(),
                List.of("repos", "{templateOwner}", "{templateRepo}", "generate"));
        Assert.assertEquals(descent.skipped().get(0).total(), 1);
    }

    @Test
    public void descentStopsAtALevelWithALiteralChild() {
        // Because that is a real choice.
        PathTree tree = github();
        PathTree.Resolution.Found user = (PathTree.Resolution.Found) PathTree.resolve(tree, List.of("user"));
        PathTree.Descent descent = PathTree.autoDescend(user.node(), user.path());
        // `user` has operations of its own AND literal children; choosing for the caller there would be guessing
        // which resource they meant.
        Assert.assertEquals(descent.path(), List.of("user"));
        Assert.assertEquals(descent.skipped().size(), 0);
    }

    @Test
    public void descentTerminatesOnALeaf() {
        PathTree tree = github();
        PathTree.Resolution.Found zen = (PathTree.Resolution.Found) PathTree.resolve(tree, List.of("zen"));
        PathTree.Descent descent = PathTree.autoDescend(zen.node(), zen.path());
        Assert.assertEquals(descent.path(), List.of("zen"));
        Assert.assertEquals(descent.node().operations().size(), 1);
    }

    // -----------------------------------------------------------------------
    // The declaration index
    // -----------------------------------------------------------------------

    @Test
    public void aDuplicateNameKeepsTheFirstDeclaration() {
        // So a collision cannot pass unnoticed.
        Declarations index = Declarations.index(List.of(
                new TypeDef.ErrorDef("ClientError", "the correction", false, Optional.empty()),
                new TypeDef.Alias("ClientError", "Central's placeholder", new TypeRef(""))));
        Assert.assertEquals(index.names(), List.of("ClientError"));
        Assert.assertTrue(index.get("ClientError") instanceof TypeDef.ErrorDef);
    }

    @Test
    public void githubsDeclarationRosterIs1227NamesAndItsOperationsAreNotInIt() {
        Declarations index = Declarations.index(FixtureCorpus.libraryFor("ballerinax__github").typeDefs());
        // 1227 and not 1224: reading `anyDataTypes` added github's three, which used to be dropped.
        Assert.assertEquals(index.names().size(), 1227);
        // Operations are addressed by path, not by name, which is why the oracle's bijection is scoped to
        // declarations.
        Assert.assertNull(index.get("repos"));
    }

    // -----------------------------------------------------------------------
    // Name matching
    // -----------------------------------------------------------------------

    @Test
    public void normalisationIsWhatStatusAcceptedSpellingsHaveInCommon() {
        Assert.assertEquals(Names.normalise("STATUS_ACCEPTED"), "statusaccepted");
        Assert.assertEquals(Names.normalise("StatusAccepted"), "statusaccepted");
        Assert.assertEquals(Names.normalise("client_id"), "clientid");
        Assert.assertEquals(Names.normalise("clientId"), "clientid");
        Assert.assertEquals(Names.normalise("'import"), "import");
    }

    @Test
    public void anExactMatchWinsBeforeNormalisationIsEvenTried() {
        // github's ManifestConversions declares BOTH clientId and client_id, so the exact spelling has to be
        // honoured or one of them is unreachable.
        List<String> names = List.of("clientId", "client_id");
        Assert.assertEquals(Names.match("clientId", names), new Names.Match.Found("clientId"));
        Assert.assertEquals(Names.match("client_id", names), new Names.Match.Found("client_id"));
    }

    @Test
    public void aNormalisedCollisionIsReportedWithEveryMatch() {
        // Never resolved silently.
        Names.Match match = Names.match("CLIENTID", List.of("clientId", "client_id"));
        Assert.assertTrue(match instanceof Names.Match.Ambiguous);
        Assert.assertEquals(
                ((Names.Match.Ambiguous) match).candidates().stream().sorted().toList(),
                List.of("clientId", "client_id"));
    }

    @Test
    public void aMissSuggestsNamesByTheLongestRunOfCharactersTheyShare() {
        List<String> names = List.of(
                "FullRepository", "MinimalRepository", "NullableRepository", "Repository", "SimpleUser",
                "Issue");
        List<String> candidates = Names.nearMisses("FullRepo", names);
        Assert.assertEquals(candidates.get(0), "FullRepository", "the closest first");
        for (String expected : new String[] {"MinimalRepository", "NullableRepository", "Repository"}) {
            Assert.assertTrue(candidates.contains(expected), expected + " shares 'Repo'");
        }
        Assert.assertFalse(candidates.contains("Issue"),
                "four characters of overlap is the floor, and 'Issue' shares none");
    }

    @Test
    public void candidatesAreCapped() {
        // Because the alternative is the whole roster on stderr: 33,431 bytes for github's 1,224 names.
        Declarations index = Declarations.index(FixtureCorpus.libraryFor("ballerinax__github").typeDefs());
        Assert.assertTrue(Names.nearMisses("Repository", index.names()).size() <= Names.MAX_CANDIDATES);
    }

    @Test
    public void aRequestThatResemblesNothingGetsNoCandidatesRatherThanNoise() {
        Names.Match match = Names.match("zzzzzz", List.of("FullRepository", "SimpleUser"));
        Assert.assertTrue(match instanceof Names.Match.Missing);
        Assert.assertEquals(((Names.Match.Missing) match).candidates(), List.of());
    }

    // -----------------------------------------------------------------------
    // The discovery corpus
    // -----------------------------------------------------------------------

    @Test(dataProvider = "recordedLookups")
    public void theRecordedLookupResolves(String fixture, String path, int operations) {
        PathTree tree = PathTree.build(PathTree.operationsOf(clientOf(fixture)));
        PathTree.Resolution resolution = PathTree.resolve(tree, PathTree.splitPath(path));
        Assert.assertTrue(resolution instanceof PathTree.Resolution.Found, path + " is unreachable");
        int found = PathTree.operationsUnder(((PathTree.Resolution.Found) resolution).node()).size();
        Assert.assertEquals(found, operations, path + " moved from " + operations + " to " + found);
        Assert.assertTrue(found > 0, "a zero-hit pin cannot masquerade as a working index");
    }

    @Test
    public void everyOperationInTheCorpusCarriesAReturnType() {
        // Which is what the lookup came for. Two of the golden run's twelve turns went to learning what an
        // operation returns; if a signature can render without one, the verb that replaces those turns is
        // sometimes silently useless.
        for (String slug : new String[] {
                "ballerinax__github", "ballerinax__slack", "ballerinax__googleapis.gmail"}) {
            for (ClientClass client : FixtureCorpus.libraryFor(slug).clients()) {
                for (Fn fn : client.functions()) {
                    Assert.assertFalse(fn.returns().type().name().isEmpty(),
                            slug + " " + client.name() + ": a signature with no return type");
                }
            }
        }
    }

    @Test
    public void httpsFourResourceClientsEachCarrySeven() {
        // The measurement `ops` refuses to guess between, and the reason --client exists.
        for (String name : new String[] {"Client", "FailoverClient", "LoadBalanceClient", "StatusCodeClient"}) {
            Assert.assertEquals(PathTree.operationsOf(clientOf("ballerina__http", name)).size(), 7, name);
        }
    }
}
