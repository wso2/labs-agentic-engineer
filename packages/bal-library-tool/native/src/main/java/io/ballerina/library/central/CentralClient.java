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

package io.ballerina.library.central;

import com.google.gson.JsonArray;
import com.google.gson.JsonElement;
import com.google.gson.JsonNull;
import com.google.gson.JsonObject;
import com.google.gson.JsonParser;
import io.ballerina.library.Failure;
import io.ballerina.library.QualifiedName;
import io.ballerina.library.Result;
import io.ballerina.library.Version;
import io.ballerina.library.cache.DocsCache;
import io.ballerina.library.central.schema.CentralDocs;
import io.ballerina.library.central.schema.Schema;

import java.net.URLEncoder;
import java.nio.charset.StandardCharsets;
import java.time.ZonedDateTime;
import java.time.format.DateTimeFormatter;
import java.time.format.DateTimeParseException;
import java.util.ArrayList;
import java.util.Comparator;
import java.util.List;

/**
 * Everything that talks to Ballerina Central.
 *
 * <p>This is the boundary: it is the only module that handles untyped JSON off a socket, and the only one that
 * can fail for reasons outside the process. Callers get a {@link Result} — a network hiccup is a value here,
 * not an exception threading through the render pipeline.
 *
 * @since 0.1.0
 */
public final class CentralClient {

    public static final String CENTRAL_BASE_URL = "https://api.central.ballerina.io/2.0/";

    /**
     * How long Central's answer to "what is the latest version" is believed.
     *
     * <p>The measured lookup episode runs 70 to 260 seconds, so ten minutes spans a whole episode without a
     * second registry round trip — they cost 1.0 to 1.5s each — while a package published mid-run is still
     * picked up. It is the one mutable response this reader caches; a docs payload for a named version is
     * immutable and never expires.
     */
    public static final long LATEST_TTL_MS = 600_000;

    /** How many hits one search asks Central for. */
    private static final int SEARCH_LIMIT = 30;

    /**
     * The pull count below which a package is treated as unadopted and moved to the end of the results.
     *
     * <p>Measured against Central rather than guessed. For {@code q=http client} its relevance order is right at
     * the top — {@code ballerina/http} first — and the damage is four abandoned packages salted through the middle:
     * {@code tharmigank/http.client.wrapper} at 1 pull ranks FOURTH, {@code sabtharm/http} at 18 ranks seventh, and
     * {@code lakshansivagnanasothy/client_stubs} at 122 ranks eighth. An agent reading top-down picks one of them.
     *
     * <p>1,000 is where the two populations separate on this evidence. Everything below it in the measured samples
     * is a personal experiment; the lowest-pull packages a caller might legitimately want — {@code ballerina/mqtt}
     * at 2,460 and {@code choreo/mediation.log_message} at 2,890 — sit clearly above it. A low count is not a
     * verdict on quality, only on adoption, which is why these are demoted rather than dropped.
     */
    private static final long ADOPTION_FLOOR = 1_000;

    private CentralClient() {
    }

    /**
     * A version, and the two things a caller may need to know about where it came from.
     *
     * @param stale the registry was unreachable and this came off disk unverified
     * @param supplied the CALLER chose this version — {@code --version}, or a {@code Dependencies.toml} the
     *     reader was pointed at — rather than the reader resolving it. It decides what a later 404 from the
     *     docs endpoint means: a version the caller chose is theirs to correct, and one the reader resolved is
     *     not, so advice about changing it would name a command they never wrote.
     */
    public record ResolvedVersion(Version version, boolean stale, boolean supplied) {

        public ResolvedVersion(Version version, boolean stale) {
            this(version, stale, false);
        }
    }

    // -----------------------------------------------------------------------
    // The retry loop
    // -----------------------------------------------------------------------

    /**
     * Statuses worth trying again. A 404 is an answer — the package is not there — and retrying it only spends
     * the caller's budget.
     */
    private static boolean isRetryableStatus(int status) {
        return status == 429 || status == 502 || status == 503 || status == 504;
    }

