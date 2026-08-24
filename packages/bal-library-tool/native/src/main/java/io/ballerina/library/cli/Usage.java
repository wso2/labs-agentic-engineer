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

package io.ballerina.library.cli;

import java.util.List;

/**
 * The PROSE of the usage text: everything a reader cannot derive from the grammar.
 *
 * <p>Kept out of {@link Cli} so the dispatcher stays about dispatch, and split from {@link UsageRenderer} along
 * the line ADR-0012 draws: every LIST — the synopsis, the verbs, the flags — is rendered from {@link Commands}'
 * picocli model, and every PARAGRAPH is written here. A sentence that names a flag, a verb or a label therefore
 * appears exactly once in this package, and a label cannot disagree with itself.
 *
 * <p>Paragraphs are stored as sentences and wrapped on the way out, so editing one never means re-breaking its
 * lines by hand. Only the literal blocks — the session walk-through and the examples — are laid out here, because
 * for those the line breaks ARE the content.
 *
 * <p>What this text is FOR narrowed twice. ADR-0011 made it the whole agent contract, because the companion skill
 * held a second copy in another repository on another release clock and that copy had drifted. ADR-0013 moved every
 * rule about something the OUTPUT prints into the view that prints it, where it is read at the moment it applies.
 * ADR-0022 finishes the narrowing: what is left here answers "what can I ask, and how" — the grammar, the session
 * and the verbs. The rules a caller carries INTO a lookup (a note is an import, an absent note is not, a '## Next'
 * block is a pointer, a failure 'kind' is a branch) are the agent's standing instructions rather than this
 * command's usage, and they live in the {@code ballerina} skill; the ADR records what holds them honest.
 *
 * @since 0.1.0
 */
final class Usage {

    private Usage() {
    }

    /**
     * The root text: what the tool is, what it can be asked, and how to walk it.
     *
     * <p>It opens with a NUMBERED SESSION rather than a paragraph about the walk. Eight lines teach the whole flow
     * — which verb starts, how {@code -s} attaches, what one call with every type looks like — where the previous
     * prose described the shape of the documents instead and left the caller to infer the commands.
     *
     * <p>And it ENDS with the verb list. ADR-0022: everything after it was a rule for the reader of an answer, not
     * a description of the command, and it is the skill's to state.
     */
    static String root(Commands.Grammar grammar) {
        return new StringBuilder()
                .append(UsageRenderer.rootSynopsis(grammar))
                .append("\n").append(UsageRenderer.prose(INTRO, 0))
                .append("\n").append(SESSION)
                .append("\n").append(UsageRenderer.verbList(grammar))
                .toString();
    }

    private static final String INTRO =
            "Read a Ballerina package off Central. A signature from here is the source for what compiles; a "
                    + "readme, a web search and a remembered API are not.";

    /**
     * The typical session, as commands.
     *
     * <p>Written out rather than generated: the VALUE of this block is the specific packages and the specific
     * selector shapes, which no model of the grammar knows. The verbs and flags inside it are still checked
     * against the grammar by {@code LibraryToolTest}, so a renamed verb fails the build here rather than
     * misleading a reader.
     */
    private static final String SESSION = """
            Typical session:

              0. Don't know which package?   bal library find "mysql database"
              1. Map + quickstart code:      bal library overview ballerinax/mysql
              2. Search anywhere with -s (the scope is the verb you attach it to):
                   bal library client ballerinax/github -s "cache"      # within the client
                   bal library overview ballerina/http -s "cookie"      # every kind at once
              3. A call and every type it needs:
                   bal library client ballerinax/github Client delete repos/{owner}/{repo}/actions/caches -r
              4. Setup, auth and worked recipes:  bal library guide ballerinax/github
            """;

    // The four paragraphs that used to close this text — what a `## Next` block is, that a note IS the import and
    // an absent one is not, that every other reading rule prints itself, and the stream-and-`kind` contract — are
    // in `skills/ballerina/SKILL.md` (ADR-0022). None of them described a command; each was an instruction to the
    // reader of an answer, which is what a skill is for. The per-verb notes below still carry the applied halves,
    // beside the verbs whose output prints a note.

