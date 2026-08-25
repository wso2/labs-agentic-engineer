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

import io.ballerina.library.model.Library;
import io.ballerina.library.model.TypeDef;
import io.ballerina.library.symbols.PathTree;
import io.ballerina.library.symbols.Surface;
import io.ballerina.library.views.Containers;
import io.ballerina.library.views.Guide;
import io.ballerina.library.views.Overview;
import io.ballerina.library.views.TypeView;
import org.testng.Assert;
import org.testng.annotations.DataProvider;
import org.testng.annotations.Test;

import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

/**
 * The report documents, snapshotted, plus the composition rules that decide their shape.
 *
 * <p>{@link ViewsAgreeTest} proves a view never invents a signature; this proves the documents themselves do not
 * change silently. The two together are why a rendering change has to be a reviewable diff rather than something an
 * agent discovers at run time.
 *
 * <p>These snapshots are also where the ORDERING is pinned. The path tree sorts ties with a locale collator, not
 * with {@code String::compareTo}, and the two disagree on real github segments — {@code compareTo} moves
 * {@code {owner}} from position 2 to position 10 by putting punctuation before letters. Nothing else in the suite
 * would notice.
 *
 * @since 0.1.0
 */
public class ViewsTest {

    /**
     * Where each fixture's bare {@code client} listing lands on the tier ladder.
     *
     * <p>The measurement the byte budget was chosen against, kept as an assertion so a change to the budget shows
     * up as a change to this table rather than as a quietly worse document. {@code SIGNATURE} means every
     * signature printed in full; {@code INDEX} means names only; {@code GROUPED} means path roots with counts.
     */
    private static final Map<String, String> CLIENT_TIER = Map.ofEntries(
            // In full: 15 to 113 signatures, all 44 of sheets' remote functions and all 113 of redis'. The redis
            // case is the one that matters — it was measured as the single most productive lookup of the
            // 2026-08-15 sweep, answering a whole connector with no follow-up, and a COUNT limit would have
            // replaced it with an index.
            Map.entry("ballerinax__sap", "signature"),
            Map.entry("ballerinax__googleapis.gmail", "signature"),
            Map.entry("ballerinax__googleapis.sheets", "signature"),
            Map.entry("ballerinax__redis", "signature"),
            Map.entry("ballerina__graphql", "signature"),
            // Names only: slack's 175 and twilio's 200 are past 20,000 bytes of signatures but well inside a
            // column listing, which for a name-addressed surface IS its index.
            Map.entry("ballerinax__slack", "index"),
            Map.entry("ballerinax__twilio", "index"),
            // Grouped: github's 903 resource functions are past even the name listing, at roughly 40 bytes a path.
            Map.entry("ballerinax__github", "grouped"));

    /**
     * How many error declarations each fixture has, counted from its published source.
     *
     * <p>HTTP-11: {@code ballerina/http} reads 65, not 56. Its source declares 65 public error types — 64
     * {@code distinct} plus {@code StatusCodeResponseDataBindingError}, a union of three of them — and the nine the
     * old filter missed are published under an alias category rather than under {@code errors}.
     *
     * <p>{@code ballerinax/sap} is deliberately absent: its {@code ClientError} is the re-export
     * {@code simpleNameReferenceTypes} always carried, so sap declares no error of its own (SAP-01).
     */
    private static final Map<String, Integer> ERROR_COUNTS = Map.of(
            "ballerina__http", 65,
            "ballerina__graphql", 9,
            "ballerinax__kafka", 3,
            "ballerinax__googleapis.gmail", 3,
            "ballerinax__googleapis.sheets", 3,
            "ballerinax__redis", 1,
            "ballerina__xlsx", 11,
            "ballerina__log", 1);

    @DataProvider(name = "fixtures")
    public Object[][] fixtures() {
        return FixtureCorpus.fixtureRows();
    }

    private static Path viewSnapshot(String slug, String view) {
        return FixtureCorpus.SNAPSHOTS_DIR.resolve(slug + "." + view + ".md");
    }

    private static String render(String slug, Surface.Scope scope) {
        Result<String> view = Containers.render(
                FixtureCorpus.loadedFixture(slug), scope, Containers.Options.bare());
        Assert.assertTrue(view.isOk(), scope.verb() + " failed for " + slug + ": "
                + (view.isOk() ? "" : view.failure().describe()));
        return view.value();
    }

    // -----------------------------------------------------------------------
    // Snapshots
    // -----------------------------------------------------------------------

    @Test(dataProvider = "fixtures")
    public void theMapIsUnchanged(String slug) {
        FixtureCorpus.matchesSnapshot(
                viewSnapshot(slug, "overview"),
                Overview.render(FixtureCorpus.loadedFixture(slug)),
                slug + " overview");
    }

    /**
     * One snapshot per scope, because the three verbs share one implementation.
     *
     * <p>That sharing is the reason all three are pinned rather than one: a change made for the shape
     * {@code client} sees lands on {@code class} and {@code funcs} at the same time, and 91 classes or 7 module
     * functions are the shapes least likely to be checked by hand.
     */
    @Test(dataProvider = "fixtures")
    public void everyScopesListingIsUnchanged(String slug) {
        for (Surface.Scope scope : Surface.Scope.values()) {
            if (Surface.of(FixtureCorpus.libraryFor(slug), scope).isEmpty()) {
                continue;
            }
            FixtureCorpus.matchesSnapshot(
                    viewSnapshot(slug, scope.verb()), render(slug, scope), slug + " " + scope.verb());
        }
    }

    // -----------------------------------------------------------------------
    // The budget and the tiers
    // -----------------------------------------------------------------------

    @Test
    public void theTierLadderFiresExactlyWhereTheMeasurementsSayItShould() {
        for (Map.Entry<String, String> expected : CLIENT_TIER.entrySet()) {
            String document = render(expected.getKey(), Surface.Scope.CLIENT);
            String row = document.lines().filter(line -> line.startsWith("| Showing"))
                    .findFirst().orElseThrow(() -> new AssertionError(expected.getKey()
                            + ": no Showing row — a package with several clients answers with a roster, so it "
                            + "does not belong in this table"));
            String tier = switch (expected.getValue()) {
                case "signature" -> "signature";
                case "index" -> "by name, no signatures";
                case "grouped" -> "grouped, no names";
                default -> throw new AssertionError("unknown tier " + expected.getValue());
            };
            Assert.assertTrue(row.contains(tier),
                    expected.getKey() + " should render at " + expected.getValue() + ": " + row);
        }
    }

    @Test(dataProvider = "fixtures")
    public void aListingNeverExceedsItsByteBudgetUnlessAllWasAsked(String slug) {
        // Never paginated: a listing over budget is re-rendered coarser, so the bound is a property of every
        // document rather than of the packages that happen to be small.
        for (Surface.Scope scope : Surface.Scope.values()) {
            if (Surface.of(FixtureCorpus.libraryFor(slug), scope).isEmpty()) {
                continue;
            }
            String document = render(slug, scope);
            int fenced = fencedBytes(document);
            Assert.assertTrue(fenced <= Containers.MAX_LISTING_BYTES,
                    slug + " " + scope.verb() + ": " + fenced + " bytes of quoted content");
        }
    }

    @Test
    public void allIgnoresTheBudgetAndIsTheOnlyThingThatDoes() {
        // `--all` is the escape hatch: it exists so a caller who genuinely needs 903 signatures can have them, and
        // it is hidden from --help so nobody reaches for it first. github is the case that proves both halves.
        LoadedPackage github = FixtureCorpus.loadedFixture("ballerinax__github");
        String bounded = render("ballerinax__github", Surface.Scope.CLIENT);
        Assert.assertTrue(fencedBytes(bounded) <= Containers.MAX_LISTING_BYTES);

        Result<String> all = Containers.render(github, Surface.Scope.CLIENT,
                new Containers.Options(List.of("Client"), null, false, true));
        Assert.assertTrue(all.isOk(), all.isOk() ? "" : all.failure().describe());
        Assert.assertTrue(fencedBytes(all.value()) > Containers.MAX_LISTING_BYTES * 4,
                "--all on github's client is tens of thousands of bytes of signatures");
    }

