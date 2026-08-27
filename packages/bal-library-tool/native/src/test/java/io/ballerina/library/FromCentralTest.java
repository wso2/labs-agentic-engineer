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
import com.google.gson.JsonElement;
import com.google.gson.JsonObject;
import io.ballerina.library.central.schema.CentralDocs;
import io.ballerina.library.central.schema.Schema;
import io.ballerina.library.model.Fn;
import io.ballerina.library.model.FromCentral;
import org.testng.Assert;
import org.testng.annotations.Test;

import java.util.List;
import java.util.Optional;

/**
 * Which module of a payload gets rendered.
 *
 * <p>Every fixture in the corpus is single-module, so this is the one behaviour the corpus cannot test by
 * construction — and the one the cache's coordinate check depends on, since verifying one module and rendering
 * another verifies nothing. The payloads here are therefore assembled rather than recorded.
 *
 * @since 0.1.0
 */
public class FromCentralTest {

    private static QualifiedName qualified(String name) {
        Result<QualifiedName> parsed = QualifiedName.parse(name);
        Assert.assertTrue(parsed.isOk(), name);
        return parsed.value();
    }

    /**
     * A recorded payload rearranged into several modules, so "the requested one" and "the first one" are different
     * answers. {@code kafka} is the donor because it is small and its module carries every array the schema
     * requires.
     */
    private static CentralDocs multiModule(List<String> ids, String org) {
        JsonObject raw = FixtureCorpus.loadRawFixture("ballerinax__kafka").getAsJsonObject();
        JsonElement template = raw.getAsJsonObject("docsData").getAsJsonArray("modules").get(0);

        JsonArray modules = new JsonArray();
        for (String id : ids) {
            JsonObject module = template.deepCopy().getAsJsonObject();
            module.addProperty("id", id);
            module.addProperty("orgName", org);
            module.addProperty("summary", "I am " + id);
            modules.add(module);
        }
        JsonObject docsData = new JsonObject();
        docsData.add("modules", modules);
        JsonObject wrapper = new JsonObject();
        wrapper.add("docsData", docsData);

        Result<CentralDocs> parsed = Schema.parse(wrapper, "assembled");
        Assert.assertTrue(parsed.isOk(), parsed.isOk() ? "" : parsed.failure().describe());
        return parsed.value();
    }

    @Test
    public void theRequestedModuleIsRendered() {
        // Not whichever one Central listed first.
        CentralDocs docs = multiModule(List.of("other", "kafka", "another"), "ballerinax");
        Result<CentralDocs.Module> selected = FromCentral.selectModule(docs, qualified("ballerinax/kafka"));
        Assert.assertTrue(selected.isOk());
        Assert.assertEquals(FromCentral.fromCentral(selected.value()).name(), "ballerinax/kafka");
        Assert.assertEquals(FromCentral.fromCentral(selected.value()).description(), "I am kafka");
    }

    @Test
    public void aSubmoduleIsReachedThroughItsDottedId() {
        // The way Central names it.
        CentralDocs docs = multiModule(List.of("googleapis", "googleapis.gmail"), "ballerinax");
        Result<CentralDocs.Module> selected =
                FromCentral.selectModule(docs, qualified("ballerinax/googleapis.gmail"));
        Assert.assertTrue(selected.isOk());
        Assert.assertEquals(
                FromCentral.fromCentral(selected.value()).name(), "ballerinax/googleapis.gmail");
    }

    @Test
    public void theOrgHasToMatchToo() {
        // So a same-named module from another org is not substituted.
        CentralDocs docs = multiModule(List.of("kafka"), "someoneelse");
        Assert.assertFalse(FromCentral.selectModule(docs, qualified("ballerinax/kafka")).isOk());
    }

    @Test
    public void noMatchingModuleFailsLoudlyAndNamesWhatCentralReturned() {
        CentralDocs docs = multiModule(List.of("notkafka"), "ballerinax");
        Result<CentralDocs.Module> selected = FromCentral.selectModule(docs, qualified("ballerinax/kafka"));
        Assert.assertFalse(selected.isOk());
        Failure.SchemaDrift failure = (Failure.SchemaDrift) selected.failure();
        Assert.assertTrue(failure.describe().contains("ballerinax/notkafka"));
        Assert.assertFalse(failure.suggestion().isEmpty());
    }

    @Test
    public void aResourcePathKeepsAParametersTypeAndNameApart() {
        // The mistake the language-server reader warns about in prose ("never merge them into one string") has no
        // field to live in.
        List<Fn.PathSegment> paths = FromCentral.createPaths(
                Optional.of("repos/[string owner]/[string repo]/code\\-scanning"));
        Assert.assertEquals(paths, List.of(
                new Fn.PathSegment.Literal("repos"),
                new Fn.PathSegment.Parameter("string", "owner"),
                new Fn.PathSegment.Parameter("string", "repo"),
                new Fn.PathSegment.Literal("code\\-scanning")));
    }

    @Test
    public void aBracketedSegmentWithNoSpaceInsideIsNotAParameter() {
        // Central emits an odd bracketed form without a type, and it stays a literal.
        Assert.assertEquals(FromCentral.createPaths(Optional.of("[\"quoted\"]")),
                List.of(new Fn.PathSegment.Literal("[\"quoted\"]")));
        Assert.assertEquals(FromCentral.createPaths(Optional.empty()), List.of());
    }
}
