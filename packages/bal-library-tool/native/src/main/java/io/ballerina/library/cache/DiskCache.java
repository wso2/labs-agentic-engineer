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

package io.ballerina.library.cache;

import com.google.gson.JsonElement;
import com.google.gson.JsonObject;
import com.google.gson.JsonParser;

import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.AtomicMoveNotSupportedException;
import java.nio.file.FileAlreadyExistsException;
import java.nio.file.Files;
import java.nio.file.LinkOption;
import java.nio.file.Path;
import java.nio.file.StandardCopyOption;
import java.nio.file.attribute.PosixFileAttributeView;
import java.nio.file.attribute.PosixFilePermission;
import java.nio.file.attribute.PosixFilePermissions;
import java.nio.file.attribute.UserPrincipal;
import java.util.ArrayList;
import java.util.Comparator;
import java.util.List;
import java.util.Set;
import java.util.function.DoubleSupplier;
import java.util.regex.Pattern;
import java.util.stream.Stream;

/**
 * The cache, on disk.
 *
 * <pre>
 *   &lt;root&gt;/v1/docs/&lt;org&gt;/&lt;name&gt;/&lt;version&gt;.json      mode 0600, no TTL
 *   &lt;root&gt;/v1/latest/&lt;org&gt;/&lt;name&gt;.json              {"version":"6.0.0","atMs":…}
 * </pre>
 *
 * <p>{@code v1} is the on-disk format generation, bumped only when the stored bytes change meaning.
 * Deliberately not a build identity — see {@link DocsCache} for why the raw payload is what gets stored.
 *
 * <p>Entries are stored UNCOMPRESSED, exactly as Central served them. Disk is not the constrained resource:
 * a runner's mounts are emptyDirs and the cache does not outlive the run. Compression would add a level to
 * choose, a corruption mode to handle and a compress step on the write path, to save bytes nobody is paying
 * for.
 *
 * <p>Concurrency is structural rather than hypothetical — fan-out is a runner's default and recorded runs
 * show two subagents' shell calls interleaving seconds apart in one container sharing one {@code $HOME}. So
 * every write goes to a per-process temp file and is then moved atomically. There is deliberately no lock and
 * no single-flight: two processes that miss the same package both fetch and both move, the content is
 * equivalent, and no third process can observe a partial file. A lock could outlive the client's own budget
 * and hang a run; a duplicate download is the cheaper failure.
 *
 * @since 0.1.0
 */
public final class DiskCache implements DocsCache {

    private static final String FORMAT = "v1";

    /** Every path segment has to be one of these before it can reach a join. */
    private static final Pattern SAFE_SEGMENT = Pattern.compile("^[A-Za-z0-9_.-]+$");

    private static final Set<PosixFilePermission> ENTRY_MODE =
            PosixFilePermissions.fromString("rw-------");

    private final Path root;
    private final boolean usable;
    private final long pid;
    private final DoubleSupplier random;

    private DiskCache(Path root, boolean usable, long pid, DoubleSupplier random) {
        this.root = root;
        this.usable = usable;
        this.pid = pid;
        this.random = random;
    }

    /**
     * A disk cache at {@code root}, or a store that silently does nothing if the root is not usable. The
     * caller cannot tell the two apart on purpose, and never has to handle a cache error, because there is
     * no cache error to handle.
     */
    public static DiskCache at(Path root, int mode) {
        return at(root, mode, ProcessHandle.current().pid(), Math::random);
    }

    /** {@code pid} and {@code random} are injectable so a concurrency test can force one temp name. */
    public static DiskCache at(Path root, int mode, long pid, DoubleSupplier random) {
        return new DiskCache(root, isUsableRoot(root, mode), pid, random);
    }

    /**
     * Is this root usable, and is it ours?
     *
     * <p>Public because the process wrapper walks the candidate locations and needs to ask before committing
     * to one; creating a store per candidate just to inspect it would create every rung it tried.
     *
     * <p>Symlinks are not followed: a root that is a symlink is refused outright, because following one is
     * how a writable-looking path becomes somebody else's directory. A root owned by another user is refused
     * for the same reason.
     */
    public static boolean isUsableRoot(Path root, int mode) {
        try {
            createDirectories(root, mode);
        } catch (IOException | RuntimeException ignored) {
            return false;
        }
        try {
            if (Files.isSymbolicLink(root) || !Files.isDirectory(root, LinkOption.NOFOLLOW_LINKS)) {
                return false;
            }
            return isOurs(root);
        } catch (IOException | RuntimeException ignored) {
            return false;
        }
    }