    /**
     * A collapsed section states its cost, and offers {@code --all} last.
     *
     * <p>ADR-0013 and the pre-announcement rule together: the escape hatch appears where it applies, with the byte
     * figure that makes choosing it a decision rather than a gamble, and nowhere else. Its absence from
     * {@code --help} is asserted in {@link LibraryToolTest}.
     */
    @Test
    public void aCollapsedListingNamesItsCostAndOffersTheEscapeHatchLast() {
        String document = render("ballerinax__github", Surface.Scope.CLIENT);
        List<String> bullets = document.lines().filter(line -> line.startsWith("- ")).toList();
        Assert.assertFalse(bullets.isEmpty(), document);
        String last = bullets.get(bullets.size() - 1);
        Assert.assertTrue(last.contains("--all"), "the escape hatch is offered last: " + last);
        Assert.assertTrue(last.contains("last resort"), last);
        Assert.assertTrue(Pattern.compile("[\\d,]+ bytes").matcher(last).find(),
                "with its cost stated: " + last);
        for (String bullet : bullets.subList(0, bullets.size() - 1)) {
            Assert.assertFalse(bullet.contains("--all"), "offered once, not repeatedly: " + bullet);
        }
    }

    /** The bytes inside every fenced block, which is what the budget bounds. */
    private static int fencedBytes(String document) {
        StringBuilder inside = new StringBuilder();
        boolean fenced = false;
        for (String line : document.split("\n", -1)) {
            if (line.startsWith("```")) {
                fenced = !fenced;
                continue;
            }
            if (fenced) {
                inside.append(line).append('\n');
            }
        }
        return Texts.byteLength(inside.toString());
    }

    // -----------------------------------------------------------------------
    // Resolution and tolerance
    // -----------------------------------------------------------------------

    @Test
    public void aSingleResultIsAnsweredInFullRatherThanByPrintingItsNameBack() {
        // T15. An exact one-of-many name match printed the name and forced a second call for the signature the
        // caller had already identified. One result is the case where the richest tier always fits.
        LoadedPackage kafka = FixtureCorpus.loadedFixture("ballerinax__kafka");
        Result<String> view = Containers.render(kafka, Surface.Scope.CLIENT,
                new Containers.Options(List.of("Producer", "send")));
        Assert.assertTrue(view.isOk(), view.isOk() ? "" : view.failure().describe());
        String document = view.value();
        // `send` is an EXACT member name, so it wins over the substring pass that would also have matched
        // `sendWithMetadata` — which is what makes "exactly one result" reachable at all.
        Assert.assertTrue(document.contains("| Showing | the declaration in full"), document);
        Assert.assertTrue(document.contains("remote function send("), document);
        // The FULL tier is the only one that prints the `# +` parameter rows, which is what makes it richer rather
        // than merely shorter than the listing.
        Assert.assertTrue(document.contains("# + "), document);
        // And the types the signature names arrive with it, one level deep, so the common flow is one call.
        Assert.assertTrue(document.contains("## The types it names"), document);
    }

    @Test
    public void aMemberNameResolvesAndNamesItsOwnerRatherThanFailing() {
        // T3, the sweep's most-hit ergonomic bug: a name that was not a container was discarded, and the
        // suggestion rebuilt the command WITHOUT it, so following the advice looped.
        LoadedPackage http = FixtureCorpus.loadedFixture("ballerina__http");
        Result<String> view = Containers.render(http, Surface.Scope.CLASS,
                new Containers.Options(List.of("toStringValue")));
        Assert.assertTrue(view.isOk(), view.isOk() ? "" : view.failure().describe());
        Assert.assertTrue(view.value().contains("| Note | `toStringValue` is declared on `Cookie`"),
                view.value());
        Assert.assertTrue(view.value().contains("bal library class ballerina/http Cookie toStringValue"),
                view.value());
    }

    @Test
    public void aMemberOnSeveralContainersIsARosterOfOwnersNotAFailure() {
        // The other half of T3. Picking one owner silently is what the path side refuses to do, so the answer is
        // the owners with counts and the command that opens each.
        LoadedPackage kafka = FixtureCorpus.loadedFixture("ballerinax__kafka");
        Result<String> view = Containers.render(kafka, Surface.Scope.CLIENT,
                new Containers.Options(List.of("commit")));
        Assert.assertTrue(view.isOk(), view.isOk() ? "" : view.failure().describe());
        Assert.assertTrue(view.value().contains("owners"), view.value());
        Assert.assertTrue(view.value().contains("`bal library client ballerinax/kafka Caller commit`"),
                view.value());
        Assert.assertTrue(view.value().contains("`bal library client ballerinax/kafka Consumer commit`"),
                view.value());
    }

    @Test
    public void aVerbGivenAnotherKindsSymbolStillAnswersAndNamesTheCanonicalVerb() {
        // T6. `ops <pkg> <constant>` failed with a client-ambiguity error for a name the package declares plainly.
        // Without tolerance every kind guess risks a wasted round trip; with it the split costs one printed line.
        LoadedPackage http = FixtureCorpus.loadedFixture("ballerina__http");

        // A class, asked of `client`.
        Result<String> asClient = Containers.render(http, Surface.Scope.CLIENT,
                new Containers.Options(List.of("Cookie")));
        Assert.assertTrue(asClient.isOk(), asClient.isOk() ? "" : asClient.failure().describe());
        Assert.assertTrue(asClient.value().contains("| Note | `Cookie` is addressed by `class`"),
                asClient.value());
        Assert.assertTrue(asClient.value().contains("bal library class ballerina/http Cookie"),
                asClient.value());

        // A record, asked of `client`: not a callable at all, so the answer is the code register with a `//` note.
        Result<String> asType = Containers.render(http, Surface.Scope.CLIENT,
                new Containers.Options(List.of("ClientConfiguration")));
        Assert.assertTrue(asType.isOk(), asType.isOk() ? "" : asType.failure().describe());
        Assert.assertTrue(asType.value().contains(
                "// Note: ClientConfiguration is a declaration, not a callable"), asType.value());
        Assert.assertTrue(asType.value().contains("public type ClientConfiguration record"), asType.value());
        // The note is written in the register of the document it lands in, never as a bare `note:` prefix that
        // fits neither.
        Assert.assertFalse(asType.value().contains("| Note |"), "a table row in the code register");
    }

    @Test
    public void aClientIsALegalArgumentToClassAndItsSelectorGrammarFollowsIt() {
        // The claim a design revision got wrong: HTTP-verb parsing cannot be confined to one verb, because in
        // Ballerina a client IS a class. `ballerina/http:Client` declares seven resource functions either way.
        LoadedPackage http = FixtureCorpus.loadedFixture("ballerina__http");
        Result<String> view = Containers.render(http, Surface.Scope.CLASS,
                new Containers.Options(List.of("Client", "get", "path")));
        Assert.assertTrue(view.isOk(), view.isOk() ? "" : view.failure().describe());
        Assert.assertTrue(view.value().contains("is addressed by `client`"), view.value());
        // Quoted from what the tool prints, not re-spelled from memory: the rest-parameter form is
        // `[PathParamType ...path]`, with the ellipsis bound to the NAME. An assertion written the other way round
        // passes only against a renderer that has the same bug.
        Assert.assertTrue(view.value().contains("resource function get [PathParamType ...path]"),
                view.value());

        // And on a container WITHOUT resource functions the same token is a member name, finds none, and the
        // recovery names what that container actually declares.
        Result<String> cookie = Containers.render(http, Surface.Scope.CLASS,
                new Containers.Options(List.of("Cookie", "get")));
        Assert.assertTrue(cookie.isOk(), cookie.isOk() ? "" : cookie.failure().describe());
        Assert.assertTrue(cookie.value().contains("| Matched | nothing on `Cookie`"), cookie.value());
        Assert.assertTrue(cookie.value().contains("toStringValue"), cookie.value());
    }