    /** {@code Retry-After} in either of its legal forms, as milliseconds, or {@code -1}. */
    public static long parseRetryAfter(String header, long nowMs) {
        if (header == null) {
            return -1;
        }
        String trimmed = header.trim();
        if (trimmed.isEmpty()) {
            return -1;
        }
        try {
            return Long.parseLong(trimmed) * 1000;
        } catch (NumberFormatException notAnInteger) {
            // Fall through to the date form.
        }
        try {
            ZonedDateTime when = ZonedDateTime.parse(trimmed, DateTimeFormatter.RFC_1123_DATE_TIME);
            return Math.max(0, when.toInstant().toEpochMilli() - nowMs);
        } catch (DateTimeParseException notADate) {
            return -1;
        }
    }

    /** Exponential backoff with up to 25% jitter, so parallel callers do not resonate. */
    public static long backoffMs(int attempt, long baseDelayMs, double jitter) {
        return (long) Math.floor(baseDelayMs * Math.pow(2, attempt) * (1 + jitter * 0.25));
    }

    /**
     * What one attempt produced, before the URL and the attempt count are known.
     *
     * <p>Separate from {@link Failure} on purpose: the retry loop branches on {@code retryable} and
     * {@code retryAfterMs}, which a finished {@code Failure} has no field for and no caller should see.
     */
    private sealed interface Outcome {

        record Body(JsonElement value) implements Outcome { }

        /** {@code retryAfterMs} is negative when upstream did not say. */
        record Spent(String message, Integer status, boolean timedOut, boolean retryable, long retryAfterMs)
                implements Outcome { }
    }

    private static Outcome attemptFetch(String url, HttpOptions options) {
        HttpTransport.Reply reply = options.transport().get(url, options.timeoutMs());
        return switch (reply) {
            case HttpTransport.Reply.TimedOut ignored -> new Outcome.Spent(null, null, true, true, -1);
            case HttpTransport.Reply.Failed failed ->
                    new Outcome.Spent(failed.message(), null, false, true, -1);
            case HttpTransport.Reply.Answered answered -> {
                if (!answered.isOk()) {
                    boolean retryable = isRetryableStatus(answered.status());
                    long retryAfterMs =
                            retryable ? parseRetryAfter(answered.retryAfter(), options.now()) : -1;
                    yield new Outcome.Spent(
                            "HTTP " + answered.status(), answered.status(), false, retryable, retryAfterMs);
                }
                try {
                    JsonElement parsed = JsonParser.parseString(answered.body());
                    yield new Outcome.Body(parsed == null ? JsonNull.INSTANCE : parsed);
                } catch (RuntimeException malformed) {
                    // Upstream serving something that is not JSON is not a transient condition.
                    yield new Outcome.Spent(
                            "malformed JSON: " + malformed.getMessage(), null, false, false, -1);
                }
            }
        };
    }

    /**
     * GET a JSON document, retrying the failures that are worth retrying and stopping at a wall-clock budget.
     *
     * <p>Retries live here and nowhere else: Central is a remote service that can 429 or 5xx for reasons that
     * pass, whereas everything else this package does is local and deterministic.
     */
    public static Result<JsonElement> fetchJson(String url, HttpOptions options) {
        long deadline = options.now() + options.budgetMs();
        Outcome.Spent last = null;

        for (int attempt = 0; attempt < options.maxAttempts(); attempt++) {
            if (attempt > 0) {
                long remaining = deadline - options.now();
                if (remaining <= 0) {
                    break;
                }
                long wait = last != null && last.retryAfterMs() >= 0
                        ? last.retryAfterMs()
                        : backoffMs(attempt - 1, options.baseDelayMs(), options.jitter());
                options.sleep(Math.min(remaining, wait));
            }
            switch (attemptFetch(url, options)) {
                case Outcome.Body body -> {
                    return Result.ok(body.value());
                }
                case Outcome.Spent spent -> {
                    if (!spent.retryable()) {
                        return Result.err(toFailure(spent, url, attempt + 1, options.budgetMs()));
                    }
                    last = spent;
                }
            }
        }

        if (last == null) {
            return Result.err(new Failure.Upstream(
                    url, options.maxAttempts(), "no attempt was made", Failure.UPSTREAM_SUGGESTION, null));
        }
        return Result.err(toFailure(last, url, options.maxAttempts(), options.budgetMs()));
    }

    private static Failure toFailure(Outcome.Spent spent, String url, int attempts, long budgetMs) {
        if (spent.timedOut()) {
            return new Failure.Timeout(url, budgetMs, Failure.TIMEOUT_SUGGESTION);
        }
        return new Failure.Upstream(
                url, attempts, spent.message(), Failure.UPSTREAM_SUGGESTION, spent.status());
    }

