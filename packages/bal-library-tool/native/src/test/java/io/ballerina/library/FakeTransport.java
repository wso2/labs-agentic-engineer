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

import io.ballerina.library.central.HttpTransport;

import java.util.ArrayList;
import java.util.List;
import java.util.function.Function;

/**
 * Central, replayed.
 *
 * <p>Every retry, status-code and cache test drives this instead of a socket, which is what makes the
 * boundary's behaviour — which failures are worth retrying, which are answers, and what each one costs the
 * caller — assertable in milliseconds and without a network.
 *
 * @since 0.1.0
 */
final class FakeTransport implements HttpTransport {

    private final Function<String, Reply> answer;
    private final List<String> urls = new ArrayList<>();

    private FakeTransport(Function<String, Reply> answer) {
        this.answer = answer;
    }

    /** Answers every call the same way. */
    static FakeTransport always(Reply reply) {
        return new FakeTransport(url -> reply);
    }

    /** Answers by URL, which is how the versions endpoint and the docs endpoint are told apart. */
    static FakeTransport routing(Function<String, Reply> answer) {
        return new FakeTransport(answer);
    }

    /** Answers each call from the queue in order, and fails loudly when it runs dry. */
    static FakeTransport scripted(List<Reply> replies) {
        List<Reply> queue = new ArrayList<>(replies);
        int[] index = {0};
        return new FakeTransport(url -> {
            if (index[0] >= queue.size()) {
                throw new AssertionError("scripted transport ran out of responses at call " + (index[0] + 1));
            }
            return queue.get(index[0]++);
        });
    }

    /** A transport that must never be reached, for tests that assert an argument error came first. */
    static FakeTransport never() {
        return new FakeTransport(url -> {
            throw new AssertionError("must not reach the network: " + url);
        });
    }

    static Reply ok(String body) {
        return new Reply.Answered(200, body);
    }

    static Reply status(int status) {
        return new Reply.Answered(status, "");
    }

    @Override
    public Reply get(String url, long timeoutMs) {
        urls.add(url);
        return answer.apply(url);
    }

    int calls() {
        return urls.size();
    }

    List<String> urls() {
        return List.copyOf(urls);
    }
}