    @Test
    public void aConstructorIsPartOfTheContainerAndIsReachable() {
        // T14: `ops` could not address one, and the only document that carried it was `overview --client <Name>`,
        // which no longer exists. `init` is the one method every caller has to write.
        LoadedPackage sheets = FixtureCorpus.loadedFixture("ballerinax__googleapis.sheets");
        String whole = render("ballerinax__googleapis.sheets", Surface.Scope.CLIENT);
        Assert.assertTrue(whole.contains("## Constructor — 1"), whole);
        Assert.assertTrue(whole.contains("function init("), whole);

        Result<String> byName = Containers.render(sheets, Surface.Scope.CLIENT,
                new Containers.Options(List.of("Client", "init")));
        Assert.assertTrue(byName.isOk(), byName.isOk() ? "" : byName.failure().describe());
        Assert.assertTrue(byName.value().contains("function init("), byName.value());
    }

    @Test
    public void aClientWithBothHalvesIsAnsweredWithBothSplitByCallForm() {
        // `ballerina/http`'s `Client` declares 7 resource functions and 19 named ones. The shipped view printed
        // the 7 under a fact row reading `(7 of 7)` and said nothing about `execute`, `forward`, `submit`, the
        // promise set or the circuit-breaker controls — reachable from no verb in the tool (ADR-0019).
        LoadedPackage http = FixtureCorpus.loadedFixture("ballerina__http");
        Result<String> view = Containers.render(http, Surface.Scope.CLIENT,
                new Containers.Options(List.of("Client")));
        Assert.assertTrue(view.isOk(), view.isOk() ? "" : view.failure().describe());
        String document = view.value();
        Assert.assertTrue(document.contains("## Resource functions —"), document);
        Assert.assertTrue(document.contains("## Remote functions —"), document);
        // The call form is printed on every section heading, because `->` versus `.` is the fact a caller came for
        // and both signature errors in the 2026-08-15 sweep came from its absence.
        Assert.assertTrue(document.contains(", call with `->`"), document);
        Assert.assertTrue(document.contains("## Normal functions —"), document);
        Assert.assertTrue(document.contains(", call with `.`"), document);
        Assert.assertTrue(document.contains("execute"), document);
        Assert.assertTrue(document.contains("getCookieStore"), document);
    }

    @Test
    public void aScopeWithNothingInItSaysWhereTheCallableSurfaceIs() {
        // A honest empty answer rather than an implied absence: kafka declares no module-level function, and the
        // reply names the verbs that DO have something plus their counts.
        Result<String> view = Containers.render(FixtureCorpus.loadedFixture("ballerinax__kafka"),
                Surface.Scope.MODULE, Containers.Options.bare());
        Assert.assertTrue(view.isOk(), view.isOk() ? "" : view.failure().describe());
        Assert.assertTrue(view.value().contains("| Module functions | this package declares none |"),
                view.value());
        Assert.assertTrue(view.value().contains("`bal library client ballerinax/kafka`"), view.value());
        Assert.assertTrue(view.value().contains("`bal library class ballerinax/kafka`"), view.value());
    }

    @Test
    public void aModuleFunctionCarriesPublicAndAMemberDoesNot() {
        // The renderer is chosen by SCOPE rather than by shape, which is the same split the API document draws.
        // Asserting the keyword is what catches the wrong one.
        String funcs = render("ballerina__http", Surface.Scope.MODULE);
        Assert.assertTrue(funcs.contains("public isolated function "), funcs);
        String classes = render("ballerina__http", Surface.Scope.CLASS);
        Assert.assertFalse(classes.contains("\npublic isolated function "),
                "a member does not carry public");
    }

    /**
     * A selector spelled the way the document PRINTS it resolves, in one token or in two.
     *
     * <p>The measured defect: {@code client ballerinax/slack Client "post chat\.postMessage"} matched nothing,
     * while the same request with the accessor as its own argument matched. Ballerina escapes the dot in a path
     * segment, so {@code chat\.postMessage} is what every fenced signature this tool emits contains — and an
     * agent that copies one back as a single quoted argument was told there is no such operation, on a client
     * declaring 174. It cost a real run one call.
     *
     * <p>The cause is not the tree, which has always taken both spellings: it is that a one-token selector is
     * matched against an entry's LABEL, and the label is built unescaped for prose. So the two spellings met on
     * a comparison that had never been given the same normalisation the path walk has, and the register split
     * that the whole design rests on leaked into the argument grammar.
     */
    @Test
    public void aSelectorResolvesInTheSpellingTheDocumentPrints() {
        LoadedPackage slack = FixtureCorpus.loadedFixture("ballerinax__slack");
        for (List<String> selectors : List.of(
                List.of("Client", "post chat\\.postMessage"),
                List.of("Client", "post chat.postMessage"),
                List.of("Client", "post", "chat\\.postMessage"),
                List.of("Client", "post", "chat.postMessage"))) {
            Result<String> view = Containers.render(slack, Surface.Scope.CLIENT,
                    new Containers.Options(selectors));
            Assert.assertTrue(view.isOk(), selectors + ": " + (view.isOk() ? "" : view.failure().describe()));
            Assert.assertTrue(view.value().contains("resource function post chat\\.postMessage"),
                    selectors + " did not resolve:\n" + view.value());
        }

        // The same for github's `-`, which needs the same escape and appears in 40-odd paths.
        LoadedPackage github = FixtureCorpus.loadedFixture("ballerinax__github");
        for (String path : List.of("code\\-scanning/alerts", "code-scanning/alerts")) {
            Result<String> view = Containers.render(github, Surface.Scope.CLIENT,
                    new Containers.Options(List.of("Client", "get repos/{owner}/{repo}/" + path)));
            Assert.assertTrue(view.isOk(), path + ": " + (view.isOk() ? "" : view.failure().describe()));
            Assert.assertTrue(view.value().contains("code\\-scanning/alerts"), path + ":\n" + view.value());
        }
    }

    /**
     * A miss in the CODE register is bounded, like a miss in the report register already was.
     *
     * <p>Measured: {@code client ballerinax/github Client nosuchthingatall -r} answered with 42,746 bytes — every
     * one of 903 labels, on a single line, for a typo. The report register answers the same miss in about 800 and
     * points at the listing, so the budget rule (ADR-0020) held everywhere except the one path a caller reaches
     * by making a mistake, which is the path least worth spending 10,000 tokens on.
     *
     * <p>Bounded by the same {@code MAX_LISTING_BYTES}, and it must still name the recovery — an empty answer
     * that offers no next command is what ADR-0014 was written about.
     */
    /**
     * {@code new} addresses the constructor, which Ballerina spells {@code init}.
     *
     * <p>Measured twice, in two separate sweeps: an agent asked for
     * {@code client ballerinax/redis Client new}, was told nothing matched on a container declaring 112 members,
     * and found it on the next call as {@code init}. One wasted round trip each time, for a guess that is correct
     * in most languages an agent has read.
     *
     * <p>An INPUT alias, not an output one — the document still prints {@code init}, because it prints what the
     * package declares. That is the same trade already made for {@code {owner}} / {@code [string owner]} and for
     * the escaped path spellings: the tool accepts what a caller will plausibly type and quotes only what is real.
     *
     * <p><b>A REAL {@code new} wins.</b> github declares {@code repos/[string owner]/[string repo]/codespaces/'new},
     * so on that client the token addresses an operation and the alias must not shadow it. The path walk runs before
     * the alias for exactly this reason, and the second half of this test pins it — an alias that outranked a
     * declared name would be the tool answering a different question from the one asked.
     */
    @Test
    public void newAddressesTheConstructorUnlessSomethingIsReallyCalledThat() {
        for (String slug : FixtureCorpus.listFixtures()) {
            LoadedPackage loaded = FixtureCorpus.loadedFixture(slug);
            for (Surface.Container container : Surface.of(loaded.library(), Surface.Scope.CLIENT)) {
                if (container.constructor().isEmpty() || declaresNew(container)) {
                    continue;
                }
                Result<String> view = Containers.render(loaded, Surface.Scope.CLIENT,
                        new Containers.Options(List.of(container.name(), "new")));
                Assert.assertTrue(view.isOk(), slug + ": " + (view.isOk() ? "" : view.failure().describe()));
                Assert.assertFalse(view.value().contains("| Matched | nothing"),
                        slug + "/" + container.name() + ": `new` found no constructor:\n" + view.value());
                Assert.assertTrue(view.value().contains("function init("),
                        slug + "/" + container.name() + ": the real name is not printed:\n" + view.value());
            }
        }

        // And the declared one wins where there is one.
        Result<String> github = Containers.render(FixtureCorpus.loadedFixture("ballerinax__github"),
                Surface.Scope.CLIENT, new Containers.Options(List.of("Client", "new")));
        Assert.assertTrue(github.isOk(), github.isOk() ? "" : github.failure().describe());
        Assert.assertTrue(github.value().contains("codespaces/'new("),
                "a declared `new` must outrank the constructor alias:\n" + github.value());
    }