    // -----------------------------------------------------------------------
    // Version resolution
    // -----------------------------------------------------------------------

    /**
     * The version to read a coordinate at, whether it names a package or a module of one.
     *
     * <p>The registry lists PACKAGES. A module of a package has no row of its own, so
     * {@code ballerinax/aws.auth} — the type of the only required field of the only record
     * {@code ballerinax/aws.s3}'s constructor takes — resolved to nothing and the reader could only tell the
     * caller to pin a version by hand. Measured over a seven-case eval sweep that instruction cost seven
     * lookups across two runs and produced both of the sweep's dead-end exits, because the version it asks for
     * is printed in a footer the caller has usually not seen.
     *
     * <p>Nothing about the document was ever missing: {@code docs/<org>/<module>/<version>} answers for a
     * module perfectly well, at the version of the package that contains it. So the module's version is one
     * question the registry CAN answer — asked about the parent — and this asks it rather than delegating it.
     *
     * <p>The full name is always tried first, because a dotted name is far more often a package than a module:
     * {@code ballerinax/googleapis.sheets} is its own package and must not cost a second round trip. Only a
     * {@code PackageNotFound} falls back, and then to each shorter prefix in turn — {@code a.b.c} is a module
     * of {@code a.b} if that exists, and of {@code a} if it does not, both of which are legal. A wrong guess
     * cannot survive: a version the module was not published at is a 404 from the docs endpoint.
     */
    public static Result<ResolvedVersion> resolveLatestVersion(QualifiedName qualified, HttpOptions options) {
        Result<ResolvedVersion> direct = resolvePublishedVersion(qualified, options);
        if (direct.isOk() || !(direct.failure() instanceof Failure.PackageNotFound)) {
            return direct;
        }

        List<QualifiedName> parents = containingPackages(qualified);
        for (QualifiedName parent : parents) {
            Result<ResolvedVersion> viaParent = resolvePublishedVersion(parent, options);
            if (viaParent.isOk()) {
                // Recorded under the MODULE's own key, so a second lookup of it costs one call rather than
                // re-walking the prefixes. The entry is as true as the parent's: a module carries its
                // package's version, which is what makes reading it at that version correct in the first place.
                options.cache().writeLatest(
                        new DocsCache.PackageKey(qualified.org(), qualified.name()),
                        new DocsCache.LatestEntry(viaParent.value().version().text(), options.now()));
                return viaParent;
            }
        }
        return parents.isEmpty() ? direct : Result.err(noContainingPackage(qualified, parents));
    }

    /**
     * The packages a dotted coordinate could be a module of, longest first.
     *
     * <p>Empty for an undotted name, which is every ordinary package — so the common path adds no work and
     * cannot reach the fallback at all.
     */
    private static List<QualifiedName> containingPackages(QualifiedName qualified) {
        List<QualifiedName> parents = new ArrayList<>();
        String name = qualified.name();
        for (int dot = name.lastIndexOf('.'); dot > 0; dot = name.lastIndexOf('.', dot - 1)) {
            Result<QualifiedName> parent = QualifiedName.parse(qualified.org() + "/" + name.substring(0, dot));
            if (parent.isOk()) {
                parents.add(parent.value());
            }
        }
        return parents;
    }

    /**
     * The dotted name resolved as neither a package nor a module of one.
     *
     * <p>It names what was tried, because the caller's next move differs by which half is wrong: a misspelled
     * module of a real package is a different fix from a misspelled package, and after the walk above the
     * reader knows which it is looking at.
     */
    private static Failure noContainingPackage(QualifiedName qualified, List<QualifiedName> parents) {
        StringBuilder tried = new StringBuilder();
        for (QualifiedName parent : parents) {
            tried.append(tried.isEmpty() ? "" : ", ").append(parent.qualified());
        }
        return new Failure.PackageNotFound(
                qualified.qualified(),
                "Central publishes no package under this name, and none of the packages it could be a module "
                        + "of exists either (tried " + tried + "). Check the org/name spelling; "
                        + "`bal library find " + qualified.name() + "` lists what Central publishes.");
    }