    /**
     * Whether the current user owns the directory.
     *
     * <p>There is no {@code getuid()} on the JVM, so this asks the filesystem for the owner principal and
     * compares its name to {@code user.name}. A filesystem that cannot report an owner — which is what
     * Windows does for some volumes — is accepted rather than refused: the check exists to avoid writing into
     * somebody else's directory, and refusing every root we cannot interrogate would disable caching wholesale
     * on a platform the installers support.
     */
    private static boolean isOurs(Path root) throws IOException {
        String expected = System.getProperty("user.name");
        if (expected == null || expected.isEmpty()) {
            return true;
        }
        UserPrincipal owner;
        try {
            owner = Files.getOwner(root, LinkOption.NOFOLLOW_LINKS);
        } catch (UnsupportedOperationException ignored) {
            return true;
        }
        return owner == null || owner.getName().equals(expected) || owner.getName().endsWith("\\" + expected);
    }

    /**
     * {@code mkdir -p} with a mode where the platform has one.
     *
     * <p>{@link PosixFilePermissions} throws on a filesystem with no POSIX view, and the installers support
     * Windows, so the mode is applied only when it means something. Windows inherits the parent's ACL, which
     * for a user's own profile directory is already private.
     */
    private static void createDirectories(Path directory, int mode) throws IOException {
        if (Files.isDirectory(directory)) {
            return;
        }
        if (directory.getFileSystem().supportedFileAttributeViews().contains("posix")) {
            Files.createDirectories(
                    directory, PosixFilePermissions.asFileAttribute(permissions(mode)));
            return;
        }
        Files.createDirectories(directory);
    }

    private static Set<PosixFilePermission> permissions(int mode) {
        StringBuilder bits = new StringBuilder(9);
        int[] masks = {0400, 0200, 0100, 0040, 0020, 0010, 0004, 0002, 0001};
        String letters = "rwxrwxrwx";
        for (int index = 0; index < masks.length; index++) {
            bits.append((mode & masks[index]) != 0 ? letters.charAt(index) : '-');
        }
        return PosixFilePermissions.fromString(bits.toString());
    }

    // -----------------------------------------------------------------------
    // Paths
    // -----------------------------------------------------------------------

    private static boolean isSafeSegment(String segment) {
        return SAFE_SEGMENT.matcher(segment).matches() && !".".equals(segment) && !"..".equals(segment);
    }

    /**
     * The path of an entry, or {@code null} if any part of it is not obviously safe.
     *
     * <p>Three independent checks, because the first is a regex someone could later loosen: every COORDINATE
     * is validated raw, every path segment is validated again after a suffix is attached, and then the
     * RESOLVED path has to still start with the root. {@code QualifiedName} and {@code Version} already
     * reject {@code .} and {@code ..}, which makes all of this the inner guard rather than the only one.
     *
     * <p>The raw check is not redundant with the segment check: {@code ..} with {@code .json} attached
     * becomes {@code ...json}, which is a perfectly ordinary filename and passes. That is harmless in itself
     * — it traverses nothing — but a coordinate this store would not accept as a directory name should not be
     * accepted as a file name either, or the two guards disagree about what a valid key is.
     */
    private Path entryPath(List<String> coordinates, List<String> segments) {
        for (String coordinate : coordinates) {
            if (!isSafeSegment(coordinate)) {
                return null;
            }
        }
        for (String segment : segments) {
            if (!isSafeSegment(segment)) {
                return null;
            }
        }
        Path base = root.toAbsolutePath().normalize();
        Path candidate = base;
        for (String segment : segments) {
            candidate = candidate.resolve(segment);
        }
        candidate = candidate.normalize();
        return candidate.startsWith(base) && !candidate.equals(base) ? candidate : null;
    }

    private Path docsPath(DocsKey key) {
        return entryPath(
                List.of(key.org(), key.name(), key.version()),
                List.of(FORMAT, "docs", key.org(), key.name(), key.version() + ".json"));
    }

    private Path docsDir(PackageKey key) {
        return entryPath(
                List.of(key.org(), key.name()), List.of(FORMAT, "docs", key.org(), key.name()));
    }

    private Path latestPath(PackageKey key) {
        return entryPath(
                List.of(key.org(), key.name()), List.of(FORMAT, "latest", key.org(), key.name() + ".json"));
    }

    // -----------------------------------------------------------------------
    // Reading and writing
    // -----------------------------------------------------------------------

    /** ENOENT, EACCES, a truncated file, a file that is not JSON: all one thing, which is "no entry". */
    private static JsonElement readJson(Path path) {
        try {
            String text = Files.readString(path, StandardCharsets.UTF_8);
            JsonElement parsed = JsonParser.parseString(text);
            return parsed == null || parsed.isJsonNull() ? null : parsed;
        } catch (IOException | RuntimeException ignored) {
            return null;
        }
    }

    /** Temp name in the SAME directory as its target, so the move stays on one filesystem. */
    private Path tempPathFor(Path target) {
        String suffix = Long.toHexString((long) (random.getAsDouble() * 0xffffffffL));
        return target.resolveSibling(target.getFileName() + "." + pid + "-" + suffix + ".tmp");
    }

