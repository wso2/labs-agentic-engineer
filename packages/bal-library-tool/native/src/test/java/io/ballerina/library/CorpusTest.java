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

import org.testng.Assert;
import org.testng.annotations.DataProvider;
import org.testng.annotations.Test;

/**
 * Coverage across Ballerina libraries: every fixture renders to exactly the bytes in its snapshot.
 *
 * <p>THIS is the test that proves the port. The nine {@code .bal} files were produced by the TypeScript reader
 * and verified against real packages; nothing in this repository generated them. If this passes, the schema, the
 * IR, the seven patches and the renderer are all correct together against 927KB of oracle for
 * {@code ballerinax/github} alone.
 *
 * <p>Offline and deterministic, so it runs on every PR in seconds. What it cannot catch — Central changing under
 * us — is {@link KeySpaceTest}'s job.
 *
 * @since 0.1.0
 */
public class CorpusTest {

    @DataProvider(name = "fixtures")
    public Object[][] fixtures() {
        return FixtureCorpus.fixtureRows();
    }

    @Test
    public void theCorpusIsNotEmpty() {
        // A misconfigured glob would otherwise make this whole class pass vacuously.
        int found = FixtureCorpus.listFixtures().size();
        Assert.assertTrue(found >= 8, "expected the recorded corpus, found " + found + " fixtures");
    }

    /**
     * Routed through {@code matchesSnapshot} so this honours {@code UPDATE_SNAPSHOTS=1} like every other
     * snapshot test. Reading the file directly meant a NEWLY recorded fixture could never be enrolled — the
     * first run threw {@code NoSuchFileException} whether or not the escape hatch was set, and the only way
     * to add a package to the corpus was to hand-transcribe a megabyte of rendered Ballerina.
     */
    @Test(dataProvider = "fixtures")
    public void rendersExactlyAsSnapshotted(String slug) {
        FixtureCorpus.matchesSnapshot(
                FixtureCorpus.snapshotPath(slug), FixtureCorpus.renderFixture(slug), "rendered output for " + slug);
    }
}