    /**
     * The latest published version of one package.
     *
     * <p>{@code registry/packages/<org>/<name>} answers with just this package's versions, newest first. The
     * alternative — listing the org and filtering client-side — costs about 45 seconds for {@code ballerinax},
     * which has roughly a thousand packages, and that cost lands on every lookup an agent makes without an
     * explicit version.
     */
    private static Result<ResolvedVersion> resolvePublishedVersion(QualifiedName qualified, HttpOptions options) {
        DocsCache cache = options.cache();
        DocsCache.PackageKey key = new DocsCache.PackageKey(qualified.org(), qualified.name());

        // `--refresh` re-resolves unconditionally. An earlier draft made the re-download conditional on the
        // version having changed, which made the flag a no-op in exactly the case its own error message
        // recommends it for.
        if (!options.refresh()) {
            DocsCache.LatestEntry entry = cache.readLatest(key);
            // The lower bound matters as much as the TTL: a clock that jumped backwards leaves a
            // future-stamped entry looking fresh forever.
            if (entry != null && options.now() >= entry.atMs()
                    && options.now() - entry.atMs() < LATEST_TTL_MS) {
                Result<Version> cached = Version.parse(entry.version());
                if (cached.isOk()) {
                    return Result.ok(new ResolvedVersion(cached.value(), false));
                }
            }
        }

        String url = CENTRAL_BASE_URL + "registry/packages/" + encode(qualified.org())
                + "/" + encode(qualified.name());
        Result<JsonElement> response = fetchJson(url, options);
        if (!response.isOk()) {
            // Central answers an unpublished org/name with a 400, not a 404. Either way the fact is "no such
            // package", and reporting it as a transport error would send the caller looking at the network for
            // a typo.
            Integer status = response.failure() instanceof Failure.Upstream upstream ? upstream.status() : null;
            if (status != null && (status == 400 || status == 404)) {
                return Result.err(notFound(qualified));
            }
            Version offline = offlineVersion(cache, key, options);
            if (offline != null) {
                return Result.ok(new ResolvedVersion(offline, true));
            }
            return response.cast();
        }

        String latest = Coordinates.newestVersion(response.value());
        if (latest == null) {
            return Result.err(notFound(qualified));
        }
        // Through the parser rather than trusted: this is a string off the network, and the cache turns it
        // into a path segment.
        Result<Version> parsed = Version.parse(latest);
        if (!parsed.isOk()) {
            return parsed.cast();
        }
        cache.writeLatest(key, new DocsCache.LatestEntry(parsed.value().text(), options.now()));
        return Result.ok(new ResolvedVersion(parsed.value(), false));
    }

    /**
     * The best version answer available with the registry unreachable: an expired {@code latest} entry first,
     * then the newest docs payload already on disk.
     *
     * <p>Without this, a warm cached payload plus one registry blip is a hard failure that can burn the
     * client's full budget — four times over in a four-invocation episode. The {@code stale} flag is how the
     * caller learns to say so on the provenance line rather than claiming a version it did not verify.
     */
    private static Version offlineVersion(DocsCache cache, DocsCache.PackageKey key, HttpOptions options) {
        DocsCache.LatestEntry expired = cache.readLatest(key);
        if (expired != null) {
            Result<Version> parsed = Version.parse(expired.version());
            // Only trust a stamp that is not from the future; a bogus one is no better than the listing below.
            if (parsed.isOk() && options.now() >= expired.atMs()) {
                return parsed.value();
            }
        }
        for (String candidate : cache.listVersions(key)) {
            Result<Version> parsed = Version.parse(candidate);
            if (parsed.isOk()) {
                return parsed.value();
            }
        }
        return null;
    }

    /**
     * The registry had no row for this name.
     *
     * <p>A dotted name used to get separate advice here — that it was probably a MODULE, and that the caller
     * should pin a version by hand. {@link #resolveLatestVersion} now does that walk itself, so this failure
     * only reaches a caller for a name that is not a module of anything either, and the message that says so
     * is {@link #noContainingPackage}. Advice about a recovery the reader has already attempted would be worse
     * than none: it reads as an untried option.
     */
    private static Failure notFound(QualifiedName qualified) {
        return new Failure.PackageNotFound(
                qualified.qualified(),
                "Check the org/name spelling; `bal library find <name>` lists what Central publishes.");
    }

