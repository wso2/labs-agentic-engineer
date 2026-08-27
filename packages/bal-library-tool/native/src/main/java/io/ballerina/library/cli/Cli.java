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

import io.ballerina.library.Failure;
import io.ballerina.library.LoadedPackage;
import io.ballerina.library.Loader;
import io.ballerina.library.QualifiedName;
import io.ballerina.library.Result;
import io.ballerina.library.central.CentralClient;
import io.ballerina.library.central.HttpOptions;
import io.ballerina.library.central.SearchHit;
import io.ballerina.library.render.Documents;
import io.ballerina.library.symbols.Surface;
import io.ballerina.library.views.Containers;
import io.ballerina.library.views.Find;
import io.ballerina.library.views.Guide;
import io.ballerina.library.views.Overview;
import io.ballerina.library.views.TypeView;
import picocli.CommandLine;

import java.util.List;
import java.util.function.Consumer;

/**
 * {@code bal library} — argv in, exit code out.
 *
 * <p>The stream discipline is the contract:
 *
 * <ul>
 *   <li>stdout — the requested document, and nothing else: no progress, no banner. A usage request is a request
 *       like any other, so {@code --help} is that document
 *   <li>stderr — on failure, one JSON object matching {@link Failure}, and nothing else
 *   <li>exit 0 — success, and stdout is COMPLETE
 *   <li>exit 1 — every failure, whatever went wrong. What to do next is {@code kind} and {@code suggestion} in
 *       the JSON, never the code (ADR-0015)
 * </ul>
 *
 * <p>VERB-FIRST, not mode flags. Eight distinct nouns should not be eight modifiers on one command, and a LEADING
 * verb is the form that is safe under version skew: a verb has no {@code /}, so a stale binary fails it against
 * the qualified-name pattern and says {@code validation}, naming the verbs it does know. A verb placed after the
 * package is the unsafe form — a stale binary reads it as a version and reports {@code package-not-found}, which
 * the skill teaches means "retry".
 *
 * <p>No {@link System#exit} here, and no environment read: the cache, the transport and the project directory
 * arrive as arguments, so a test drives the real command — parsing, streams and exit codes together — against a
 * recorded payload and a temporary directory. {@link LibraryTool} is the only place that touches the process.
 *
 * @since 0.1.0
 */
public final class Cli {

    private Cli() {
    }

    /** Where the two streams go. Injected so a test can capture both without a subprocess. */
    public record Streams(Consumer<String> out, Consumer<String> errorOut) { }

    public static int run(List<String> argv, Streams streams, HttpOptions http) {
        return run(argv, streams, http, null);
    }

    /**
     * @param projectDir the Ballerina project the process is standing in, or {@code null}. Discovered by
     *     {@link LibraryTool}, never by a flag — see {@link Commands}
     */
    public static int run(List<String> argv, Streams streams, HttpOptions http, String projectDir) {
        Commands.Grammar grammar = Commands.Grammar.create();

        CommandLine.ParseResult parsed;
        try {
            parsed = grammar.line().parseArgs(argv.toArray(new String[0]));
        } catch (CommandLine.ParameterException cause) {
            // picocli's own error printing is deliberately never used: stderr has to hold exactly one `Failure`
            // object, and picocli would write a usage block beside it.
            return fail(describeParseError(cause, grammar), streams);
        }

        // A usage request is answered, not failed: it goes to stdout at exit 0, because "exit 0 means stdout is
        // complete" is what a redirecting caller relies on, and usage on stderr under a zero code would hand it an
        // empty file.
        if (!parsed.hasSubcommand() || parsed.isUsageHelpRequested()) {
            streams.out().accept(Usage.root(grammar));
            return 0;
        }

        CommandLine.ParseResult verb = parsed.subcommand();
        String name = verb.commandSpec().name();
        if (verb.isUsageHelpRequested()) {
            streams.out().accept(Usage.verb(grammar, name));
            return 0;
        }

        Failure argumentError = validate(name, grammar, verb);
        if (argumentError != null) {
            return fail(argumentError, streams);
        }

        // `--refresh` is a verb flag, so it is only known once the verb's arguments are parsed. The transport and
        // the cache arrive from the process wrapper, so the options are rebuilt here rather than there.
        HttpOptions resolved = http.withRefresh(refreshOf(grammar, name));
        Loader.LoadOptions options = new Loader.LoadOptions(resolved, projectDir);

        Result<String> document = dispatch(grammar, name, options, resolved);
        if (!document.isOk()) {
            return fail(document.failure(), streams);
        }
        // The one point every document passes through, which is why the length stamp goes here: a view added
        // later carries it without being told. See Documents.withLength for what it defends against.
        streams.out().accept(Documents.withLength(document.value()));
        return 0;
    }