    /** Does this container hold anything genuinely named {@code new} — a member, or a path segment? */
    private static boolean declaresNew(Surface.Container container) {
        return container.memberNames().stream().anyMatch(name -> name.equalsIgnoreCase("new"))
                || container.operations().stream()
                        .anyMatch(operation -> operation.segments().stream()
                                .anyMatch(segment -> PathTree.readableSegment(segment).equalsIgnoreCase("new")));
    }

    @Test
    public void aMissInTheCodeRegisterIsBoundedAndStillNamesTheRecovery() {
        for (String slug : FixtureCorpus.listFixtures()) {
            LoadedPackage loaded = FixtureCorpus.loadedFixture(slug);
            for (Surface.Container container : Surface.of(loaded.library(), Surface.Scope.CLIENT)) {
                Result<String> view = Containers.render(loaded, Surface.Scope.CLIENT,
                        new Containers.Options(
                                List.of(container.name(), "zzznosuchthing"), null, true, false));
                Assert.assertTrue(view.isOk(), slug + ": " + (view.isOk() ? "" : view.failure().describe()));
                String document = view.value();
                Assert.assertTrue(Texts.byteLength(document) <= 4_000,
                        slug + "/" + container.name() + ": a miss cost "
                                + Texts.byteLength(document) + " bytes:\n" + document);
                // Still Ballerina, and still a way out.
                Assert.assertFalse(document.contains("| "), slug + ": a table in the code register");
                Assert.assertTrue(document.contains("bal library client"),
                        slug + ": the miss offers no next command:\n" + document);
            }
        }
    }

    @Test
    public void aSelectorThatMatchesNothingIsAnsweredWithWhatIsThere() {
        // Exit 0 with the alternatives rather than a failure: an empty selection is a fact about the container,
        // and the caller's next move is in the document. It must never offer a command without the argument that
        // failed — the shipped `ops` path did exactly that.
        for (String slug : FixtureCorpus.listFixtures()) {
            for (Surface.Container container : Surface.of(
                    FixtureCorpus.libraryFor(slug), Surface.Scope.CLIENT)) {
                Result<String> view = Containers.render(FixtureCorpus.loadedFixture(slug),
                        Surface.Scope.CLIENT,
                        new Containers.Options(List.of(container.name(), "zzznosuchthing")));
                Assert.assertTrue(view.isOk(), slug + ": " + (view.isOk() ? "" : view.failure().describe()));
                Assert.assertTrue(view.value().contains("| Requested | `zzznosuchthing` |"), view.value());
                Assert.assertTrue(view.value().contains("-s \"zzznosuchthing\""),
                        slug + ": the recovery dropped the argument that failed");
            }
        }
    }

    // -----------------------------------------------------------------------
    // Anchored paths, and locating one segment under a matched prefix
    // -----------------------------------------------------------------------

    @Test
    public void aWildcardNamesEveryBranchItAlsoMatchedRatherThanTakingTheBusiestSilently() {
        // GITHUB-02. `*` matches any child and children are ordered busiest-first, so `repos/*/*` meant "the
        // busiest branch" and returned 420 of 421 under exit 0 with nothing in the header to say so.
        LoadedPackage github = FixtureCorpus.loadedFixture("ballerinax__github");
        String document = client(github, "repos/*/*");
        // Named by where it GOES, not where it forks: `repos/{templateOwner}` alone is not an address.
        Assert.assertTrue(document.contains(
                "| Also matched | `repos/{templateOwner}/{templateRepo}/generate` (1), not included here |"),
                document);
        // ADR-0013. The row names the branch; this says what the row MEANS for the answer under it, and it is
        // printed here rather than in `--help` so it is read at the moment a short answer is on screen.
        String meaning = "short by exactly the paths in the 'Also matched' rows above";
        Assert.assertTrue(document.contains(meaning), document);
        // And only when a branch was actually dropped, or it is noise on every other lookup.
        Assert.assertFalse(client(github, "repos").contains(meaning));
    }

    @Test
    public void aTrailingSegmentIsLocatedUnderTheMatchedPrefixWhenItIsUnambiguous() {
        // `repos/owner/repo/caches` is a real request: `caches` exists, at `.../actions/caches`. The anchored
        // answer — "no `caches` under `repos/{owner}/{repo}`" — is correct and costs a round trip.
        LoadedPackage github = FixtureCorpus.loadedFixture("ballerinax__github");
        String document = client(github, "repos/owner/repo/caches");
        Assert.assertTrue(document.contains(
                "| Located | `repos/{owner}/{repo}/actions/caches` — the only match for that segment under the "
                        + "requested prefix |"), document);
        Assert.assertTrue(document.contains("resource function delete repos/[string owner]/[string repo]"
                + "/actions/caches("), document);
    }

    @Test
    public void aTrailingSegmentFoundInSeveralPlacesIsListedRatherThanPicked() {
        // Rule 2, and it is the whole reason anchoring exists: picking one of several is the failure the anchored
        // walk was built to prevent, so the answer stops at the list.
        LoadedPackage github = FixtureCorpus.loadedFixture("ballerinax__github");
        // `secrets` exists under `actions`, `codespaces` AND `dependabot`, and they are three different APIs.
        String document = client(github, "repos/owner/repo/secrets");
        Assert.assertTrue(document.contains("3 paths carry that segment"), document);
        Assert.assertTrue(document.contains("repos/{owner}/{repo}/actions/secrets"), document);
        Assert.assertTrue(document.contains("repos/{owner}/{repo}/dependabot/secrets"), document);
        // Every path it offers has to be one the tree actually holds, which is asserted exhaustively in
        // ViewsAgreeTest; here the point is that more than one was found and none was chosen.
        Assert.assertFalse(document.contains("| Located |"), document);
    }

    @Test
    public void placeholderSpellingsAllAddressTheSameSegment() {
        // Already shipped and kept: an agent that copied a path out of a fenced signature types `[string owner]`,
        // one that read it off a tree types `{owner}`, and one that typed it from memory types `owner`.
        LoadedPackage github = FixtureCorpus.loadedFixture("ballerinax__github");
        String bare = client(github, "repos/owner/repo");
        // The Path row is the only place the normalisation is visible: it echoes what the TREE holds, not what was
        // typed, so a caller can see which of the three spellings they are now navigating.
        Assert.assertTrue(bare.contains("| Path | `repos/{owner}/{repo}` |"), bare);
        Assert.assertEquals(bodyOf(client(github, "repos/{owner}/{repo}")), bodyOf(bare));
        Assert.assertEquals(bodyOf(client(github, "repos/[string owner]/[string repo]")), bodyOf(bare));
        Assert.assertTrue(client(github, "repos/*/*").contains("| Also matched |"));
    }

    /** A document with the Selector row cut out, which is the one line that echoes what the caller typed. */
    private static String bodyOf(String document) {
        return document.lines().filter(line -> !line.startsWith("| Selector"))
                .collect(java.util.stream.Collectors.joining("\n"));
    }

    private static String client(LoadedPackage loaded, String path) {
        Result<String> view = Containers.render(loaded, Surface.Scope.CLIENT,
                new Containers.Options(List.of("Client", path)));
        Assert.assertTrue(view.isOk(), view.isOk() ? "" : view.failure().describe());
        return view.value();
    }

    // -----------------------------------------------------------------------
    // The map
    // -----------------------------------------------------------------------

    @Test
    public void theMapNamesEveryScopeAndPointsAtTheVerbThatOpensIt() {
        String document = Overview.render(FixtureCorpus.loadedFixture("ballerina__http"));
        // SQL-03's shape: a derived client object counts as a client, or the entry document denies that
        // `ClientObject` exists in a package whose whole point is clients.
        Assert.assertTrue(document.contains("| Clients | 10"), clientsRow(document));
        Assert.assertTrue(document.contains("`bal library client ballerina/http`"), clientsRow(document));
        Assert.assertTrue(document.contains("| Classes | 91"), document);
        Assert.assertTrue(document.contains("| Module functions | 7 — `bal library funcs ballerina/http` |"),
                document);
    }