    // -----------------------------------------------------------------------
    // The docs payload
    // -----------------------------------------------------------------------

    /**
     * The API docs for one published version, from disk when they are already there.
     *
     * <p>This is where the cache belongs: above the retry loop, so a hit costs no attempt, and below the
     * schema, so what gets stored is not derived from our own code. It is also the reason the addressed verbs
     * are affordable at all — at 4.9 to 6.6 seconds and 12.4MB per invocation the CLI can only be asked once
     * per package, which is what forces a 22,829-line document to be navigated by hand. Once re-opening a
     * package is cheap, four precise questions beat one big answer.
     *
     * <p>ANY problem with a cached entry is a miss, never a failure: a missing file, an unreadable one, a
     * truncated one, one that is not JSON, one the schema no longer accepts, one whose coordinates do not
     * match its own path. Each of those drops the entry and uses the network, so a corrupt entry cannot
     * produce a wrong document and heals on the next successful fetch.
     */
    public static Result<CentralDocs> fetchDocs(
            QualifiedName qualified, ResolvedVersion resolved, HttpOptions options) {
        Version version = resolved.version();
        DocsCache cache = options.cache();
        DocsCache.DocsKey key =
                new DocsCache.DocsKey(qualified.org(), qualified.name(), version.text());
        String label = qualified.versioned(version);

        if (options.refresh()) {
            cache.removeDocs(key);
        } else {
            JsonElement cached = cache.readDocs(key);
            if (cached != null) {
                Result<CentralDocs> parsed = Coordinates.match(cached, qualified, version)
                        ? Schema.parse(cached, label)
                        : null;
                if (parsed != null && parsed.isOk()) {
                    return parsed;
                }
                cache.removeDocs(key);
            }
        }

        String url = CENTRAL_BASE_URL + "docs/" + encode(qualified.org())
                + "/" + encode(qualified.name()) + "/" + encode(version.text());
        Result<JsonElement> response = fetchJson(url, options);
        if (!response.isOk()) {
            // 404 here is specific: the org/name may well exist, this VERSION does not — and which half the
            // caller can act on depends on who chose the version. A version they passed is theirs to correct.
            // One the reader resolved is not: telling them to "omit the version" names what they already did,
            // and for a module resolved through its containing package the wrong half is the module name.
            if (response.failure() instanceof Failure.Upstream upstream
                    && upstream.status() != null && upstream.status() == 404) {
                return Result.err(new Failure.PackageNotFound(label, missingVersion(
                        qualified, version, resolved.supplied(), options)));
            }
            return response.cast();
        }
        Result<CentralDocs> parsed = Schema.parse(response.value(), label);
        if (!parsed.isOk()) {
            return parsed.cast();
        }
        // Written only after it parses: a payload this reader cannot read is not worth storing, and storing it
        // would make every later run pay the same drift.
        cache.writeDocs(key, response.value());
        return parsed;
    }

    /**
     * The docs endpoint answered 404: the org/name may well exist, this VERSION does not.
     *
     * <p>T10, closed in the failure rather than in the grammar. Which half the caller can act on depends on who
     * chose the version — and since the redesign, NEITHER answer is "omit the version", because there is no
     * version argument to omit. A version this reader resolved is its own problem to explain; a version a
     * {@code Dependencies.toml} locked is a real skew between the project and Central, so the failure NAMES what
     * Central publishes instead of telling the caller to go and look, which no verb would have let them do.
     */
    private static String missingVersion(
            QualifiedName qualified, Version version, boolean supplied, HttpOptions options) {
        if (!supplied) {
            return "Central published no '" + qualified.qualified() + "' at " + version.text()
                    + ", the version resolved for it. Check the name — `bal library find "
                    + qualified.name() + "` lists what Central publishes.";
        }
        String published = publishedVersions(qualified, options);
        return "Your project locks '" + qualified.qualified() + "' at " + version.text()
                + ", which Central does not publish"
                + (published == null ? "" : "; published versions are " + published)
                + ". Reconcile Dependencies.toml with the registry — this tool reads the version the project "
                + "pins, so a lookup and a build see the same one.";
    }

