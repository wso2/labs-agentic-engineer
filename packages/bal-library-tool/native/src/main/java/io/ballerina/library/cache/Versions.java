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

/**
 * Compare two published versions the way a human would.
 *
 * <p>Lexicographic order is wrong in both directions that matter: {@code 1.9.0} sorts above {@code 1.10.0},
 * and {@code 2.0.0-alpha} above {@code 2.0.0}. This only has to pick the newest thing already on disk, so it
 * is dotted-numeric with a prerelease suffix ranking below its own release, and not a semver implementation.
 *
 * @since 0.1.0
 */
public final class Versions {

    private Versions() {
    }

    public static int compare(String left, String right) {
        Parts a = split(left);
        Parts b = split(right);
        int width = Math.max(a.numbers().length, b.numbers().length);
        for (int index = 0; index < width; index++) {
            int difference = at(a.numbers(), index) - at(b.numbers(), index);
            if (difference != 0) {
                return difference;
            }
        }
        if (a.prerelease().equals(b.prerelease())) {
            return 0;
        }
        // A release outranks any prerelease of the same core version.
        if (a.prerelease().isEmpty()) {
            return 1;
        }
        if (b.prerelease().isEmpty()) {
            return -1;
        }
        return a.prerelease().compareTo(b.prerelease()) < 0 ? -1 : 1;
    }

    private record Parts(int[] numbers, String prerelease) { }

    private static Parts split(String value) {
        int dash = value.indexOf('-');
        String core = dash == -1 ? value : value.substring(0, dash);
        String[] pieces = core.split("\\.", -1);
        int[] numbers = new int[pieces.length];
        for (int index = 0; index < pieces.length; index++) {
            numbers[index] = parseOrZero(pieces[index]);
        }
        return new Parts(numbers, dash == -1 ? "" : value.substring(dash + 1));
    }

    /** A non-numeric piece counts as 0, which is what {@code parseInt(...) || 0} does in the source. */
    private static int parseOrZero(String piece) {
        try {
            return Integer.parseInt(piece);
        } catch (NumberFormatException ignored) {
            return 0;
        }
    }

    private static int at(int[] numbers, int index) {
        return index < numbers.length ? numbers[index] : 0;
    }
}
