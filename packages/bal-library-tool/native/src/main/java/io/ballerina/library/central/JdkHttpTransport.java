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

import java.io.IOException;
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.net.http.HttpTimeoutException;
import java.time.Duration;

/**
 * The real transport, on the JDK's own client.
 *
 * <p>Nothing to bundle: {@code java.net.http} has been in the platform since 11, and the tool jar carries no
 * third-party classes of its own. The client is built once per process because a fresh one per attempt would
 * throw away the connection pool between the two requests a lookup makes.
 *
 * @since 0.1.0
 */
public final class JdkHttpTransport implements HttpTransport {

    private final HttpClient client;

    public JdkHttpTransport() {
        this.client = HttpClient.newBuilder()
                .followRedirects(HttpClient.Redirect.NORMAL)
                .connectTimeout(Duration.ofSeconds(30))
                .build();
    }

    @Override
    public Reply get(String url, long timeoutMs) {
        HttpRequest request;
        try {
            request = HttpRequest.newBuilder(URI.create(url))
                    .GET()
                    .timeout(Duration.ofMillis(timeoutMs))
                    .header("Accept", "application/json")
                    .build();
        } catch (IllegalArgumentException malformed) {
            return new Reply.Failed("bad url: " + malformed.getMessage());
        }

        try {
            HttpResponse<String> response = client.send(request, HttpResponse.BodyHandlers.ofString());
            String retryAfter = response.headers().firstValue("retry-after").orElse(null);
            return new Reply.Answered(response.statusCode(), response.body(), retryAfter);
        } catch (HttpTimeoutException timedOut) {
            return new Reply.TimedOut();
        } catch (IOException failed) {
            return new Reply.Failed("network error: " + describe(failed));
        } catch (InterruptedException interrupted) {
            Thread.currentThread().interrupt();
            return new Reply.Failed("network error: interrupted");
        }
    }

    private static String describe(Throwable cause) {
        String message = cause.getMessage();
        return message == null || message.isEmpty() ? cause.getClass().getSimpleName() : message;
    }
}
