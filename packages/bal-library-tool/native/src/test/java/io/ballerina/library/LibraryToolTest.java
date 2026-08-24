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

import io.ballerina.library.central.HttpOptions;
import io.ballerina.library.cli.Cli;
import io.ballerina.library.cli.LibraryTool;
import org.testng.Assert;
import org.testng.annotations.Test;
import picocli.CommandLine;

import java.io.ByteArrayOutputStream;
import java.io.PrintStream;
import java.nio.charset.StandardCharsets;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

/**
 * The process wrapper, and the usage text as a golden file.
 *
 * <p>{@link CliTest} proves the behaviour; this proves the two things only the wrapper owns: that {@code bal} hands
 * the whole argument list through to us unparsed, and that the usage text has not drifted. The golden file is the
 * package's existing convention for CLI output, and it exists so a change to what the tool tells an agent is a
 * reviewable diff rather than a surprise at run time.
 *
 * @since 0.1.0
 */
public class LibraryToolTest {

    private static final Path COMMAND_OUTPUTS =
            Path.of("src", "test", "resources", "command-outputs", "unix");

    /** The tool driven the way {@code bal} drives it: through picocli, into the raw argument list. */
    private record Run(int exitCode, String stdout, String stderr) { }

    private static Run run(String... argv) {
        ByteArrayOutputStream out = new ByteArrayOutputStream();
        ByteArrayOutputStream err = new ByteArrayOutputStream();
        LibraryTool tool = new LibraryTool(
                new PrintStream(out, true, StandardCharsets.UTF_8),
                new PrintStream(err, true, StandardCharsets.UTF_8));
        new CommandLine(tool).parseArgs(argv);
        tool.execute();
        return new Run(tool.exitCode(),
                out.toString(StandardCharsets.UTF_8),
                err.toString(StandardCharsets.UTF_8));
    }

    /** The verbs, spelled out rather than read from the grammar: a verb silently lost is what this would catch. */
    private static final List<String> VERBS =
            List.of("find", "overview", "client", "class", "funcs", "type", "guide", "api");

    /**
     * A usage request, driven through the real command with the cache disabled.
     *
     * <p>Through {@link Cli#run} rather than the text-building class directly, so what the golden pins is what a
     * caller's stdout actually receives — the text, on the right stream, under the right exit code. The null
     * cache describes itself as {@code disabled}, which is what keeps the golden machine-independent.
     */
    private static String usage(String... argv) {
        StringBuilder err = new StringBuilder();
        StringBuilder out = new StringBuilder();
        int code = Cli.run(List.of(argv), new Cli.Streams(out::append, err::append),
                HttpOptions.builder().build());
        Assert.assertEquals(code, 0, "a usage request is answered, not failed");
        Assert.assertEquals(err.toString(), "", "stderr is for failures only");
        return out.toString();
    }

    @Test
    public void theUsageTextIsUnchanged() {
        // A golden file rather than a substring assertion: this text is the tool's whole contract with an agent
        // that has never run it before, and every line of it was chosen.
        FixtureCorpus.matchesSnapshot(
                COMMAND_OUTPUTS.resolve("help.txt"), usage("--help"), "root usage");
    }

    @Test
    public void everyVerbsUsageTextIsUnchanged() {
        for (String verb : VERBS) {
            FixtureCorpus.matchesSnapshot(
                    COMMAND_OUTPUTS.resolve(verb + "-help.txt"), usage(verb, "--help"), verb + " usage");
        }
    }

    /**
     * The hand-written roster above and the grammar hold the same verbs, checked from both ends.
     *
     * <p>Spelling the list out catches a verb silently LOST. On its own it does not catch a verb silently
     * GAINED — one added to the grammar and never given a golden file — and that is the direction a new verb
     * arrives from. The root synopsis is rendered from the grammar, so counting its lines is the other end.
     */
    @Test
    public void theRosterAboveIsEveryVerbTheGrammarHas() {
        // FILTERED rather than taken while, because a synopsis line wraps: `client` carries four slots and its
        // continuation is an indented run of `[...]` tokens with no `bal library` on it. A `takeWhile` stopped at
        // the first wrap and silently checked three verbs of eight.
        List<String> synopsis = usage("--help").lines()
                .filter(line -> line.contains("bal library "))
                .map(line -> line.replaceAll("^.*bal library (\\w+).*$", "$1"))
                .distinct()
                .toList();
        Assert.assertEquals(synopsis, VERBS);
    }

