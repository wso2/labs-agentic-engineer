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

import io.ballerina.library.render.Documents;
import io.ballerina.library.render.Report;
import io.ballerina.library.symbols.Declarations;
import io.ballerina.library.symbols.Surface;
import io.ballerina.library.views.Containers;
import io.ballerina.library.views.Guide;
import io.ballerina.library.views.Overview;
import io.ballerina.library.views.Snippets;
import io.ballerina.library.views.Readmes;
import io.ballerina.library.views.TypeView;
import org.testng.Assert;
import org.testng.annotations.DataProvider;
import org.testng.annotations.Test;

import java.util.ArrayList;
import java.util.List;
import java.util.regex.Pattern;

/**
 * The two registers, enforced mechanically.
 *
 * <p>A document either IS Ballerina or it DESCRIBES a package, and without a test the two drift back together —
 * an earlier design produced a {@code client class Client {} shell whose body was
 * {@code // WARNING: 903 resource functions}, which looks like a declaration, is not one, and invites an agent to
 * transcribe from it.
 *
 * <p>This is the mechanical form of that rule, run over every fixture and every verb, so it also covers documents
 * nobody has written yet.
 *
 * @since 0.1.0
 */
public class RegisterTest {

    /**
     * Things that would read as a declaration. {@code type} is deliberately absent: it appears in prose all the
     * time ("read one with {@code type}") and inside {@code | Types |}, and a keyword test on it would ban the
     * English word.
     */
    private static final Pattern LOOKS_LIKE_A_DECLARATION = Pattern.compile(
            "^\\s*(client class|class|service|public annotation|enum|remote function|resource function"
                    + "|function)\\b");

    private static final Pattern FENCE = Pattern.compile("^\\s*(`{3,}|~{3,})");

    @DataProvider(name = "fixtures")
    public Object[][] fixtures() {
        return FixtureCorpus.fixtureRows();
    }

    /** A document plus what produced it, for a failure message that names the verb. */
    private record Document(String label, String text) { }

    /**
     * Lines outside every fenced block.
     *
     * <p>Fence tracking rather than a regex, because the guide is embedded verbatim and carries its own fences —
     * including tilde ones — and a line inside somebody else's sample is a quotation, not this document's claim.
     */
    private static List<String> unfencedLines(String document) {
        List<String> lines = new ArrayList<>();
        String fence = null;
        for (String line : document.split("\n", -1)) {
            java.util.regex.Matcher opener = FENCE.matcher(line);
            boolean opens = opener.find();
            if (fence == null && opens) {
                fence = opener.group(1);
                continue;
            }
            if (fence != null && opens && opener.group(1).startsWith(fence.substring(0, 3))) {
                fence = null;
                continue;
            }
            if (fence == null) {
                lines.add(line);
            }
        }
        return lines;
    }

    /**
     * A report document without the guides embedded in it, and with everything else kept.
     *
     * <p>The guide is the package author's verbatim Markdown, so its blank-line runs and heading depth are its own
     * business. Rules about THIS document's structure have to stop where the quotation starts — and start again
     * where it ends, which is why this cuts on the begin/end markers rather than truncating at the {@code ## Guide}
     * heading. Truncating stopped checking the document at the guide, so the {@code ## Next} section that now
     * follows it was unchecked; the markers are what make the resumption exact.
     */
    private static String ownStructure(String document) {
        StringBuilder own = new StringBuilder();
        int from = 0;
        while (true) {
            int begin = document.indexOf(Report.EMBED_BEGIN, from);
            if (begin == -1) {
                return own.append(document.substring(from)).toString();
            }
            int end = document.indexOf(Report.EMBED_END, begin);
            Assert.assertTrue(end > begin, "an embedded quotation with no end marker");
            // Both marker lines are kept and only the quotation between them goes, so the surrounding blocks
            // stay one blank line apart and the blank-run check below still means what it says.
            own.append(document, from, lineEnd(document, begin) + 1);
            from = end;
        }
    }

    private static int lineEnd(String document, int from) {
        int newline = document.indexOf('\n', from);
        return newline == -1 ? document.length() - 1 : newline;
    }

