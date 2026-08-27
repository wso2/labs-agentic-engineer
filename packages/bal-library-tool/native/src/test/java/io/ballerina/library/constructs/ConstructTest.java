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

package io.ballerina.library.constructs;

import io.ballerina.library.FixtureCorpus;
import org.testng.Assert;
import org.testng.annotations.DataProvider;
import org.testng.annotations.Test;

import java.util.ArrayList;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.TreeMap;
import java.util.regex.Pattern;
import java.util.stream.Collectors;

/**
 * Every Ballerina syntax dimension, one construct at a time.
 *
 * <p>The recorded corpus answers "did anything move?" for nine real packages. This answers the two
 * questions it cannot: WHICH construct moved, and what about constructs no package in the corpus happens
 * to use. Both matter because the fidelity register's fixes are staged — a stage changes how one family of
 * types renders and must leave the rest untouched, and a 20,000-line snapshot diff cannot show that.
 *
 * <p>Coverage tooling does not help here and is worth being explicit about: the suite already reaches 93%
 * of instructions, and most of the register's defects are on covered lines. {@code Schema.java:88} reading
 * a class as a bare name is executed by every fixture and is the cause of 340 hollow declarations. What was
 * missing was never reachability — it was an assertion about the language.
 *
 * <p>Failures come in two kinds and the message says which:
 *
 * <ul>
 *   <li><b>a regression</b> — a construct with no open finding changed, or one with an open finding changed
 *       into something that is still not its declaration. Revert or explain.</li>
 *   <li><b>a fix landed</b> — the output now equals the construct's real declaration. Promote the row:
 *       move {@code shouldRender} into {@code renders}, drop the finding, and tick it off in
 *       {@code docs/design/draft/bal-library-central-fidelity.md}.</li>
 * </ul>
 *
 * @since 0.1.0
 */
public class ConstructTest {

    /** A register ID: the package prefix the audit used, then a two-digit number. */
    private static final Pattern FINDING = Pattern.compile("^[A-Z0-9]+-[0-9]{2}$");

    /**
     * The syntax families this suite claims to cover. An assertion rather than a comment, so deleting the
     * last case of a family fails rather than quietly narrowing what the suite means.
     */
    private static final Set<String> DIMENSIONS = Set.of(
            "records", "fields", "types", "errors", "enums", "constants", "objects", "callables",
            "services", "annotations");

    @DataProvider(name = "constructs")
    public Object[][] constructs() {
        List<Construct> cases = Constructs.all();
        Object[][] rows = new Object[cases.size()][1];
        for (int index = 0; index < cases.size(); index++) {
            rows[index][0] = cases.get(index);
        }
        return rows;
    }

    /**
     * The regression net. One construct, one payload, one exact expected document body.
     *
     * <p>Per-case rather than one big snapshot on purpose: a fix to closed records fails
     * {@code records/closed} and nothing else, which is what makes "we did not break the others" a thing
     * the suite states rather than a thing the author hopes.
     */
    @Test(dataProvider = "constructs")
    public void aConstructRendersExactlyAsRecorded(Construct construct) {
        String actual = construct.actual();
        if (actual.equals(construct.renders())) {
            return;
        }
        Assert.fail(diagnose(construct, actual));
    }

    private static String diagnose(Construct construct, String actual) {
        boolean fixed = actual.equals(construct.shouldRender());
        StringBuilder message = new StringBuilder();
        message.append(construct.id()).append(" — ").append(construct.claim()).append("\n\n");
        if (fixed) {
            message.append("A FIX LANDED. The output is now the construct's real declaration, which is "
                            + "what ").append(construct.finding())
                    .append(" asked for.\nPromote this row: move `shouldRender` into `renders`, drop the "
                            + "finding, and tick ").append(construct.finding())
                    .append(" off in the fidelity register.\n");
            return message.toString();
        }
        message.append(construct.hasOpenGap()
                        ? "A REGRESSION, on a construct that was ALREADY wrong (" + construct.finding()
                                + "). It changed into something that is still not its declaration.\n"
                        : "A REGRESSION. This construct was faithful and no longer is.\n")
                .append("\n")
                .append(FixtureCorpus.firstDifference(construct.renders(), actual))
                .append("\n\nrecorded:\n")
                .append(indent(construct.renders()))
                .append("\nactual:\n")
                .append(indent(actual));
        if (construct.hasOpenGap()) {
            message.append("\nthe declaration it should be (").append(construct.finding()).append("):\n")
                    .append(indent(construct.shouldRender()));
        }
        return message.toString();
    }

    private static String indent(String text) {
        return text.lines().map(line -> "  | " + line).collect(Collectors.joining("\n")) + "\n";
    }

