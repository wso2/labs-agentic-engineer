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

import com.google.gson.JsonObject;
import io.ballerina.cli.BLauncherCmd;
import io.ballerina.library.cache.CacheLocation;
import io.ballerina.library.cache.DiskCache;
import io.ballerina.library.cache.DocsCache;
import io.ballerina.library.central.DependenciesToml;
import io.ballerina.library.central.HttpOptions;
import picocli.CommandLine;

import java.io.PrintStream;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;

/**
 * Entry point for the {@code bal library} CLI tool: the process wrapper.
 *
 * <p>Kept apart from {@link Cli} so the CLI's behaviour is testable without a subprocess. This is ALSO the only
 * class that reads the environment or touches a filesystem the caller did not name, which is what keeps every
 * test in the suite hermetic: they drive {@link Cli#run} with an injected cache and cannot accidentally read or
 * write a developer's real {@code ~/.cache}.
 *
 * <p>{@code bal} hands the whole argument list through unparsed — verified: all ten arguments of a realistic
 * invocation arrive raw — so the tool owns its own grammar, and {@link System#exit} here is what carries the
 * exit code to the shell.
 *
 * @since 0.1.0
 */
@CommandLine.Command(name = "library")
public class LibraryTool implements BLauncherCmd {

    @CommandLine.Parameters(arity = "0..*")
    private List<String> argList;

    /**
     * Declared here only so {@code bal}'s own launcher hands it over.
     *
     * <p>Everything else passes through untouched — verified, all ten arguments of a realistic invocation arrive
     * raw — but {@code --help} is the exception: without a declaration the launcher rejects it with
     * {@code ballerina: unknown option: '--help'} at exit 1, which is the launcher's error rather than ours and
     * not the usage text a caller asked for. Declaring it, then putting it back on the argument list, keeps
     * {@link Cli} the only thing that decides what help means for which verb.
     */
    @CommandLine.Option(names = {"--help", "-h"}, hidden = true)
    private boolean helpFlag;

    private final PrintStream outStream;
    private final PrintStream errStream;

    /** Whether {@link #execute()} ends the process. Off in tests, which assert the code instead. */
    private final boolean exits;

    private int exitCode;

    public LibraryTool() {
        this(System.out, System.err, true);
    }

    /** For tests: both streams captured, and no {@link System#exit}. */
    public LibraryTool(PrintStream outStream, PrintStream errStream) {
        this(outStream, errStream, false);
    }

    private LibraryTool(PrintStream outStream, PrintStream errStream, boolean exits) {
        this.outStream = outStream;
        this.errStream = errStream;
        this.exits = exits;
    }

    /** The code the last {@link #execute()} produced. Only meaningful for the non-exiting constructor. */
    public int exitCode() {
        return exitCode;
    }

    @Override
    public String getName() {
        return "library";
    }

    @Override
    public void execute() {
        List<String> argv = new ArrayList<>(argList == null ? List.of() : argList);
        if (helpFlag) {
            argv.add("--help");
        }
        HttpOptions http = HttpOptions.builder().cache(buildCache()).build();
        Cli.Streams streams = new Cli.Streams(outStream::print, errStream::print);

        int code;
        try {
            code = Cli.run(argv, streams, http, discoverProject());
        } catch (RuntimeException cause) {
            // Nothing in the pipeline throws by design; if something does, it is a defect in this tool and the
            // caller still needs a machine-readable line rather than a Java stack trace on stdout.
            errStream.print(internalFailure(cause) + "\n");
            code = 1;
        }

        outStream.flush();
        errStream.flush();
        this.exitCode = code;
        // Only a non-zero code needs `System.exit`: `bal` exits 0 on its own, and short-circuiting the launcher
        // on the success path would skip whatever it does after a tool returns.
        if (exits && code != 0) {
            System.exit(code);
        }
    }

    /** A defect in this tool, still reported as the one JSON object on stderr the contract promises. */
    private static String internalFailure(Throwable cause) {
        String message = cause.getMessage();
        JsonObject json = new JsonObject();
        json.addProperty("kind", "internal");
        json.addProperty("message", message == null || message.isEmpty()
                ? cause.getClass().getName()
                : message);
        return json.toString();
    }

    /**
     * The first candidate location that actually works.
     *
     * <p>{@link CacheLocation#candidates} is pure and cannot tell whether a directory is writable, so trying
     * them is this class's job — it is already the only one allowed to touch a filesystem the caller did not
     * name. The case this exists for is a container whose {@code $HOME} exists and is read-only, which is a shape
     * a runner genuinely has: without the retry, the default rung would be chosen, fail, and silently disable
     * caching rather than reaching a temp directory.
     *
     * <p>If every candidate fails, the null store is the answer. Never a failure: cache trouble is not the
     * caller's problem.
     */
    private static DocsCache buildCache() {
        CacheLocation.Environment environment = new CacheLocation.Environment(
                System.getenv(),
                property("user.home"),
                property("java.io.tmpdir"),
                property("user.name"));

        List<CacheLocation.Candidate> candidates = CacheLocation.candidates(environment);
        for (CacheLocation.Candidate candidate : candidates) {
            if (candidate instanceof CacheLocation.Candidate.Disabled) {
                return DocsCache.NULL;
            }
            CacheLocation.Candidate.Directory directory = (CacheLocation.Candidate.Directory) candidate;
            Path root = Path.of(directory.root());
            if (!DiskCache.isUsableRoot(root, directory.mode())) {
                continue;
            }
            return DiskCache.at(root, directory.mode());
        }

        // Nothing worked. `describe()` still has to name something for a diagnostic, so the last candidate is
        // reported as the one that was tried.
        CacheLocation.Candidate last = candidates.get(candidates.size() - 1);
        return last instanceof CacheLocation.Candidate.Directory directory
                ? DiskCache.at(Path.of(directory.root()), directory.mode())
                : DocsCache.NULL;
    }

    /** A system property that a stripped container can genuinely be missing. */
    private static String property(String name) {
        String value = System.getProperty(name);
        return value == null ? "" : value;
    }

    /**
     * The Ballerina project this process is standing in, or {@code null}.
     *
     * <p>The whole of what replaced {@code --project-dir}. It is HERE rather than in {@link Cli} because reading
     * the process's own directory is reading the environment, and this class is the only one allowed to — which is
     * what keeps every test in the suite hermetic and what lets {@link Cli#run} be driven against a temporary tree.
     *
     * <p>A project is found by walking up for a {@code Ballerina.toml}, which is what a build does. The version
     * beside each package in the sibling {@code Dependencies.toml} is then the version the component will actually
     * compile against, so a lookup and a build cannot disagree — and no caller had to know to ask for that.
     */
    private static String discoverProject() {
        String cwd = property("user.dir");
        if (cwd.isEmpty()) {
            return null;
        }
        Path project = DependenciesToml.discoverProject(Path.of(cwd));
        return project == null ? null : project.toString();
    }

    @Override
    public void printLongDesc(StringBuilder sb) {
        sb.append(Usage.root(Commands.Grammar.create()));
    }

    /** Deprecated on {@link BLauncherCmd} itself, but still abstract, so it has to be implemented. */
    @Override
    @Deprecated
    public void printUsage(StringBuilder sb) {
        sb.append("  bal library <find|overview|client|class|funcs|type|guide|api> [args]\n");
    }

    @Override
    public void setParentCmdParser(CommandLine parentCmdParser) {
    }
}
