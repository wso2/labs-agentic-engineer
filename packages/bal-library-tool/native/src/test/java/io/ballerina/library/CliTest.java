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
import com.google.gson.JsonParser;
import io.ballerina.library.central.DependenciesToml;
import io.ballerina.library.central.HttpOptions;
import io.ballerina.library.central.HttpTransport;
import io.ballerina.library.cli.Cli;
import io.ballerina.library.render.Documents;
import io.ballerina.library.views.Readmes;
import org.testng.Assert;
import org.testng.annotations.Test;

import java.io.IOException;
import java.io.UncheckedIOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.List;
import java.util.regex.Pattern;

/**
 * The command's contract: stdout carries the requested document and nothing else, stderr carries one JSON failure,
 * and the exit code says only whether stdout is complete.
 *
 * <p>The skill depends on all three — it redirects stdout straight into a file, and it has no fallback source, so
 * the failure JSON has to say plainly which kind of thing went wrong, since exit 1 alone no longer distinguishes
 * them (ADR-0015). The verb grammar adds one more thing to pin: every mistyped or version-skewed call must fail
 * LOUDLY as {@code validation} rather than resolving as something else and reporting a Central failure the agent
 * will retry.
 *
 * @since 0.1.0
 */
public class CliTest {

    /**
     * The verbs, spelled out rather than read from the grammar.
     *
     * <p>A verb silently LOST is what this catches; {@code LibraryToolTest} checks the other direction, that the
     * grammar holds no verb this roster is missing.
     */
    private static final List<String> VERBS =
            List.of("find", "overview", "client", "class", "funcs", "type", "guide", "api");

    /** Captures both streams, so a test can assert stdout stayed empty on failure. */
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

        /** The one JSON object a failing run writes to stderr. */
        private JsonObject failure() {
            JsonElement parsed = JsonParser.parseString(err.toString());
            Assert.assertTrue(parsed.isJsonObject(), "stderr is not one JSON object: " + err);
            return parsed.getAsJsonObject();
        }