    // -----------------------------------------------------------------------
    // Per-verb
    // -----------------------------------------------------------------------

    /**
     * One verb's own text: its synopsis, the description it also contributes to the root list, the reading rules
     * that belong to ITS reader, its own flags, and its examples.
     */
    static String verb(Commands.Grammar grammar, String name) {
        StringBuilder text = new StringBuilder()
                .append(UsageRenderer.verbSynopsis(grammar, name))
                .append("\n").append(UsageRenderer.prose(grammar.verb(name)));
        for (String note : notes(name)) {
            text.append("\n").append(UsageRenderer.prose(note, 0));
        }
        String flags = UsageRenderer.verbFlagList(grammar, name);
        if (!flags.isEmpty()) {
            text.append("\n").append(flags);
        }
        return text.append("\n").append(examples(name)).toString();
    }

    /**
     * The three container verbs share one note about the split, because they share one implementation.
     *
     * <p>Repeating it is the trade ADR-0013 makes deliberately: the alternative is a section at the root that a
     * caller choosing between eight verbs has to read first, which is exactly the section that ADR removed.
     */
    private static final String KIND_SPLIT =
            "A guess costs a line, not a call. This verb answers for a symbol of another kind too, prepending "
                    + "one line that names the canonical verb — so 'client' on a class, or on a record, still "
                    + "shows it. A name that is a MEMBER rather than a container resolves the same way and names "
                    + "its owner.";

    private static final String SELECTOR_RULE =
            "The selector grammar follows the CONTAINER, not the verb: a container that declares resource "
                    + "functions reads 'get'/'post'/'delete' as an accessor and the token after it as a path; one "
                    + "that does not reads the same token as a member name. Paths are anchored and matched from "
                    + "the first segment, '*' is a wildcard segment, and {owner}, owner and [string owner] all "
                    + "address the same one. A segment also answers to its escaped spelling, and 'new' addresses "
                    + "the constructor — unless the container really declares one, which wins.";

    private static final String BUDGET_RULE =
            "Never paginated. A listing over its byte budget is re-rendered coarser — signatures become names, "
                    + "names become groups with counts — and whatever collapsed says so with its size. Narrow "
                    + "with -s or a selector rather than piping: a window cuts the import notes off the lines it "
                    + "keeps. Line one states the document's own length, so a window that cut is visible — fewer "
                    + "lines than it says means the answer is incomplete.";

