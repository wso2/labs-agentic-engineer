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
import com.google.gson.JsonParser;
import io.ballerina.library.cache.CacheLocation;
import io.ballerina.library.cache.DiskCache;
import io.ballerina.library.cache.DocsCache;
import io.ballerina.library.cache.Versions;
import io.ballerina.library.central.CentralClient;
import io.ballerina.library.central.HttpOptions;
import io.ballerina.library.central.HttpTransport;
import io.ballerina.library.cli.Cli;
import org.testng.Assert;
import org.testng.annotations.Test;

import java.io.IOException;
import java.io.UncheckedIOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.stream.Stream;

/**
 * The cache, driven through the real command.
 *
 * <p>Two properties matter more than the rest and both are tested against stdout rather than against the store: a
 * hit produces the SAME DOCUMENT as a fetch, and a cache that is broken in any way produces NO OBSERVABLE EFFECT
 * — no byte on stderr, no non-zero exit, no wrong document. The second is what makes "cache trouble is never the
 * caller's problem" a fact rather than an intention, and it is the one an implementation is most likely to break
 * by helpfully reporting something.
 *
 * @since 0.1.0
 */
public class CacheTest {

    private static final String SLUG = "ballerinax__kafka";
    private static final String PKG = "ballerinax/kafka";
    private static final String VERSION = "4.6.5";

    /**
     * Every verb that reads a package, so both cache properties are asserted over the whole surface.
     *
     * <p>Spelled out rather than read from the grammar on purpose: a verb ADDED and never wired through to the
     * cache is the failure these two tests exist to catch, and a list derived from the grammar would grow to
     * include it silently. {@code LibraryToolTest} checks the roster against the grammar from the other end.
     *
     * <p>{@code find} is absent because it reads no package: a registry query resolves no version and is never
     * cached, so it declares no {@code --refresh} at all.
     */
    private static final List<List<String>> PACKAGE_VERBS = List.of(
            List.of("overview", PKG),
            List.of("client", PKG),
            List.of("class", PKG),
            List.of("funcs", PKG),
            List.of("type", PKG, "TopicPartition"),
            List.of("guide", PKG),
            List.of("api", PKG));

    /** Central, replayed, counting how many times each endpoint was hit. */
    private static final class CountingCentral {

        private final String payload = FixtureCorpus.loadRawFixture(SLUG).toString();
        private int docs;
        private int versions;

        private HttpTransport transport() {
            return FakeTransport.routing(url -> {
                if (url.contains("/docs/")) {
                    docs++;
                    return FakeTransport.ok(payload);
                }
                versions++;
                return FakeTransport.ok("[\"" + VERSION + "\"]");
            });
        }
    }

    /** Captures both streams, so a test can assert the cache said nothing. */
    private static final class Capture {

        private final StringBuilder out = new StringBuilder();
        private final StringBuilder err = new StringBuilder();

        private Cli.Streams streams() {
            return new Cli.Streams(out::append, err::append);
        }

        private String stdout() {
            return out.toString();
        }

        private String stderr() {
            return err.toString();
        }
    }

    private static Path freshRoot() {
        try {
            return Files.createTempDirectory("bal-cache-");
        } catch (IOException cause) {
            throw new UncheckedIOException(cause);
        }
    }

    private static DocsCache cacheAt(Path root) {
        return DiskCache.at(root, 0700);
    }

    private static Path docsEntry(Path root) {
        return root.resolve("v1").resolve("docs").resolve("ballerinax").resolve("kafka")
                .resolve(VERSION + ".json");
    }

    private static HttpOptions.Builder options(HttpTransport transport, DocsCache cache) {
        return HttpOptions.builder()
                .transport(transport)
                .cache(cache)
                .baseDelayMs(1)
                .sleeper(millis -> {
                    // Nothing in this class asserts wall-clock behaviour.
                });
    }

    private static JsonElement readEntry(Path root) {
        return JsonParser.parseString(FixtureCorpus.read(docsEntry(root)));
    }

    // -----------------------------------------------------------------------
    // Hit and miss
    // -----------------------------------------------------------------------

