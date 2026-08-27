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

import com.google.gson.JsonElement;
import com.google.gson.JsonObject;
import io.ballerina.library.central.CentralClient;
import io.ballerina.library.central.DependenciesToml;
import io.ballerina.library.central.HttpOptions;
import io.ballerina.library.central.HttpTransport;
import io.ballerina.library.central.SearchHit;
import io.ballerina.library.central.schema.CentralDocs;
import io.ballerina.library.central.schema.Schema;
import org.testng.Assert;
import org.testng.annotations.Test;

import java.util.List;
import java.util.Map;

/**
 * The boundary, driven without a network: which failures are worth retrying, which are answers, and what each one
 * costs the caller.
 *
 * @since 0.1.0
 */
public class ClientTest {

    private static final QualifiedName GITHUB = QualifiedName.parse("ballerinax/github").value();

    /** Fast enough that the retry path costs a test run nothing. */
    private static HttpOptions.Builder fast(HttpTransport transport) {
        return HttpOptions.builder()
                .transport(transport)
                .maxAttempts(3)
                .baseDelayMs(1)
                .budgetMs(5_000)
                .timeoutMs(1_000)
                .sleeper(millis -> {
                    // The backoff is asserted by `backoffMs` directly; sleeping here only slows the suite.
                });
    }

    // -----------------------------------------------------------------------
    // Retries
    // -----------------------------------------------------------------------

    @Test
    public void a503IsRetriedAndTheRetrysAnswerIsUsed() {
        FakeTransport transport = FakeTransport.scripted(List.of(
                FakeTransport.status(503), FakeTransport.ok("{\"hello\":\"world\"}")));
        Result<JsonElement> result =
                CentralClient.fetchJson("https://example.test/x", fast(transport).build());
        Assert.assertTrue(result.isOk());
        Assert.assertEquals(result.value().getAsJsonObject().get("hello").getAsString(), "world");
        Assert.assertEquals(transport.calls(), 2);
    }

    @Test
    public void retriesStopAtMaxAttemptsAndReportHowManyWereSpent() {
        FakeTransport transport = FakeTransport.scripted(List.of(
                FakeTransport.status(502), FakeTransport.status(502), FakeTransport.status(502)));
        Result<JsonElement> result =
                CentralClient.fetchJson("https://example.test/x", fast(transport).build());
        Assert.assertFalse(result.isOk());
        Failure.Upstream failure = (Failure.Upstream) result.failure();
        Assert.assertEquals(failure.attempts(), 3);
        Assert.assertEquals(transport.calls(), 3);
    }

    @Test
    public void a404IsAnAnswerNotAHiccupSoItIsNeverRetried() {
        FakeTransport transport = FakeTransport.always(FakeTransport.status(404));
        Result<JsonElement> result =
                CentralClient.fetchJson("https://example.test/x", fast(transport).build());
        Assert.assertFalse(result.isOk());
        Assert.assertTrue(result.failure() instanceof Failure.Upstream);
        Assert.assertEquals(transport.calls(), 1);
    }

    @Test
    public void aBodyThatIsNotJsonIsNotRetriedEither() {
        FakeTransport transport =
                FakeTransport.always(FakeTransport.ok("<html>maintenance</html>"));
        Result<JsonElement> result =
                CentralClient.fetchJson("https://example.test/x", fast(transport).build());
        Assert.assertFalse(result.isOk());
        Assert.assertEquals(transport.calls(), 1);
    }

    @Test
    public void aRequestThatNeverAnswersBecomesATimeoutNotAHang() {
        FakeTransport transport = FakeTransport.always(new HttpTransport.Reply.TimedOut());
        Result<JsonElement> result = CentralClient.fetchJson(
                "https://example.test/slow", fast(transport).maxAttempts(1).build());
        Assert.assertFalse(result.isOk());
        Assert.assertTrue(result.failure() instanceof Failure.Timeout);
    }