    // -----------------------------------------------------------------------
    // The verbs
    // -----------------------------------------------------------------------

    private static Result<String> dispatch(
            Commands.Grammar grammar, String name, Loader.LoadOptions options, HttpOptions http) {
        Commands.Client client = grammar.client();
        Commands.Klass klass = grammar.klass();
        Commands.Funcs funcs = grammar.funcs();
        Commands.Type type = grammar.type();
        Commands.Guide guide = grammar.guide();
        return switch (name) {
            case "find" -> findDocument(grammar.find(), http);
            case "overview" -> render(grammar.overview().pkg, options, loaded ->
                    Result.ok(Overview.render(loaded,
                            new Overview.Options(grammar.overview().search.query))));
            case "client" -> render(client.pkg, options, loaded ->
                    Containers.render(loaded, Surface.Scope.CLIENT, containerOptions(
                            client.selectors, client.search.query, client.depth)));
            case "class" -> render(klass.pkg, options, loaded ->
                    Containers.render(loaded, Surface.Scope.CLASS, containerOptions(
                            klass.selectors, klass.search.query, klass.depth)));
            case "funcs" -> render(funcs.pkg, options, loaded ->
                    Containers.render(loaded, Surface.Scope.MODULE, containerOptions(
                            funcs.selectors, funcs.search.query, funcs.depth)));
            case "type" -> render(type.pkg, options, loaded ->
                    TypeView.render(loaded, new TypeView.Options(
                            type.names == null ? List.of() : type.names,
                            type.search.query, type.depth.resolve)));
            case "guide" -> render(guide.pkg, options, loaded ->
                    Guide.render(loaded, new Guide.Options(
                            guide.chunk, guide.search.query, guide.module)));
            case "api" -> render(grammar.api().pkg, options, loaded -> Result.ok(apiDocument(loaded)));
            default -> Result.err(unknownVerb(name, grammar));
        };
    }

    private static Containers.Options containerOptions(
            List<String> selectors, String search, Commands.Depth depth) {
        return new Containers.Options(
                selectors == null ? List.of() : selectors, search, depth.resolve, depth.all);
    }

    private static Result<String> findDocument(Commands.Find find, HttpOptions http) {
        Result<SearchHit.Results> results = CentralClient.searchPackages(find.keywords, http);
        return results.isOk()
                ? Result.ok(Find.render(find.keywords, results.value()))
                : results.cast();
    }

    /** How one verb turns a loaded package into its document. */
    private interface View {

        Result<String> of(LoadedPackage loaded);
    }

    /**
     * Load once, then render. The load is the expensive half — one payload of up to 12.4MB — so it happens exactly
     * once per invocation, which is also what makes several addressed calls share one cached copy.
     */
    private static Result<String> render(String pkg, Loader.LoadOptions options, View view) {
        Result<QualifiedName> qualified = QualifiedName.parse(pkg);
        if (!qualified.isOk()) {
            return qualified.cast();
        }
        Result<LoadedPackage> loaded = Loader.loadPackage(qualified.value(), options);
        return loaded.isOk() ? view.of(loaded.value()) : loaded.cast();
    }

    /**
     * The resolved coordinates lead the document: {@code api} is the code register, where a comment is the only
     * thing a Ballerina file can carry, and a 22,829-line document left over from an earlier lookup is otherwise
     * indistinguishable from a fresh one.
     */
    private static String apiDocument(LoadedPackage loaded) {
        return Documents.headerComment("Resolved: " + loaded.label(), loaded.warning()) + "\n"
                + Documents.toSyntaxString(loaded.library());
    }

    // -----------------------------------------------------------------------
    // Argument errors
    // -----------------------------------------------------------------------

    /** Whichever verb was matched, and whether it asked for a fresh payload. */
    private static boolean refreshOf(Commands.Grammar grammar, String name) {
        return switch (name) {
            case "overview" -> grammar.overview().resolution.refresh;
            case "client" -> grammar.client().resolution.refresh;
            case "class" -> grammar.klass().resolution.refresh;
            case "funcs" -> grammar.funcs().resolution.refresh;
            case "type" -> grammar.type().resolution.refresh;
            case "guide" -> grammar.guide().resolution.refresh;
            case "api" -> grammar.api().resolution.refresh;
            // `find` reads no package and never touches the cache, so it does not declare the flag at all and
            // picocli rejects it there.
            default -> false;
        };
    }

