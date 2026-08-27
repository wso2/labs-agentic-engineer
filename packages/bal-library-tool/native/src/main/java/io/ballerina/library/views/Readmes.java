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

package io.ballerina.library.views;

import io.ballerina.library.central.schema.CentralDocs;

import java.util.ArrayList;
import java.util.List;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

/**
 * The package's own guide, which answers the question a signature cannot: how is this used — auth, config,
 * the shape of a call.
 *
 * <p>Central serves it as the module's {@code description}, byte-identical to the {@code docs/README.md} a
 * published {@code .bala} carries — verified against {@code ballerinax/kafka@4.6.5}, 7,463 bytes, zero diff.
 * Reading it here rather than off disk is what makes it available BEFORE a build has resolved the package,
 * which is exactly when a connector nobody has written against is hardest to guess at.
 *
 * <p>It is the largest part of the overview for most packages — {@code postgresql} is 23.6 of 26KB,
 * {@code graphql} 17.9 of 20.1 — and that is the right trade. It is the "how is this used" answer, and the
 * reason the recorded traces never found it is that nothing put it in front of them.
 *
 * @since 0.1.0
 */
public final class Readmes {

    private static final Pattern FENCE = Pattern.compile("^\\s*(```|~~~)");
    private static final Pattern HEADING = Pattern.compile("^(#{1,6})(\\s)");

    private Readmes() {
    }

    /** One module's guide. A package publishes one document per module. */
    public record ModuleReadme(String module, String markdown) { }

    /**
     * Every module that wrote a guide, in Central's order.
     *
     * <p>Modules without one are dropped rather than emitted empty: a heading with nothing under it reads
     * as a truncated download.
     */
    public static List<ModuleReadme> collect(CentralDocs docs) {
        List<ModuleReadme> readmes = new ArrayList<>();
        for (CentralDocs.Module module : docs.modules()) {
            String markdown = module.description().orElse("").trim();
            if (!markdown.isEmpty()) {
                readmes.add(new ModuleReadme(module.id(), markdown));
            }
        }
        return List.copyOf(readmes);
    }

    /**
     * Push every ATX heading down by {@code levels}, so an embedded guide sits under the host document's
     * outline instead of competing with it.
     *
     * <p>This is what lets {@code grep '^## '} on an overview return the overview's own sections rather than
     * the readme's. Fenced blocks are skipped, because {@code #} at the start of a line inside a fence is a
     * shell comment, a Ballerina doc comment or a Python comment — not a heading — and promoting one would
     * corrupt a code sample the agent is about to copy.
     *
     * <p>Level 6 is the floor: HTML has no {@code h7}, and a heading pushed past it would stop being a
     * heading at all.
     */
    public static String demoteHeadings(String markdown, int levels) {
        boolean inFence = false;
        String[] lines = markdown.split("\n", -1);
        StringBuilder result = new StringBuilder();
        for (int index = 0; index < lines.length; index++) {
            String line = lines[index];
            if (index > 0) {
                result.append('\n');
            }
            if (FENCE.matcher(line).find()) {
                inFence = !inFence;
                result.append(line);
                continue;
            }
            if (inFence) {
                result.append(line);
                continue;
            }
            Matcher heading = HEADING.matcher(line);
            if (!heading.find()) {
                result.append(line);
                continue;
            }
            int hashes = heading.group(1).length();
            int depth = Math.min(6, hashes + levels);
            result.append("#".repeat(depth)).append(line.substring(hashes));
        }
        return result.toString();
    }

    /**
     * One fenced block: the info string its opening fence carried, and the lines inside it.
     *
     * <p>The language is not decoration. {@code Snippets} selects the blocks a coding agent can act on, and a
     * {@code bash} block full of {@code curl} is not one — nor is a {@code toml} block of deployment settings.
     * Measured across the eleven packages of the 2026-08-15 corpus, every fence in every guide carries a tag:
     * 61 {@code ballerina}, 8 {@code bash}, 1 {@code toml}, none bare. So the tag is a reliable filter, and the
     * one blind spot it leaves — a future readme that fences Ballerina with no tag — costs a dropped snippet
     * rather than a wrong one.
     */
    public record Block(String language, String code) { }

    /** Every fenced block with its language, in document order. */
    public static List<Block> blocks(String markdown) {
        List<Block> blocks = new ArrayList<>();
        StringBuilder current = null;
        String language = "";
        for (String line : markdown.split("\n", -1)) {
            String stripped = line.stripLeading();
            if (stripped.startsWith("```")) {
                if (current == null) {
                    current = new StringBuilder();
                    language = stripped.substring(3).trim().toLowerCase(java.util.Locale.ROOT);
                } else {
                    blocks.add(new Block(language, current.toString()));
                    current = null;
                    language = "";
                }
                continue;
            }
            if (current != null) {
                current.append(line).append('\n');
            }
        }
        // An unclosed fence still holds code, and a readme with one is exactly the sloppy kind this checks.
        if (current != null) {
            blocks.add(new Block(language, current.toString()));
        }
        return List.copyOf(blocks);
    }
}