    @Test
    public void aMissFetchesAHitDoesNotAndBothProduceTheSameDocument() {
        Path root = freshRoot();
        DocsCache cache = cacheAt(root);
        CountingCentral central = new CountingCentral();
        HttpOptions http = options(central.transport(), cache).build();

        Capture cold = new Capture();
        Assert.assertEquals(Cli.run(List.of("overview", PKG), cold.streams(), http), 0);
        Assert.assertEquals(central.docs, 1);

        Capture warm = new Capture();
        Assert.assertEquals(Cli.run(List.of("overview", PKG), warm.streams(), http), 0);
        Assert.assertEquals(central.docs, 1, "the second run must not fetch the docs again");
        Assert.assertEquals(central.versions, 1, "nor re-resolve the version inside the TTL");

        // Byte-identical, with nothing stripped first. A hit and a miss are the same answer, and since the
        // provenance row went away there is no longer a line that says which one this was — so the document is
        // run-order-independent and a test can compare the whole of it.
        Assert.assertEquals(warm.stdout(), cold.stdout());
    }

    @Test
    public void theEntryIsThePayloadAsCentralServedItUncompressed() {
        Path root = freshRoot();
        DocsCache cache = cacheAt(root);
        JsonElement payload = FixtureCorpus.loadRawFixture(SLUG);
        cache.writeDocs(new DocsCache.DocsKey("ballerinax", "kafka", VERSION), payload);
        // No compression level to choose, no bad-gzip corruption mode to handle. Disk is not the constrained
        // resource: a runner's mounts are emptyDirs and the cache dies with the run.
        Assert.assertEquals(readEntry(root), payload);
    }

    @Test
    public void everyVerbReadsTheOneCachedPayloadSoSevenQuestionsCostOneFetch() {
        Path root = freshRoot();
        DocsCache cache = cacheAt(root);
        CountingCentral central = new CountingCentral();
        HttpOptions http = options(central.transport(), cache).build();

        List<List<String>> invocations = PACKAGE_VERBS;
        for (List<String> argv : invocations) {
            Capture capture = new Capture();
            Assert.assertEquals(Cli.run(argv, capture.streams(), http), 0, String.join(" ", argv));
        }
        // This is the whole point of the cache. At 4.9 to 6.6 seconds per invocation the CLI can only be asked
        // once, which is what forced a 22,829-line document to be navigated by hand.
        Assert.assertEquals(central.docs, 1);
    }

    // -----------------------------------------------------------------------
    // Every way an entry can be wrong
    // -----------------------------------------------------------------------

    @Test
    public void eachCorruptionModeFallsThroughToTheNetworkSilently() {
        String payload = FixtureCorpus.loadRawFixture(SLUG).toString();
        JsonElement wrongCoordinates = FixtureCorpus.loadRawFixture(SLUG);
        wrongCoordinates.getAsJsonObject().getAsJsonObject("docsData").getAsJsonArray("modules")
                .forEach(module -> module.getAsJsonObject().addProperty("version", "9.9.9"));

        List<String[]> cases = new ArrayList<>(List.of(
                new String[] {"truncated", payload.substring(0, payload.length() / 2)},
                new String[] {"not JSON at all", "<html>maintenance</html>"},
                new String[] {"empty", ""},
                new String[] {"JSON but not a payload", "{\"hello\":\"world\"}"},
                new String[] {"schema drift",
                        "{\"apiDocsVersion\":\"1.0.0\",\"docsData\":{\"modules\":"
                                + "[{\"id\":\"kafka\",\"orgName\":\"ballerinax\"}]}}"},
                new String[] {"coordinates that do not match the path", wrongCoordinates.toString()}));

        for (String[] entry : cases) {
            String label = entry[0];
            Path root = freshRoot();
            DocsCache cache = cacheAt(root);
            write(docsEntry(root), entry[1]);

            CountingCentral central = new CountingCentral();
            Capture capture = new Capture();
            int exitCode = Cli.run(
                    List.of("overview", PKG), capture.streams(), options(central.transport(), cache).build());

            Assert.assertEquals(exitCode, 0, label + ": must still succeed");
            Assert.assertEquals(capture.stderr(), "", label + ": must say nothing");
            Assert.assertEquals(central.docs, 1, label + ": must fetch");
            // Self-healing: the bad entry is replaced, so the next run is a hit.
            Assert.assertEquals(readEntry(root), FixtureCorpus.loadRawFixture(SLUG), label + ": rewritten");
        }
    }

    @Test
    public void aPayloadTheSchemaRejectsIsNeverWritten() {
        // So the drift is not made permanent.
        Path root = freshRoot();
        DocsCache cache = cacheAt(root);
        Capture capture = new Capture();
        HttpTransport transport = FakeTransport.routing(url -> url.contains("/docs/")
                ? FakeTransport.ok("{\"docsData\":{\"modules\":[]}}")
                : FakeTransport.ok("[\"" + VERSION + "\"]"));
        int exitCode = Cli.run(List.of("overview", PKG), capture.streams(), options(transport, cache).build());
        Assert.assertEquals(exitCode, 1);
        Assert.assertFalse(Files.exists(docsEntry(root).getParent()), "no directory, let alone an entry");
    }