    // -----------------------------------------------------------------------
    // The matrix's own invariants
    // -----------------------------------------------------------------------

    @Test
    public void everySyntaxFamilyHasAtLeastOneCase() {
        Set<String> covered = Constructs.all().stream()
                .map(Construct::dimension)
                .collect(Collectors.toCollection(LinkedHashSet::new));
        Assert.assertEquals(covered, DIMENSIONS,
                "the suite's claimed coverage and its actual cases disagree");
    }

    @Test
    public void everyCaseIdIsUniqueAndEveryFindingIsWellFormed() {
        Set<String> ids = new LinkedHashSet<>();
        for (Construct construct : Constructs.all()) {
            Assert.assertTrue(ids.add(construct.id()), "duplicate construct id: " + construct.id());
            Assert.assertFalse(construct.claim().isBlank(), construct.id() + " states no claim");
            for (String finding : construct.findings()) {
                Assert.assertTrue(FINDING.matcher(finding).matches(),
                        construct.id() + " names " + finding + ", which is not a register ID");
            }
        }
    }

    /**
     * A construct with no open finding must be faithful, and one with a finding must have a gap. Cheap, and
     * it catches the tempting way to make a failure go away: nulling {@code shouldRender} so the case
     * asserts today's output and claims it is correct.
     */
    @Test
    public void aCaseWithNoFindingClaimsToBeFaithful() {
        for (Construct construct : Constructs.all()) {
            Assert.assertEquals(construct.hasOpenGap(), construct.finding() != null,
                    construct.id() + " disagrees with itself about whether it has an open finding");
        }
    }

    /**
     * Which findings have a construct-level test, by name.
     *
     * <p>A set rather than a count, so that landing a stage of the fix plan produces a reviewable diff
     * instead of a number nobody can check. This is also the honest statement of the suite's reach: ONE of
     * the register's defects is still open and pinned at the language level here. The rest of what remains is
     * about the addressed verbs, the cross-package footer, {@code overview}'s counts and the readmes —
     * properties of a view rather than of a construct — and they belong to {@code ViewsTest} and
     * {@code ViewsAgreeTest}.
     *
     * <p>A finding LEAVES this list when it is fixed. Stage 0 cleared IO-03, SLACK-08, SLACK-09 and SQL-08;
     * stage 2 cleared GITHUB-01, HTTP-01, HTTP-04, HTTP-06, HTTP-08, HTTP-13, KAFKA-01, PSQL-01, SAP-01,
     * SLACK-01, SLACK-03, SLACK-04, SLACK-05, SLACK-06, SLACK-07, SQL-04 and SQL-05; stage 3 cleared EMAIL-01,
     * HTTP-05, KAFKA-02, KAFKA-10, PSQL-03, SHEETS-02 and SQL-01; stage 4 cleared HTTP-03 and KAFKA-05; and
     * stage 5 cleared HTTP-07, SAP-05, SLACK-11 and SLACK-12. Their cases now assert the right answer as
     * {@code renders} and keep guarding it as a regression net. The register is where a cleared finding is
     * ticked off; this list only tracks what is still owed.
     *
     * <p>PSQL-04 stays, on the half of it that is not ours. Stage 5 rendered the member DESCRIPTIONS Central
     * publishes — 13 of the corpus's 65 members carry one. No member anywhere in the payload carries its
     * VALUE, and {@code VERIFY-CA}, {@code pgoutput} and {@code all_tables} appear zero times in
     * postgresql's payload and zero times on Central's own page, so {@code enums/member-values} pins a gap
     * that reading Central more carefully cannot close. It is here to keep saying so.
     */
    private static final List<String> PINNED = List.of("PSQL-04");

    @Test
    public void theOpenGapTallyIsWhatTheRegisterSays() {
        List<Construct> cases = Constructs.all();
        long faithful = cases.stream().filter(construct -> !construct.hasOpenGap()).count();
        long broken = cases.size() - faithful;

        Map<String, List<String>> byFinding = new TreeMap<>();
        for (Construct construct : cases) {
            for (String finding : construct.findings()) {
                byFinding.computeIfAbsent(finding, key -> new ArrayList<>()).add(construct.id());
            }
        }

        Assert.assertEquals(cases.size(), 86, "the construct matrix changed size");
        Assert.assertEquals(faithful, 85, "the number of constructs we render faithfully changed");
        Assert.assertEquals(broken, 1, "the number of constructs with an open finding changed");
        Assert.assertEquals(List.copyOf(byFinding.keySet()), PINNED,
                "the set of findings this suite pins changed; case index: " + byFinding);
    }
}