    @Test
    public void aRosterRowIsNeverADeadEnd() {
        // ADR-0019: `overview`'s old unconditional `ops <pkg> <path>` pointer answered "none in any client" on
        // `ballerinax/aws.s3` and pointed back at `overview`, a two-call loop carrying no information. Every row
        // now ends in the command that opens it, and PointersTest re-runs all of them.
        for (String slug : FixtureCorpus.listFixtures()) {
            String document = Overview.render(FixtureCorpus.loadedFixture(slug));
            // Every bullet in the map, roster row or Next pointer: none of them may be a statement with no
            // command in it, because that is the shape that makes a reader guess the follow-up.
            List<String> rows = document.lines().filter(line -> line.startsWith("- ")).toList();
            Assert.assertFalse(rows.isEmpty(), slug + ": the map offers nothing");
            for (String row : rows) {
                Assert.assertTrue(row.contains("`bal library "), slug + ": a bullet with no command: " + row);
            }
        }
    }

    @Test
    public void aRosterIsCappedAndSaysHowToReachTheRest() {
        // `ballerina/http` declares 91 classes and `ballerinax/postgresql` 126. A roster is read to CHOOSE one,
        // and a list nobody can hold is not a choice — the search line beside it is.
        String document = Overview.render(FixtureCorpus.loadedFixture("ballerinax__postgresql"));
        long rows = document.lines().filter(line -> line.contains(" · `bal library ")).count();
        Assert.assertTrue(rows <= Overview.MAX_ROSTER_ROWS * 3,
                "three scopes, each capped at " + Overview.MAX_ROSTER_ROWS + ": " + rows);
        Assert.assertTrue(document.contains("more, not listed — `bal library class ballerinax/postgresql "
                + "-s \"<what it does>\"`"), document);
    }

    /** The Clients row, for a failure message that shows what was there instead. */
    private static String clientsRow(String document) {
        return document.lines().filter(line -> line.startsWith("| Clients"))
                .findFirst().orElse("(no Clients row)");
    }

    @Test
    public void aPackageWithNoClientsIsANormalCaseNotAnError() {
        // Nothing in the corpus has zero clients, so this is asserted against a library with parts removed.
        LoadedPackage context = FixtureCorpus.loadedFixture("ballerina__http");
        Library noClientsAtAll = context.library()
                .withClients(List.of())
                .withTypeDefs(context.library().typeDefs().stream()
                        .filter(typeDef -> !(typeDef instanceof TypeDef.ObjectDef object
                                && object.role() == TypeDef.ObjectDef.Role.CLIENT))
                        .toList());
        String document = Overview.render(context.withLibrary(noClientsAtAll));
        Assert.assertTrue(document.contains("| Clients | none |"), clientsRow(document));
        // IO-01: the row says what the surface IS rather than naming one half of it. `ballerina/io` has 28 module
        // functions AND 15 classes carrying 67 methods, and the old sentence named only the first.
        Assert.assertTrue(document.contains("| Classes | 91"), document);
        Assert.assertTrue(document.contains("| Module functions | 7"), document);
        Assert.assertFalse(document.contains("\n## Clients"), "no roster for a scope with nothing in it");
    }

    /**
     * Errors are NAMED on the facts row and declared by {@code type}, which is the trade that let the section go.
     *
     * <p>The section was 292 lines of the corpus and the second largest thing in the document. What could not go
     * with it is the NAMES: {@code BucketAlreadyOwnedByYouError} and {@code InvalidRangeError} are not guessable,
     * and a row reading "6, listed below" with nothing below names a fact and withholds the token needed to reach
     * it. So this asserts both halves — the row names them, and the names it prints resolve.
     */
    @Test(dataProvider = "fixtures")
    public void errorsAreNamedOnTheFactsRowAndReadWithType(String slug) {
        LoadedPackage context = FixtureCorpus.loadedFixture(slug);
        String document = Overview.render(context);
        Assert.assertFalse(document.contains("\n## Errors"), slug + ": the section moved to `type`");

        Integer expected = ERROR_COUNTS.get(slug);
        if (expected == null) {
            // PSQL-05: the row no longer ASSERTS where the errors come from. "operations return the
            // language-level `error`" is true of github and false of postgresql (every operation returns
            // `sql:Error`), of sap (`ClientError`) and of every connector that reuses a dependency's hierarchy.
            Assert.assertTrue(document.contains("| Errors | none declared here; each operation names its "
                    + "error type in its `returns` clause |"));
            return;
        }
        String row = document.lines().filter(line -> line.startsWith("| Errors"))
                .findFirst().orElseThrow();
        Assert.assertTrue(row.startsWith("| Errors | " + expected), slug + ": " + row);
        Assert.assertTrue(row.contains("bal library type " + context.qualified().qualified() + " <Name>"),
                slug + ": " + row);

        // Only `ballerina/http`, at 65, is too wide to name them all — and it says so rather than counting
        // silently. Everything else prints every name, and every printed name resolves.
        if (expected > 60) {
            Assert.assertTrue(row.contains("too many to name here"), row);
            return;
        }
        List<String> named = new ArrayList<>();
        Matcher name = Pattern.compile("`([A-Z][A-Za-z0-9_]*)`").matcher(row);
        while (name.find()) {
            named.add(name.group(1));
        }
        Assert.assertEquals(named.size(), expected.intValue(), slug + ": " + row);
        Result<String> read = TypeView.render(context, new TypeView.Options(named, false));
        Assert.assertTrue(read.isOk(), slug + ": " + (read.isOk() ? "" : read.failure().describe()));
    }

    @Test
    public void moduleLevelVariablesGetTheirOwnCountRatherThanTheOtherBucket() {
        // The bucket stage 6a emptied: 61 new declarations landing in "other" would have re-created SHEETS-04 in
        // the same line that fixed it.
        String http = Overview.render(FixtureCorpus.loadedFixture("ballerina__http"));
        Assert.assertTrue(http.contains("61 module-level variables"), http);
        Assert.assertFalse(http.contains(" other),"), "the other bucket has to stay empty");
    }

    // -----------------------------------------------------------------------
    // The cross-kind search
    // -----------------------------------------------------------------------

    @Test
    public void theCrossKindSearchRendersSurfaceMatchesAndOnlyNamesDocumentationOnes() {
        // Both tiers, measured on `ballerinax/github`. `upload` is 7 surface matches against 12 in documentation
        // alone: rendering all 19 would bury the seven the caller came for, and dropping the twelve would lose the
        // vague capability query the flag exists for. `pagination` is the extreme — 0 surface, 14 documentation —
        // so without the second tier that query has no answer at all rather than a short one.
        LoadedPackage github = FixtureCorpus.loadedFixture("ballerinax__github");
        int[] upload = filterCounts(Overview.render(github, new Overview.Options("upload")), "upload");
        Assert.assertTrue(upload[0] > 0, "surface matches are load-bearing: " + upload[0]);
        Assert.assertTrue(upload[1] > upload[0], "so are documentation matches: " + upload[1]);

        String document = Overview.render(github, new Overview.Options("pagination"));
        int[] pagination = filterCounts(document, "pagination");
        Assert.assertEquals(pagination[0], 0, "no name or type mentions pagination");
        Assert.assertTrue(pagination[1] > 0, "and the documentation is the only place it appears");
        Assert.assertTrue(document.contains("matched documentation only"), document);

        // A row carries the kind, the owner and the command, so one call turns "which symbol does X?" into an
        // addressed follow-up.
        Assert.assertTrue(Overview.render(github, new Overview.Options("upload")).contains(" · `bal library "),
                "every row ends in a runnable command");
    }

    /** The two counts on the Filter row: surface matches, then documentation-only ones. */
    private static int[] filterCounts(String document, String query) {
        Matcher filter = Pattern.compile("\\| Filter \\| `" + Pattern.quote(query)
                        + "` — ([\\d,]+) by name or type, ([\\d,]+) more by documentation only \\|")
                .matcher(document);
        Assert.assertTrue(filter.find(), document.split("\n\n")[1]);
        return new int[] {
                Integer.parseInt(filter.group(1).replace(",", "")),
                Integer.parseInt(filter.group(2).replace(",", ""))};
    }