    @Test
    public void retryAfterIsHonouredInBothOfItsLegalForms() {
        long now = 1_700_000_000_000L;
        Assert.assertEquals(CentralClient.parseRetryAfter("120", now), 120_000);
        Assert.assertEquals(CentralClient.parseRetryAfter("  30  ", now), 30_000);
        // The date form, one minute into the future from `now`.
        String date = java.time.format.DateTimeFormatter.RFC_1123_DATE_TIME.format(
                java.time.Instant.ofEpochMilli(now + 60_000).atZone(java.time.ZoneOffset.UTC));
        Assert.assertEquals(CentralClient.parseRetryAfter(date, now), 60_000);
        // A date already past is zero rather than negative, so it cannot rewind the deadline.
        String past = java.time.format.DateTimeFormatter.RFC_1123_DATE_TIME.format(
                java.time.Instant.ofEpochMilli(now - 60_000).atZone(java.time.ZoneOffset.UTC));
        Assert.assertEquals(CentralClient.parseRetryAfter(past, now), 0);
        Assert.assertEquals(CentralClient.parseRetryAfter(null, now), -1);
        Assert.assertEquals(CentralClient.parseRetryAfter("not a delay", now), -1);
    }

    @Test
    public void backoffGrowsExponentiallyAndStaysInsideItsJitterBand() {
        // The jitter exists so parallel callers do not resonate; the band is what keeps it bounded.
        for (int attempt = 0; attempt < 4; attempt++) {
            long floor = CentralClient.backoffMs(attempt, 200, 0.0);
            long ceiling = CentralClient.backoffMs(attempt, 200, 1.0);
            Assert.assertEquals(floor, (long) (200 * Math.pow(2, attempt)));
            Assert.assertEquals(ceiling, (long) (200 * Math.pow(2, attempt) * 1.25));
        }
    }

    // -----------------------------------------------------------------------
    // Version resolution
    // -----------------------------------------------------------------------

    /**
     * A version a PROJECT locked that Central does not publish.
     *
     * <p>T10, and the only reachable "supplied version" path now that there is no {@code --version} flag: the
     * version came out of a {@code Dependencies.toml}, so "omit the version to take the latest" names a step the
     * caller cannot take. The failure lists what IS published instead, which is why no {@code versions} verb is
     * needed — and the list is fetched on a path that has already failed, so it degrades to no list rather than to
     * a different failure.
     */
    @Test
    public void aVersionTheProjectLockedAndCentralDoesNotPublishNamesTheOnesItDoes() {
        FakeTransport transport = FakeTransport.routing(url -> url.contains("/docs/")
                ? FakeTransport.status(404)
                : FakeTransport.ok("[\"6.0.0\", \"5.1.0\"]"));
        Result<CentralDocs> result = CentralClient.fetchDocs(
                GITHUB, supplied("9.9.9"), fast(transport).build());
        Assert.assertFalse(result.isOk());
        Failure.PackageNotFound failure = (Failure.PackageNotFound) result.failure();
        Assert.assertEquals(failure.qualified(), "ballerinax/github:9.9.9");
        Assert.assertTrue(failure.suggestion().contains("Your project locks"), failure.suggestion());
        Assert.assertTrue(failure.suggestion().contains("published versions are 6.0.0, 5.1.0"),
                failure.suggestion());
        Assert.assertFalse(failure.suggestion().contains("omit the version"),
                "there is no version argument to omit");
    }

    @Test
    public void aRegistryThatCannotListVersionsStillReportsTheFailureItWasAsked() {
        // Best-effort, and it has to stay that way: a second failure while composing a message must not replace
        // the failure the caller actually hit.
        FakeTransport transport = FakeTransport.always(FakeTransport.status(500));
        Result<CentralDocs> result = CentralClient.fetchDocs(
                GITHUB, supplied("9.9.9"), fast(transport).build());
        Assert.assertFalse(result.isOk());
        Assert.assertTrue(result.failure() instanceof Failure.Upstream, result.failure().describe());
    }