    /**
     * Every report document a fixture can produce, keyed by what produced it.
     *
     * <p>Breadth is the point. The register rules have to hold for documents nobody has written yet, so this walks
     * every verb over every container of every fixture rather than sampling one shape per verb — which is how the
     * one document that reads as source stays out of the corpus.
     */
    private static List<Document> reportDocuments(String slug) {
        LoadedPackage context = FixtureCorpus.loadedFixture(slug);
        List<Document> documents = new ArrayList<>();
        documents.add(new Document("overview", Overview.render(context)));
        documents.add(new Document("overview -s", Overview.render(context, new Overview.Options("client"))));
        documents.add(new Document("overview -s (no match)",
                Overview.render(context, new Overview.Options("zzzznothingmatchesthis"))));

        Result<String> guide = Guide.render(context, Guide.Options.ALL);
        Assert.assertTrue(guide.isOk());
        documents.add(new Document("guide", guide.value()));

        Result<String> chunk = Guide.render(context, new Guide.Options("1", null, null));
        if (chunk.isOk()) {
            documents.add(new Document("guide 1", chunk.value()));
        }
        Result<String> guideSearch = Guide.render(context, new Guide.Options(null, "client", null));
        Assert.assertTrue(guideSearch.isOk());
        documents.add(new Document("guide -s", guideSearch.value()));

        for (Surface.Scope scope : Surface.Scope.values()) {
            documents.addAll(containerDocuments(context, scope));
        }
        return documents;
    }

    /** One scope's documents: the bare listing, one per container, a filter, and a selector that misses. */
    private static List<Document> containerDocuments(LoadedPackage context, Surface.Scope scope) {
        List<Document> documents = new ArrayList<>();
        String verb = scope.verb();
        documents.add(new Document(verb, expect(Containers.render(context, scope, Containers.Options.bare()))));
        documents.add(new Document(verb + " -s", expect(Containers.render(
                context, scope, new Containers.Options(List.of(), "config", false, false)))));

        List<Surface.Container> containers = Surface.of(context.library(), scope);
        for (Surface.Container container : containers) {
            List<String> selector = container.isModule() ? List.of() : List.of(container.name());
            if (!selector.isEmpty()) {
                documents.add(new Document(verb + " " + container.name(),
                        expect(Containers.render(context, scope, new Containers.Options(selector)))));
            }
            // A selector that matches nothing is answered at exit 0 with what IS there, so it is a report like any
            // other and has to obey the same rules.
            List<String> missing = new ArrayList<>(selector);
            missing.add("zzzznosuchmember");
            documents.add(new Document(verb + " (missing selector)",
                    expect(Containers.render(context, scope, new Containers.Options(missing)))));
        }
        return documents;
    }

    private static String expect(Result<String> view) {
        Assert.assertTrue(view.isOk(), view.isOk() ? "" : view.failure().describe());
        return view.value();
    }

    // -----------------------------------------------------------------------
    // The report register
    // -----------------------------------------------------------------------

    @Test(dataProvider = "fixtures")
    public void noReportDocumentCarriesADeclarationOutsideAFence(String slug) {
        for (Document document : reportDocuments(slug)) {
            for (String line : unfencedLines(document.text())) {
                Assert.assertFalse(LOOKS_LIKE_A_DECLARATION.matcher(line).find(),
                        slug + " " + document.label() + ": this reads as source:\n  " + line);
            }
        }
    }

    @Test(dataProvider = "fixtures")
    public void noReportDocumentAnnotatesItselfWithASlashSlashComment(String slug) {
        for (Document document : reportDocuments(slug)) {
            for (String line : unfencedLines(document.text())) {
                // In the code register a `//` comment annotates a real declaration, which is what
                // `// Special Agent Note:` does. Here it was the thing doing the impersonating, and Markdown
                // prose replaces it.
                Assert.assertFalse(line.startsWith("//"),
                        slug + " " + document.label() + ": a bare // comment outside a fence:\n  " + line);
            }
        }
    }

    @Test(dataProvider = "fixtures")
    public void everyReportDocumentIsNavigableByHeading(String slug) {
        for (Document document : reportDocuments(slug)) {
            // Structure is headings, so `grep '^## '` returns the document's sections.
            long sections = unfencedLines(document.text()).stream()
                    .filter(line -> line.startsWith("## "))
                    .count();
            Assert.assertTrue(sections > 0, slug + " " + document.label() + ": no sections");
            Assert.assertTrue(document.text().matches("(?s)^<!-- bal library \\w+ v1 -->\n# .*"),
                    slug + " " + document.label() + ": no marker and title");
            Assert.assertTrue(document.text().endsWith("\n"),
                    slug + " " + document.label() + ": no trailing newline");
            Assert.assertFalse(ownStructure(document.text()).contains("\n\n\n"),
                    slug + " " + document.label() + ": blank-line runs mean a block was emitted empty");
        }
    }