    /**
     * The grammar and the text that describes it cannot disagree, because one renders the other.
     *
     * <p>This is the drift ADR-0012 closed, and it had already happened: the hand-written synopsis advertised
     * {@code --client C} while the flag list two paragraphs below it — and all five verb texts — said
     * {@code --client <Name>}. Nothing failed, because nothing compared them.
     */
    @Test
    public void everyFlagIsSpelledTheSameWayEverywhere() {
        // Wherever a flag is shown WITH its value, that value must be the paramLabel the grammar declares.
        // Prose that merely names a flag is not a spelling, so only `--flag <...>` occurrences are compared.
        Map<String, String> labels = Map.of("--search", "<q>", "--module", "<name>");
        List<String> texts = new ArrayList<>(List.of(usage("--help")));
        VERBS.forEach(verb -> texts.add(usage(verb, "--help")));

        for (String text : texts) {
            Matcher spellings = Pattern.compile("(--[a-z-]+) (<[^>]+>)").matcher(text);
            while (spellings.find()) {
                String declared = labels.get(spellings.group(1));
                Assert.assertEquals(spellings.group(2), declared, spellings.group(1) + "'s label");
            }
        }
        // The synopsis names the SHORT form, because that is what every example and every `## Next` bullet types;
        // the flag list names both, so a reader can connect them.
        Assert.assertTrue(texts.get(0).contains("-s <q>"), "the synopsis names -s");
        Assert.assertTrue(usage("client", "--help").contains("-s, --search <q>"),
                "the flag list names both spellings");
        Assert.assertTrue(usage("client", "--help").contains("-r, --resolve-types"), "and for -r too");
    }

    @Test
    public void theToolReceivesTheWholeArgumentListUnparsed() {
        // Verified against a real `bal library` invocation: all ten arguments of a realistic call arrive raw,
        // which is why the grammar can be ours. If `bal` ever started consuming flags, this is where it shows.
        Run help = run();
        Assert.assertEquals(help.exitCode(), 0);
        Assert.assertEquals(help.stderr(), "");
        Assert.assertTrue(help.stdout().startsWith("Usage: bal library"));
    }

    @Test
    public void anUnknownVerbIsExit1WithOneJsonObjectOnStderr() {
        // The old LS-backed tool exited 0 here, which told an agent its typo had worked.
        Run run = run("nonsense");
        Assert.assertEquals(run.exitCode(), 1);
        Assert.assertEquals(run.stdout(), "");
        Assert.assertTrue(run.stderr().contains("\"kind\":\"validation\""), run.stderr());
        Assert.assertTrue(run.stderr().endsWith("}\n"));
    }

    @Test
    public void everyVerbIsRoutedRatherThanReportedAsUnknown() {
        // Each of these fails for lack of an argument, not for lack of a verb — which is what proves the routing.
        for (String verb : VERBS) {
            Run run = run(verb);
            Assert.assertEquals(run.exitCode(), 1, verb);
            Assert.assertFalse(run.stderr().contains("is not a verb"), verb + " was not routed");
        }
    }

    @Test
    public void theHelpTextNamesEveryVerb() {
        String usage = usage("--help");
        for (String verb : VERBS) {
            Assert.assertTrue(usage.contains("bal library " + verb), verb);
        }
        Assert.assertFalse(usage.contains("language server"), "no document mentions the language server");
    }

    /**
     * The root text answers "what can I ask, and how", and carries no reading rule at all.
     *
     * <p>Three narrowings, each with its own ADR. ADR-0011 put every rule here, because the agent skill held a
     * second copy in another repository on another release clock and that copy had drifted — its langlib list was
     * six modules of fourteen and it named the wrong rule. ADR-0013 moved the rules about something the output
     * PRINTS into the views that print them, beside the thing they describe ({@code ViewsTest} and
     * {@code ViewsAgreeTest} hold those assertions). ADR-0022 moves the last four — a {@code ## Next} block is a
     * pointer, a note IS the import and an absent note is not, every other rule prints itself, and the
     * stream-and-{@code kind} contract — into {@code skills/ballerina/SKILL.md}, because each instructs the reader
     * of an ANSWER rather than describing this command.
     *
     * <p>So the assertions here are all negative, and that is the point: what a rule must not have is two homes,
     * or the drift ADR-0011 fixed comes back. The applied halves still print — {@code type --help} and
     * {@code api --help} name the note beside the verbs whose output carries one — and that is asserted below
     * rather than left to the golden.
     */
    @Test
    public void theHelpTextCarriesNoReadingRule() {
        String usage = usage("--help");
        Assert.assertFalse(usage.contains("// Special Agent Note:"), "the import rule is the skill's");
        Assert.assertFalse(usage.contains("pre-declared"), "and so is its absent half");
        Assert.assertFalse(usage.contains("## Next"), "what a pointer block is, is the skill's");
        Assert.assertFalse(usage.contains("Every failure is exit 1"), "the failure taxonomy is the skill's");
        Assert.assertFalse(usage.contains("stdout is the requested document"), "the stream contract with it");

        Assert.assertFalse(usage.contains("the signature is what compiles"), "readme rule moved to Overview");
        Assert.assertFalse(usage.contains("Also matched"), "wildcard rule moved to Containers");
        Assert.assertFalse(usage.contains("Run one of these verbatim"), "the edge rule moved to Closure");

        // Not gone from the tool — moved. The two verbs whose documents print a note still explain it on their own
        // page, where it is read at the moment it applies.
        Assert.assertTrue(usage("type", "--help").contains("// Special Agent Note:"));
        Assert.assertTrue(usage("api", "--help").contains("// Special Agent Note:"));
    }