    @Test
    public void aNotFoundOnAVersionTheReaderResolvedBlamesTheNameRatherThanTheVersion() {
        // The reader resolved the version, so "omit the version" would name a step the caller never took. It is
        // reachable through the module walk: a parent that exists supplies a version for a module that does not,
        // and then only the name can be wrong.
        FakeTransport transport = FakeTransport.always(FakeTransport.status(404));
        Result<CentralDocs> result = CentralClient.fetchDocs(
                GITHUB, resolved("6.0.0"), fast(transport).build());
        Assert.assertFalse(result.isOk());
        Failure.PackageNotFound failure = (Failure.PackageNotFound) result.failure();
        Assert.assertFalse(failure.suggestion().contains("omit the version"), failure.suggestion());
        Assert.assertTrue(failure.suggestion().contains("Check the name"), failure.suggestion());
    }

    /** A version the caller pinned. */
    private static CentralClient.ResolvedVersion supplied(String version) {
        return new CentralClient.ResolvedVersion(Version.parse(version).value(), false, true);
    }

    /** A version the reader resolved on the caller's behalf. */
    private static CentralClient.ResolvedVersion resolved(String version) {
        return new CentralClient.ResolvedVersion(Version.parse(version).value(), false, false);
    }

    @Test
    public void theNewestVersionIsTheFirstEntryCentralReturns() {
        FakeTransport transport =
                FakeTransport.always(FakeTransport.ok("[\"6.0.0\",\"5.4.1\",\"5.4.0\"]"));
        Result<CentralClient.ResolvedVersion> result =
                CentralClient.resolveLatestVersion(GITHUB, fast(transport).build());
        Assert.assertTrue(result.isOk());
        Assert.assertEquals(result.value().version().text(), "6.0.0");
        Assert.assertFalse(result.value().stale());
    }

    @Test
    public void centrals400ForAnUnpublishedNameReadsAsNoSuchPackage() {
        // The most common caller mistake is a typo, and Central reports it as a 400.
        FakeTransport transport = FakeTransport.always(FakeTransport.status(400));
        Result<CentralClient.ResolvedVersion> result =
                CentralClient.resolveLatestVersion(GITHUB, fast(transport).build());
        Assert.assertFalse(result.isOk());
        Assert.assertTrue(result.failure() instanceof Failure.PackageNotFound);
        Assert.assertEquals(transport.calls(), 1);
    }

    @Test
    public void anEmptyVersionListMeansThePackageDoesNotExist() {
        FakeTransport transport = FakeTransport.always(FakeTransport.ok("[]"));
        Result<CentralClient.ResolvedVersion> result =
                CentralClient.resolveLatestVersion(GITHUB, fast(transport).build());
        Assert.assertFalse(result.isOk());
        Assert.assertTrue(result.failure() instanceof Failure.PackageNotFound);
    }

    @Test
    public void theVersionCentralReportsGoesThroughTheParserRatherThanBeingTrusted() {
        // It becomes a cache path segment, and `..` satisfies Central's own format.
        FakeTransport transport = FakeTransport.always(FakeTransport.ok("[\"..\"]"));
        Result<CentralClient.ResolvedVersion> result =
                CentralClient.resolveLatestVersion(GITHUB, fast(transport).maxAttempts(1).build());
        Assert.assertFalse(result.isOk());
        Assert.assertTrue(result.failure() instanceof Failure.Validation);
    }

    // -----------------------------------------------------------------------
    // Modules of a package
    //
    // The registry lists packages, so a module has no row of its own. These pin the walk that finds its
    // version through the package containing it — without which `ballerinax/aws.auth`, the type of the only
    // required field of `ballerinax/aws.s3`'s constructor, is unreadable unless the caller already knows a
    // version to pin.
    // -----------------------------------------------------------------------

