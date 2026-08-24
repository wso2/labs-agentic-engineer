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

import org.testng.Assert;
import org.testng.annotations.Test;

/**
 * The parsed coordinates, and the one class of input that used to pass.
 *
 * <p>{@code .} and {@code ..} satisfy both patterns and are legal path traversal. Nothing derived a filesystem
 * path from these values until the docs cache did, so this is the guard that keeps the parsed type itself from
 * ever holding one.
 *
 * @since 0.1.0
 */
public class QualifiedNameTest {

    @Test
    public void aWellFormedPackageNameRoundTrips() {
        Result<QualifiedName> parsed = QualifiedName.parse("ballerinax/googleapis.gmail");
        Assert.assertTrue(parsed.isOk());
        Assert.assertEquals(parsed.value().qualified(), "ballerinax/googleapis.gmail");
    }

    @Test
    public void aTraversalSegmentIsRejectedInEitherPosition() {
        for (String input : new String[] {
                "../..", "./.", "ballerinax/..", "../github", "./github", "ballerinax/."}) {
            Result<QualifiedName> parsed = QualifiedName.parse(input);
            Assert.assertFalse(parsed.isOk(), input + " must not parse");
            Assert.assertTrue(parsed.failure() instanceof Failure.Validation, input);
        }
    }

    @Test
    public void aTraversalVersionIsRejectedWhileARealVersionWithDotsIsNot() {
        Assert.assertFalse(Version.parse("..").isOk());
        Assert.assertFalse(Version.parse(".").isOk());
        Assert.assertTrue(Version.parse("2.16.6").isOk());
        Assert.assertTrue(Version.parse("1.0.0-alpha.1+build.7").isOk());
    }

    @Test
    public void aDottedNameThatIsNotATraversalStillParses() {
        // Packages really are named this way.
        Assert.assertTrue(QualifiedName.parse("ballerinax/client.config").isOk());
        Assert.assertTrue(QualifiedName.parse("ballerina/lang.value").isOk());
    }

    @Test
    public void aVersionSuffixInThePackageNameIsRejectedWithTheAdviceToDropIt() {
        Result<QualifiedName> parsed = QualifiedName.parse("ballerinax/github:6.0.0");
        Assert.assertFalse(parsed.isOk());
        Failure.Validation failure = (Failure.Validation) parsed.failure();
        Assert.assertTrue(failure.suggestion().contains("Drop any ':version' suffix"));
    }

    @Test
    public void theVersionedLabelIsOneSpelling() {
        // One method because it was hand-built at four sites, and a label that differs between a document header
        // and the failure about that document is a label an agent cannot correlate.
        QualifiedName qualified = QualifiedName.parse("ballerina/http").value();
        Assert.assertEquals(qualified.versioned(Version.parse("2.16.6").value()), "ballerina/http:2.16.6");
    }
}
