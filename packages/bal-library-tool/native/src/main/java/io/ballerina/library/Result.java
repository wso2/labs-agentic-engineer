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

/**
 * A value or a {@link Failure}, never an exception.
 *
 * <p>The reader has exactly one caller shape — a command whose stdout is a document and whose stderr
 * is one JSON object — so a network hiccup, a bad argument and a payload we cannot parse are all
 * values that travel back to the one place that knows what to do with them. Nothing in the render
 * pipeline throws.
 *
 * @param <T> the value a successful result carries
 * @since 0.1.0
 */
public sealed interface Result<T> {

    /** A value. */
    record Ok<T>(T value) implements Result<T> { }

    /** A failure. The type parameter is phantom, so an error can be re-typed by {@link #cast()}. */
    record Err<T>(Failure failure) implements Result<T> { }

    static <T> Result<T> ok(T value) {
        return new Ok<>(value);
    }

    static <T> Result<T> err(Failure failure) {
        return new Err<>(failure);
    }

    default boolean isOk() {
        return this instanceof Ok<T>;
    }

    /**
     * The value. Only legal on an {@code Ok}, which is the discipline every call site follows by
     * checking {@link #isOk()} first.
     */
    default T value() {
        return switch (this) {
            case Ok<T> ok -> ok.value();
            case Err<T> err -> throw new IllegalStateException("not ok: " + err.failure());
        };
    }

    /** The failure. Only legal on an {@code Err}. */
    default Failure failure() {
        return switch (this) {
            case Ok<T> ok -> throw new IllegalStateException("not a failure: " + ok.value());
            case Err<T> err -> err.failure();
        };
    }

    /** The same failure at another value type, so a guard clause can return it unchanged. */
    default <U> Result<U> cast() {
        return switch (this) {
            case Ok<T> ok -> throw new IllegalStateException("only a failure can be re-typed");
            case Err<T> err -> Result.err(err.failure());
        };
    }

}