    @Test
    public void aRootWhoseParentIsARegularFileDisablesCaching() {
        // Pointed at a path under a FILE rather than at a mode-000 directory: the latter goes vacuous under a
        // root CI user, and a test that passes because it is running as root is worse than no test.
        Path parent = freshRoot().resolve("regular-file");
        write(parent, "not a directory");
        DocsCache cache = cacheAt(parent.resolve("cache"));

        CountingCentral central = new CountingCentral();
        HttpOptions http = options(central.transport(), cache).build();

        Capture first = new Capture();
        Assert.assertEquals(Cli.run(List.of("overview", PKG), first.streams(), http), 0);
        Assert.assertEquals(first.stderr(), "");
        Assert.assertTrue(cache.describe().contains("unusable"));

        Capture again = new Capture();
        Assert.assertEquals(Cli.run(List.of("overview", PKG), again.streams(), http), 0);
        Assert.assertEquals(central.docs, 2, "nothing was cached, so the second run fetches too");
    }

    @Test
    public void aCoordinateThatIsNotObviouslySafeNeverReachesTheFilesystem() {
        Path root = freshRoot();
        DocsCache cache = cacheAt(root);
        JsonElement anything = JsonParser.parseString("{\"anything\":true}");
        // `QualifiedName` and `Version` reject all of these first; this is the inner guard, kept because the
        // outer one is a regex someone could loosen.
        for (String version : new String[] {"..", ".", "../../etc/passwd", "a/b", "", "with space"}) {
            DocsCache.DocsKey key = new DocsCache.DocsKey("ballerinax", "kafka", version);
            cache.writeDocs(key, anything);
            Assert.assertNull(cache.readDocs(key), "version " + version);
        }
        for (String org : new String[] {"..", ".", "../..", "a/b"}) {
            DocsCache.DocsKey key = new DocsCache.DocsKey(org, "kafka", VERSION);
            cache.writeDocs(key, anything);
            Assert.assertNull(cache.readDocs(key), "org " + org);
            Assert.assertEquals(cache.listVersions(new DocsCache.PackageKey(org, "kafka")), List.of(),
                    "listVersions " + org);
        }
        Assert.assertEquals(children(root).size(), 0, "not even the format directory should exist");
    }

    // -----------------------------------------------------------------------
    // Concurrency
    // -----------------------------------------------------------------------

    @Test
    public void twoConcurrentWritersLeaveOneEntryAndNoTempFiles() {
        Path root = freshRoot();
        // Forced onto the same temp name, which is the worst case a per-pid suffix is meant to avoid. Both still
        // move onto the same target, and the move is atomic: no third process can observe a partial file.
        DocsCache collide = DiskCache.at(root, 0700, 1234, () -> 0.5);
        JsonElement payload = FixtureCorpus.loadRawFixture(SLUG);
        DocsCache.DocsKey key = new DocsCache.DocsKey("ballerinax", "kafka", VERSION);

        collide.writeDocs(key, payload);
        collide.writeDocs(key, payload);

        Assert.assertEquals(children(docsEntry(root).getParent()), List.of(VERSION + ".json"),
                "one entry, and nothing ending in .tmp");
        Assert.assertEquals(readEntry(root), payload);
    }

    // -----------------------------------------------------------------------
    // The versions list: TTL, refresh, offline
    // -----------------------------------------------------------------------

    @Test
    public void theVersionsListIsBelievedForTenMinutesAndReAskedAfter() {
        Path root = freshRoot();
        DocsCache cache = cacheAt(root);
        CountingCentral central = new CountingCentral();
        long[] now = {1_000_000};

        HttpOptions http = options(central.transport(), cache).clock(() -> now[0]).build();
        Cli.run(List.of("overview", PKG), new Capture().streams(), http);
        Assert.assertEquals(central.versions, 1);

        now[0] += CentralClient.LATEST_TTL_MS - 1;
        Cli.run(List.of("overview", PKG), new Capture().streams(), http);
        Assert.assertEquals(central.versions, 1, "one millisecond inside the TTL is still inside it");

        now[0] += 1;
        Cli.run(List.of("overview", PKG), new Capture().streams(), http);
        Assert.assertEquals(central.versions, 2, "at the boundary it is re-asked");
    }

