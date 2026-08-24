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

import io.ballerina.library.cache.DocsCache;

import java.util.concurrent.ThreadLocalRandom;
import java.util.function.DoubleSupplier;
import java.util.function.LongSupplier;

/**
 * Everything about a lookup that a test needs to control.
 *
 * <p>The transport, the cache, the clock and the jitter are injected rather than reached for, which is what
 * lets the whole command be driven in-process against a recorded payload and a temporary directory instead of
 * Central and {@code $HOME}. The defaults are what production uses.
 *
 * @since 0.1.0
 */
public final class HttpOptions {

    /** Per-attempt ceiling. Central is slow for large packages; this is not a p99. */
    private static final long DEFAULT_TIMEOUT_MS = 120_000;

    private static final int DEFAULT_MAX_ATTEMPTS = 3;

    /** Wall clock across every attempt including backoff. */
    private static final long DEFAULT_BUDGET_MS = 300_000;

    private static final long DEFAULT_BASE_DELAY_MS = 200;

    private final HttpTransport transport;
    private final long timeoutMs;
    private final int maxAttempts;
    private final long budgetMs;
    private final long baseDelayMs;
    private final DocsCache cache;
    private final boolean refresh;
    private final LongSupplier clock;
    private final DoubleSupplier jitter;
    private final Sleeper sleeper;

    /**
     * The real transport, created on first use.
     *
     * <p>Every test replaces it, and building an {@link java.net.http.HttpClient} eagerly would open a
     * selector and a thread pool in a suite that never makes a request.
     */
    private static final class RealTransport {

        private static final HttpTransport INSTANCE = new JdkHttpTransport();

        private RealTransport() {
        }
    }

    private HttpOptions(Builder builder) {
        this.transport = builder.transport == null ? RealTransport.INSTANCE : builder.transport;
        this.timeoutMs = builder.timeoutMs;
        this.maxAttempts = builder.maxAttempts;
        this.budgetMs = builder.budgetMs;
        this.baseDelayMs = builder.baseDelayMs;
        this.cache = builder.cache;
        this.refresh = builder.refresh;
        this.clock = builder.clock;
        this.jitter = builder.jitter;
        this.sleeper = builder.sleeper;
    }

    /** How the retry loop waits. Injectable so a test never actually sleeps. */
    public interface Sleeper {

        void sleep(long millis);
    }

    public static Builder builder() {
        return new Builder();
    }

    public HttpTransport transport() {
        return transport;
    }

    public long timeoutMs() {
        return timeoutMs;
    }

    public int maxAttempts() {
        return maxAttempts;
    }

    public long budgetMs() {
        return budgetMs;
    }

    public long baseDelayMs() {
        return baseDelayMs;
    }

    /**
     * Where already-fetched payloads live. Defaults to a store that does nothing, which is what keeps the
     * render pipeline free of environment reads and every test hermetic.
     */
    public DocsCache cache() {
        return cache;
    }

    /** Ignore any cached copy and rewrite it. */
    public boolean refresh() {
        return refresh;
    }

    /**
     * The same options with {@code --refresh} applied.
     *
     * <p>The CLI cannot build these itself: the transport and the cache are handed to it by the process wrapper,
     * and the flag is not known until the arguments are parsed. Without this the flag parses and is then silently
     * dropped, which is the exact failure the grammar refuses everywhere else — and worse here, because
     * {@code --refresh} is the recovery a {@code symbol-not-found} failure recommends.
     */
    public HttpOptions withRefresh(boolean value) {
        if (value == refresh) {
            return this;
        }
        return builder()
                .transport(transport)
                .timeoutMs(timeoutMs)
                .maxAttempts(maxAttempts)
                .budgetMs(budgetMs)
                .baseDelayMs(baseDelayMs)
                .cache(cache)
                .refresh(value)
                .clock(clock)
                .jitter(jitter)
                .sleeper(sleeper)
                .build();
    }

    public long now() {
        return clock.getAsLong();
    }

    public double jitter() {
        return jitter.getAsDouble();
    }

    public void sleep(long millis) {
        sleeper.sleep(millis);
    }

    /** Everything unset, plus a transport. */
    public static final class Builder {

        private HttpTransport transport;
        private long timeoutMs = DEFAULT_TIMEOUT_MS;
        private int maxAttempts = DEFAULT_MAX_ATTEMPTS;
        private long budgetMs = DEFAULT_BUDGET_MS;
        private long baseDelayMs = DEFAULT_BASE_DELAY_MS;
        private DocsCache cache = DocsCache.NULL;
        private boolean refresh;
        private LongSupplier clock = System::currentTimeMillis;
        private DoubleSupplier jitter = () -> ThreadLocalRandom.current().nextDouble();
        private Sleeper sleeper = Builder::sleepQuietly;

        private Builder() {
        }

        private static void sleepQuietly(long millis) {
            try {
                Thread.sleep(millis);
            } catch (InterruptedException interrupted) {
                Thread.currentThread().interrupt();
            }
        }

        public Builder transport(HttpTransport value) {
            this.transport = value;
            return this;
        }

        public Builder timeoutMs(long value) {
            this.timeoutMs = value;
            return this;
        }

        public Builder maxAttempts(int value) {
            this.maxAttempts = value;
            return this;
        }

        public Builder budgetMs(long value) {
            this.budgetMs = value;
            return this;
        }

        public Builder baseDelayMs(long value) {
            this.baseDelayMs = value;
            return this;
        }

        public Builder cache(DocsCache value) {
            this.cache = value;
            return this;
        }

        public Builder refresh(boolean value) {
            this.refresh = value;
            return this;
        }

        public Builder clock(LongSupplier value) {
            this.clock = value;
            return this;
        }

        public Builder jitter(DoubleSupplier value) {
            this.jitter = value;
            return this;
        }

        public Builder sleeper(Sleeper value) {
            this.sleeper = value;
            return this;
        }

        public HttpOptions build() {
            return new HttpOptions(this);
        }
    }
}