    private static List<String> notes(String verb) {
        return switch (verb) {
            case "find" -> List.of(
                    "Central's relevance order is kept, with one change: packages under 1,000 pulls are moved to "
                            + "the end. The pull count is printed beside every hit, because a low count is not a "
                            + "verdict on quality — only on adoption.",
                    "It matches names, summaries and keywords — not documentation, and not questions. One broad "
                            + "noun works better than a sentence. Takes no --refresh: a registry query resolves "
                            + "no version and is never cached.");
            case "overview" -> List.of(
                    "A MAP, not a dump: counts, the guide's chunk index, a roster whose every row ends in the "
                            + "command that opens it, and last, every Ballerina block the readme published. No "
                            + "generated signatures — the container verbs print those, in full, within their own "
                            + "budget.",
                    "With -s it is the CROSS-KIND search, over clients, classes, module functions, declarations "
                            + "and guide chunk titles at once. That is the question no kind-specific verb can "
                            + "answer, because you do not yet know the kind.",
                    "Ordered so that a window keeps the useful half: facts, '## Next' and the rosters first, and "
                            + "the quoted code last because it is as long as the readme made it. If you cut it "
                            + "off, line one says so.");
            case "client" -> List.of(
                    "Bare, it lists what the package has: one client is answered whole — constructor, resource "
                            + "functions by path, remote and normal ones by name — and several give a roster with "
                            + "counts.",
                    SELECTOR_RULE,
                    KIND_SPLIT,
                    BUDGET_RULE);
            case "class" -> List.of(
                    "The same shape as 'client', over the objects reached with '.': classes, object types, "
                            + "service types and listeners. A client IS a class, so a client name works here too "
                            + "and says which verb is canonical.",
                    SELECTOR_RULE,
                    KIND_SPLIT,
                    BUDGET_RULE);
            case "funcs" -> List.of(
                    "The callable surface of a package with no client, and the utilities of one that has them. A "
                            + "bare token matches anywhere in a name and '*' is a wildcard, so 'create*' selects "
                            + "a family.",
                    KIND_SPLIT,
                    BUDGET_RULE);
            case "type" -> List.of(
                    "A trailing '// Special Agent Note:' says what the line alone cannot. A 'FROM <module> module' "
                            + "clause IS the import to add, written exactly as printed; a prefixed name without one "
                            + "is pre-declared and needs none. Any other clause is about the line, not an import.",
                    "It is how you read a package's ERRORS: 'overview' names them on its Errors row and does not "
                            + "declare them, and this verb takes several names at once, so one call reads the "
                            + "whole set with its subtype chain.",
                    "All-or-nothing across names: if one does not resolve, the run fails with candidates and "
                            + "stdout stays empty, because 'exit 0 means stdout is complete' is what a "
                            + "redirecting caller relies on.");
            case "guide" -> List.of(
                    "Central's prose, on the package author's release clock, so it can be out of date where a "
                            + "signature cannot. Names in it that this version does not declare are listed before "
                            + "the quotation rather than left to fail at build time.",
                    "A CHUNK is a readme section that carries code, prose and all — the account setup and the "
                            + "'you must also enable the Drive API' notes are the part no signature can tell you. "
                            + "'overview' prints the chunk index; pass a number or a title fragment to read one.");
            case "api" -> List.of(
                    "How large: ballerinax/github is 927KB here, 738 of them types — which 'type' reads one at a "
                            + "time and 'overview' leaves out for that reason.",
                    "Reads like 'type': a trailing '// Special Agent Note:' whose clause says 'FROM <module> module' "
                            + "is the import to add, and a prefixed name without one is pre-declared.",
                    "Its '// --- Service ---' section is the only place attachability is answered. A service "
                            + "object type gets a 'service X on new Listener(...)' template when the package's "
                            + "listener is known to accept it; the rest are listed under a note saying that "
                            + "cannot be confirmed, and writing 'on new Listener(...)' for one of those does not "
                            + "compile.",
                    "It is also the only verb that names a 'configurable', which is module-private and therefore "
                            + "unreachable by 'type'.");
            // Unreachable: picocli rejects an unknown verb before dispatch, so reaching here is a defect in this
            // class rather than a caller mistake.
            default -> throw new IllegalArgumentException("no such verb: " + verb);
        };
    }

    private static String examples(String verb) {
        return switch (verb) {
            case "find" -> "  bal library find kafka messaging\n";
            case "overview" -> """
                      bal library overview ballerinax/github
                      bal library overview ballerina/http -s "cookie"
                    """;
            case "client" -> """
                      bal library client ballerinax/github
                      bal library client ballerinax/github repos
                      bal library client ballerinax/github Client delete repos/{owner}/{repo}/actions/caches -r
                      bal library client ballerinax/twilio -s "message"
                    """;
            case "class" -> """
                      bal library class ballerina/http
                      bal library class ballerina/http Cookie
                      bal library class ballerina/http Cookie toStringValue -r
                    """;
            case "funcs" -> """
                      bal library funcs ballerina/uuid
                      bal library funcs ballerina/crypto 'hash*'
                      bal library funcs ballerina/crypto -s "sha256" -r
                    """;
            case "type" -> """
                      bal library type ballerina/http ClientRequestError -r
                      bal library type ballerinax/aws.auth AuthConfig
                      bal library type ballerinax/github -s "repository"
                    """;
            case "guide" -> """
                      bal library guide ballerinax/twilio
                      bal library guide ballerinax/googleapis.sheets 2
                      bal library guide ballerina/http --module http
                    """;
            case "api" -> "  bal library api ballerinax/sap\n";
            default -> throw new IllegalArgumentException("no such verb: " + verb);
        };
    }
}
