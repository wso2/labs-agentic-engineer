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

import com.google.gson.JsonArray;
import com.google.gson.JsonObject;
import io.ballerina.library.central.schema.CentralDocs;
import io.ballerina.library.central.schema.Schema;
import io.ballerina.library.views.Readmes;
import org.testng.Assert;
import org.testng.annotations.DataProvider;
import org.testng.annotations.Test;

import java.util.List;

/**
 * The package's own guide: that it is there, that it is passed through untouched, and that embedding it does not
 * corrupt the code samples inside it.
 *
 * <p>The last one is the risk worth a test. The guide is the section an agent copies a working call out of, and a
 * heading transform that reached inside a fenced block would rewrite {@code #} comment lines in Ballerina, shell
 * and Python samples.
 *
 * @since 0.1.0
 */
public class ReadmeTest {

    @DataProvider(name = "fixtures")
    public Object[][] fixtures() {
        return FixtureCorpus.fixtureRows();
    }

    /** One module of an assembled payload. A {@code null} guide means the key is absent entirely. */
    private record Stub(String id, String guide) { }

    /**
     * One module's worth of payload, built by taking a real module and replacing only its guide — the schema
     * requires every array, so a hand-written stub could not be parsed.
     */
    private static CentralDocs docsWith(List<Stub> modules) {
        JsonObject template = FixtureCorpus.loadRawFixture("ballerinax__kafka")
                .getAsJsonObject().getAsJsonObject("docsData").getAsJsonArray("modules")
                .get(0).getAsJsonObject();

        JsonArray built = new JsonArray();
        for (Stub module : modules) {
            JsonObject entry = template.deepCopy().getAsJsonObject();
            entry.addProperty("id", module.id());
            if (module.guide() == null) {
                entry.remove("description");
            } else {
                entry.addProperty("description", module.guide());
            }
            built.add(entry);
        }
        JsonObject docsData = new JsonObject();
        docsData.add("modules", built);
        JsonObject wrapper = new JsonObject();
        wrapper.add("docsData", docsData);

        Result<CentralDocs> parsed = Schema.parse(wrapper, "assembled");
        Assert.assertTrue(parsed.isOk(), parsed.isOk() ? "" : parsed.failure().describe());
        return parsed.value();
    }

    @Test(dataProvider = "fixtures")
    public void everyPackageInTheCorpusPublishesAGuide(String slug) {
        // The overview leans on this: the guide is most packages' largest section and the answer to "how is this
        // used". A fixture without one would make the overview tests pass for the wrong reason.
        List<Readmes.ModuleReadme> readmes = Readmes.collect(FixtureCorpus.loadFixture(slug));
        Assert.assertTrue(readmes.size() >= 1, slug + " publishes no guide");
        Assert.assertTrue(readmes.get(0).markdown().length() > 500, slug + "'s guide is suspiciously short");
    }

    @Test
    public void theGuideIsPassedThroughUntouchedOnlyTrimmed() {
        CentralDocs docs = docsWith(List.of(new Stub("kafka", "\n\n## Overview\n\nbody\n\n")));
        Assert.assertEquals(Readmes.collect(docs),
                List.of(new Readmes.ModuleReadme("kafka", "## Overview\n\nbody")));
    }

    @Test
    public void aModuleWithoutAGuideIsDropped() {
        // Rather than carried as an empty section: a heading with nothing under it reads as a truncated download.
        CentralDocs docs = docsWith(List.of(
                new Stub("a", "   "), new Stub("b", null), new Stub("c", "real")));
        Assert.assertEquals(
                Readmes.collect(docs).stream().map(Readmes.ModuleReadme::module).toList(), List.of("c"));
    }

    @Test
    public void headingsAreDemotedSoTheHostDocumentKeepsOneOutline() {
        Assert.assertEquals(
                Readmes.demoteHeadings("# Top\n## Second\ntext", 2), "### Top\n#### Second\ntext");
    }

    @Test
    public void aHeadingCannotBeDemotedPastLevelSix() {
        // Because that stops being a heading: HTML has no h7.
        Assert.assertEquals(
                Readmes.demoteHeadings("##### Five\n###### Six", 2), "###### Five\n###### Six");
    }

    @Test
    public void aHashInsideAFencedBlockIsLeftAlone() {
        // Because it is a comment in someone's sample.
        String guide = String.join("\n", List.of(
                "## Setup", "", "```ballerina", "# The star count.", "int stars = 0;", "```", "", "## Next"));
        String demoted = Readmes.demoteHeadings(guide, 2);
        Assert.assertTrue(demoted.contains("\n#### Setup") || demoted.startsWith("#### Setup"));
        Assert.assertTrue(demoted.contains("#### Next"));
        Assert.assertTrue(demoted.contains("\n# The star count.\n"),
                "a Ballerina doc comment must survive verbatim");
    }

    @Test
    public void aTildeFenceCountsAsAFenceToo() {
        Assert.assertEquals(
                Readmes.demoteHeadings("~~~\n# not a heading\n~~~", 2), "~~~\n# not a heading\n~~~");
    }

    @Test
    public void aLineThatOnlyLooksLikeAHeadingIsLeftAlone() {
        // No space after the hashes, so it is not an ATX heading.
        Assert.assertEquals(
                Readmes.demoteHeadings("#hashtag\n####### seven", 2), "#hashtag\n####### seven");
    }
}
