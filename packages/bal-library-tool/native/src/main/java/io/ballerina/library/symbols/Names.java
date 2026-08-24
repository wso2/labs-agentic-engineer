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

package io.ballerina.library.symbols;

import io.ballerina.library.Texts;

import java.util.Comparator;
import java.util.List;
import java.util.Locale;

/**
 * Turning the name an agent typed into the name a package declares.
 *
 * <p>Exact match first, then normalised equality — and MORE THAN ONE normalised match is a failure with
 * every match listed, never a silent pick. That is not theoretical: {@code ballerinax/github}'s
 * {@code ManifestConversions} declares both {@code clientId} and {@code client_id}, and
 * {@code ballerina/http} has 61 constant-versus-class collisions of the {@code STATUS_ACCEPTED} /
 * {@code StatusAccepted} shape. Picking one of those and printing it would answer a different question
 * than the one asked, with nothing in the output to say so.
 *
 * @since 0.1.0
 */
public final class Names {

    /** How many near-misses a failure carries. */
    public static final int MAX_CANDIDATES = 8;

    /** Shorter overlaps than this are noise — every identifier shares {@code e} with every other. */
    private static final int MIN_OVERLAP = 4;

    private Names() {
    }

    /** How a requested name resolved against a roster. */
    public sealed interface Match {

        record Found(String name) implements Match { }

        /** Several declarations normalise to the same thing. The caller has to choose. */
        record Ambiguous(List<String> candidates) implements Match { }

        record Missing(List<String> candidates) implements Match { }
    }

    /** Letters and digits, lower-cased: what {@code STATUS_ACCEPTED} and {@code StatusAccepted} share. */
    public static String normalise(String name) {
        return name.replaceAll("[^A-Za-z0-9]", "").toLowerCase(Locale.ROOT);
    }

    /**
     * Resolve one requested name against a roster.
     *
     * <p>{@code names} is the declaration roster in the package's own order; the returned name is always
     * one of them verbatim, so the caller never has to reconcile the spelling it asked for with the
     * spelling it got.
     */
    public static Match match(String requested, List<String> names) {
        String trimmed = requested.trim();
        if (names.contains(trimmed)) {
            return new Match.Found(trimmed);
        }

        String wanted = normalise(trimmed);
        List<String> normalised = wanted.isEmpty()
                ? List.of()
                : names.stream().filter(name -> normalise(name).equals(wanted)).toList();
        if (normalised.size() == 1) {
            return new Match.Found(normalised.get(0));
        }
        if (normalised.size() > 1) {
            return new Match.Ambiguous(normalised);
        }

        return new Match.Missing(nearMisses(trimmed, names));
    }

    /**
     * Names worth suggesting for a request that matched nothing.
     *
     * <p>Ranked by the longest run of characters shared with the request, which puts
     * {@code FullRepository} first for {@code FullRepo} and still surfaces {@code Repository} and
     * {@code MinimalRepository} behind it. Capped, because the alternative is the whole roster — 33,431
     * bytes for github's 1,224 names — inside a JSON object on stderr.
     */
    public static List<String> nearMisses(String requested, List<String> names) {
        record Scored(String name, int overlap) { }
        return names.stream()
                .map(name -> new Scored(name, longestCommonSubstring(requested, name)))
                .filter(scored -> scored.overlap() >= MIN_OVERLAP)
                .sorted(Comparator.comparingInt(Scored::overlap).reversed()
                        .thenComparing(Scored::name, Texts.LOCALE_ORDER))
                .limit(MAX_CANDIDATES)
                .map(Scored::name)
                .toList();
    }

    /** The longest run of characters two strings share, case-insensitively. */
    private static int longestCommonSubstring(String left, String right) {
        String a = left.toLowerCase(Locale.ROOT);
        String b = right.toLowerCase(Locale.ROOT);
        int best = 0;
        // O(n·m) over identifier-length strings, run once per candidate on a miss only.
        int[] previous = new int[b.length() + 1];
        for (int i = 1; i <= a.length(); i++) {
            int[] current = new int[b.length() + 1];
            for (int j = 1; j <= b.length(); j++) {
                if (a.charAt(i - 1) == b.charAt(j - 1)) {
                    current[j] = previous[j - 1] + 1;
                    best = Math.max(best, current[j]);
                }
            }
            previous = current;
        }
        return best;
    }

    /** Every distinct candidate across several matches, in the order they were offered. */
    public static List<String> candidatesOf(Match match) {
        return switch (match) {
            case Match.Found ignored -> List.of();
            case Match.Ambiguous ambiguous -> ambiguous.candidates();
            case Match.Missing missing -> missing.candidates();
        };
    }
}