    private void writeAtomically(Path target, String contents, int mode) {
        if (!usable) {
            return;
        }
        Path temp = tempPathFor(target);
        try {
            createDirectories(target.getParent(), mode);
            Files.writeString(temp, contents, StandardCharsets.UTF_8);
            restrict(temp);
            move(temp, target);
        } catch (IOException | RuntimeException ignored) {
            // No space, no permission, a vanished parent: leave nothing behind and say nothing.
            try {
                Files.deleteIfExists(temp);
            } catch (IOException | RuntimeException alsoIgnored) {
                // The temp file is already gone, or was never created.
            }
        }
    }

    /**
     * The last-writer-wins move, atomic where the platform offers one.
     *
     * <p>A POSIX rename replaces the target and is atomic, which is the whole concurrency story: no third
     * process can observe a partial file. Windows has no rename-over, so the fallback is a replacing move —
     * not atomic, but the only thing available, and the content two writers race with is equivalent.
     */
    private static void move(Path temp, Path target) throws IOException {
        try {
            Files.move(temp, target, StandardCopyOption.ATOMIC_MOVE);
        } catch (AtomicMoveNotSupportedException | FileAlreadyExistsException retry) {
            Files.move(temp, target, StandardCopyOption.REPLACE_EXISTING);
        }
    }

    private static void restrict(Path file) {
        if (!file.getFileSystem().supportedFileAttributeViews().contains("posix")) {
            return;
        }
        try {
            Files.getFileAttributeView(file, PosixFileAttributeView.class).setPermissions(ENTRY_MODE);
        } catch (IOException | RuntimeException ignored) {
            // A filesystem that will not narrow the mode is still a usable cache.
        }
    }

    @Override
    public JsonElement readDocs(DocsKey key) {
        if (!usable) {
            return null;
        }
        Path path = docsPath(key);
        return path == null ? null : readJson(path);
    }

    @Override
    public void writeDocs(DocsKey key, JsonElement payload) {
        Path path = docsPath(key);
        if (path == null) {
            return;
        }
        String contents;
        try {
            contents = payload.toString();
        } catch (RuntimeException ignored) {
            return;
        }
        writeAtomically(path, contents, 0700);
    }

    @Override
    public void removeDocs(DocsKey key) {
        if (!usable) {
            return;
        }
        Path path = docsPath(key);
        if (path == null) {
            return;
        }
        try {
            Files.deleteIfExists(path);
        } catch (IOException | RuntimeException ignored) {
            // Best effort; the next successful fetch overwrites it anyway.
        }
    }

    @Override
    public LatestEntry readLatest(PackageKey key) {
        if (!usable) {
            return null;
        }
        Path path = latestPath(key);
        if (path == null) {
            return null;
        }
        JsonElement raw = readJson(path);
        if (raw == null || !raw.isJsonObject()) {
            return null;
        }
        JsonObject entry = raw.getAsJsonObject();
        JsonElement version = entry.get("version");
        JsonElement atMs = entry.get("atMs");
        boolean valid = version != null && version.isJsonPrimitive() && version.getAsJsonPrimitive().isString()
                && !version.getAsString().isEmpty()
                && atMs != null && atMs.isJsonPrimitive() && atMs.getAsJsonPrimitive().isNumber();
        return valid ? new LatestEntry(version.getAsString(), atMs.getAsLong()) : null;
    }

    @Override
    public void writeLatest(PackageKey key, LatestEntry entry) {
        Path path = latestPath(key);
        if (path == null) {
            return;
        }
        JsonObject json = new JsonObject();
        json.addProperty("version", entry.version());
        json.addProperty("atMs", entry.atMs());
        writeAtomically(path, json.toString(), 0700);
    }

    @Override
    public List<String> listVersions(PackageKey key) {
        if (!usable) {
            return List.of();
        }
        Path directory = docsDir(key);
        if (directory == null) {
            return List.of();
        }
        try (Stream<Path> entries = Files.list(directory)) {
            List<String> versions = new ArrayList<>();
            entries.map(path -> path.getFileName().toString())
                    .filter(file -> file.endsWith(".json"))
                    .map(file -> file.substring(0, file.length() - ".json".length()))
                    .forEach(versions::add);
            versions.sort(Comparator.comparing(version -> version, (a, b) -> Versions.compare(b, a)));
            return List.copyOf(versions);
        } catch (IOException | RuntimeException ignored) {
            // Includes the UncheckedIOException a lazily-evaluated directory stream can throw mid-walk.
            return List.of();
        }
    }

    @Override
    public String describe() {
        if (!usable) {
            return root + " (unusable; caching disabled)";
        }
        return Files.isDirectory(root) ? root + " (writable)" : root + " (unusable; caching disabled)";
    }
}