    private static final QualifiedName AWS_AUTH = QualifiedName.parse("ballerinax/aws.auth").value();

    /** Routes the registry by coordinate, which is the only thing these tests need to tell calls apart. */
    private static FakeTransport registry(Map<String, String> versionsByCoordinate) {
        return FakeTransport.routing(url -> {
            for (Map.Entry<String, String> entry : versionsByCoordinate.entrySet()) {
                if (url.endsWith("/registry/packages/" + entry.getKey())) {
                    return FakeTransport.ok(entry.getValue());
                }
            }
            return FakeTransport.status(404);
        });
    }

    @Test
    public void aModuleResolvesAtTheVersionOfThePackageContainingIt() {
        FakeTransport transport = registry(Map.of("ballerinax/aws", "[\"1.0.1\",\"1.0.0\"]"));
        Result<CentralClient.ResolvedVersion> result =
                CentralClient.resolveLatestVersion(AWS_AUTH, fast(transport).build());
        Assert.assertTrue(result.isOk());
        Assert.assertEquals(result.value().version().text(), "1.0.1");
        // Two calls: the module's own row, which does not exist, then the package's.
        Assert.assertEquals(transport.calls(), 2);
    }

    @Test
    public void aDottedNameThatIsItsOwnPackageNeverProbesAParent() {
        // The common case by a wide margin — `googleapis.sheets`, `googleapis.gmail`, `aws.s3` are all
        // packages. Falling back on a hit would put a second round trip on every one of them.
        QualifiedName sheets = QualifiedName.parse("ballerinax/googleapis.sheets").value();
        FakeTransport transport = registry(Map.of("ballerinax/googleapis.sheets", "[\"5.0.0\"]"));
        Result<CentralClient.ResolvedVersion> result =
                CentralClient.resolveLatestVersion(sheets, fast(transport).build());
        Assert.assertTrue(result.isOk());
        Assert.assertEquals(result.value().version().text(), "5.0.0");
        Assert.assertEquals(transport.calls(), 1);
    }

    @Test
    public void theWalkTriesEachShorterPrefixInTurn() {
        // `a.b.c` is a module of `a.b` when that exists and of `a` when it does not; both are legal, so the
        // walk cannot stop at the first prefix.
        QualifiedName deep = QualifiedName.parse("ballerina/one.two.three").value();
        FakeTransport transport = registry(Map.of("ballerina/one", "[\"3.1.0\"]"));
        Result<CentralClient.ResolvedVersion> result =
                CentralClient.resolveLatestVersion(deep, fast(transport).build());
        Assert.assertTrue(result.isOk());
        Assert.assertEquals(result.value().version().text(), "3.1.0");
        Assert.assertEquals(transport.calls(), 3);
    }

    @Test
    public void aNameThatIsNeitherAPackageNorAModuleSaysWhatWasTried() {
        // The caller's next move differs by which half is misspelled, and after the walk the reader knows.
        FakeTransport transport = registry(Map.of());
        Result<CentralClient.ResolvedVersion> result =
                CentralClient.resolveLatestVersion(AWS_AUTH, fast(transport).build());
        Assert.assertFalse(result.isOk());
        Failure.PackageNotFound failure = (Failure.PackageNotFound) result.failure();
        Assert.assertEquals(failure.qualified(), "ballerinax/aws.auth");
        Assert.assertTrue(failure.suggestion().contains("tried ballerinax/aws"), failure.suggestion());
    }

    @Test
    public void aModuleWhoseParentIsUnreachableDoesNotReportTheParentsFailure() {
        // A 500 on the module's own row is a transport fact, not "no such package", and must not be converted
        // into one by a walk that never should have started.
        FakeTransport transport = FakeTransport.always(FakeTransport.status(500));
        Result<CentralClient.ResolvedVersion> result =
                CentralClient.resolveLatestVersion(AWS_AUTH, fast(transport).maxAttempts(1).build());
        Assert.assertFalse(result.isOk());
        Assert.assertTrue(result.failure() instanceof Failure.Upstream, result.failure().getClass().getName());
    }