    @Test
    public void aSearchThatMatchesNothingSaysSoAndStillPointsSomewhere() {
        String document = Overview.render(FixtureCorpus.loadedFixture("ballerinax__kafka"),
                new Overview.Options("zzznothingmatchesthis"));
        Assert.assertTrue(document.contains("| Filter | `zzznothingmatchesthis` — 0 by name or type"),
                document);
        Assert.assertTrue(document.contains("## Next"), document);
        Assert.assertTrue(document.contains("bal library overview ballerinax/kafka"), document);
    }

    @Test
    public void searchingAContainerFiltersOnParameterAndTypeNamesTooNotOnlyOnTheName() {
        // The reason `-s` searches the signature rather than the name alone: an agent that knows it holds an
        // `ActionsCacheList` and wants the call returning one has no other way to ask.
        Result<String> view = Containers.render(FixtureCorpus.loadedFixture("ballerinax__github"),
                Surface.Scope.CLIENT, new Containers.Options(List.of(), "ActionsCacheList", false, false));
        Assert.assertTrue(view.isOk(), view.isOk() ? "" : view.failure().describe());
        Assert.assertTrue(view.value().contains("ActionsCacheList"), view.value());
        Assert.assertTrue(view.value().contains("| Filter | `ActionsCacheList` —"), view.value());
    }

    /**
     * {@code -s} matches a PATH, which its own flag description has always claimed and it never did.
     *
     * <p>Measured on a real run: {@code client ballerinax/github Client -s "repos/&#123;owner&#125;/&#123;repo&#125;/issues"}
     * matched nothing on a client declaring 903 resource functions, and the agent fell back to the bare noun —
     * one wasted turn. The same string works perfectly as a POSITIONAL selector, so the failure was purely in
     * the filter.
     *
     * <p>The cause: a resource function's searchable text joins its segments with SPACES, and the query was
     * split on whitespace only, so a {@code /} in the query could never appear in the haystack.
     *
     * <p>Note what this is NOT. The positional selector is anchored and ordered — that is the correctness
     * property ADR's path walk exists for. {@code -s} is a filter, and stays an unordered AND over segments,
     * consistent with how it already treats whitespace-separated words.
     */
    @Test
    public void searchingAContainerMatchesAPathAndNotOnlyAName() {
        LoadedPackage github = FixtureCorpus.loadedFixture("ballerinax__github");
        for (String query : List.of(
                "repos/{owner}/{repo}/issues",
                "repos/owner/repo/issues",
                "actions/caches",
                "issues/comments")) {
            Result<String> view = Containers.render(github, Surface.Scope.CLIENT,
                    new Containers.Options(List.of(), query, false, false));
            Assert.assertTrue(view.isOk(), query + ": " + (view.isOk() ? "" : view.failure().describe()));
            Assert.assertFalse(view.value().contains("| Matched | nothing"),
                    query + " matched nothing:\n" + view.value().split("\n\n")[1]);
            Assert.assertTrue(filterCounts(view.value(), query)[0] > 0, query + ":\n" + view.value());
        }

        // A query that is genuinely absent still reports nothing, or the fix would just be a match-everything.
        Result<String> absent = Containers.render(github, Surface.Scope.CLIENT,
                new Containers.Options(List.of(), "zzznope/alsonope", false, false));
        Assert.assertTrue(absent.value().contains("| Matched | nothing"), absent.value());
    }

    /**
     * The escaped spelling is searchable too, because it is the one the documents print.
     *
     * <p>The same normalisation the positional selector got: a caller who copies
     * {@code chat\.postMessage} out of a fenced signature and puts it behind {@code -s} is copying this tool's
     * own output.
     */
    @Test
    public void searchingAContainerAcceptsTheEscapedSpelling() {
        LoadedPackage slack = FixtureCorpus.loadedFixture("ballerinax__slack");
        for (String query : List.of("chat\\.postMessage", "chat.postMessage")) {
            Result<String> view = Containers.render(slack, Surface.Scope.CLIENT,
                    new Containers.Options(List.of(), query, false, false));
            Assert.assertTrue(view.isOk(), query + ": " + (view.isOk() ? "" : view.failure().describe()));
            Assert.assertTrue(view.value().contains("chat\\.postMessage"),
                    query + " did not find it:\n" + view.value());
        }
    }

    /**
     * A path parameter answers to brackets WITHOUT the type, which is what half-remembering produces.
     *
     * <p>Measured: an agent typed {@code repos/[owner]/[repo]/issues} — the declaration form
     * {@code [string owner]} with the type dropped — and matched nothing on 903 resource functions. Three
     * spellings resolved and this fourth did not.
     *
     * <p>It was not merely unhandled, it was mis-parsed: the old pattern required something before the name, so
     * on {@code [owner]} it backtracked and read the parameter's name as {@code r}.
     */
    @Test
    public void aPathParameterAnswersToBracketsWithoutTheType() {
        LoadedPackage github = FixtureCorpus.loadedFixture("ballerinax__github");
        for (String path : List.of(
                "repos/[owner]/[repo]/issues",
                "repos/[string owner]/[string repo]/issues",
                "repos/{owner}/{repo}/issues",
                "repos/owner/repo/issues")) {
            Result<String> view = Containers.render(github, Surface.Scope.CLIENT,
                    new Containers.Options(List.of("Client", path)));
            Assert.assertTrue(view.isOk(), path + ": " + (view.isOk() ? "" : view.failure().describe()));
            Assert.assertFalse(view.value().contains("| Matched | nothing"),
                    path + " did not resolve:\n" + view.value().split("\n\n")[1]);
        }
    }

    /**
     * An accessor and a path in ONE argument reach the path walk, whatever spelling the path is in.
     *
     * <p>A caller copying a line out of a fenced signature copies the whole thing —
     * {@code get [PathParamType ...path]} — as a single quoted argument. Split across two arguments every
     * spelling already resolved; as one token only the display spelling did, because a one-token selector was
     * compared against an entry's LABEL and never handed to the walk that understands the other spellings.
     *
     * <p>So the one-token form now splits on its first word when that word is an accessor the container
     * declares. Member names cannot contain whitespace, which is what makes the split safe.
     */
    @Test
    public void anAccessorAndAPathInOneArgumentReachThePathWalk() {
        LoadedPackage http = FixtureCorpus.loadedFixture("ballerina__http");
        for (String selector : List.of(
                "get [PathParamType ...path]",
                "get [path]",
                "get {...path}")) {
            Result<String> view = Containers.render(http, Surface.Scope.CLIENT,
                    new Containers.Options(List.of("Client", selector)));
            Assert.assertTrue(view.isOk(), selector + ": " + (view.isOk() ? "" : view.failure().describe()));
            Assert.assertFalse(view.value().contains("| Matched | nothing"),
                    selector + " did not resolve:\n" + view.value().split("\n\n")[1]);
        }

        // A leading word that is NOT an accessor stays one member name, or `Producer send` would be parsed as
        // an accessor called `Producer`.
        Result<String> member = Containers.render(FixtureCorpus.loadedFixture("ballerinax__kafka"),
                Surface.Scope.CLIENT, new Containers.Options(List.of("Producer", "send")));
        Assert.assertTrue(member.value().contains("function send("), member.value());
    }

    // -----------------------------------------------------------------------
    // The guide
    // -----------------------------------------------------------------------

