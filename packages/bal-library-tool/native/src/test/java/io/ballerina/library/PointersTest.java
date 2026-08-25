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

import io.ballerina.library.central.HttpOptions;
import io.ballerina.library.central.HttpTransport;
import io.ballerina.library.cli.Cli;
import io.ballerina.library.symbols.Surface;
import io.ballerina.library.views.Containers;
import io.ballerina.library.views.Guide;
import io.ballerina.library.views.Overview;
import org.testng.Assert;
import org.testng.annotations.DataProvider;
import org.testng.annotations.Test;

import java.util.ArrayList;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Set;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

/**
 * Every command a document prints is RUN, and has to answer.
 *
 * <p>"A pointer that cannot answer is worse than no pointer" (ADR-0019) was a rule enforced by three separate
 * assertions about three separate bugs, each written after the fact: {@code overview} offered
 * {@code ops <pkg> <path>} to a package with no paths and got "none in any client" back, a two-call loop;
 * {@code ops}' signature bullet interpolated {@code (root)} — a DISPLAY token — into a command, so following the
 * tool's own instruction produced "the first segment is already wrong" under exit 0; and a
 * {@code symbol-not-found} suggestion rebuilt the command WITHOUT the argument that had failed, so an agent that
 * followed it ran the same wrong shape again.
 *
 * <p>All three are the same defect, and this is the general form of the test: extract every {@code bal library}
 * command from every document, run it through the real CLI against the recorded payload, and require exit 0.
 * A new pointer cannot be added wrong.
 *
 * <p>Two exclusions, both principled. A command containing an angle-bracket slot is a TEMPLATE — {@code <Name>} is
 * the grammar, not an argument — and a command naming a DIFFERENT package is an ADR-0009 cross-package edge, which
 * by design is not followed and whose payload this fixture cannot serve. Both are asserted as shapes rather than
 * silently skipped, so an exclusion cannot become a hiding place.
 *
 * @since 0.1.0
 */
public class PointersTest {

    /** A command as a document prints it, inside backticks. */
    private static final Pattern COMMAND = Pattern.compile("`(bal library [^`]+)`");

    @DataProvider(name = "fixtures")
    public Object[][] fixtures() {
        return FixtureCorpus.fixtureRows();
    }

    /** Central, replayed: the versions endpoint then the docs endpoint. */
    private static HttpOptions centralFor(String slug) {
        String docs = FixtureCorpus.loadRawFixture(slug).toString();
        HttpTransport transport = FakeTransport.routing(url -> url.contains("/docs/")
                ? FakeTransport.ok(docs)
                : FakeTransport.ok("[\"" + FixtureCorpus.FIXTURE_VERSION.text() + "\"]"));
        return HttpOptions.builder()
                .transport(transport)
                .baseDelayMs(1)
                .sleeper(millis -> {
                    // No test here asserts wall-clock behaviour.
                })
                .build();
    }

    /**
     * Every document a fixture can produce, from every verb that reads a package.
     *
     * <p>Deliberately the same breadth {@code RegisterTest} uses: a pointer printed only by the roster of a
     * package with 91 classes is exactly the one nobody checks by hand.
     */
    private static List<String> documentsOf(LoadedPackage context) {
        List<String> documents = new ArrayList<>();
        documents.add(Overview.render(context));
        documents.add(Overview.render(context, new Overview.Options("client")));
        documents.add(expect(Guide.render(context, Guide.Options.ALL)));
        Result<String> chunk = Guide.render(context, new Guide.Options("1", null, null));
        if (chunk.isOk()) {
            documents.add(chunk.value());
        }
        for (Surface.Scope scope : Surface.Scope.values()) {
            documents.add(expect(Containers.render(context, scope, Containers.Options.bare())));
            documents.add(expect(Containers.render(context, scope,
                    new Containers.Options(List.of(), "config", false, false))));
            for (Surface.Container container : Surface.of(context.library(), scope)) {
                if (container.isModule()) {
                    continue;
                }
                documents.add(expect(Containers.render(context, scope,
                        new Containers.Options(List.of(container.name())))));
                documents.add(expect(Containers.render(context, scope,
                        new Containers.Options(List.of(container.name(), "zzznosuchmember")))));
            }
        }
        return documents;
    }

    private static String expect(Result<String> view) {
        Assert.assertTrue(view.isOk(), view.isOk() ? "" : view.failure().describe());
        return view.value();
    }

    @Test(dataProvider = "fixtures")
    public void everyCommandADocumentPrintsRunsAndAnswers(String slug) {
        LoadedPackage context = FixtureCorpus.loadedFixture(slug);
        String pkg = context.qualified().qualified();
        HttpOptions http = centralFor(slug);

        Set<String> runnable = new LinkedHashSet<>();
        Set<String> templates = new LinkedHashSet<>();
        Set<String> foreign = new LinkedHashSet<>();
        for (String document : documentsOf(context)) {
            Matcher command = COMMAND.matcher(document);
            while (command.find()) {
                String text = command.group(1).trim();
                if (text.contains("<") || text.contains(">")) {
                    templates.add(text);
                } else if (!text.contains(" " + pkg)) {
                    foreign.add(text);
                } else {
                    runnable.add(text);
                }
            }
        }

        Assert.assertFalse(runnable.isEmpty(), slug + ": no document printed a runnable command");
        for (String text : runnable) {
            StringBuilder out = new StringBuilder();
            StringBuilder err = new StringBuilder();
            int code = Cli.run(argv(text), new Cli.Streams(out::append, err::append), http);
            Assert.assertEquals(code, 0, slug + ": `" + text + "` failed with " + err);
            Assert.assertFalse(out.toString().isBlank(), slug + ": `" + text + "` answered with nothing");
        }

        // A template is the grammar rather than an argument, so it has to LOOK like one: every angle-bracket slot
        // is a placeholder name, never a value that leaked out of a rendering.
        for (String text : templates) {
            Assert.assertTrue(Pattern.compile("<[A-Za-z][^>]*>").matcher(text).find(),
                    slug + ": `" + text + "` has an angle bracket that is not a slot");
        }
        // A foreign command is a cross-package edge (ADR-0009), so it has to name a real coordinate and this
        // package must not be it.
        for (String text : foreign) {
            Assert.assertTrue(Pattern.compile("bal library \\w+ [\\w.]+/[\\w.]+").matcher(text).find(),
                    slug + ": `" + text + "` names no package coordinate");
        }
    }

    /** Split a printed command into argv, honouring the single quotes a path selector needs. */
    private static List<String> argv(String command) {
        List<String> tokens = new ArrayList<>();
        Matcher token = Pattern.compile("'([^']*)'|\"([^\"]*)\"|(\\S+)").matcher(command);
        while (token.find()) {
            tokens.add(token.group(1) != null ? token.group(1)
                    : token.group(2) != null ? token.group(2) : token.group(3));
        }
        // `bal library` is the launcher, not an argument.
        Assert.assertEquals(tokens.subList(0, 2), List.of("bal", "library"), command);
        return tokens.subList(2, tokens.size());
    }
}