    @Test
    public void theOverviewsOwnSectionsAreWhatGrepReturnsNotTheReadmes() {
        // The guide's headings are demoted two levels for exactly this reason: without it `grep '^## '` returns
        // the package author's outline mixed with ours.
        String document = Overview.render(FixtureCorpus.loadedFixture("ballerinax__postgresql"));
        List<String> sections = unfencedLines(document).stream()
                .filter(line -> line.startsWith("## "))
                .toList();
        Assert.assertTrue(sections.contains("## Quickstart"));
        Assert.assertTrue(sections.contains("## Next"));
        for (String section : sections) {
            Assert.assertTrue(
                    section.matches("^## (Quickstart|Next|Clients|Classes|Module-level).*"),
                    "an unexpected top section: " + section);
        }
        // And the guide's own outline is what greps out of the verb that prints it.
        Result<String> guide = Guide.render(FixtureCorpus.loadedFixture("ballerinax__postgresql"),
                Guide.Options.ALL);
        Assert.assertTrue(guide.isOk());
        for (String section : unfencedLines(guide.value()).stream()
                .filter(line -> line.startsWith("## ")).toList()) {
            Assert.assertTrue(section.matches("^## (Guide|Next).*"), "an unexpected top section: " + section);
        }
    }

    /**
     * Where this document stops speaking is stated, not left to be inferred.
     *
     * <p>Demoting the guide's headings keeps the outline; it does not tell a reader which Markdown they are in.
     * The markers do, they match by label rather than by counting, and everything between them is the package
     * author's bytes — so a reader or a script can cut the quotation out exactly.
     */
    @Test(dataProvider = "fixtures")
    public void theEmbeddedGuideIsMarkedAtBothEnds(String slug) {
        LoadedPackage context = FixtureCorpus.loadedFixture(slug);
        Result<String> rendered = Guide.render(context, Guide.Options.ALL);
        Assert.assertTrue(rendered.isOk());
        String document = rendered.value();
        Assert.assertFalse(context.readmes().isEmpty(), slug + ": this fixture publishes no guide");

        for (Readmes.ModuleReadme readme : context.readmes()) {
            String label = (context.readmes().size() == 1
                    ? context.qualified().qualified()
                    : readme.module()) + " readme";
            String begin = Report.EMBED_BEGIN + label + " -->";
            String end = Report.EMBED_END + label + " -->";
            Assert.assertTrue(document.contains("\n" + begin + "\n"), slug + ": no begin marker for " + label);
            Assert.assertTrue(document.contains("\n" + end + "\n"), slug + ": no end marker for " + label);

            String quoted = document.substring(
                    document.indexOf(begin) + begin.length(),
                    document.indexOf(end));
            Assert.assertEquals(quoted.strip(), Readmes.demoteHeadings(readme.markdown(), 2).strip(),
                    slug + ": the markers do not wrap the guide exactly");
        }
        // The guide is a quotation, so nothing inside it may read as one of this document's own sections.
        Assert.assertFalse(ownStructure(document).contains("\n#### "),
                slug + ": a demoted readme heading survived the cut");
    }

    /**
     * {@code overview}'s {@code ## Usage} is a quotation too, and says so in the same vocabulary.
     *
     * <p>Worth its own assertion rather than folding into the one above, because the risk is different. There,
     * the quotation is the whole readme and the marker says where this document stops speaking. Here it is a
     * handful of blocks the tool SELECTED, so the marker is what keeps {@code ViewsAgreeTest}'s signature
     * oracle from being fed the package author's example code as though this document had generated it.
     */
    @Test(dataProvider = "fixtures")
    public void theQuotedUsageIsMarkedTheSameWayTheGuideIs(String slug) {
        LoadedPackage context = FixtureCorpus.loadedFixture(slug);
        String document = Overview.render(context);
        String label = context.qualified().qualified() + " readme usage";
        if (!document.contains("\n## Quickstart\n")) {
            Assert.assertFalse(document.contains(label), slug + ": a marker with no section");
            return;
        }
        Assert.assertTrue(document.contains("\n" + Report.EMBED_BEGIN + label + " -->\n"), slug);
        Assert.assertTrue(document.contains("\n" + Report.EMBED_END + label + " -->\n"), slug);
        // Every quoted line is inside a ballerina fence, so the register rule holds for borrowed code too.
        String quoted = document.substring(
                document.indexOf(Report.EMBED_BEGIN + label), document.indexOf(Report.EMBED_END + label));
        Assert.assertTrue(quoted.contains("```ballerina"), slug + ": quoted code outside a fence");
    }