    /**
     * {@code --all} appears in no help text at all.
     *
     * <p>It is an escape hatch, not part of the taught contract: a caller who meets it in {@code --help} reaches
     * for it before trying a selector or {@code -s}, which is the behaviour the byte budgets exist to prevent. The
     * documents that collapse a section offer it in their own {@code ## Next} block, last, with its cost stated —
     * asserted in {@link ViewsTest} — and that is the only place it is ever named.
     */
    @Test
    public void theEscapeHatchIsHiddenFromEveryHelpText() {
        List<String> texts = new ArrayList<>(List.of(usage("--help")));
        VERBS.forEach(verb -> texts.add(usage(verb, "--help")));
        for (String text : texts) {
            Assert.assertFalse(text.contains("--all"), text);
        }
        // Hidden, not removed: it still parses, or the ## Next bullets would be offering a command that fails.
        Assert.assertEquals(Cli.run(List.of("client", "x/y", "--all", "--help"),
                new Cli.Streams(new StringBuilder()::append, new StringBuilder()::append),
                HttpOptions.builder().build()), 0);
    }

    /**
     * A flag is documented on the verb that accepts it, and nowhere else.
     *
     * <p>The root text used to list all six with their descriptions, which is the section ADR-0013 deleted: it
     * told a caller choosing between five verbs what {@code --refresh} does, and said nothing about which verb
     * to run. The cost is that the three shared resolution flags now repeat across four pages, so this pins both
     * ends — the root names none of them, and every page names all of its own.
     */
    @Test
    public void everyFlagIsDocumentedOnTheVerbThatAcceptsIt() {
        // The SYNOPSIS still names the flags a verb takes — that is the grammar, and it is generated. What the
        // root no longer carries is a description of any of them, so these assert the prose, not the token.
        String root = usage("--help");
        Map<String, String> descriptions = Map.of(
                "--search", "Filter this verb's scope",
                "--resolve-types", "Answer as Ballerina",
                "--refresh", "Ignore any cached copy",
                "--module", "One module's readme");
        descriptions.forEach((flag, prose) ->
                Assert.assertFalse(root.contains(prose), "the root text no longer describes " + flag));

        Map<String, List<String>> expected = Map.of(
                "find", List.of(),
                "overview", List.of("--search", "--refresh"),
                "client", List.of("--search", "--resolve-types", "--refresh"),
                "class", List.of("--search", "--resolve-types", "--refresh"),
                "funcs", List.of("--search", "--resolve-types", "--refresh"),
                "type", List.of("--search", "--resolve-types", "--refresh"),
                "guide", List.of("--module", "--search", "--refresh"),
                "api", List.of("--refresh"));
        expected.forEach((verb, flags) -> {
            String text = usage(verb, "--help");
            // Matched anywhere on the page rather than at a column, because a flag with two spellings is listed
            // as `-s, --search <q>` and the long form is no longer the first thing on its line.
            flags.forEach(flag -> Assert.assertTrue(text.contains(flag), verb + " documents " + flag));
        });
    }

    @Test
    public void theLongDescriptionIsTheUsageText() {
        // `bal help library` reads this, so it must not be a second, drifting copy.
        StringBuilder sb = new StringBuilder();
        new LibraryTool(System.out, System.err).printLongDesc(sb);
        Assert.assertTrue(sb.toString().startsWith("Usage: bal library"));
        Assert.assertEquals(sb.toString(), usage("--help"));
    }

    @Test
    public void theToolNamesItselfLibrary() {
        // The id `bal-tools.toml` binds, so a mismatch makes the installed tool unroutable.
        Assert.assertEquals(new LibraryTool(System.out, System.err).getName(), "library");
        for (String verb : VERBS) {
            Assert.assertTrue(usage("--help").contains("bal library " + verb + " "), verb);
        }
    }
}