        private String field(String name) {
            JsonElement value = failure().get(name);
            return value == null ? null : value.getAsString();
        }
    }

    /**
     * A document opens on its own marker, then on the heading or comment given.
     *
     * <p>Asserted through a helper rather than as a literal prefix because line one now carries the document's
     * LENGTH, which is content-dependent — see {@code Documents.withLength}. Pinning the number would make every
     * one of these tests fail on any content change, reporting a renderer edit as a marker regression.
     */
    private static void opensWith(String stdout, String marker, String then) {
        Assert.assertTrue(stdout.startsWith(marker), "expected to open on " + marker + ", got:\n" + stdout);
        String firstLine = stdout.lines().findFirst().orElse("");
        Assert.assertTrue(firstLine.matches(".*· \\d+ lines.*"),
                "line one states no length: " + firstLine);
        Assert.assertTrue(stdout.lines().skip(1).findFirst().orElse("").equals(then)
                        || stdout.substring(firstLine.length()).startsWith("\n" + then),
                "expected " + then + " after the marker, got:\n" + stdout);
    }

    /** Central, replayed: the versions endpoint then the docs endpoint. */
    private static HttpOptions centralFor(String slug, String version) {
        String docs = FixtureCorpus.loadRawFixture(slug).toString();
        return options(FakeTransport.routing(url -> url.contains("/docs/")
                ? FakeTransport.ok(docs)
                : FakeTransport.ok("[\"" + version + "\"]")));
    }

    private static HttpOptions options(HttpTransport transport) {
        return HttpOptions.builder()
                .transport(transport)
                .baseDelayMs(1)
                .sleeper(millis -> {
                    // No test here asserts wall-clock behaviour.
                })
                .build();
    }

    private static HttpOptions never() {
        return options(FakeTransport.never());
    }

    // -----------------------------------------------------------------------
    // Usage and argument errors
    // -----------------------------------------------------------------------

    /**
     * A transport that answers the docs endpoint and FAILS the registry, which is what a version has to make
     * unnecessary. It is also the real shape of the case that motivated the flag: a module that is not its own
     * package has no registry row at all, so nothing can resolve a latest for it.
     */
    private static HttpOptions docsOnlyFor(String slug) {
        String docs = FixtureCorpus.loadRawFixture(slug).toString();
        return options(FakeTransport.routing(url -> url.contains("/docs/")
                ? FakeTransport.ok(docs)
                : FakeTransport.status(404)));
    }

    /**
     * Versions are resolved INTERNALLY, and there is no argument for one anywhere in the grammar.
     *
     * <p>T11 recorded why the flag went rather than being kept and documented: it was invisible in {@code --help},
     * its syntax differed per verb, and no agent in the sweep ever passed it — so every lookup answered for
     * Central's latest while the build compiled against something else. The project directory replaces it, and it
     * is discovered rather than passed.
     */
    @Test
    public void aVersionIsResolvedFromTheProjectAndIsNotAnArgumentOnAnyVerb() {
        Path projectDir = tempDir();
        write(projectDir.resolve("Ballerina.toml"), "[package]\norg = \"acme\"\nname = \"app\"\n");
        write(projectDir.resolve("Dependencies.toml"),
                "[[package]]\norg = \"ballerinax\"\nname = \"kafka\"\nversion = \"4.6.5\"\n");

        for (List<String> argv : List.of(
                List.of("overview", "ballerinax/kafka"),
                List.of("client", "ballerinax/kafka"),
                List.of("class", "ballerinax/kafka"),
                List.of("type", "ballerinax/kafka", "TopicPartition"),
                List.of("guide", "ballerinax/kafka"),
                List.of("api", "ballerinax/kafka"))) {
            Capture capture = new Capture();
            // The transport asserts the registry is never reached, which is what proves the lock was used.
            Assert.assertEquals(
                    Cli.run(argv, capture.streams(), docsOnlyFor("ballerinax__kafka"),
                            projectDir.toString()),
                    0, String.join(" ", argv) + " -> " + capture.stderr());
            Assert.assertFalse(capture.stdout().isEmpty(), String.join(" ", argv));
        }

        // And no verb accepts a version flag, on any spelling.
        for (String flag : List.of("--version", "--project-dir")) {
            Capture capture = new Capture();
            Assert.assertEquals(Cli.run(List.of("overview", "ballerinax/kafka", flag, "4.6.5"),
                    capture.streams(), never()), 1, flag);
            Assert.assertEquals(capture.field("kind"), "validation", flag);
        }
    }

    @Test
    public void aProjectIsFoundByWalkingUpFromWhereTheProcessStands() {
        // What a build does, and it needs no argument. A lookup from a nested directory of a component resolves
        // the same version the component compiles against.
        Path projectDir = tempDir();
        write(projectDir.resolve("Ballerina.toml"), "[package]\nname = \"app\"\n");
        Path nested = projectDir.resolve("modules").resolve("deep");
        mkdirs(nested);
        Assert.assertEquals(DependenciesToml.discoverProject(nested), projectDir);
        // Outside a project there is nothing to find, and Central's latest is then the correct answer anyway:
        // it is what adding the import would resolve to.
        Assert.assertNull(DependenciesToml.discoverProject(tempDir()));
    }

    @Test
    public void aDottedNameThatIsNeitherAPackageNorAModuleNamesWhatWasTried() {
        // S3-01, second half. `bal library type ballerinax/aws.auth AuthConfig` used to fail here and be told
        // to pin a version by hand, because version resolution goes through the registry and the registry has
        // no row for a module. It resolves through the containing package now (ClientTest owns that walk), so
        // reaching this message means the walk ran and found nothing — at which point the name really is
        // wrong, `search` really is the recovery, and the reader can say which half was tried.
        Capture capture = new Capture();
        HttpOptions registryMissing = options(FakeTransport.routing(url -> FakeTransport.status(404)));
        Assert.assertEquals(
                Cli.run(List.of("type", "ballerinax/aws.auth", "AuthConfig"),
                        capture.streams(), registryMissing), 1);
        Assert.assertEquals(capture.stdout(), "");
        Assert.assertTrue(capture.stderr().contains("tried ballerinax/aws"), capture.stderr());
        Assert.assertTrue(capture.stderr().contains("bal library find"), capture.stderr());
        // Advice about pinning a version would now name a recovery the reader has already attempted.
        Assert.assertFalse(capture.stderr().contains("--version"), capture.stderr());
        // An undotted name has no containing package to try, and keeps the plain spelling advice.
        Capture typo = new Capture();
        Assert.assertEquals(
                Cli.run(List.of("type", "ballerinax/githbu", "X"), typo.streams(), registryMissing), 1);
        Assert.assertTrue(typo.stderr().contains("bal library find"), typo.stderr());
        Assert.assertFalse(typo.stderr().contains("tried "), typo.stderr());
    }

    @Test
    public void aVersionShapedArgumentIsRejectedWithTheRuleThatReplacedIt() {
        // The silent half of KAFKA-08: `ops <pkg> 4.6.5` succeeded, reading the version as a PATH. Left alone it
        // would now be read as a selector or a declaration name and reported as symbol-not-found on a
        // "declaration" called 4.6.5, which names neither the mistake nor what to do. There is no argument to
        // move it to, so the suggestion states the new rule.
        for (List<String> argv : List.of(
                List.of("client", "ballerinax/kafka", "4.6.5"),
                List.of("class", "ballerinax/kafka", "4.6.5"),
                List.of("funcs", "ballerinax/kafka", "4.6.5"),
                List.of("type", "ballerinax/kafka", "4.6.5", "TopicPartition"),
                List.of("guide", "ballerinax/kafka", "4.6.5"))) {
            Capture capture = new Capture();
            Assert.assertEquals(Cli.run(argv, capture.streams(), never()), 1, String.join(" ", argv));
            Assert.assertEquals(capture.stdout(), "", String.join(" ", argv));
            Assert.assertEquals(capture.field("kind"), "validation", String.join(" ", argv));
            Assert.assertTrue(capture.field("message").contains("looks like a version"), capture.stderr());
            Assert.assertTrue(capture.field("suggestion").contains("Dependencies.toml"), capture.stderr());
        }
    }

    @Test
    public void noArgumentsPrintsUsageOnStdoutAndSucceeds() {
        // Usage is a document, not a failure: it goes where every document goes, under the code that says
        // stdout is complete. A caller redirecting stdout gets the text rather than an empty file.
        Capture capture = new Capture();
        Assert.assertEquals(Cli.run(List.of(), capture.streams(), never()), 0);
        Assert.assertEquals(capture.stderr(), "");
        Assert.assertTrue(capture.stdout().startsWith("Usage: bal library"));
    }

    @Test
    public void helpIsTheSameAsNoArguments() {
        Capture capture = new Capture();
        Assert.assertEquals(Cli.run(List.of("--help"), capture.streams(), never()), 0);
        Assert.assertTrue(capture.stdout().startsWith("Usage: bal library"));
        // ADR-0013. The text used to end with a `Cache:` line — the one place the cache was allowed to speak,
        // and how an operator proved it was alive inside a runner. It is gone: this text is read by an agent
        // choosing a verb, and where the tool keeps its cache is not one of that reader's questions. The state
        // is still reachable through `DocsCache.describe()`, which `CacheTest` covers.
        Assert.assertFalse(capture.stdout().contains("Cache:"));
    }

    @Test
    public void everyVerbHasItsOwnHelp() {
        for (String verb : VERBS) {
            Capture capture = new Capture();
            Assert.assertEquals(Cli.run(List.of(verb, "--help"), capture.streams(), never()), 0, verb);
            Assert.assertEquals(capture.stderr(), "", verb);
            Assert.assertTrue(capture.stdout().startsWith("Usage: bal library " + verb), verb);
        }
    }

    @Test
    public void aVersionSuffixInThePackageNameIsRejectedBeforeAnyRequest() {
        Capture capture = new Capture();
        Assert.assertEquals(
                Cli.run(List.of("overview", "ballerinax/github:6.0.0"), capture.streams(), never()), 1);
        Assert.assertEquals(capture.stdout(), "");
        Assert.assertEquals(capture.field("kind"), "validation");
        Assert.assertTrue(capture.field("suggestion").contains("Drop any ':version' suffix"));
    }

    @Test
    public void anUnrecognisedFlagIsAUsageErrorNotAVersionCentralIsAskedAbout() {
        // The regression this pins: `--refresh` on a stale binary used to resolve as the VERSION, so it reported
        // `package-not-found` at exit 1 — which the skill teaches means "Central could not answer, run it once
        // more". The agent then retried a command that could never succeed.
        Capture capture = new Capture();
        Assert.assertEquals(
                Cli.run(List.of("overview", "ballerina/http", "--nonesuch"), capture.streams(), never()), 1);
        Assert.assertEquals(capture.stdout(), "");
        Assert.assertEquals(capture.field("kind"), "validation");
        Assert.assertTrue(capture.field("message").contains("--nonesuch"), capture.stderr());
        Assert.assertTrue(capture.field("suggestion").contains("Known flags are"));
    }

    @Test
    public void aFirstPositionalThatIsNotAVerbNamesEveryVerb() {
        Capture capture = new Capture();
        Assert.assertEquals(Cli.run(List.of("opps", "ballerinax/github"), capture.streams(), never()), 1);
        Assert.assertEquals(capture.field("kind"), "validation");
        Assert.assertTrue(capture.field("suggestion").contains(String.join(", ", VERBS)),
                capture.stderr());
    }

    @Test
    public void aBarePackageIsRejectedWithTheVerbItProbablyMeant() {
        // The deliberate departure from the Node CLI, which defaulted a leading package to `overview`. Under
        // `bal`, `bal library ballerinax/github` reads as a subcommand typo, so it is `validation` — and the suggestion
        // hands back the exact command rather than making the caller read the usage block.
        Capture capture = new Capture();
        Assert.assertEquals(Cli.run(List.of("ballerinax/github"), capture.streams(), never()), 1);
        Assert.assertEquals(capture.field("kind"), "validation");
        Assert.assertTrue(capture.field("suggestion").contains("bal library overview ballerinax/github"),
                capture.stderr());
    }

    @Test
    public void aVerbLeadsAndNoVerbTakesAPositionalItHasNoMeaningFor() {
        // A leading verb has no slash and fails the qualified-name pattern as `validation`, which is loud. The old
        // hazard was a TRAILING one landing in the version slot and coming back as `package-not-found`, which the
        // skill teaches means "retry" — so the agent retried a command that could never succeed. With the version
        // slot gone the hazard is gone with it: `overview` takes exactly one positional, so a second is rejected
        // before any request.
        Capture capture = new Capture();
        Assert.assertEquals(
                Cli.run(List.of("overview", "ballerinax/github", "client"), capture.streams(), never()), 1);
        Assert.assertEquals(capture.stdout(), "");
        Assert.assertEquals(capture.field("kind"), "validation");
        Assert.assertTrue(capture.field("message").contains("client"), capture.stderr());
    }

    @Test
    public void aFlagWithoutItsValueIsAUsageError() {
        for (List<String> argv : List.of(
                List.of("overview", "ballerinax/github", "-s"),
                List.of("guide", "ballerinax/github", "--module"),
                // The value must not be silently taken from the next flag either: picocli 4.0.1 consumes it, and
                // the setting that changes that arrived in 4.4.
                List.of("client", "ballerinax/github", "-s", "-r"),
                List.of("guide", "ballerinax/github", "--module", "--refresh"))) {
            Capture capture = new Capture();
            Assert.assertEquals(Cli.run(argv, capture.streams(), never()), 1, String.join(" ", argv));
            Assert.assertEquals(capture.field("kind"), "validation", String.join(" ", argv));
            Assert.assertFalse(capture.field("suggestion").isEmpty(), String.join(" ", argv));
        }
    }

    @Test
    public void flagEqualsValueIsAccepted() {
        // Because it is the form half the world types.
        Capture capture = new Capture();
        int exitCode = Cli.run(
                List.of("client", "ballerina/http", "--search=cookie"),
                capture.streams(),
                centralFor("ballerina__http", "2.16.6"));
        Assert.assertEquals(exitCode, 0, capture.stderr());
        Assert.assertTrue(capture.stdout().contains("| Filter | `cookie`"), capture.stdout());
    }

    @Test
    public void shortHelpWorksWhereverItAppears() {
        for (List<String> argv : List.of(
                List.of("-h"),
                List.of("client", "ballerinax/github", "-h"),
                List.of("type", "x/y", "Name", "-h"))) {
            Capture capture = new Capture();
            Assert.assertEquals(Cli.run(argv, capture.streams(), never()), 0, String.join(" ", argv));
            Assert.assertTrue(capture.stdout().startsWith("Usage: bal library"), String.join(" ", argv));
        }
    }

    @Test
    public void eachVerbRejectsAPositionalItHasNoMeaningFor() {
        for (List<String> argv : List.of(
                List.of("overview", "ballerinax/github", "extra"),
                List.of("guide", "ballerinax/github", "1", "extra"),
                List.of("api", "ballerinax/github", "extra"),
                List.of("find", "kafka", "-r"))) {
            Capture capture = new Capture();
            Assert.assertEquals(Cli.run(argv, capture.streams(), never()), 1, String.join(" ", argv));
            Assert.assertEquals(capture.field("kind"), "validation", String.join(" ", argv));
        }
    }

    @Test
    public void aVerbThatNeedsAnArgumentSaysWhichOne() {
        // A bare `type <pkg>` must not become a second `api`, and the failure names BOTH ways forward rather
        // than only the one the caller happened to leave out.
        Capture capture = new Capture();
        Assert.assertEquals(Cli.run(List.of("type", "ballerinax/github"), capture.streams(), never()), 1);
        Assert.assertTrue(capture.field("message").contains("a declaration name or a search query"),
                capture.stderr());
        Assert.assertTrue(capture.field("suggestion").contains("-s "), capture.stderr());

        Capture noPackage = new Capture();
        Assert.assertEquals(Cli.run(List.of("overview"), noPackage.streams(), never()), 1);
        Assert.assertTrue(noPackage.field("message").contains("needs a package"));

        Capture noKeywords = new Capture();
        Assert.assertEquals(Cli.run(List.of("find"), noKeywords.streams(), never()), 1);
        Assert.assertTrue(noKeywords.field("message").contains("at least one keyword"));
    }

    @Test
    public void aFlagTheVerbDoesNotTakeIsRejectedNotSilentlyIgnored() {
        // The regression this pins: `overview --deps` used to exit 0 with the flag dropped. That is the same
        // silent class as an unknown flag resolving to a version — the caller believes it asked for something it
        // did not get, and nothing in the output says otherwise. Here the rejection is structural: `--deps` is
        // declared on `type` and nowhere else.
        List<List<String>> cases = List.of(
                List.of("overview", "ballerinax/kafka", "-r"),
                List.of("overview", "ballerinax/kafka", "--module", "kafka"),
                List.of("guide", "ballerinax/kafka", "-r"),
                List.of("api", "ballerinax/kafka", "-s", "x"),
                List.of("api", "ballerinax/kafka", "-r"),
                List.of("find", "kafka", "--refresh"),
                List.of("find", "kafka", "-s", "x"));
        List<String> expected = List.of(
                "--resolve-types belongs to 'client', 'class', 'funcs' and 'type'",
                "--module belongs to 'guide'",
                "--resolve-types belongs to 'client', 'class', 'funcs' and 'type'",
                "--search belongs to 'overview', 'client', 'class', 'funcs', 'type' and 'guide'",
                "--resolve-types belongs to 'client', 'class', 'funcs' and 'type'",
                "--refresh belongs to 'overview', 'client', 'class', 'funcs', 'type', 'guide' and 'api'",
                "--search belongs to 'overview', 'client', 'class', 'funcs', 'type' and 'guide'");

        for (int index = 0; index < cases.size(); index++) {
            List<String> argv = cases.get(index);
            Capture capture = new Capture();
            Assert.assertEquals(Cli.run(argv, capture.streams(), never()), 1, String.join(" ", argv));
            Assert.assertEquals(capture.stdout(), "", String.join(" ", argv));
            Assert.assertEquals(capture.field("kind"), "validation", String.join(" ", argv));
            Assert.assertTrue(capture.field("suggestion").contains(expected.get(index)),
                    String.join(" ", argv) + " → " + capture.field("suggestion"));
        }
    }

    @Test
    public void eachVerbStillAcceptsTheFlagsItDoesTake() {
        HttpOptions github = centralFor("ballerinax__github", "6.0.0");
        List<List<String>> accepted = List.of(
                List.of("overview", "ballerinax/github", "-s", "cache"),
                List.of("client", "ballerinax/github", "Client", "repos", "--all"),
                List.of("client", "ballerinax/github", "Client", "repos", "-r"),
                List.of("class", "ballerinax/github", "-s", "config"),
                List.of("funcs", "ballerinax/github"),
                List.of("type", "ballerinax/github", "FullRepository", "-r"),
                List.of("guide", "ballerinax/github", "-s", "auth"),
                // `--refresh` applies to every verb that reads a package.
                List.of("api", "ballerinax/github", "--refresh"));
        for (List<String> argv : accepted) {
            Capture capture = new Capture();
            Assert.assertEquals(Cli.run(argv, capture.streams(), github), 0,
                    String.join(" ", argv) + " → " + capture.stderr());
        }
    }

    // -----------------------------------------------------------------------
    // Verb dispatch
    // -----------------------------------------------------------------------

    @Test
    public void theOverviewIsMarkdownAndNothingOutsideAFenceLooksLikeADeclaration() {
        Capture capture = new Capture();
        int exitCode = Cli.run(List.of("overview", "ballerinax/kafka"), capture.streams(),
                centralFor("ballerinax__kafka", "4.6.5"));
        Assert.assertEquals(exitCode, 0);
        Assert.assertEquals(capture.stderr(), "");
        opensWith(capture.stdout(), "<!-- bal library overview v1 ·", "# ballerinax/kafka 4.6.5");
        Assert.assertFalse(capture.stdout().contains("\nclient class "));
        // The map generates no signature at all, so a `remote function` line outside the readme quotation would
        // mean it had started dumping again.
        Assert.assertFalse(capture.stdout().contains("\nremote function "), capture.stdout());
    }

    /**
     * The verb that took the readme out of {@code overview}, driven end to end.
     *
     * <p>Both halves matter and only one of them is about {@code guide}. It has to reproduce the whole readme
     * verbatim — that is the point of splitting it out — and {@code overview} has to stop carrying it, or the
     * split cost a verb and saved nothing.
     */
    @Test
    public void guideIsTheReadmeVerbatimAndOverviewNoLongerCarriesIt() {
        Capture capture = new Capture();
        int exitCode = Cli.run(List.of("guide", "ballerinax/kafka"), capture.streams(),
                centralFor("ballerinax__kafka", "4.6.5"));
        Assert.assertEquals(exitCode, 0, capture.stderr());
        Assert.assertEquals(capture.stderr(), "");
        opensWith(capture.stdout(), "<!-- bal library guide v1 ·", "# ballerinax/kafka 4.6.5 — guide");

        String readme = Readmes.collect(FixtureCorpus.loadFixture("ballerinax__kafka")).get(0).markdown();
        Assert.assertTrue(capture.stdout().contains(Readmes.demoteHeadings(readme, 2)), "verbatim");

        Capture overview = new Capture();
        Assert.assertEquals(Cli.run(List.of("overview", "ballerinax/kafka"), overview.streams(),
                centralFor("ballerinax__kafka", "4.6.5")), 0);
        // The readme's PROSE is what left — 29% of it account setup on this corpus, none of it actionable.
        Assert.assertFalse(overview.stdout().contains("##### Key Features"), overview.stdout());
        Assert.assertTrue(capture.stdout().contains("##### Key Features"), "and it is here instead");
        // What overview kept is the code, and the pointer to the rest with its size.
        Assert.assertTrue(overview.stdout().contains("\n## Quickstart\n"));
        Assert.assertTrue(overview.stdout().contains("`bal library guide ballerinax/kafka`"));
        Assert.assertFalse(overview.stdout().contains(Readmes.demoteHeadings(readme, 2)),
                "the entry document must not still carry the whole readme");
    }

    @Test
    public void apiIsTheWholeBallerinaDocumentUnderTheResolvedCoordinates() {
        Capture capture = new Capture();
        int exitCode = Cli.run(List.of("api", "ballerinax/sap"), capture.streams(),
                centralFor("ballerinax__sap", "1.3.1"));
        Assert.assertEquals(exitCode, 0);
        Assert.assertEquals(capture.stderr(), "");
        // The committed snapshot is still the whole body, byte for byte — the stamp is applied to the EXPECTED
        // value rather than tolerated in the actual one, so a renderer change still fails here.
        Assert.assertEquals(capture.stdout(), Documents.withLength(
                "// Resolved: ballerinax/sap:1.3.1\n" + FixtureCorpus.readSnapshot("ballerinax__sap")));
    }

    @Test
    public void typePrintsDeclarationsAndNothingElse() {
        Capture capture = new Capture();
        int exitCode = Cli.run(List.of("type", "ballerinax/kafka", "TopicPartition"), capture.streams(),
                centralFor("ballerinax__kafka", "4.6.5"));
        Assert.assertEquals(exitCode, 0);
        opensWith(capture.stdout(), "// ballerinax/kafka:4.6.5 ·", "");
        Assert.assertTrue(capture.stdout().contains("\npublic type TopicPartition record {|\n"));
        // The code register carries no report furniture. Note that `#` is NOT the test: a leading `# ` here is a
        // Ballerina doc comment, which is the language's own syntax and is exactly what belongs in this register.
        Assert.assertFalse(capture.stdout().contains("```"), "no fences: the whole document is Ballerina");
        Assert.assertFalse(capture.stdout().contains("<!-- bal library"), "no report format marker");
        Assert.assertFalse(capture.stdout().contains("\n| "), "no Markdown tables");
    }

    @Test
    public void aTypeNameThatDoesNotResolveFailsWithCandidatesAndNoPartialDocument() {
        Capture capture = new Capture();
        int exitCode = Cli.run(
                List.of("type", "ballerinax/kafka", "TopicPartition", "TopicPartitio"),
                capture.streams(), centralFor("ballerinax__kafka", "4.6.5"));
        Assert.assertEquals(exitCode, 1);
        // All-or-nothing: "exit 0 means stdout is complete" is what redirecting callers rely on, so one bad name
        // suppresses the good one too.
        Assert.assertEquals(capture.stdout(), "");
        Assert.assertEquals(capture.field("kind"), "symbol-not-found");
        Assert.assertEquals(capture.failure().getAsJsonArray("requested").get(0).getAsString(),
                "TopicPartitio");
        Assert.assertTrue(capture.failure().getAsJsonArray("candidates").toString()
                .contains("TopicPartition"));
    }

    @Test
    public void aNameThatDiffersOnlyInCaseOrPunctuationStillResolves() {
        Capture capture = new Capture();
        int exitCode = Cli.run(List.of("type", "ballerinax/kafka", "topic_partition"), capture.streams(),
                centralFor("ballerinax__kafka", "4.6.5"));
        Assert.assertEquals(exitCode, 0);
        Assert.assertTrue(capture.stdout().contains("\npublic type TopicPartition record {|\n"));
    }

    @Test
    public void aContainerVerbNavigatesAClientsPathsAsMarkdown() {
        Capture capture = new Capture();
        int exitCode = Cli.run(List.of("client", "ballerinax/googleapis.gmail"), capture.streams(),
                centralFor("ballerinax__googleapis.gmail", "4.2.0"));
        Assert.assertEquals(exitCode, 0, capture.stderr());
        opensWith(capture.stdout(), "<!-- bal library client v1 ·", "# Clients — ballerinax/googleapis.gmail `Client`");
    }

    @Test
    public void aPackageWithSeveralClientsIsARosterRatherThanAFailure() {
        // The shipped verb FAILED here — "`ops` cannot pick one, pass --client with one of: …" — at exit 1, which
        // an agent had to recover from. A roster is a better answer at exit 0: it names each client with its
        // counts and the command that opens it, so the next call is addressed rather than guessed.
        Capture capture = new Capture();
        int exitCode = Cli.run(List.of("client", "ballerina/http"), capture.streams(),
                centralFor("ballerina__http", "2.16.6"));
        Assert.assertEquals(exitCode, 0, capture.stderr());
        for (String name : List.of("Client", "FailoverClient", "LoadBalanceClient", "StatusCodeClient")) {
            Assert.assertTrue(capture.stdout().contains("`" + name + "`"), name);
            Assert.assertTrue(capture.stdout().contains("`bal library client ballerina/http " + name + "`"),
                    name);
        }
    }

    @Test
    public void namingAContainerResolvesIt() {
        Capture capture = new Capture();
        int exitCode = Cli.run(List.of("client", "ballerina/http", "FailoverClient"),
                capture.streams(), centralFor("ballerina__http", "2.16.6"));
        Assert.assertEquals(exitCode, 0, capture.stderr());
        Assert.assertTrue(capture.stdout().contains("`FailoverClient`"));
    }

    @Test
    public void namingSomethingNoVerbHoldsFailsWithCandidates() {
        // Kind tolerance is tried first — every other scope, then the declaration roster — so reaching a failure
        // means the name really is not in the package. The candidates are the closest names it does hold.
        Capture capture = new Capture();
        int exitCode = Cli.run(List.of("client", "ballerina/http", "FailoverClientt"), capture.streams(),
                centralFor("ballerina__http", "2.16.6"));
        Assert.assertEquals(exitCode, 1);
        Assert.assertEquals(capture.stdout(), "");
        Assert.assertEquals(capture.field("kind"), "symbol-not-found");
        Assert.assertTrue(capture.failure().getAsJsonArray("candidates").toString()
                .contains("FailoverClient"), capture.stderr());
        // And the suggestion never rebuilds the command without the argument that failed.
        Assert.assertTrue(capture.field("suggestion").contains("FailoverClientt"), capture.stderr());
    }

    @Test
    public void aPackageWithNoModuleFunctionsGetsAnHonestEmptyReportAtExit0() {
        Capture capture = new Capture();
        int exitCode = Cli.run(List.of("funcs", "ballerinax/kafka"), capture.streams(),
                centralFor("ballerinax__kafka", "4.6.5"));
        Assert.assertEquals(exitCode, 0, capture.stderr());
        Assert.assertTrue(capture.stdout().contains("| Module functions | this package declares none |"),
                capture.stdout());
        Assert.assertTrue(capture.stdout().contains("`bal library client ballerinax/kafka`"),
                capture.stdout());
    }

    @Test
    public void resolvingTypesSwitchesTheDocumentToTheCodeRegister() {
        // The amended ADR-0008: the register is a property of the DOCUMENT, not the verb. A `-r` answer is nothing
        // but declarations, so it is pasteable whole even though a report verb reached it.
        Capture capture = new Capture();
        int exitCode = Cli.run(
                List.of("client", "ballerinax/kafka", "Producer", "send", "-r"),
                capture.streams(), centralFor("ballerinax__kafka", "4.6.5"));
        Assert.assertEquals(exitCode, 0, capture.stderr());
        opensWith(capture.stdout(), "// ballerinax/kafka:4.6.5 ·", "");
        // A fence at line start would be this document's own structure; one inside a `#` doc comment is the
        // package author's sample, and kafka documents `producer->send` exactly that way.
        Assert.assertFalse(Pattern.compile("^\\s*```", Pattern.MULTILINE).matcher(capture.stdout()).find(),
                "no fences of our own: the whole document is Ballerina");
        Assert.assertFalse(capture.stdout().contains("<!-- bal library"), "no report format marker");
        Assert.assertFalse(capture.stdout().contains("\n| "), "no Markdown tables");
        Assert.assertTrue(capture.stdout().contains("remote function send("), capture.stdout());
    }

    @Test
    public void findIsMarkdownThatDisclosesItsOwnOrdering() {
        String body = """
                {"count": 42, "packages": [
                  {"organization":"nobody","name":"kafka.helper","version":"0.1.0","pullCount":2},
                  {"organization":"ballerinax","name":"kafka","version":"4.6.5","summary":"Kafka","pullCount":60747}
                ]}
                """;
        Capture capture = new Capture();
        int exitCode = Cli.run(List.of("find", "kafka", "messaging"), capture.streams(),
                options(FakeTransport.always(FakeTransport.ok(body))));
        Assert.assertEquals(exitCode, 0, capture.stderr());
        Assert.assertEquals(capture.stderr(), "");
        opensWith(capture.stdout(), "<!-- bal library find v1 ·", "# Find — `kafka messaging`");
        // The order is partly ours, so the document says which part rather than presenting it all as Central's.
        Assert.assertTrue(capture.stdout().contains(
                "| Order | Central's relevance, with packages under 1,000 pulls moved to the end."),
                capture.stdout());
        Assert.assertTrue(capture.stdout().indexOf("ballerinax/kafka")
                < capture.stdout().indexOf("nobody/kafka.helper"));
        // The count is printed beside every hit, because that is what makes the demotion checkable rather than
        // something the caller has to take on trust.
        Assert.assertTrue(capture.stdout().contains("60,747 pulls"));
    }

    @Test
    public void findWithNoMatchesSaysSoAndStillPointsSomewhere() {
        Capture capture = new Capture();
        int exitCode = Cli.run(List.of("find", "nosuchthing"), capture.streams(),
                options(FakeTransport.always(FakeTransport.ok("{\"count\":0,\"packages\":[]}"))));
        Assert.assertEquals(exitCode, 0);
        Assert.assertTrue(capture.stdout().contains("\n## No packages matched\n"));
        // A real gap in the shipped verb: zero matches printed a sentence and no way forward.
        Assert.assertTrue(capture.stdout().contains("bal library find nosuchthing"), capture.stdout());
    }

    @Test
    public void findReportsAnUpstreamFailureAtExit1() {
        Capture capture = new Capture();
        int exitCode = Cli.run(List.of("find", "kafka"), capture.streams(),
                HttpOptions.builder()
                        .transport(FakeTransport.always(FakeTransport.status(503)))
                        .maxAttempts(1)
                        .baseDelayMs(1)
                        .sleeper(millis -> { })
                        .build());
        Assert.assertEquals(exitCode, 1);
        Assert.assertEquals(capture.stdout(), "");
        Assert.assertEquals(capture.field("kind"), "upstream");
    }

    // -----------------------------------------------------------------------
    // Version resolution
    // -----------------------------------------------------------------------

    @Test
    public void anUnknownPackageExits1AndNamesItself() {
        // Rather than printing nothing at 0.
        Capture capture = new Capture();
        int exitCode = Cli.run(List.of("overview", "ballerinax/nope"), capture.streams(),
                HttpOptions.builder()
                        .transport(FakeTransport.always(FakeTransport.ok("[]")))
                        .maxAttempts(1)
                        .baseDelayMs(1)
                        .sleeper(millis -> { })
                        .build());
        Assert.assertEquals(exitCode, 1);
        Assert.assertEquals(capture.stdout(), "");
        Assert.assertEquals(capture.field("kind"), "package-not-found");
    }

    @Test
    public void aLockedDependenciesTomlVersionOutranksCentralsLatest() {
        Path projectDir = tempDir();
        write(projectDir.resolve("Ballerina.toml"), "[package]\nname = \"app\"\n");
        write(projectDir.resolve("Dependencies.toml"),
                "[[package]]\norg = \"ballerinax\"\nname = \"sap\"\nversion = \"1.3.1\"\n");

        String payload = FixtureCorpus.loadRawFixture("ballerinax__sap").toString();
        Capture capture = new Capture();
        int exitCode = Cli.run(
                List.of("api", "ballerinax/sap"),
                capture.streams(),
                options(FakeTransport.routing(url -> {
                    // Reaching the versions endpoint would mean the lock was ignored.
                    Assert.assertTrue(url.contains("/docs/"), url);
                    return FakeTransport.ok(payload);
                })),
                projectDir.toString());
        Assert.assertEquals(exitCode, 0, capture.stderr());
        opensWith(capture.stdout(), "// Resolved: ballerinax/sap:1.3.1 ·", "");
        // And nothing in the document discloses the resolution. It is the tool's job, not a fact the caller has to
        // reconcile — which is what "no version syntax and no disclosure" means in practice.
        Assert.assertFalse(capture.stdout().contains("Dependencies.toml"), capture.stdout());
        Assert.assertFalse(capture.stdout().contains("--version"), capture.stdout());
    }

    @Test
    public void aLockedVersionCentralDoesNotPublishNamesTheOnesItDoes() {
        // T10, closed in the failure rather than in the grammar. "Verify the version is published" was advice
        // naming a step no verb let the caller take; with no version argument left, the answer has to be the list.
        Path projectDir = tempDir();
        write(projectDir.resolve("Ballerina.toml"), "[package]\nname = \"app\"\n");
        write(projectDir.resolve("Dependencies.toml"),
                "[[package]]\norg = \"ballerinax\"\nname = \"sap\"\nversion = \"9.9.9\"\n");

        Capture capture = new Capture();
        int exitCode = Cli.run(
                List.of("api", "ballerinax/sap"),
                capture.streams(),
                options(FakeTransport.routing(url -> url.contains("/docs/")
                        ? FakeTransport.status(404)
                        : FakeTransport.ok("[\"1.3.1\", \"1.3.0\"]"))),
                projectDir.toString());
        Assert.assertEquals(exitCode, 1);
        Assert.assertEquals(capture.stdout(), "");
        Assert.assertEquals(capture.field("kind"), "package-not-found");
        Assert.assertTrue(capture.field("suggestion").contains("published versions are 1.3.1, 1.3.0"),
                capture.stderr());
    }

    @Test
    public void aPackageWithNoGuideStillRendersEveryOtherSection() {
        JsonElement stripped = FixtureCorpus.loadRawFixture("ballerinax__sap");
        stripped.getAsJsonObject().getAsJsonObject("docsData").getAsJsonArray("modules")
                .forEach(module -> module.getAsJsonObject().remove("description"));

        Capture capture = new Capture();
        int exitCode = Cli.run(List.of("overview", "ballerinax/sap"), capture.streams(),
                options(FakeTransport.routing(url -> url.contains("/docs/")
                        ? FakeTransport.ok(stripped.toString())
                        : FakeTransport.ok("[\"1.3.1\"]"))));
        Assert.assertEquals(exitCode, 0, capture.stderr());
        // No guide means no `## Usage`, since there is nothing to quote — and nothing else about the document
        // changes. The Document row says so rather than leaving the absence to be inferred.
        Assert.assertFalse(capture.stdout().contains("\n## Quickstart\n"));
        Assert.assertTrue(capture.stdout().contains("| Guide | none published |"), capture.stdout());
        Assert.assertTrue(capture.stdout().contains("\n## Clients — 1\n"), "the map half is unaffected");
    }

    // -----------------------------------------------------------------------
    // The stream contract
    // -----------------------------------------------------------------------

    @Test
    public void aFailingRunLeavesStdoutEmptyAndStderrHoldingExactlyOneJsonObject() {
        // Both halves matter to a caller that redirects: a partial document under a non-zero exit is worse than
        // none, and a usage block beside the JSON would break every parser.
        List<List<String>> failures = List.of(
                List.of("overview", "not-a-package"),
                List.of("type", "ballerinax/kafka"),
                List.of("nonsense"),
                List.of("overview", "ballerinax/kafka", "-r"));
        for (List<String> argv : failures) {
            Capture capture = new Capture();
            int exitCode = Cli.run(argv, capture.streams(), never());
            Assert.assertEquals(exitCode, 1, String.join(" ", argv));
            Assert.assertEquals(capture.stdout(), "", String.join(" ", argv));
            Assert.assertEquals(capture.field("kind"), "validation", String.join(" ", argv));
            Assert.assertTrue(capture.stderr().endsWith("}\n"), String.join(" ", argv));
        }
    }

    /**
     * ADR-0015. There are two codes, and the second one is every failure.
     *
     * <p>What replaced the old split is the {@code kind} field, asserted here beside each code: an argument
     * error, an upstream error and an unresolved name are one code and three kinds. The split they used to have
     * was inverted on three of the four paths that mattered — {@code --help} failed at 2, and
     * {@code package-not-found} said "retryable" for a name no retry can fix — and no caller noticed, because
     * a piped {@code $?} is the pipe's.
     */
    @Test
    public void everyFailureIsExitOneAndTheKindSaysWhatWentWrong() {
        record Case(String kind, List<String> argv, HttpOptions http) { }
        HttpOptions missing = HttpOptions.builder()
                .transport(FakeTransport.always(FakeTransport.status(400)))
                .maxAttempts(1).baseDelayMs(1).sleeper(millis -> { }).build();
        HttpOptions broken = HttpOptions.builder()
                .transport(FakeTransport.always(FakeTransport.status(500)))
                .maxAttempts(1).baseDelayMs(1).sleeper(millis -> { }).build();
        HttpOptions kafka = centralFor("ballerinax__kafka", "4.6.5");

        Assert.assertEquals(Cli.run(List.of("overview", "ballerinax/kafka"), new Capture().streams(), kafka), 0);
        Assert.assertEquals(Cli.run(List.of("--help"), new Capture().streams(), never()), 0);

        List<Case> failures = List.of(
                new Case("package-not-found", List.of("overview", "no-such-org/no-such-pkg"), missing),
                new Case("upstream", List.of("overview", "ballerinax/kafka"), broken),
                new Case("validation", List.of("overview", "ballerina/http:2.16.6"), never()),
                new Case("validation", List.of("overview", "ballerina/http", "-r"), never()),
                new Case("validation", List.of("nonsense"), never()),
                new Case("symbol-not-found", List.of("type", "ballerinax/kafka", "NoSuchType"), kafka));
        for (Case failure : failures) {
            Capture capture = new Capture();
            String label = String.join(" ", failure.argv());
            Assert.assertEquals(Cli.run(failure.argv(), capture.streams(), failure.http()), 1, label);
            Assert.assertEquals(capture.field("kind"), failure.kind(), label);
        }
    }

    // -----------------------------------------------------------------------
    // Failure text as golden files
    // -----------------------------------------------------------------------

    /**
     * The exact bytes of the failures an agent has to act on.
     *
     * <p>The structural assertions above check that a `kind` and a `suggestion` are there; these check what they
     * SAY. That is not redundant, and the reason is on record: the `search` help text once described a ranking the
     * design had measured and rejected, and every structural test stayed green. A golden file is what turns a
     * change in what the tool tells an agent into a reviewable diff.
     *
     * <p>One file per failure, each holding the single JSON line the run wrote to stderr.
     */
    @Test
    public void theFailureTextIsUnchanged() {
        record Case(String name, List<String> argv) { }
        List<Case> cases = List.of(
                new Case("unknown-verb", List.of("nonsense")),
                new Case("bare-package", List.of("ballerinax/github")),
                new Case("version-suffix", List.of("overview", "ballerinax/github:6.0.0")),
                new Case("foreign-flag", List.of("overview", "ballerinax/kafka", "-r")),
                new Case("unknown-flag", List.of("overview", "ballerina/http", "--nonesuch")),
                new Case("no-package", List.of("overview")),
                new Case("no-keywords", List.of("find")),
                new Case("no-type-names", List.of("type", "ballerinax/github")));

        for (Case failure : cases) {
            Capture capture = new Capture();
            Assert.assertEquals(Cli.run(failure.argv(), capture.streams(), never()), 1,
                    String.join(" ", failure.argv()));
            Assert.assertEquals(capture.stdout(), "", String.join(" ", failure.argv()));
            FixtureCorpus.matchesSnapshot(
                    Path.of("src", "test", "resources", "command-outputs", "unix",
                            "failure-" + failure.name() + ".json"),
                    capture.stderr(),
                    failure.name() + " failure text");
        }
    }

    @Test
    public void theSymbolNotFoundTextIsUnchanged() {
        // Kept apart from the argument failures because it needs a payload: the candidates are real near-misses
        // from a real declaration roster, which is the part of the message worth pinning.
        Capture capture = new Capture();
        Assert.assertEquals(Cli.run(List.of("type", "ballerinax/kafka", "TopicPartitio"), capture.streams(),
                centralFor("ballerinax__kafka", "4.6.5")), 1);
        Assert.assertEquals(capture.stdout(), "");
        FixtureCorpus.matchesSnapshot(
                Path.of("src", "test", "resources", "command-outputs", "unix", "failure-symbol-not-found.json"),
                capture.stderr(),
                "symbol-not-found failure text");
    }

    // -----------------------------------------------------------------------

    private static Path tempDir() {
        try {
            return Files.createTempDirectory("bal-library-");
        } catch (IOException cause) {
            throw new UncheckedIOException(cause);
        }
    }

    private static void mkdirs(Path path) {
        try {
            Files.createDirectories(path);
        } catch (IOException cause) {
            throw new UncheckedIOException(cause);
        }
    }

    private static void write(Path path, String contents) {
        try {
            Files.writeString(path, contents, StandardCharsets.UTF_8);
        } catch (IOException cause) {
            throw new UncheckedIOException(cause);
        }
    }
}