    // -----------------------------------------------------------------------
    // Suggestions
    // -----------------------------------------------------------------------

    @Test
    public void everyFailureTheOutsideWorldCanCauseCarriesASuggestion() {
        // Three of them used to omit it — the three that fire during a Central outage, when the reader has
        // nothing else to offer.
        Result<JsonElement> timeout = CentralClient.fetchJson("https://example.invalid/x",
                fast(FakeTransport.always(new HttpTransport.Reply.TimedOut())).maxAttempts(1).build());
        Assert.assertTrue(timeout.failure() instanceof Failure.Timeout);
        Assert.assertFalse(((Failure.Timeout) timeout.failure()).suggestion().isEmpty());

        Result<JsonElement> upstream = CentralClient.fetchJson("https://example.invalid/x",
                fast(FakeTransport.always(FakeTransport.status(500))).maxAttempts(1).build());
        Assert.assertTrue(upstream.failure() instanceof Failure.Upstream);
        Assert.assertFalse(((Failure.Upstream) upstream.failure()).suggestion().isEmpty());

        Result<CentralDocs> drift = Schema.parse(
                com.google.gson.JsonParser.parseString("{\"docsData\":{\"modules\":[]}}"), "x/y:1.0.0");
        Assert.assertFalse(drift.isOk());
        Failure.SchemaDrift failure = (Failure.SchemaDrift) drift.failure();
        // Addressed to a human on purpose: no argument the agent can change will make a payload this reader
        // cannot parse.
        Assert.assertTrue(failure.suggestion().contains("Report the"));
    }

    /**
     * The drift this exists to catch: {@code UPSTREAM_SUGGESTION} once told the agent to "write the code from
     * what you already know", while {@code --help} told it no failure is licence to guess. The failure object is
     * what an agent is reading at the moment it is blocked, so when the two disagree the object wins and the
     * tool loses the single behaviour it exists to prevent. Only asserting the content — not merely that a
     * suggestion is present — turns that divergence back into a red build.
     */
    @Test
    public void noFailureOffersARememberedSignatureAsTheWayOut() {
        List<String> whenCentralIsTheProblem = List.of(
                Failure.UPSTREAM_SUGGESTION, Failure.TIMEOUT_SUGGESTION, Failure.SCHEMA_DRIFT_SUGGESTION);
        for (String suggestion : whenCentralIsTheProblem) {
            Assert.assertTrue(suggestion.contains("bala/<org>/<name>/"),
                    "a lookup Central blocked still has an answer on disk, and has to name it: " + suggestion);
            Assert.assertTrue(suggestion.contains("Never fall back to a remembered signature"),
                    "a blocked agent guesses unless something forbids it: " + suggestion);
        }

        // Retryability is the one thing these three do not share, so it is asserted per kind.
        Assert.assertTrue(Failure.UPSTREAM_SUGGESTION.contains("once more"));
        Assert.assertTrue(Failure.TIMEOUT_SUGGESTION.contains("once more"));
        Assert.assertFalse(Failure.SCHEMA_DRIFT_SUGGESTION.contains("once more"),
                "schema drift is not a retry: no change of arguments will help");
    }

    @Test
    public void aNetworkErrorIsRetriedAndThenReportedAsUpstream() {
        FakeTransport transport =
                FakeTransport.always(new HttpTransport.Reply.Failed("network error: connection refused"));
        Result<JsonElement> result =
                CentralClient.fetchJson("https://example.invalid/x", fast(transport).build());
        Assert.assertFalse(result.isOk());
        Failure.Upstream failure = (Failure.Upstream) result.failure();
        Assert.assertEquals(failure.attempts(), 3);
        Assert.assertNull(failure.status(), "a request that never answered has no status line");
    }