    @Test
    public void aClockThatJumpedBackwardsDoesNotMakeAFutureStampedEntryImmortal() {
        Path root = freshRoot();
        DocsCache cache = cacheAt(root);
        CountingCentral central = new CountingCentral();
        long[] now = {5_000_000};
        HttpOptions http = options(central.transport(), cache).clock(() -> now[0]).build();

        Cli.run(List.of("overview", PKG), new Capture().streams(), http);
        Assert.assertEquals(central.versions, 1);
        now[0] = 1_000;
        Cli.run(List.of("overview", PKG), new Capture().streams(), http);
        Assert.assertEquals(central.versions, 2, "an entry stamped in the future is not fresh, it is wrong");
    }

    @Test
    public void refreshReResolvesAndReDownloadsUnconditionally() {
        Path root = freshRoot();
        DocsCache cache = cacheAt(root);
        CountingCentral central = new CountingCentral();
        // The SAME options object for both runs, so the only thing that can change the outcome is the flag on the
        // argument list. Handing the second run a pre-refreshed `HttpOptions` would test the client and skip the
        // wiring — which is how `--refresh` came to parse and then be silently dropped.
        HttpOptions http = options(central.transport(), cache).build();

        Cli.run(List.of("overview", PKG), new Capture().streams(), http);
        Assert.assertEquals(central.docs, 1);

        Capture warm = new Capture();
        Cli.run(List.of("overview", PKG), warm.streams(), http);
        Assert.assertEquals(central.docs, 1, "without the flag the second run is a hit");

        Capture refreshed = new Capture();
        // Unconditional on purpose. An earlier draft made the re-download conditional on the version having
        // changed, which made the flag a no-op in exactly the case its own error message recommends it for.
        Assert.assertEquals(Cli.run(List.of("overview", PKG, "--refresh"), refreshed.streams(), http), 0);
        Assert.assertEquals(central.docs, 2, "--refresh must re-download");
        Assert.assertEquals(central.versions, 2, "and re-resolve");
    }

    @Test
    public void everyVerbHonoursRefresh() {
        // The flag is global, so every verb that loads a package has to carry it through. A verb that parses it and
        // drops it is the silent class of mistake the grammar refuses everywhere else.
        List<List<String>> invocations = PACKAGE_VERBS;
        for (List<String> argv : invocations) {
            Path root = freshRoot();
            DocsCache cache = cacheAt(root);
            CountingCentral central = new CountingCentral();
            HttpOptions http = options(central.transport(), cache).build();

            Assert.assertEquals(Cli.run(argv, new Capture().streams(), http), 0, String.join(" ", argv));
            Assert.assertEquals(central.docs, 1, String.join(" ", argv));

            List<String> withFlag = new ArrayList<>(argv);
            withFlag.add("--refresh");
            Assert.assertEquals(Cli.run(withFlag, new Capture().streams(), http), 0,
                    String.join(" ", withFlag));
            Assert.assertEquals(central.docs, 2, String.join(" ", withFlag) + " did not re-download");
        }
    }

    @Test
    public void withTheRegistryUnreachableAndAPayloadOnDiskTheLookupStillAnswers() {
        // And says it is unverified. Without this, a warm cached payload plus one blip is a hard failure that
        // can burn the client's whole budget — four times over in a four-verb episode.
        Path root = freshRoot();
        DocsCache cache = cacheAt(root);
        long[] now = {1_000_000};
        Cli.run(List.of("overview", PKG), new Capture().streams(),
                options(new CountingCentral().transport(), cache).clock(() -> now[0]).build());

        now[0] += CentralClient.LATEST_TTL_MS * 2;
        Capture offline = new Capture();
        HttpTransport blip = FakeTransport.routing(url -> {
            Assert.assertFalse(url.contains("/docs/"), "the docs must come off disk, not the network");
            return FakeTransport.status(503);
        });
        int exitCode = Cli.run(List.of("overview", PKG), offline.streams(),
                options(blip, cache).clock(() -> now[0]).maxAttempts(1).build());

        Assert.assertEquals(exitCode, 0);
        Assert.assertTrue(offline.stdout().contains("\n| Warning | the registry was unreachable, so this "
                + "version came off disk unchecked |\n"));
    }