    /** Every argument check picocli cannot make, in the order a caller meets them. */
    private static Failure validate(
            String name, Commands.Grammar grammar, CommandLine.ParseResult verb) {
        Failure smuggled = rejectFlagShapedValues(verb);
        if (smuggled != null) {
            return smuggled;
        }
        Failure version = rejectVersionArguments(grammar, name);
        if (version != null) {
            return version;
        }
        return rejectEmptySelections(grammar, name);
    }

    /**
     * The emptiness checks picocli is not allowed to make.
     *
     * <p>{@code <org/name>} is declared with {@code arity = "1"} and enforced by the parser, but a variable-arity
     * slot cannot be: a minimum arity there makes picocli consume an unrecognised flag as a value (see
     * {@code Commands.Find#keywords}). So these stay here, and they are the only argument validation left.
     */
    private static Failure rejectEmptySelections(Commands.Grammar grammar, String name) {
        if (!Commands.REQUIRES_POSITIONALS.contains(name)) {
            return null;
        }
        if ("find".equals(name)) {
            List<String> keywords = grammar.find().keywords;
            if (keywords == null || keywords.isEmpty()) {
                return new Failure.Validation(
                        "'find' needs at least one keyword.",
                        "Pass what you are looking for, e.g. bal library find kafka messaging.");
            }
        }
        if ("type".equals(name)) {
            // A bare `type <pkg>` must not become a second `api`, so it needs either a name or a query — and the
            // failure names BOTH ways forward rather than only the one the caller happened to leave out.
            List<String> names = grammar.type().names;
            boolean searched = grammar.type().search.query != null
                    && !grammar.type().search.query.isBlank();
            if ((names == null || names.isEmpty()) && !searched) {
                return new Failure.Validation(
                        "'type' needs a declaration name or a search query.",
                        "Name it — bal library type ballerinax/github FullRepository — or search for it: "
                                + "bal library type ballerinax/github -s \"repository\".");
            }
        }
        return null;
    }

    /** A version, as Central publishes them. No declaration name, selector or path can look like one. */
    private static final java.util.regex.Pattern VERSION_SHAPED =
            java.util.regex.Pattern.compile("^\\d+\\.\\d+\\.\\d+([-+.].*)?$");

    /**
     * A version passed as an argument, when versions are no longer arguments.
     *
     * <p>T11's real fix. The flag is gone and resolution is internal, so a version-shaped token is a caller
     * carrying an old habit — and left alone it would be read as a selector or a declaration name and reported as
     * {@code symbol-not-found} on a "declaration" called {@code 4.6.5}, which names neither the mistake nor what
     * to do. The suggestion states the new rule instead, because there is no argument to move it to.
     */
    private static Failure rejectVersionArguments(Commands.Grammar grammar, String name) {
        List<String> tokens = switch (name) {
            case "client" -> grammar.client().selectors;
            case "class" -> grammar.klass().selectors;
            case "funcs" -> grammar.funcs().selectors;
            case "type" -> grammar.type().names;
            case "guide" -> grammar.guide().chunk == null ? null : List.of(grammar.guide().chunk);
            default -> null;
        };
        if (tokens == null) {
            return null;
        }
        String misplaced = tokens.stream()
                .filter(token -> VERSION_SHAPED.matcher(token).matches())
                .findFirst()
                .orElse(null);
        if (misplaced == null) {
            return null;
        }
        return new Failure.Validation(
                "'" + misplaced + "' looks like a version, and this tool does not take one.",
                "The version is resolved from your project: run inside the component whose "
                        + "Dependencies.toml locks it, and the lookup matches what `bal build` compiles "
                        + "against. Outside a project it is Central's latest. Drop the argument.");
    }

    /**
     * {@code -s -r} must not silently take {@code -r} as the query.
     *
     * <p>picocli 4.0.1 — the version on the Ballerina distribution's classpath — consumes the next token as an
     * option value even when it looks like a flag, and the setting that changes that arrived in 4.4. So the check
     * is here: a value beginning with {@code -} followed by a letter is a caller who forgot the value, not a
     * package named {@code -r}. This is exactly the silent class the design refuses, and it is a few lines rather
     * than a newer dependency the distribution does not ship.
     */
    private static Failure rejectFlagShapedValues(CommandLine.ParseResult verb) {
        for (CommandLine.Model.OptionSpec option : verb.matchedOptions()) {
            Object value = option.getValue();
            if (value instanceof String text && FLAG_SHAPED.matcher(text).matches()) {
                return new Failure.Validation(
                        "Missing value for " + option.longestName() + "; it took '" + text
                                + "' as its value.",
                        "Pass the value after the flag, or as " + option.longestName() + "=value.");
            }
        }
        return null;
    }

    /** {@code --all} or {@code -r}, but not a negative number or a lone hyphen. */
    private static final java.util.regex.Pattern FLAG_SHAPED =
            java.util.regex.Pattern.compile("^--?[A-Za-z][\\w-]*$");