    // -----------------------------------------------------------------------
    // Schema drift
    // -----------------------------------------------------------------------

    @Test
    public void anAbsentDeclarationBucketIsAModuleWithNoneOfThemNotDrift() {
        // Central OMITS a bucket when the module has none of that kind. Requiring all 30 cost the tool
        // ~15% of Central — pinecone.vector, weaviate, azure_cosmosdb and sendgrid each failed EVERY verb
        // with a wall of "expected an array, received nothing", and an agent with no readable signatures
        // hand-rolled the connector over http:Client instead.
        JsonObject module = onlyModule("ballerinax__sap");
        int configurablesBefore = module.getAsJsonArray("configurables").size();
        module.remove("records");

        Result<CentralDocs> result = Schema.parse(wrap(module), "ballerinax/sap:1.3.1");
        Assert.assertTrue(result.isOk(), "an omitted bucket must not refuse the whole package");
        CentralDocs.Module parsed = result.value().modules().get(0);
        Assert.assertTrue(parsed.records().isEmpty());
        // The trap in defaulting: "absent means empty" must not quietly become "present means empty".
        // A bucket that IS populated has to survive alongside one that is gone.
        Assert.assertEquals(parsed.configurables().size(), configurablesBefore,
                "defaulting an absent bucket must not blank a populated one");
    }

    @Test
    public void aDeclarationBucketPresentButNotAnArrayIsStillDrift() {
        // The narrowed drift signal: a key Central DOES send, in a shape the reader cannot walk, is a
        // payload that changed rather than a package that is unusual. A rename is caught instead by
        // KeySpaceTest, which snapshots the whole key space across the corpus.
        JsonObject module = onlyModule("ballerinax__sap");
        module.addProperty("records", "no longer an array");

        Result<CentralDocs> result = Schema.parse(wrap(module), "ballerinax/sap:1.3.1");
        Assert.assertFalse(result.isOk());
        Failure.SchemaDrift failure = (Failure.SchemaDrift) result.failure();
        Assert.assertEquals(
                failure.issues().stream().map(Failure.SchemaIssue::path).toList(),
                List.of("docsData.modules.0.records"));
    }

    private static JsonObject onlyModule(String fixture) {
        return FixtureCorpus.loadRawFixture(fixture).getAsJsonObject()
                .getAsJsonObject("docsData").getAsJsonArray("modules").get(0).getAsJsonObject();
    }

    private static JsonObject wrap(JsonObject module) {
        JsonObject wrapper = new JsonObject();
        JsonObject docsData = new JsonObject();
        com.google.gson.JsonArray modules = new com.google.gson.JsonArray();
        modules.add(module);
        docsData.add("modules", modules);
        wrapper.add("docsData", docsData);
        return wrapper;
    }

    @Test
    public void aPayloadWithNoModulesAtAllIsDriftNotAnEmptyLibrary() {
        Result<CentralDocs> result = Schema.parse(
                com.google.gson.JsonParser.parseString("{\"docsData\":{\"modules\":[]}}"), "x/y:1.0.0");
        Assert.assertFalse(result.isOk());
    }

    @Test
    public void theValidatorReportsEveryMismatchAtOnceRatherThanTheFirst() {
        // The person reading a drift failure is about to extend the schema and needs the whole list; a
        // fail-fast validator turns one review into four round trips. Mistyped rather than absent: an
        // absent bucket is a module with none of that kind, so a module carrying only id and orgName now
        // parses as an empty module rather than reporting thirty issues.
        JsonObject module = onlyModule("ballerinax__sap");
        for (String bucket : List.of("records", "clients", "functions", "errors", "constants", "enums")) {
            module.addProperty(bucket, "no longer an array");
        }

        Result<CentralDocs> result = Schema.parse(wrap(module), "ballerinax/sap:1.3.1");
        Assert.assertFalse(result.isOk());
        Failure.SchemaDrift failure = (Failure.SchemaDrift) result.failure();
        Assert.assertEquals(failure.issues().size(), 6,
                "six mistyped buckets should report six paths, got " + failure.issues().size());
        Assert.assertTrue(failure.issues().stream()
                .anyMatch(issue -> issue.path().equals("docsData.modules.0.clients")));
    }

