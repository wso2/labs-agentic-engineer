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

import picocli.CommandLine.Model.CommandSpec;
import picocli.CommandLine.Model.OptionSpec;
import picocli.CommandLine.Model.PositionalParamSpec;

import java.util.ArrayList;
import java.util.Comparator;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;

/**
 * Every LIST in the usage text, rendered from {@link Commands}' picocli model.
 *
 * <p>picocli owns the MODEL; the layout is ours (ADR-0012). Its own renderer was measured against this text and
 * gets the two list sections right, but not the synopsis: it sorts options alphabetically and emits them BEFORE
 * the positionals, in a tool whose whole grammar is positional-first, and 4.0.1 — the version on the Ballerina
 * distribution's classpath — has no knob that reorders them. So {@code Help.synopsis} is not used and these
 * forty lines are, which is the trade that keeps {@code <org/name>} in front of the flags.
 *
 * <p>What this deletes is a class of drift no test could see. The synopsis, the flag list and the verb list were
 * three hand-written copies of what {@code Commands} already declares, and they had already disagreed: the root
 * synopsis advertised {@code --client C} where every other mention said {@code --client <Name>}. A label can no
 * longer be wrong here, only ugly.
 *
 * @since 0.1.0
 */
final class UsageRenderer {

    /** The widest line the text may reach. Chosen to fit an 80-column terminal with a little slack. */
    private static final int WIDTH = 84;

    /** The gap between a label column and its prose. */
    private static final int GAP = 3;

    private static final String PACKAGE_SLOT = "<org/name>";

    private UsageRenderer() {
    }

    // -----------------------------------------------------------------------
    // Synopsis
    // -----------------------------------------------------------------------

    /**
     * The {@code Usage:} block: one line per verb, carrying only the flags that verb alone takes.
     *
     * <p>The shared resolution flags are deliberately absent — repeating them five times would triple the block
     * to say the same thing, and the flag list below names them once with who accepts them.
     */
    static String rootSynopsis(Commands.Grammar grammar) {
        String heading = "Usage: ";
        int pad = grammar.verbs().stream().mapToInt(String::length).max().orElse(0);
        Set<String> shared = sharedFlags(grammar);

        StringBuilder text = new StringBuilder();
        for (String verb : grammar.verbs()) {
            CommandSpec spec = grammar.verb(verb);
            List<String> slots = new ArrayList<>(positionalSlots(spec));
            for (OptionSpec option : orderedOptions(spec)) {
                if (!shared.contains(option.longestName())) {
                    slots.add(slot(option));
                }
            }
            String label = "bal library " + verb + " ".repeat(pad - verb.length()) + " ";
            String prefix = text.isEmpty() ? heading : " ".repeat(heading.length());
            text.append(wrap(prefix + label, slots, heading.length() + label.length()));
        }
        return text.toString();
    }

    /** One verb's full synopsis, shared flags included: this is the only place they are spelled out per verb. */
    static String verbSynopsis(Commands.Grammar grammar, String verb) {
        CommandSpec spec = grammar.verb(verb);
        List<String> slots = new ArrayList<>(positionalSlots(spec));
        for (OptionSpec option : orderedOptions(spec)) {
            slots.add(slot(option));
        }
        String label = "Usage: bal library " + verb + " ";
        return wrap(label, slots, label.length());
    }

    private static List<String> positionalSlots(CommandSpec spec) {
        boolean required = Commands.REQUIRES_POSITIONALS.contains(spec.name());
        return spec.positionalParameters().stream()
                .map(positional -> slot(positional, required))
                .toList();
    }

    /**
     * {@code <org/name>}, {@code [<version>]}, {@code <Name>...} — repetition and required-ness, from the spec.
     *
     * <p>Repetition is read off the INDEX range rather than the arity: {@code <Name>} and {@code <keywords>}
     * occupy an unbounded index and so repeat, but neither may declare a minimum arity — that is what would let
     * picocli swallow a foreign flag as a value ({@code Commands.Search#keywords}). A variadic slot is therefore
     * never bracketed: this grammar has no verb that declares one and tolerates it being empty, and the two that
     * picocli cannot enforce are enforced by {@code Cli.rejectEmptyLists} instead.
     */
    private static String slot(PositionalParamSpec positional, boolean variadicRequired) {
        boolean repeats = positional.index().max() == Integer.MAX_VALUE || positional.arity().max() > 1;
        if (repeats) {
            // BRACKETED unless the verb requires it. `bal library client <org/name>` with no selector is the
            // primary form — it lists what the package has — so a synopsis reading `<Name|selector>...` contradicts
            // the first example printed four lines below it.
            String slot = positional.paramLabel() + "...";
            return variadicRequired ? slot : "[" + slot + "]";
        }
        return positional.arity().min() == 0 ? "[" + positional.paramLabel() + "]" : positional.paramLabel();
    }

