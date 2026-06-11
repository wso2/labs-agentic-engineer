/**
 * Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
 *
 * WSO2 LLC. licenses this file to you under the Apache License,
 * Version 2.0 (the "License"); you may not use this file except
 * in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

export const earsGuidance = `# EARS Requirement Format

All requirements MUST be written in EARS (Easy Approach to Requirements Syntax) format. EARS produces precise, unambiguous, testable requirements using six patterns.

## Pattern Templates

| Pattern      | When to Use                          | Template                                                       |
| ------------ | ------------------------------------ | -------------------------------------------------------------- |
| Ubiquitous   | Always applies, no conditions        | The <entity> SHALL <action>                                    |
| State-Driven | While in a specific state            | WHILE <condition>, the <entity> SHALL <action>                 |
| Event-Driven | Triggered by an event                | WHEN <trigger>, the <entity> SHALL <action>                    |
| Unwanted     | Error / exception handling           | IF <condition>, THEN the <entity> SHALL <action>               |
| Optional     | Feature-flag / configurable behavior | WHERE <feature>, the <entity> SHALL <action>                   |
| Complex      | Two conditions combined              | <Pattern1>, <Pattern2>, the <entity> SHALL <action>            |

## Pattern Selection

- No conditions → Ubiquitous
- Continuous state ("is active", "in X mode", "user is authenticated") → WHILE
- Point-in-time trigger ("user clicks", "file uploaded", "session expires") → WHEN
- Negative / error / exception condition → IF ... THEN
- Configurable or feature-flagged behavior → WHERE
- Two conditions combined → Complex (max 2, keep clear)

Key distinctions:
- WHILE = continuous state; WHEN = instantaneous event. "While connection is active" vs "When connection is established".
- IF-THEN is reserved for errors and unwanted behavior — NOT for normal user actions (use WHEN) or feature toggles (use WHERE).

## Writing Rules

- Use "SHALL" — never "should", "must", or "will".
- Use active voice: "The system SHALL encrypt data", not "Data shall be encrypted".
- One behavior per requirement — split compound statements into separate bullets.
- Must be testable with measurable criteria (e.g., "within 200ms", "up to 1000 concurrent users"), not vague terms ("quickly", "scales well").
- Focus on what, not how — no implementation details, tech choices, or algorithms.
- Capitalize pattern keywords (WHILE, WHEN, IF, THEN, WHERE, SHALL).

## Examples

- Ubiquitous: "The system SHALL encrypt all data at rest using AES-256."
- State-Driven: "WHILE in maintenance mode, the system SHALL display a maintenance banner to all users."
- Event-Driven: "WHEN a user submits the registration form, the system SHALL validate all required fields."
- Unwanted: "IF authentication fails three times, THEN the system SHALL lock the account for 15 minutes."
- Optional: "WHERE two-factor authentication is enabled, the system SHALL require OTP after password verification."
- Complex: "WHILE in production mode, IF an unhandled exception occurs, THEN the system SHALL notify the operations team."
`;