    /**
     * picocli's parse errors, as this command's contract.
     *
     * <p>Every one of them is {@code validation} with a {@code suggestion}, because the recovery is always an edit
     * to the argument list. The thing that must NOT happen is an unrecognised flag becoming a positional: that is
     * how {@code --refresh} used to resolve as the VERSION and report {@code package-not-found}, which the skill
     * teaches means "Central could not answer, run it once more".
     */
    private static Failure describeParseError(
            CommandLine.ParameterException cause, Commands.Grammar grammar) {
        if (cause instanceof CommandLine.UnmatchedArgumentException unmatched) {
            List<String> tokens = unmatched.getUnmatched();
            String token = tokens.isEmpty() ? "" : tokens.get(0);
            boolean atRoot = "library".equals(unmatched.getCommandLine().getCommandSpec().name());
            if (atRoot && !token.startsWith("-")) {
                return unknownVerb(token, grammar);
            }
            if (token.startsWith("-")) {
                return foreignFlag(token, unmatched.getCommandLine().getCommandSpec().name(), grammar);
            }
            return new Failure.Validation(
                    "Unexpected argument '" + token + "'.",
                    "Run `bal library " + unmatched.getCommandLine().getCommandSpec().name()
                            + " --help` for what this verb takes.");
        }
        if (cause instanceof CommandLine.MissingParameterException missing) {
            return missingArgument(missing);
        }
        return new Failure.Validation(
                firstLine(cause.getMessage()),
                "Run with --help for usage. Known flags are " + UsageRenderer.knownFlags(grammar) + ".");
    }

    /**
     * An argument the grammar requires and the caller did not pass.
     *
     * <p>picocli raises this now that {@code <org/name>} is declared with {@code arity = "1"}, which replaced the
     * per-verb null check each package verb carried. What that check was worth is the SUGGESTION — picocli's own
     * "Missing required parameter" names the slot but not the shape that goes in it — so the naming survives here,
     * keyed by the slot rather than by the verb it was written under.
     */
    private static Failure missingArgument(CommandLine.MissingParameterException missing) {
        String verb = missing.getCommandLine().getCommandSpec().name();
        boolean missingPackage = missing.getMissing().stream()
                .anyMatch(arg -> "<org/name>".equals(arg.paramLabel()));
        if (missingPackage) {
            return new Failure.Validation(
                    "'" + verb + "' needs a package.",
                    "Pass 'org/name', e.g. bal library " + verb + " ballerinax/github.");
        }
        // A missing OPTION value rather than a positional: the flag was passed with nothing after it.
        return new Failure.Validation(
                firstLine(missing.getMessage()),
                "Pass the value after the flag, or as --flag=value.");
    }

    /**
     * A flag this verb does not take.
     *
     * <p>The rejection itself is structural — {@code --module} is declared on {@code guide} and nowhere else — so
     * all this adds is the sentence that says which verb wanted it. Without it the message would be "Unknown
     * option '--module'", which is true and leaves the caller to guess.
     */
    private static Failure foreignFlag(String token, String verb, Commands.Grammar grammar) {
        String flag = token.split("=", 2)[0];
        List<String> owners = grammar.flagOwners().get(flag);
        if (owners == null) {
            return new Failure.Validation(
                    "Unknown option '" + token + "'.",
                    "Known flags are " + UsageRenderer.knownFlags(grammar)
                            + ". Run with --help for usage.");
        }
        String quoted = UsageRenderer.sentenceList(owners.stream().map(owner -> "'" + owner + "'").toList());
        // Named by its LONG form whichever spelling was typed, so the sentence reads the same as the flag list on
        // the verb pages that document it.
        String canonical = grammar.canonicalFlag(flag);
        return new Failure.Validation(
                "'" + verb + "' does not take " + flag + ".",
                canonical + " belongs to " + quoted + ". Drop it, or use one of those verbs.");
    }

    private static Failure unknownVerb(String token, Commands.Grammar grammar) {
        String verbs = "The verbs are " + String.join(", ", grammar.verbs()) + ".";
        String suggestion = token.contains("/")
                ? "A verb comes first: try `bal library overview " + token + "`. " + verbs
                : verbs;
        return new Failure.Validation("'" + token + "' is not a verb.", suggestion);
    }

    private static String firstLine(String message) {
        if (message == null) {
            return "invalid arguments";
        }
        return message.split("\n", -1)[0];
    }

    /** One code for every failure. The JSON is where a caller reads what happened and what to do about it. */
    private static int fail(Failure failure, Streams streams) {
        streams.errorOut().accept(failure.describe() + "\n");
        return 1;
    }
}