    @Test
    public void aModuleCarryingNoDeclarationBucketsAtAllStillParses() {
        // The shape Central actually serves for a package with nothing in most buckets. Before the fix
        // this reported ~30 issues and refused the package outright.
        String payload = "{\"docsData\":{\"modules\":[{\"id\":\"kafka\",\"orgName\":\"ballerinax\"}]}}";
        Result<CentralDocs> result =
                Schema.parse(com.google.gson.JsonParser.parseString(payload), "ballerinax/kafka:4.6.5");
        Assert.assertTrue(result.isOk(), "a module with no declarations is empty, not drifted");
        Assert.assertTrue(result.value().modules().get(0).clients().isEmpty());
    }

    // -----------------------------------------------------------------------
    // Dependencies.toml
    // -----------------------------------------------------------------------

    @Test
    public void dependenciesTomlYieldsTheLockedVersionOfEachPackage() {
        String toml = """
                [ballerina]
                dependencies-toml-version = "2"

                [[package]]
                org = "ballerina"
                name = "http"
                version = "2.16.6"

                [[package]]
                org = "ballerinax"
                name = "github"
                version = "6.0.0"
                modules = [{org = "ballerinax", packageName = "github", moduleName = "github"}]
                """;
        Map<String, String> versions = DependenciesToml.parse(toml);
        Assert.assertEquals(versions.get("ballerina/http"), "2.16.6");
        Assert.assertEquals(versions.get("ballerinax/github"), "6.0.0");
        // The `[ballerina]` table is metadata, not a package.
        Assert.assertEquals(versions.size(), 2);
    }

    @Test
    public void aTruncatedDependenciesTomlEntryIsSkippedRatherThanHalfRead() {
        Map<String, String> versions =
                DependenciesToml.parse("[[package]]\norg = \"ballerinax\"\nname = \"github\"\n");
        Assert.assertEquals(versions.size(), 0);
    }

    // -----------------------------------------------------------------------
    // Registry search
    // -----------------------------------------------------------------------

    @Test
    public void anUnadoptedPackageIsMovedOutOfTheMiddleOfTheResults() {
        // The real defect, in Central's real order for `q=http client`: tharmigank/http.client.wrapper has ONE
        // pull and ranks fourth, above ballerina/sql and ballerina/websocket. An agent reading top-down picks it.
        String body = """
                {"count": 1351, "packages": [
                  {"organization":"ballerina","name":"http","version":"2.16.6","pullCount":1862507},
                  {"organization":"ballerinax","name":"client.config","version":"3.1.0","pullCount":133297},
                  {"organization":"ballerinax","name":"health.clients.fhir","version":"2.0.0","pullCount":12509},
                  {"organization":"tharmigank","name":"http.client.wrapper","version":"0.1.0","pullCount":1},
                  {"organization":"ballerina","name":"sql","version":"1.15.0","pullCount":171335},
                  {"organization":"ballerina","name":"websocket","version":"2.15.5","pullCount":127084}
                ]}
                """;
        Result<SearchHit.Results> result = CentralClient.searchPackages(
                List.of("http", "client"), fast(FakeTransport.always(FakeTransport.ok(body))).build());
        Assert.assertTrue(result.isOk());
        Assert.assertEquals(
                result.value().hits().stream().map(SearchHit::qualified).toList(),
                List.of("ballerina/http", "ballerinax/client.config", "ballerinax/health.clients.fhir",
                        "ballerina/sql", "ballerina/websocket", "tharmigank/http.client.wrapper"),
                "the one-pull package moves to the end and nothing else changes position");
        Assert.assertEquals(result.value().total(), 1351, "the report says how much of the index it shows");
    }