    /**
     * Always optional, so always bracketed, and named by its SHORTEST spelling.
     *
     * <p>{@code -s} rather than {@code --search}, because the short form is what every example and every
     * {@code ## Next} bullet in the tool uses. A synopsis that advertises a spelling nothing else teaches is the
     * same drift ADR-0012 exists to prevent, in the other direction.
     */
    private static String slot(OptionSpec option) {
        String name = shortestName(option);
        return "[" + (option.arity().max() == 0 ? name : name + " " + option.paramLabel()) + "]";
    }

    private static String shortestName(OptionSpec option) {
        String shortest = option.longestName();
        for (String name : option.names()) {
            if (name.length() < shortest.length()) {
                shortest = name;
            }
        }
        return shortest;
    }

    // -----------------------------------------------------------------------
    // The two lists
    // -----------------------------------------------------------------------

    /** The verbs and what each answers, from {@code @Command(description)}. */
    static String verbList(Commands.Grammar grammar) {
        List<Row> rows = grammar.verbs().stream()
                .map(verb -> new Row(verb, description(grammar.verb(verb))))
                .toList();
        return table(rows);
    }

    /**
     * A verb's own description at full width — the same string the root list shows in its narrower column.
     *
     * <p>One description rendered into two layouts, rather than the short-and-long pair the two hand-written
     * texts used to keep. That pair is where {@code overview} came to promise "No other types" in one place and
     * "No other types — they are 80% of a large package" in the other.
     */
    static String prose(CommandSpec spec) {
        return prose(description(spec), 0);
    }

    /**
     * EVERY flag this verb accepts, so the page is the whole answer for it.
     *
     * <p>It used to show only what distinguished the verb, because the root text carried a list of all six. That
     * list is gone — a flag is a property of the verb that takes it, and six descriptions at the root said
     * nothing about which of the five to run. So the shared resolution flags are repeated on each of the four
     * package-reading pages instead, which is the trade this makes: four short repetitions of one description,
     * against a caller who has to read a second section to find out what {@code --refresh} does.
     *
     * <p>{@code --help} is still excluded. It is on every verb, it is what the caller just ran, and a row saying
     * so is the one line in this text nobody needs.
     */
    static String verbFlagList(Commands.Grammar grammar, String verb) {
        List<Row> rows = orderedOptions(grammar.verb(verb)).stream()
                .map(option -> new Row(label(option), description(option)))
                .toList();
        return rows.isEmpty() ? "" : table(rows);
    }

    /**
     * {@code --module, --resolve-types, --search and --refresh} — for a failure's suggestion.
     *
     * <p>Long forms only. {@code flagOwners} keys on every spelling so that a caller who typed {@code -r} can be
     * told who owns it; a LIST of known flags that showed both spellings of each would read as twice as many flags
     * as the tool has.
     */
    static String knownFlags(Commands.Grammar grammar) {
        Map<String, List<String>> owners = grammar.flagOwners();
        Set<String> longForms = new LinkedHashSet<>();
        owners.keySet().forEach(flag -> longForms.add(grammar.canonicalFlag(flag)));
        List<String> flags = new ArrayList<>(longForms);
        flags.sort(Comparator.comparingInt(flag -> owners.get(flag).size()));
        return sentenceList(flags);
    }

    /** {@code 'a' and 'b'}, or {@code 'a', 'b' and 'c'} — a list a sentence can hold. */
    static String sentenceList(List<String> items) {
        if (items.size() == 1) {
            return items.get(0);
        }
        return String.join(", ", items.subList(0, items.size() - 1)) + " and " + items.get(items.size() - 1);
    }

    // -----------------------------------------------------------------------
    // Model queries
    // -----------------------------------------------------------------------