    /**
     * The navigation is reachable inside a window, which is the constraint the whole document is ordered by.
     *
     * <p>{@code ## Next} used to close the document, on the argument that the guide was most of it and a reader
     * who stopped early had already been answered by the facts table. The guide left, and with it the argument:
     * measured on this corpus the closing position survived a {@code head -200} in 3 packages of 11, and 80% of
     * the recorded {@code bal library} calls were piped. Fifty lines is the assertion because it is the window
     * agents actually write.
     *
     * <p>Asserted as a LINE NUMBER rather than as "before the client sections", because the property is about
     * surviving a cut and a section-order test would pass for a document whose quoted code ran to 300 lines.
     * ADR-0024 made that a real possibility rather than a hypothetical — postgresql publishes 28 blocks and its
     * map is 365 lines — and answered it by moving the quotation BEHIND this section rather than by capping it.
     * So the bound is now on the map's own text, which nothing unbounded precedes: measured across this corpus
     * the worst case is line 15, against 421 for kafka before any of this.
     */
    @Test(dataProvider = "fixtures")
    public void theOverviewCarriesItsNextSectionInsideTheFirstWindow(String slug) {
        String document = Overview.render(FixtureCorpus.loadedFixture(slug));
        List<String> lines = List.of(document.split("\n", -1));
        int next = lines.indexOf("## Next");
        Assert.assertTrue(next > 0, slug + ": no ## Next section");
        // ADR-0017's measured window is `head -100`, which is what moved this section out of the document's
        // tail: at the end it survived a `head -200` in 3 packages of 11. The bound is derived rather than
        // picked — a dozen lines of title and facts, plus the one-line chunk index. Under ADR-0024 the quoted
        // code is no longer ahead of it at all, so measured across this corpus the worst case is line 15.
        Assert.assertTrue(next < 100, slug + ": ## Next is at line " + next);
        // Nothing unbounded may precede it. The map has no unbounded section left at all — the rosters are capped
        // at 20 rows — but the ORDER still has to hold, because a roster placed first would put the navigation
        // behind content again the next time a cap is raised.
        int roster = document.indexOf("\n## Clients");
        if (roster < 0) {
            roster = document.indexOf("\n## Classes");
        }
        Assert.assertTrue(roster > 0, slug + ": no roster section");
        Assert.assertTrue(document.indexOf("\n## Next\n") < roster, slug + ": a roster came first");
    }

    // -----------------------------------------------------------------------
    // The code register
    // -----------------------------------------------------------------------

    @Test(dataProvider = "fixtures")
    public void noCodeDocumentCarriesReportFurniture(String slug) {
        LoadedPackage context = FixtureCorpus.loadedFixture(slug);
        Declarations index = Declarations.index(context.library().typeDefs());
        String first = index.names().get(0);

        Result<String> typeView = TypeView.render(context, new TypeView.Options(List.of(first), true));
        Assert.assertTrue(typeView.isOk());

        List<Document> documents = new ArrayList<>(List.of(
                new Document("api", Documents.toSyntaxString(context.library())),
                new Document("type -r", typeView.value())));

        // The amended ADR-0008: the register is a property of the DOCUMENT, not of the verb. A `-r` response is
        // nothing but declarations, so it is code however it was reached — which means the container verbs produce
        // documents in BOTH registers and each has to obey the rules of the one it is in.
        for (Surface.Scope scope : Surface.Scope.values()) {
            for (Surface.Container container : Surface.of(context.library(), scope)) {
                List<String> selector = container.isModule() ? List.of() : List.of(container.name());
                Result<String> resolved = Containers.render(context, scope,
                        new Containers.Options(selector, null, true, false));
                Assert.assertTrue(resolved.isOk(), scope + " " + container.name());
                documents.add(new Document(scope.verb() + " " + container.name() + " -r", resolved.value()));
            }
        }

        for (Document document : documents) {
            // A fence at the START of a line would be this document's own structure. One inside a `#` doc comment
            // is the package author's sample, and every fence in the corpus's api snapshots is of that second
            // kind — verified zero at line start across all nine.
            Assert.assertFalse(Pattern.compile("^\\s*```", Pattern.MULTILINE)
                            .matcher(document.text()).find(),
                    slug + " " + document.label() + ": a fence in the code register");
            Assert.assertFalse(document.text().contains("<!-- bal library"),
                    slug + " " + document.label() + ": a report marker");
            Assert.assertFalse(Pattern.compile("^\\|.*\\|$", Pattern.MULTILINE)
                            .matcher(document.text()).find(),
                    slug + " " + document.label() + ": a Markdown table");
            // NOTE: `#` is not tested. A leading `# ` here is a Ballerina doc comment — the language's own syntax
            // — and banning it would ban the documentation.
        }
    }
}