    @Test
    public void aLiveDocsFetchUnderADeadRegistryIsStillUnverified() {
        // The warning is about the VERSION, not about where the bytes came from. Here the payload is downloaded
        // fresh while the registry stays down, so a reader could reasonably assume everything was confirmed —
        // and the version it was downloaded FOR is the one thing that was not.
        Path root = freshRoot();
        DocsCache cache = cacheAt(root);
        long[] now = {1_000_000};
        Cli.run(List.of("overview", PKG), new Capture().streams(),
                options(new CountingCentral().transport(), cache).clock(() -> now[0]).build());

        // Expire the versions entry, then drop the docs entry so the payload MUST be fetched while the registry
        // stays down.
        now[0] += CentralClient.LATEST_TTL_MS * 2;
        cache.removeDocs(new DocsCache.DocsKey("ballerinax", "kafka", VERSION));

        String payload = FixtureCorpus.loadRawFixture(SLUG).toString();
        Capture capture = new Capture();
        HttpTransport transport = FakeTransport.routing(url ->
                url.contains("/docs/") ? FakeTransport.ok(payload) : FakeTransport.status(503));
        int exitCode = Cli.run(List.of("overview", PKG), capture.streams(),
                options(transport, cache).clock(() -> now[0]).maxAttempts(1).build());

        Assert.assertEquals(exitCode, 0);
        Assert.assertTrue(capture.stdout().contains("\n| Warning | the registry was unreachable, so this "
                + "version came off disk unchecked |\n"));
    }

    @Test
    public void withTheRegistryUnreachableAndNothingOnDiskTheFailureIsHonest() {
        DocsCache cache = cacheAt(freshRoot());
        Capture capture = new Capture();
        int exitCode = Cli.run(List.of("overview", PKG), capture.streams(),
                options(FakeTransport.always(FakeTransport.status(503)), cache).maxAttempts(1).build());
        Assert.assertEquals(exitCode, 1);
        Assert.assertEquals(capture.stdout(), "");
        Assert.assertTrue(capture.stderr().contains("\"kind\":\"upstream\""));
    }

    @Test
    public void theNewestVersionOnDiskIsChosenByVersionOrderNotByFilenameOrder() {
        Path root = freshRoot();
        DocsCache cache = cacheAt(root);
        for (String version : new String[] {"1.9.0", "1.10.0", "2.0.0", "2.0.0-alpha"}) {
            cache.writeDocs(new DocsCache.DocsKey("ballerinax", "kafka", version),
                    JsonParser.parseString("{\"v\":\"" + version + "\"}"));
        }
        Assert.assertEquals(
                cache.listVersions(new DocsCache.PackageKey("ballerinax", "kafka")),
                List.of("2.0.0", "2.0.0-alpha", "1.10.0", "1.9.0"));
    }

    @Test
    public void versionComparisonIsDottedNumericWithPreReleasesBelowTheirRelease() {
        // Lexicographic order is wrong in both directions that matter.
        Assert.assertTrue(Versions.compare("1.10.0", "1.9.0") > 0);
        Assert.assertTrue(Versions.compare("2.0.0", "2.0.0-alpha") > 0);
        Assert.assertTrue(Versions.compare("2.0.0-beta", "2.0.0-alpha") > 0);
        Assert.assertEquals(Versions.compare("1.2.3", "1.2.3"), 0);
        Assert.assertTrue(Versions.compare("1.2", "1.2.1") < 0);
    }

    @Test
    public void aLatestEntryThatIsNotWellFormedIsAMissRatherThanACrash() {
        Path root = freshRoot();
        DocsCache cache = cacheAt(root);
        DocsCache.PackageKey key = new DocsCache.PackageKey("ballerinax", "kafka");
        Path path = root.resolve("v1").resolve("latest").resolve("ballerinax").resolve("kafka.json");
        for (String contents : new String[] {
                "not json", "[]", "{}", "{\"version\":\"\",\"atMs\":1}", "{\"version\":\"1.0.0\"}"}) {
            write(path, contents);
            Assert.assertNull(cache.readLatest(key), contents);
        }
        write(path, "{\"version\":\"4.6.5\",\"atMs\":42}");
        Assert.assertEquals(cache.readLatest(key), new DocsCache.LatestEntry("4.6.5", 42));
    }

    // -----------------------------------------------------------------------
    // Where it lives
    // -----------------------------------------------------------------------

    private static CacheLocation.Environment env(Map<String, String> variables) {
        return new CacheLocation.Environment(variables, "/home/aep", "/tmp", "aep");
    }