    /**
     * A flag every package-reading verb accepts, and which therefore distinguishes none of them.
     *
     * <p>"Package-reading" is read off the grammar rather than listed: a verb that declares an
     * {@code <org/name>} positional reads a package. {@code search} does not, which is why it takes no
     * {@code --refresh}.
     */
    private static Set<String> sharedFlags(Commands.Grammar grammar) {
        List<String> packageVerbs = grammar.verbs().stream()
                .filter(verb -> grammar.verb(verb).positionalParameters().stream()
                        .anyMatch(positional -> PACKAGE_SLOT.equals(positional.paramLabel())))
                .toList();
        Set<String> shared = new LinkedHashSet<>();
        grammar.flagOwners().forEach((flag, owners) -> {
            if (owners.containsAll(packageVerbs)) {
                shared.add(flag);
            }
        });
        return shared;
    }

    /**
     * Declaration order, minus {@code --help} and minus anything hidden.
     *
     * <p>{@code --help} is on every verb, it is what the caller just ran, and a row saying so is the one line in
     * this text nobody needs. HIDDEN is the stronger case: {@code --all} is an escape hatch rather than part of the
     * taught contract, and this is the single filter that keeps it out of the synopsis, the flag list AND the
     * "known flags" a failure suggests — three places it would otherwise have to be excluded separately.
     */
    private static List<OptionSpec> orderedOptions(CommandSpec spec) {
        return spec.options().stream()
                .filter(option -> !option.usageHelp() && !option.hidden())
                .toList();
    }

    /**
     * EVERY spelling of a flag, short first: {@code -s, --search <q>}.
     *
     * <p>Both, because the two are used in different places and a reader needs to connect them: the prose and the
     * examples type {@code -s}, and a failure's "belongs to" sentence names {@code --search}. Showing one alone
     * left an agent unable to tell they were the same flag. {@code -h, --help} never reaches here.
     */
    private static String label(OptionSpec option) {
        List<String> names = new ArrayList<>(List.of(option.names()));
        names.sort(Comparator.comparingInt(String::length));
        String spelled = String.join(", ", names);
        return option.arity().max() == 0 ? spelled : spelled + " " + option.paramLabel();
    }

    /** picocli keeps a description as an array of lines; these are written as one sentence run and re-wrapped. */
    private static String description(CommandSpec spec) {
        return String.join(" ", spec.usageMessage().description());
    }

    private static String description(OptionSpec option) {
        return String.join(" ", option.description());
    }

    // -----------------------------------------------------------------------
    // Layout
    // -----------------------------------------------------------------------

    private record Row(String label, String text) { }

    /** A two-column block: labels padded to the widest, prose wrapped and hanging under itself. */
    private static String table(List<Row> rows) {
        int labelWidth = rows.stream().mapToInt(row -> row.label().length()).max().orElse(0);
        int indent = 2 + labelWidth + GAP;
        StringBuilder text = new StringBuilder();
        for (Row row : rows) {
            String lead = "  " + row.label() + " ".repeat(indent - 2 - row.label().length());
            text.append(paragraph(lead, row.text(), indent));
        }
        return text.toString();
    }

    /**
     * A paragraph of prose, wrapped to {@link #WIDTH} and hanging at {@code indent}.
     *
     * <p>Exposed so that {@link Usage}'s prose is stored as sentences rather than as pre-broken lines. Nothing
     * in this package is hand-wrapped: re-flowing a paragraph after an edit is the kind of upkeep that gets
     * skipped, and a text that is 88 columns in three places and 84 everywhere else looks like a mistake
     * because it is one.
     */
    static String prose(String body, int indent) {
        return paragraph(" ".repeat(indent), body, indent);
    }

    private static String paragraph(String lead, String body, int indent) {
        return wrap(lead, List.of(body.trim().split("\\s+")), indent);
    }

    /**
     * {@code lead} followed by {@code tokens}, wrapped to {@link #WIDTH}, continuing at {@code indent}.
     *
     * <p>Greedy fill, and a token is never broken. That is why a synopsis passes whole SLOTS rather than words:
     * {@code [--version <version>]} is one token here, so a line break cannot land between the flag and the
     * label it takes — which is exactly what picocli's own renderer does to it.
     */
    private static String wrap(String lead, List<String> tokens, int indent) {
        StringBuilder text = new StringBuilder(lead);
        int column = lead.length();
        boolean first = true;
        for (String token : tokens) {
            if (token.isEmpty()) {
                continue;
            }
            if (!first && column + 1 + token.length() > WIDTH) {
                text.append("\n").append(" ".repeat(indent));
                column = indent;
            } else if (!first) {
                text.append(" ");
                column++;
            }
            text.append(token);
            column += token.length();
            first = false;
        }
        return text.append("\n").toString();
    }
}