    @Test
    public void theGuideHasItsOwnVerbAndTheMapCarriesOnlyTheQuickstart() {
        // `ballerinax/postgresql` is the pathological case the split exists for: 563 lines of guide inside a
        // 619-line entry document, and genuinely useful — a long-form usage manual, not marketing. Deleting it
        // would have been wrong and carrying it made every other fact in the document unreachable behind a pipe.
        LoadedPackage context = FixtureCorpus.loadedFixture("ballerinax__postgresql");
        String overview = Overview.render(context);
        Assert.assertFalse(overview.contains("\n## Guide"), "the guide is not in the entry document");
        // ADR-0024 put the quotation LAST. Under ADR-0017 it sat before `## Next` because it was capped at
        // forty lines; uncapped, the only place an unbounded section can sit without pushing the navigation
        // behind a pipe is the end.
        Assert.assertTrue(overview.indexOf("\n## Next\n") < overview.indexOf("\n## Quickstart\n"));
        Assert.assertTrue(overview.contains("dbClient->execute"), overview);
        Assert.assertTrue(overview.contains("`bal library guide ballerinax/postgresql`"), overview);

        Result<String> guide = Guide.render(context, Guide.Options.ALL);
        Assert.assertTrue(guide.isOk(), guide.isOk() ? "" : guide.failure().describe());
        // Asserted as PROSE and not as a size ratio. It used to be `guide > overview * 3`, which stopped holding
        // when ADR-0024 moved all 28 of postgresql's code blocks into the map — the map is now 13KB of a 24KB
        // readme, and almost all of the difference is prose. That is the split working, not failing: the code is
        // in both because the reader needs it in both, and the manual around it has its own verb.
        Assert.assertTrue(guide.value().length() > overview.length(),
                "guide " + guide.value().length() + " vs overview " + overview.length());
        Assert.assertTrue(guide.value().contains("######"), "the guide carries the readme's own sections");
        Assert.assertFalse(overview.contains("######"), "and the map carries none of them");
        // ADR-0013. The rule that a readme can be stale where the signature cannot was a line of `--help` prose.
        // It is the sentence introducing the readme, so it is read where it applies.
        Assert.assertTrue(guide.value().contains("Where the two disagree, the signature is what compiles."),
                "the readme is introduced by the rule for reading it");
    }

    @Test
    public void aGuideChunkIsASectionWithItsProseAndIsAddressableTwoWays() {
        // A code-only extract would have discarded about 85% of `googleapis.sheets`' 178-line readme — including
        // "if you intend to use deleteSpreadsheet you must also enable the Google Drive API", which is not
        // inferable from any signature and is the difference between a connector that works and one that 403s.
        LoadedPackage sheets = FixtureCorpus.loadedFixture("ballerinax__googleapis.sheets");
        List<Guide.Chunk> chunks = Guide.chunksOf(sheets);
        Assert.assertFalse(chunks.isEmpty(), "sheets' readme carries code in several sections");

        Result<String> byNumber = Guide.render(sheets, new Guide.Options("1", null, null));
        Assert.assertTrue(byNumber.isOk(), byNumber.isOk() ? "" : byNumber.failure().describe());
        Assert.assertTrue(byNumber.value().contains("| Chunk | 1 of "), byNumber.value());

        Result<String> byTitle = Guide.render(
                sheets, new Guide.Options(chunks.get(0).title(), null, null));
        Assert.assertTrue(byTitle.isOk(), byTitle.isOk() ? "" : byTitle.failure().describe());
        Assert.assertEquals(byTitle.value(), byNumber.value(), "a title and its number are the same chunk");

        // And the map advertises the index, so an agent knows a recipe exists before paying for the document.
        Assert.assertTrue(Overview.render(sheets).contains("Guide chunks ("), Overview.render(sheets));
    }

    @Test
    public void aChunkThatDoesNotExistNamesEveryChunkThatDoes() {
        Result<String> view = Guide.render(FixtureCorpus.loadedFixture("ballerinax__googleapis.sheets"),
                new Guide.Options("999", null, null));
        Assert.assertFalse(view.isOk());
        Assert.assertTrue(view.failure() instanceof Failure.SymbolNotFound);
        Assert.assertTrue(view.failure().describe().contains("1. "), view.failure().describe());
    }

    @Test
    public void aModuleThatPublishesNoGuideIsAFailureThatNamesTheOnesThatDo() {
        // All-or-nothing, like `type`: a `--module` typo answering with every module's readme would be a partial
        // answer under exit 0, which is the silent class this CLI refuses everywhere.
        Result<String> view = Guide.render(
                FixtureCorpus.loadedFixture("ballerinax__kafka"), new Guide.Options("kafkaa"));
        Assert.assertFalse(view.isOk());
        Assert.assertTrue(view.failure().describe().contains("kafka"), view.failure().describe());
    }

    // -----------------------------------------------------------------------
    // Quoted code
    // -----------------------------------------------------------------------

    @Test
    public void everyBallerinaBlockInTheReadmeIsQuotedAndNothingElseIs() {
        // ADR-0024. The fence is the whole rule, and these are the two packages that killed the classifier it
        // replaced. `ballerina/log` publishes eight worked blocks and none of them constructs a client, calls one
        // with `->` or attaches a service, so the old rule reached the reader with zero examples for a package
        // whose entire surface is module-level functions.
        String log = Overview.render(FixtureCorpus.loadedFixture("ballerina__log"));
        Assert.assertTrue(log.contains("\n## Quickstart\n"), log);
        Assert.assertTrue(log.contains("log:printInfo("), log);

        // xlsx is the sharper case: the old rule kept exactly one of its thirteen blocks, and kept it because of
        // `sftp->get` — another package's client — while dropping the `@xlsx:Name` header mapping and every
        // `xlsx:parseSheet` call as demonstrating nothing.
        String xlsx = Overview.render(FixtureCorpus.loadedFixture("ballerina__xlsx"));
        String quoted = xlsx.substring(xlsx.indexOf("\n## Quickstart\n"));
        Assert.assertTrue(quoted.contains("@xlsx:Name {value: \"Employee Name\"}"), quoted);
        Assert.assertTrue(quoted.contains("xlsx:parseSheet("), quoted);

        // Order is the readme's, not a role order: a readme is written basic-first, and the reader is walking it.
        // Read off the section and not the document, because the chunk index names `@xlsx:Name` in its title
        // list and now precedes the quotation.
        Assert.assertTrue(quoted.indexOf("xlsx:parseSheet(") < quoted.indexOf("@xlsx:Name"), quoted);

        // Nothing else is quoted. slack's fourth block is a `bal run` transcript under a `bash` fence.
        String slack = Overview.render(FixtureCorpus.loadedFixture("ballerinax__slack"));
        String quickstart = slack.substring(slack.indexOf("\n## Quickstart\n"));
        Assert.assertFalse(quickstart.contains("bal run"), quickstart);
        // Including the import-only block, which the old rule dropped and this one keeps: it is Ballerina, and
        // the module alias a package is imported under is a fact the reader needs.
        Assert.assertTrue(quickstart.contains("import ballerinax/slack;"), quickstart);
    }

    @Test(dataProvider = "fixtures")
    public void aQuotedBlockIsNeverEditedTruncatedOrAnnotated(String slug) {
        String document = Overview.render(FixtureCorpus.loadedFixture(slug));
        if (!document.contains("\n## Quickstart\n")) {
            return;
        }
        String quickstart = document.substring(document.indexOf("\n## Quickstart\n"));
        // No truncation marker, and no mark on a line. ADR-0024 removed the name check: `overview` quotes the
        // package's bytes, and a reader who is told the generated signatures win does not also need the tool
        // arguing with the readme inside the quotation.
        Assert.assertFalse(quickstart.contains("\u2026 "), slug + ": a quotation was cut");
        // The MARK this tool used to emit, not the character: a package whose own readme code contains a
        // "\u26a0" is quoting its own text, and banning the character outright would fail the tool for the
        // package's content — the exact confusion between our output and theirs that ADR-0024 removed.
        Assert.assertFalse(quickstart.contains("# \u26a0 `"), slug + ": a quotation was annotated");

        // Every block the readme wrote arrived. Counted against `guide`, which reproduces the readme verbatim,
        // rather than sampled — because the failure this replaces was SILENT: an ineligible block never reached
        // the omitted counter, so `overview` dropped ten of xlsx's thirteen blocks while claiming it had left
        // nothing behind. Two documents of one tool disagreeing about how much code a package published is the
        // bug, so the assertion is that they agree.
        Result<String> guide = Guide.render(FixtureCorpus.loadedFixture(slug), Guide.Options.ALL);
        Assert.assertTrue(guide.isOk(), slug + ": " + (guide.isOk() ? "" : guide.failure().describe()));
        // Stripped, because `guide` reproduces the readme's own indentation and `ballerinax/postgresql`
        // indents most of its fences four spaces under a list item.
        long inReadme = guide.value().lines()
                .map(String::strip)
                .filter(line -> line.equals("```ballerina") || line.equals("```bal"))
                .count();
        long quoted = quickstart.lines().filter(line -> line.equals("```ballerina")).count();
        Assert.assertEquals(quoted, inReadme, slug + ": the readme's block count is not the quoted count");
    }