    @Test
    public void centralsRelevanceIsKeptBecausePopularityIsNotRelevance() {
        // The counter-case that rules out sorting the whole list by pull count. This is Central's real order for
        // `q=kafka messaging`: it puts ballerinax/kafka FIRST, and a pull-count sort would rank ballerina/crypto
        // and ballerina/http above it — burying the actual answer under two packages that merely matched.
        String body = """
                {"count": 230, "packages": [
                  {"organization":"ballerinax","name":"kafka","version":"4.6.5","pullCount":60747},
                  {"organization":"ballerina","name":"messaging","version":"1.0.0","pullCount":7836},
                  {"organization":"ballerina","name":"http","version":"2.16.6","pullCount":1862507},
                  {"organization":"ballerina","name":"crypto","version":"2.12.1","pullCount":1684059}
                ]}
                """;
        Result<SearchHit.Results> result = CentralClient.searchPackages(
                List.of("kafka", "messaging"), fast(FakeTransport.always(FakeTransport.ok(body))).build());
        Assert.assertTrue(result.isOk());
        Assert.assertEquals(result.value().hits().get(0).qualified(), "ballerinax/kafka",
                "the package the caller asked about has to stay first");
        Assert.assertEquals(
                result.value().hits().stream().map(SearchHit::qualified).toList(),
                List.of("ballerinax/kafka", "ballerina/messaging", "ballerina/http", "ballerina/crypto"),
                "every one of these is adopted, so the order is Central's untouched");
    }

    @Test
    public void aLegitimateLowPullPackageStaysWhereCentralPutIt() {
        // The floor has to separate two populations, not just "small" from "large". `ballerina/mqtt` at 2,460 pulls
        // and `choreo/mediation.log_message` at 2,890 are real packages a Kafka query should surface in place.
        String body = """
                {"count": 230, "packages": [
                  {"organization":"choreo","name":"mediation.log_message","version":"1.0.0","pullCount":2890},
                  {"organization":"nobody","name":"experiment","version":"0.1.0","pullCount":18},
                  {"organization":"ballerina","name":"mqtt","version":"1.3.0","pullCount":2460}
                ]}
                """;
        Result<SearchHit.Results> result = CentralClient.searchPackages(
                List.of("messaging"), fast(FakeTransport.always(FakeTransport.ok(body))).build());
        Assert.assertTrue(result.isOk());
        Assert.assertEquals(
                result.value().hits().stream().map(SearchHit::qualified).toList(),
                List.of("choreo/mediation.log_message", "ballerina/mqtt", "nobody/experiment"));
    }

    @Test
    public void searchSendsTheQueryAsOneQueryParameter() {
        FakeTransport transport = FakeTransport.always(FakeTransport.ok("{\"count\":0,\"packages\":[]}"));
        Result<SearchHit.Results> result =
                CentralClient.searchPackages(List.of("kafka", "messaging"), fast(transport).build());
        Assert.assertTrue(result.isOk());
        Assert.assertTrue(result.value().hits().isEmpty());
        Assert.assertTrue(transport.urls().get(0).contains("registry/search-packages?q=kafka+messaging"),
                transport.urls().get(0));
    }

    @Test
    public void aSearchEntryMissingItsCoordinatesIsDroppedRatherThanRenderedNameless() {
        String body = "{\"count\":2,\"packages\":[{\"name\":\"orphan\"},"
                + "{\"organization\":\"ballerina\",\"name\":\"io\",\"pullCount\":5}]}";
        Result<SearchHit.Results> result = CentralClient.searchPackages(
                List.of("io"), fast(FakeTransport.always(FakeTransport.ok(body))).build());
        Assert.assertTrue(result.isOk());
        Assert.assertEquals(result.value().hits().size(), 1);
        Assert.assertEquals(result.value().hits().get(0).qualified(), "ballerina/io");
    }
}
