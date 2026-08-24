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

/**
 * One Ballerina language construct, the Central payload that describes it, and the two answers.
 *
 * <p>{@code renders} is what the reader prints TODAY. {@code shouldRender} is what the construct's own
 * Ballerina declaration is. Where they are equal the case carries {@code shouldRender = null} and is a
 * plain regression guard; where they differ the case names the finding in
 * {@code docs/design/draft/bal-library-central-fidelity.md} that explains the difference.
 *
 * <p>Holding both is the design decision worth explaining. A suite that asserted only the correct answer
 * would be 30-odd failures on a green checkout, so it would be switched off; a suite that asserted only
 * today's output would go green after a fix that changed nothing and give no clue what the fix owed. With
 * both, a checkout is green, a construct that moves for the wrong reason fails by name, and a construct
 * that moves for the RIGHT reason fails too — with a message saying the finding closed and the row should
 * be promoted. That last failure is the point: it is how a stage of the fix plan proves it did what it
 * claimed to a construct nobody had to remember to re-check.
 *
 * @param id a stable dotted name, {@code dimension/case}, used in failure messages
 * @param dimension which syntax family this belongs to, for the per-dimension coverage assertion
 * @param claim one line stating what the case establishes, in the vocabulary of the language
 * @param finding the register IDs explaining the gap, comma-separated, or {@code null} when the output is
 *     already faithful. Plural because a construct is sometimes wrong in more than one way at once — an
 *     error's detail argument is dropped AND its intersection loses the separating space — and naming only
 *     one of them would leave the other with no test
 * @param payload the synthetic Central payload — exactly one construct
 * @param renders what the reader prints today, verbatim
 * @param shouldRender the correct Ballerina, or {@code null} when {@code renders} is already correct
 * @param section the one document section this case reads, or {@code null} for the whole body
 * @since 0.1.0
 */
public record Construct(
        String id,
        String dimension,
        String claim,
        String finding,
        Payload payload,
        String renders,
        String shouldRender,
        String section) {

    /** A construct the reader gets right. The case exists so that a fix elsewhere cannot break it. */
    public static Construct faithful(String id, String claim, Payload payload, String renders) {
        return new Construct(id, dimensionOf(id), claim, null, payload, renders, null, null);
    }

    /** A construct the reader gets wrong, with the register ID that has the evidence. */
    public static Construct broken(
            String id, String claim, String finding, Payload payload, String renders, String shouldRender) {
        if (finding == null || finding.isBlank()) {
            throw new IllegalArgumentException(id + " is broken but names no finding");
        }
        if (renders.equals(shouldRender)) {
            throw new IllegalArgumentException(id + " claims a gap but the two answers are equal");
        }
        return new Construct(id, dimensionOf(id), claim, finding, payload, renders, shouldRender, null);
    }

    /**
     * Read only one section of the document. For a case whose payload names a real package to reach a
     * per-package patch, and therefore inherits that package's other patches too.
     */
    public Construct inSection(String only) {
        return new Construct(id, dimension, claim, finding, payload, renders, shouldRender, only);
    }

    private static String dimensionOf(String id) {
        int slash = id.indexOf('/');
        if (slash <= 0) {
            throw new IllegalArgumentException("a construct id is dimension/case, not " + id);
        }
        return id.substring(0, slash);
    }

    /** Whether a registered finding is still open at this construct. */
    public boolean hasOpenGap() {
        return shouldRender != null;
    }

    /** The register IDs this case pins, as a list. */
    public java.util.List<String> findings() {
        return finding == null ? java.util.List.of() : java.util.List.of(finding.split(",\\s*"));
    }

    /** What the reader actually prints for this construct, right now. */
    public String actual() {
        return section == null ? payload.body() : payload.section(section);
    }
}