    // -----------------------------------------------------------------------
    // The code register
    // -----------------------------------------------------------------------

    @Test
    public void theSubtypeChainIsWhatTheErrorDeclarationsAreFor() {
        LoadedPackage http = FixtureCorpus.loadedFixture("ballerina__http");
        Result<String> view = TypeView.render(http, new TypeView.Options(
                List.of("Error", "ClientRequestError", "SslError"), false));
        Assert.assertTrue(view.isOk(), view.isOk() ? "" : view.failure().describe());
        String document = view.value();
        // Unlearnable before the detail patch: all 56 rendered as `type X error;`.
        Assert.assertTrue(document.contains(
                "\npublic type ClientRequestError distinct (ApplicationResponseError & error<Detail>);\n"));
        Assert.assertTrue(document.contains("\npublic type SslError distinct ClientError;\n"));
        Assert.assertTrue(document.contains("\npublic type Error distinct error;\n"));
        // ADR-0013. The rule came here with the declarations, from an `overview` section that no longer exists;
        // without the move it would simply have been deleted.
        Assert.assertTrue(document.contains("// The subtype chain is what `is` tests against"), document);
        // And it is not printed for a lookup that resolved no error, or it is noise on every other one.
        Result<String> plain = TypeView.render(http, new TypeView.Options(List.of("Response"), false));
        Assert.assertTrue(plain.isOk());
        Assert.assertFalse(plain.value().contains("The subtype chain"), plain.value());
    }

    @Test
    public void typeSearchesTheRosterAndNamesWhatItWillNotPrint() {
        // A bare `type <pkg>` must not become a second `api`, so it needs a name or a query. With a query the
        // code register's own two-tier rule applies: surface matches are declarations, documentation-only ones are
        // a `//` line of names, and over budget nothing is rendered and every match is named — which keeps "exit 0
        // means stdout is complete" true where a truncated set of records would not.
        Result<String> narrow = TypeView.render(FixtureCorpus.loadedFixture("ballerinax__kafka"),
                new TypeView.Options(List.of(), "TopicPartition", false));
        Assert.assertTrue(narrow.isOk(), narrow.isOk() ? "" : narrow.failure().describe());
        Assert.assertTrue(narrow.value().contains("// Search: \"TopicPartition\" —"), narrow.value());
        Assert.assertTrue(narrow.value().contains("public type TopicPartition record"), narrow.value());

        Result<String> wide = TypeView.render(FixtureCorpus.loadedFixture("ballerinax__github"),
                new TypeView.Options(List.of(), "repo", false));
        Assert.assertTrue(wide.isOk(), wide.isOk() ? "" : wide.failure().describe());
        Assert.assertTrue(wide.value().contains("over the " + Texts.count(TypeView.MAX_SEARCH_BYTES)
                + "-byte budget"), wide.value());
        Assert.assertTrue(wide.value().contains("// Matched: "), wide.value());
        Assert.assertFalse(wide.value().contains("public type "), "nothing is rendered over budget");
    }

    @Test
    public void aFooterNamesACollisionRatherThanClaimingTheLocalNameIsForeign() {
        // SHEETS-03. `ProxyConfig` sat in a list headed "not included above" in an output that declares a
        // `ProxyConfig` twelve lines earlier — two records, same name, same arity, different fields. The foreign
        // entry has to stay, because the field line needs that import; what was missing is which is which. sheets
        // has TWO such names, not the one the audit found.
        Result<String> view = TypeView.render(
                FixtureCorpus.loadedFixture("ballerinax__googleapis.sheets"),
                new TypeView.Options(List.of("ConnectionConfig"), true));
        Assert.assertTrue(view.isOk());
        Assert.assertTrue(view.value().contains("public type ProxyConfig record {|"), view.value());
        Assert.assertTrue(view.value().contains(
                "//   note: OAuth2RefreshTokenGrantConfig, ProxyConfig above are this package's own "
                        + "declarations of those names, not http's"), view.value());
    }

    @Test
    public void aClientIsAddressableByNameLikeAnyOtherDeclaration() {
        // SAP-09. `type ballerinax/sap Client` failed, asserting the package had no such declaration, and steered
        // the reader to `ClientError` — in a package where the client is 1 of 4 things Central publishes. The name
        // index was built from TypeDefs, and a client was not one.
        LoadedPackage sap = FixtureCorpus.loadedFixture("ballerinax__sap");
        Result<String> view = TypeView.render(sap, new TypeView.Options(List.of("Client"), false));
        Assert.assertTrue(view.isOk(), view.isOk() ? "" : view.failure().describe());
        Assert.assertTrue(view.value().contains("public isolated client class Client {"), view.value());
        // Byte-identical to the same declaration inside the api document, which is the whole `type` contract — and
        // the reason the separate client renderer is gone rather than kept in step by hand.
        String api = FixtureCorpus.renderFixture("ballerinax__sap");
        String declaration = view.value().substring(view.value().indexOf("# The `sap`"));
        Assert.assertTrue(api.contains(declaration.strip()), declaration);
    }

    @Test
    public void addressableAddsClientsAndDeclarationsDoesNot() {
        // The two lists answer different questions and one cannot: the map counts declarations to describe the
        // type surface and names clients on their own row, so folding them in would double-count them.
        Library sap = FixtureCorpus.libraryFor("ballerinax__sap");
        Assert.assertEquals(sap.clients().size(), 1);
        Assert.assertEquals(sap.addressable().size(), sap.declarations().size() + 1);
        Assert.assertFalse(sap.declarations().stream().anyMatch(one -> "Client".equals(one.name())));
        Assert.assertTrue(sap.addressable().stream().anyMatch(one -> "Client".equals(one.name())));
    }

    @Test
    public void configurablesAreNotInTheMapAndAreStillInTheApiDocument() {
        // A `configurable` is what a DEPLOYMENT sets in Config.toml, and it is module-private:
        // `http:maxActiveConnections` from another module is `attempt to refer to non-accessible symbol`,
        // measured. So it is disqualified for the reader the map is for — an agent writing a .bal file cannot
        // reference any of the thirteen — and it appears in exactly one package of eleven.
        String http = Overview.render(FixtureCorpus.loadedFixture("ballerina__http"));
        Assert.assertFalse(http.contains("## Configurables"), "not in the document an agent codes from");

        // The fact is not lost, only made expensive: `api` is the register that carries it, as comments rather
        // than as declarations. Unlike the errors, `type` cannot reach it — a configurable is not a declaration to
        // resolve — so without the `api` section the cut would have DELETED the fact rather than moved it.
        String api = FixtureCorpus.renderFixture("ballerina__http");
        Assert.assertTrue(api.contains("\n// --- Configurables ---\n"), "api carries them");
        Assert.assertTrue(api.contains("// maxActiveConnections = -1    # int"), "with its default and type");
        Assert.assertTrue(api.contains("[ballerina.http]"), "and the Config.toml table name");
        Assert.assertFalse(FixtureCorpus.renderFixture("ballerinax__slack").contains("--- Configurables ---"));
        Result<String> byName = TypeView.render(FixtureCorpus.loadedFixture("ballerina__http"),
                new TypeView.Options(List.of("maxActiveConnections"), false));
        Assert.assertFalse(byName.isOk(), "a configurable is not a declaration `type` can resolve");
        // Eight of http's parameter defaults name a configurable, and their "not exported by this package" note is
        // TRUE and now unqualified by anything in the map.
        Assert.assertTrue(FixtureCorpus.readSnapshot("ballerina__http")
                .contains("not exported by this package"), "the note stands");
    }

    /** Sanity: the corpus still has the path shapes these tests reason about. */
    @Test
    public void githubStillHasTheTreeTheseTestsAssumeAbout() {
        PathTree tree = PathTree.build(
                Surface.byName(Surface.of(FixtureCorpus.libraryFor("ballerinax__github"),
                        Surface.Scope.CLIENT), "Client").orElseThrow().operations());
        Assert.assertEquals(tree.total(), 903);
    }
}
