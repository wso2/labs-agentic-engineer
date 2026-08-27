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
import org.testng.Assert;
import org.testng.annotations.DataProvider;
import org.testng.annotations.Test;

/**
 * Central's payload shape, snapshotted.
 *
 * <p>Offline this asserts the recorded fixtures still describe what the schema expects. Its real value is as the
 * LIVE drift check: re-record the corpus against Central and this diff names every field that appeared, vanished
 * or moved — including the ones the reader does not read yet, which is precisely the class of change a schema that
 * strips unknown keys cannot see.
 *
 * <p>{@code BAL_LIBRARY_UPDATE_KEYSPACE=1 ./gradlew :native:test} rewrites the snapshot after a refresh.
 *
 * @since 0.1.0
 */
public class KeySpaceTest {

    @DataProvider(name = "fixtures")
    public Object[][] fixtures() {
        return FixtureCorpus.fixtureRows();
    }

    @Test
    public void thePayloadKeySpaceIsUnchanged() {
        String rendered = FixtureCorpus.renderKeySpace();
        if (FixtureCorpus.updatingKeyspace()) {
            FixtureCorpus.write(FixtureCorpus.KEYSPACE_SNAPSHOT, rendered);
            return;
        }
        String difference = FixtureCorpus.firstDifference(
                FixtureCorpus.read(FixtureCorpus.KEYSPACE_SNAPSHOT), rendered);
        Assert.assertNull(difference,
                "Central's payload shape changed. Review the diff, extend CentralDocs if the new field "
                        + "matters, then re-record with BAL_LIBRARY_UPDATE_KEYSPACE=1.\n" + difference);
    }

    @Test(dataProvider = "fixtures")
    public void theFixtureStillSatisfiesTheSchema(String slug) {
        Result<CentralDocs> parsed = Schema.parse(FixtureCorpus.loadRawFixture(slug), slug);
        Assert.assertTrue(parsed.isOk(), parsed.isOk() ? "" : parsed.failure().describe());
    }

    /**
     * Central spells a rest field's absent name two ways, and both have to parse.
     *
     * <p>Every rest field in the corpus carries {@code "name": ""} — an empty string, which a required-string
     * check accepts. {@code ballerina/time} omits the key entirely, and requiring the string rejected the WHOLE
     * document over it: all six verbs, {@code guide} included, for a field that by definition has no name.
     * The corpus cannot catch this on its own, so the shape is reproduced here by deleting the key.
     */
    @Test
    public void aRestFieldParsesWhetherItsNameIsEmptyOrAbsent() {
        JsonObject withEmptyName = FixtureCorpus.loadRawFixture("ballerina__http").getAsJsonObject();
        JsonObject restField = firstRestField(withEmptyName);
        Assert.assertEquals(restField.get("name").getAsString(), "",
                "the corpus is expected to carry the empty-string spelling");
        Assert.assertTrue(Schema.parse(withEmptyName, "empty-name").isOk());

        restField.remove("name");
        Result<CentralDocs> absent = Schema.parse(withEmptyName, "absent-name");
        Assert.assertTrue(absent.isOk(), absent.isOk() ? "" : absent.failure().describe());
    }

    /** The relaxation is scoped to rest fields: an ordinary field with no name is still drift. */
    @Test
    public void anOrdinaryFieldWithNoNameIsStillDrift() {
        JsonObject payload = FixtureCorpus.loadRawFixture("ballerina__http").getAsJsonObject();
        JsonObject ordinary = firstDeclaredField(payload);
        ordinary.remove("name");
        Assert.assertFalse(Schema.parse(payload, "no-name").isOk(),
                "a named field losing its name has to keep failing");
    }

    private static JsonObject firstRestField(JsonObject payload) {
        return findField(payload, true);
    }

    private static JsonObject firstDeclaredField(JsonObject payload) {
        return findField(payload, false);
    }

    /** The first record field that is, or is not, a rest field. */
    private static JsonObject findField(JsonObject payload, boolean rest) {
        for (JsonElement module : payload.getAsJsonObject("docsData").getAsJsonArray("modules")) {
            JsonArray records = module.getAsJsonObject().getAsJsonArray("records");
            if (records == null) {
                continue;
            }
            for (JsonElement record : records) {
                JsonArray fields = record.getAsJsonObject().getAsJsonArray("fields");
                if (fields == null) {
                    continue;
                }
                for (JsonElement field : fields) {
                    JsonObject candidate = field.getAsJsonObject();
                    JsonObject type = candidate.getAsJsonObject("type");
                    boolean isRest = type != null && type.has("isRestParam")
                            && type.get("isRestParam").getAsBoolean();
                    if (isRest == rest && (rest || candidate.has("name"))) {
                        return candidate;
                    }
                }
            }
        }
        throw new AssertionError("no " + (rest ? "rest" : "declared") + " field in the fixture");
    }
}