    private static List<String> roots(CacheLocation.Environment environment) {
        return CacheLocation.candidates(environment).stream()
                .filter(CacheLocation.Candidate.Directory.class::isInstance)
                .map(candidate -> ((CacheLocation.Candidate.Directory) candidate).root())
                .toList();
    }

    @Test
    public void theCacheLocationIsAPureFunctionOfTheEnvironment() {
        Assert.assertEquals(
                CacheLocation.candidates(env(Map.of("BAL_LIBRARY_CACHE", "off"))),
                List.of(new CacheLocation.Candidate.Disabled("BAL_LIBRARY_CACHE=off")));

        Assert.assertEquals(roots(env(Map.of("XDG_CACHE_HOME", "/xdg"))).get(0), "/xdg/bal-library");

        // Relative XDG_CACHE_HOME is invalid per the spec, so it is ignored rather than resolved against the
        // working directory — which for a coding agent is a git clone the platform commits.
        Assert.assertEquals(roots(env(Map.of("XDG_CACHE_HOME", "relative/path"))),
                List.of("/home/aep/.cache/bal-library", "/tmp/bal-library-aep"));

        Assert.assertTrue(CacheLocation.candidates(
                        new CacheLocation.Environment(Map.of(), "", "", "aep")).get(0)
                instanceof CacheLocation.Candidate.Disabled);
    }

    @Test
    public void anExplicitLocationGetsNoFallback() {
        // Rungs 1 and 2 are the caller being explicit. Landing somewhere they did not name is worse than not
        // caching at all.
        Assert.assertEquals(
                roots(env(Map.of("BAL_LIBRARY_CACHE_DIR", "/somewhere/else"))),
                List.of("/somewhere/else"));
    }

    @Test
    public void theDefaultRungIsFollowedByATempDirectory() {
        // The case a pure function cannot see: a `$HOME` that exists and is read-only, which is a shape a
        // container genuinely has. The process wrapper walks this list and takes the first usable root.
        Assert.assertEquals(roots(env(Map.of())),
                List.of("/home/aep/.cache/bal-library", "/tmp/bal-library-aep"));

        List<CacheLocation.Candidate> fallback = CacheLocation.candidates(
                new CacheLocation.Environment(Map.of(), "", "/tmp", "aep"));
        Assert.assertEquals(
                ((CacheLocation.Candidate.Directory) fallback.get(0)).root(), "/tmp/bal-library-aep");
        // Mode 0700 with the user name in it, because a temp directory is world-writable and shared with the
        // agent's own scratch files.
        Assert.assertEquals(((CacheLocation.Candidate.Directory) fallback.get(0)).mode(), 0700);
    }

    @Test
    public void theFirstUsableRootIsWhatTheTempRungIsFor() {
        // Proven against the real probe rather than by inspection: a root whose parent is a regular file cannot
        // be created for any user, so the first candidate is skipped and the second wins.
        Path parent = freshRoot().resolve("regular-file");
        write(parent, "not a directory");
        Assert.assertFalse(DiskCache.isUsableRoot(parent.resolve("cache"), 0700));
        Assert.assertTrue(DiskCache.isUsableRoot(freshRoot().resolve("second-choice"), 0700));
    }

    @Test
    public void theNullCacheStoresNothingAndSaysSo() {
        // The default everywhere outside the process wrapper, and what keeps every other test hermetic.
        DocsCache.DocsKey key = new DocsCache.DocsKey("ballerinax", "kafka", VERSION);
        DocsCache.NULL.writeDocs(key, JsonParser.parseString("{}"));
        Assert.assertNull(DocsCache.NULL.readDocs(key));
        Assert.assertNull(DocsCache.NULL.readLatest(new DocsCache.PackageKey("ballerinax", "kafka")));
        Assert.assertEquals(DocsCache.NULL.listVersions(new DocsCache.PackageKey("ballerinax", "kafka")),
                List.of());
        Assert.assertEquals(DocsCache.NULL.describe(), "disabled");
    }

    // -----------------------------------------------------------------------

    private static void write(Path path, String contents) {
        try {
            Files.createDirectories(path.getParent());
            Files.writeString(path, contents, StandardCharsets.UTF_8);
        } catch (IOException cause) {
            throw new UncheckedIOException(cause);
        }
    }

    private static List<String> children(Path directory) {
        try (Stream<Path> entries = Files.list(directory)) {
            return entries.map(path -> path.getFileName().toString()).sorted().toList();
        } catch (IOException cause) {
            throw new UncheckedIOException(cause);
        }
    }
}