    /**
     * The versions Central lists, as a sentence, or {@code null} when the registry cannot say.
     *
     * <p>Best-effort by design: this runs on a path that has ALREADY failed, so a second failure must degrade the
     * message rather than replace the failure the caller actually hit.
     */
    private static String publishedVersions(QualifiedName qualified, HttpOptions options) {
        String url = CENTRAL_BASE_URL + "registry/packages/" + encode(qualified.org())
                + "/" + encode(qualified.name());
        Result<JsonElement> response = fetchJson(url, options);
        if (!response.isOk()) {
            return null;
        }
        List<String> versions = Coordinates.publishedVersions(response.value());
        return versions.isEmpty() ? null : String.join(", ", versions);
    }

    // -----------------------------------------------------------------------
    // Registry search
    // -----------------------------------------------------------------------

    /**
     * Packages matching a free-text query, with the unadopted ones demoted.
     *
     * <p>Central's RELEVANCE is kept, because measured it is good: {@code ballerinax/kafka} is first for
     * {@code kafka messaging} and {@code ballerina/http} is first for {@code http client}. Sorting the whole list
     * by pull count instead was tried and is worse in a way that matters more — it ranks {@code ballerina/crypto}
     * above {@code ballerinax/kafka} for a Kafka query, because popularity is not relevance and the most-pulled
     * package that merely matched is almost never the answer.
     *
     * <p>So the correction is the narrowest one that fixes the actual defect: a STABLE PARTITION on
     * {@link #ADOPTION_FLOOR}, which moves the abandoned packages out of the middle of the list and changes
     * nothing else about the order.
     *
     * <p>Never a cache read or write: the query space is unbounded and the answer is the one thing about Central
     * that genuinely changes.
     */
    public static Result<SearchHit.Results> searchPackages(List<String> keywords, HttpOptions options) {
        String query = String.join(" ", keywords);
        String url = CENTRAL_BASE_URL + "registry/search-packages?q=" + encode(query)
                + "&limit=" + SEARCH_LIMIT + "&offset=0";
        Result<JsonElement> response = fetchJson(url, options);
        if (!response.isOk()) {
            return response.cast();
        }
        JsonElement raw = response.value();
        if (!raw.isJsonObject()) {
            return Result.err(new Failure.Upstream(
                    url, 1, "search answered with something other than an object",
                    Failure.UPSTREAM_SUGGESTION, null));
        }
        JsonObject body = raw.getAsJsonObject();
        List<SearchHit> hits = new ArrayList<>();
        JsonElement packages = body.get("packages");
        if (packages != null && packages.isJsonArray()) {
            for (JsonElement entry : packages.getAsJsonArray()) {
                SearchHit hit = toHit(entry);
                if (hit != null) {
                    hits.add(hit);
                }
            }
        }
        // A stable partition: `false` sorts before `true`, so the adopted keep Central's relevance order and the
        // rest follow in theirs.
        hits.sort(Comparator.comparing(hit -> hit.pullCount() < ADOPTION_FLOOR));
        int total = body.has("count") && body.get("count").isJsonPrimitive()
                ? body.get("count").getAsInt()
                : hits.size();
        return Result.ok(new SearchHit.Results(List.copyOf(hits), total));
    }

    private static SearchHit toHit(JsonElement entry) {
        if (entry == null || !entry.isJsonObject()) {
            return null;
        }
        JsonObject json = entry.getAsJsonObject();
        String org = Json.text(json, "organization");
        String name = Json.text(json, "name");
        if (org.isEmpty() || name.isEmpty()) {
            return null;
        }
        List<String> keywords = new ArrayList<>();
        JsonElement rawKeywords = json.get("keywords");
        if (rawKeywords != null && rawKeywords.isJsonArray()) {
            JsonArray array = rawKeywords.getAsJsonArray();
            for (JsonElement keyword : array) {
                if (keyword.isJsonPrimitive() && keyword.getAsJsonPrimitive().isString()) {
                    keywords.add(keyword.getAsString());
                }
            }
        }
        long pullCount = 0;
        JsonElement pulls = json.get("pullCount");
        if (pulls != null && pulls.isJsonPrimitive() && pulls.getAsJsonPrimitive().isNumber()) {
            pullCount = pulls.getAsLong();
        }
        return new SearchHit(org, name, Json.text(json, "version"), Json.text(json, "summary"),
                List.copyOf(keywords), pullCount);
    }

    private static String encode(String segment) {
        return URLEncoder.encode(segment, StandardCharsets.UTF_8);
    }
}
