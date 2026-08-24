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

/**
 * One HTTP GET, as a value.
 *
 * <p>This is the seam every retry test drives. Transport trouble is returned rather than thrown, because the
 * retry policy above it has to distinguish three outcomes that an exception hierarchy blurs: an answer with a
 * status worth retrying, a request that ran out of time, and a socket that never connected. A test that wants
 * "503 then 200" scripts two {@link Reply.Answered} values and never opens a socket.
 *
 * @since 0.1.0
 */
public interface HttpTransport {

    /** One attempt. Never throws; every outcome is a {@link Reply}. */
    Reply get(String url, long timeoutMs);

    /** What one attempt produced. */
    sealed interface Reply {

        /** Central answered. {@code retryAfter} is the raw header, or {@code null}. */
        record Answered(int status, String body, String retryAfter) implements Reply {

            public Answered(int status, String body) {
                this(status, body, null);
            }

            public boolean isOk() {
                return status >= 200 && status < 300;
            }
        }

        /** The attempt ran out of time. */
        record TimedOut() implements Reply { }

        /** The request never produced a response — DNS, TLS, a refused connection. */
        record Failed(String message) implements Reply { }
    }
}
