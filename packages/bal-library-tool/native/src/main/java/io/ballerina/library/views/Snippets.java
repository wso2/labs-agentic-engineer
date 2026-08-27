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

import io.ballerina.library.LoadedPackage;

import java.util.ArrayList;
import java.util.List;
import java.util.Set;
import java.util.stream.Collectors;

/**
 * The Ballerina code inside a package's readme. Every block of it, in the order the readme writes them.
 *
 * <p>WHY THIS EXISTS. {@code overview} used to append the whole readme so that an agent always had a worked
 * example. Measured over the eleven packages of the 2026-08-15 sweep it almost never arrived: {@code head -100} —
 * the window agents actually write — delivered a worked snippet in 0 of 11, because four fifths of what pushed
 * the code past the cut was prose. So the code is kept here and the prose is sent to {@code bal library guide}.
 *
 * <p>WHAT IT SELECTS ON IS THE FENCE, and nothing else. A block whose fence says {@code ballerina} or {@code bal}
 * is in; a shell transcript or a TOML fragment is out. ADR-0024 reversed the classifier this used to run, which
 * kept a block only when it constructed a client, called one with {@code ->}, or attached a service to a
 * listener. That is the shape of a CONNECTOR, and a package that is not one fell straight through it:
 * {@code ballerina/log} published eight worked blocks and reached the reader with none, and
 * {@code ballerina/xlsx} reached it with exactly one — the block using {@code sftp->get}, which qualified on
 * another package's client while the {@code @xlsx:Name} header mapping and every {@code xlsx:parseSheet} call
 * were dropped as demonstrating nothing. A rule that admits a foreign client and rejects the package's own entry
 * point is not a filter worth having.
 *
 * <p>WHAT IT NEVER DOES IS EDIT ONE. No truncation, no per-block cap, no dedupe, and no name check: ADR-0008 has
 * the views quoting rather than paraphrasing, and half a snippet is a paraphrase with the compiler's half
 * missing — kafka's listener example declares its {@code consumerConfiguration} nine lines above the
 * {@code service} that uses it. The section is unbounded, which is why {@link Overview} prints it last.
 *
 * <p>Three properties of quoted code follow and are accepted rather than fixed: a block may contain a literal
 * {@code ...} placeholder, a block may use a variable another block declared, and a block may name something
 * this version no longer declares. All three are the package's own text, which the sentence {@code Overview}
 * prints above the section says.
 *
 * @since 0.1.0
 */
public final class Snippets {

    /** Fence languages this reads. Everything else is a shell transcript or a config file. */
    private static final Set<String> BALLERINA_FENCES = Set.of("ballerina", "bal");

    private Snippets() {
    }

    /**
     * The readme's Ballerina blocks, ready to print.
     *
     * <p>Every module's readme contributes, in Central's order, because a package that publishes several
     * publishes one per module and the caller asked about the package.
     */
    public static List<String> select(LoadedPackage loaded) {
        List<String> blocks = new ArrayList<>();
        for (Readmes.ModuleReadme readme : loaded.readmes()) {
            for (Readmes.Block block : Readmes.blocks(readme.markdown())) {
                if (!BALLERINA_FENCES.contains(block.language())) {
                    continue;
                }
                String code = deindent(block.code());
                if (!code.isEmpty()) {
                    blocks.add(code);
                }
            }
        }
        return blocks;
    }

    /**
     * Drop the indentation a fence inherited from the list item it was nested under.
     *
     * <p>{@code ballerinax/postgresql} indents most of its fences four spaces, which the block carries
     * verbatim — and the FIRST line of a stripped block loses it while the rest keep it, so the snippet arrives
     * looking like a fragment of something larger. The common prefix is layout rather than content, so removing
     * it is not editing the quotation: the relative indentation, which is the part that means something, is
     * untouched.
     */
    private static String deindent(String code) {
        List<String> lines = List.of(code.strip().split("\n", -1));
        int common = lines.stream()
                .filter(line -> !line.isBlank())
                .mapToInt(line -> line.length() - line.stripLeading().length())
                .min()
                .orElse(0);
        if (common == 0) {
            return String.join("\n", lines);
        }
        return lines.stream()
                .map(line -> line.length() >= common ? line.substring(common) : line.stripLeading())
                .collect(Collectors.joining("\n"));
    }
}
