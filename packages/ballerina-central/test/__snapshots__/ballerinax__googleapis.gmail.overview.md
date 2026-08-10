<!-- bal-library overview v1 -->
# ballerinax/googleapis.gmail 0.0.0-fixture

| | |
|---|---|
| Source | central |
| Clients | `Client` |
| Module functions | none |
| Errors | 3, listed below |
| Types | 35 declarations (31 records, 2 unions, 2 other), not listed here — read one with `type` |

## Next

- `bal-library ops ballerinax/googleapis.gmail <path>` — navigate a client's operations
- `bal-library type ballerinax/googleapis.gmail <Name> [--deps]` — read a declaration whole
- `bal-library api ballerinax/googleapis.gmail` — every declaration, when nothing above answered

## Client `Client`

The Gmail API lets you view and manage Gmail mailbox data like threads, messages, and labels.

### Constructor

```ballerina
function init(ConnectionConfig config, string serviceUrl = "https://gmail.googleapis.com/gmail/v1") returns error?;
```

### Resource functions — 32, call with `->` and a path

```ballerina
# Lists the drafts in the user's mailbox.
resource function get users/[string userId]/drafts(Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = (), boolean? includeSpamTrash = (), int? maxResults = (), string? pageToken = (), string? q = ()) returns ListDraftsResponse|error;

# Creates a new draft with the `DRAFT` label.
resource function post users/[string userId]/drafts(DraftRequest payload, Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns Draft|error;

# Sends the specified, existing draft to the recipients in the `To`, `Cc`, and `Bcc` headers.
resource function post users/[string userId]/drafts/send(DraftRequest payload, Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns Message|error;

# Gets the specified draft.
resource function get users/[string userId]/drafts/[string id](Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = (), "minimal"|"full"|"raw"|"metadata"? format = ()) returns Draft|error;

# Replaces a draft's content.
resource function put users/[string userId]/drafts/[string id](DraftRequest payload, Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns Draft|error;

# Immediately and permanently deletes the specified draft. Does not simply trash it.
resource function delete users/[string userId]/drafts/[string id](Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns error?;

# Lists the history of all changes to the given mailbox. History results are returned in chronological order (increasing `historyId`).
resource function get users/[string userId]/history(Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = (), ("messageAdded"|"messageDeleted"|"labelAdded"|"labelRemoved")[]? historyTypes = (), string? labelId = (), int? maxResults = (), string? pageToken = (), string? startHistoryId = ()) returns ListHistoryResponse|error;

# Lists all labels in the user's mailbox.
resource function get users/[string userId]/labels(Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns ListLabelsResponse|error;

# Creates a new label.
resource function post users/[string userId]/labels(Label payload, Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns Label|error;

# Gets the specified label.
resource function get users/[string userId]/labels/[string id](Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns Label|error;

# Updates the specified label.
resource function put users/[string userId]/labels/[string id](Label payload, Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns Label|error;

# Immediately and permanently deletes the specified label and removes it from any messages and threads that it is applied to.
resource function delete users/[string userId]/labels/[string id](Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns error?;

# Patch the specified label.
resource function patch users/[string userId]/labels/[string id](Label payload, Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns Label|error;

# Lists the messages in the user's mailbox.
resource function get users/[string userId]/messages(Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = (), boolean? includeSpamTrash = (), string[]? labelIds = (), int? maxResults = (), string? pageToken = (), string? q = ()) returns ListMessagesResponse|error;

# Directly inserts a message into only this user's mailbox similar to `IMAP APPEND`, bypassing most scanning and classification. Does not send a message.
resource function post users/[string userId]/messages(MessageRequest payload, Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = (), boolean? deleted = (),             "receivedTime"|"dateHeader"? internalDateSource = ()) returns Message|error;

# Deletes many messages by message ID. Provides no guarantees that messages were not already deleted or even existed at all.
resource function post users/[string userId]/messages/batchDelete(BatchDeleteMessagesRequest payload, Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns error?;

# Modifies the labels on the specified messages.
resource function post users/[string userId]/messages/batchModify(BatchModifyMessagesRequest payload, Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns error?;

# Imports a message into only this user's mailbox, with standard email delivery scanning and classification similar to receiving via SMTP. This method doesn't perform SPF checks, so it might not work for some spam messages, such as those attempting to perform domain spoofing. This method does not send a message.
resource function post users/[string userId]/messages/'import(MessageRequest payload, Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = (), boolean? deleted = (),             "receivedTime"|"dateHeader"? internalDateSource = (), boolean? neverMarkSpam = (), boolean? processForCalendar = ()) returns Message|error;

# Sends the specified message to the recipients in the `To`, `Cc`, and `Bcc` headers. For example usage, see [Sending email](https://developers.google.com/gmail/api/guides/sending).
resource function post users/[string userId]/messages/send(MessageRequest payload, Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns Message|error;

# Gets the specified message.
resource function get users/[string userId]/messages/[string id](Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = (), "minimal"|"full"|"raw"|"metadata"? format = (), string[]? metadataHeaders = ()) returns Message|error;

# Immediately and permanently deletes the specified message. This operation cannot be undone. Prefer `messages.trash` instead.
resource function delete users/[string userId]/messages/[string id](Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns error?;

# Modifies the labels on the specified message.
resource function post users/[string userId]/messages/[string id]/modify(ModifyMessageRequest payload, Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns Message|error;

# Moves the specified message to the trash.
resource function post users/[string userId]/messages/[string id]/trash(Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns Message|error;

# Removes the specified message from the trash.
resource function post users/[string userId]/messages/[string id]/untrash(Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns Message|error;

# Gets the specified message attachment.
resource function get users/[string userId]/messages/[string messageId]/attachments/[string id](Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns Attachment|error;

# Gets the current user's Gmail profile.
resource function get users/[string userId]/profile(Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns Profile|error;

# Lists the threads in the user's mailbox.
resource function get users/[string userId]/threads(Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = (), boolean? includeSpamTrash = (), string[]? labelIds = (), int? maxResults = (), string? pageToken = (), string? q = ()) returns ListThreadsResponse|error;

# Gets the specified thread.
resource function get users/[string userId]/threads/[string id](Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = (), "full"|"metadata"|"minimal"? format = (), string[]? metadataHeaders = ()) returns MailThread|error;

# Immediately and permanently deletes the specified thread. Any messages that belong to the thread are also deleted. This operation cannot be undone. Prefer `threads.trash` instead.
resource function delete users/[string userId]/threads/[string id](Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns error?;

# Modifies the labels applied to the thread. This applies to all messages in the thread.
resource function post users/[string userId]/threads/[string id]/modify(ModifyThreadRequest payload, Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns MailThread|error;

# Moves the specified thread to the trash. Any messages that belong to the thread are also moved to the trash.
resource function post users/[string userId]/threads/[string id]/trash(Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns MailThread|error;

# Removes the specified thread from the trash. Any messages that belong to the thread are also removed from the trash.
resource function post users/[string userId]/threads/[string id]/untrash(Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns MailThread|error;
```

## Errors — 3

The subtype chain is what `is` tests against, so `e is <Name>` works off these lines directly.

```ballerina
# Defines the generic error type for the `gmail` module.
type Error distinct error;

# Error that occurs when there is an issue with inline images or attachments. This could be due to issues like file not found, unsupported file type, etc.
type FileGenericError distinct Error;

# Error that occurs when an invalid encoded value is provided for the `data` fields.
type ValueEncodeError distinct Error;
```

## Guide

*The package's own readme, verbatim, with its headings demoted two levels.*

#### Package overview

[Gmail](https://blog.google/products/gmail/) is a widely-used email service provided by Google LLC, enabling users to send and receive emails over the internet.

The `ballerinax/googleapis.gmail` package offers APIs to connect and interact with [Gmail API](https://developers.google.com/gmail/api/guides) endpoints, specifically based on [Gmail API v1](https://gmail.googleapis.com/$discovery/rest?version=v1).

#### Setup guide

To use the Gmail connector, you must have access to the Gmail REST API through a [Google Cloud Platform (GCP)](https://console.cloud.google.com/) account and a project under it. If you do not have a GCP account, you can sign up for one [here](https://cloud.google.com/).

##### Step 1: Create a Google Cloud Platform Project

1. Open the [Google Cloud Platform Console](https://console.cloud.google.com/).

2. Click on the project drop-down menu and select an existing project or create a new one for which you want to add an API key.

    ![GCP Console Project View](https://raw.githubusercontent.com/ballerina-platform/module-ballerinax-googleapis.gmail/master/docs/setup/resources/gcp-console-project-view.png)

##### Step 2: Enable Gmail API

1. Navigate to the **Library** tab and enable the Gmail API.

    ![Enable Gmail API](https://raw.githubusercontent.com/ballerina-platform/module-ballerinax-googleapis.gmail/master/docs/setup/resources/enable-gmail-api.png)

##### Step 3: Configure OAuth consent

1. Click on the **OAuth consent screen** tab in the Google Cloud Platform console.

    ![Consent Screen](https://raw.githubusercontent.com/ballerina-platform/module-ballerinax-googleapis.gmail/master/docs/setup/resources/consent-screen.png)

2. Provide a name for the consent application and save your changes.

##### Step 4: Create OAuth client

1. Navigate to the **Credentials** tab in your Google Cloud Platform console.

2. Click on **Create credentials** and select **OAuth client ID** from the dropdown menu.

    ![Create Credentials](https://raw.githubusercontent.com/ballerina-platform/module-ballerinax-googleapis.gmail/master/docs/setup/resources/create-credentials.png)

3. You will be directed to the **Create OAuth client ID** screen, where you need to fill in the necessary information as follows:

    | Field                    | Value                |
    | ------------------------ | -------------------- |
    | Application type         | Web Application      |
    | Name                     | GmailConnector       |
    | Authorized Redirect URIs | https://developers.google.com/oauthplayground |

4. After filling in these details, click on **Create**.

5. Make sure to save the provided Client ID and Client secret.

##### Step 5: Get the Access and Refresh token

**Note**: It is recommended to use the OAuth 2.0 playground to obtain the tokens.

1. Configure the OAuth playground with the OAuth client ID and client secret.

    ![OAuth Playground](https://raw.githubusercontent.com/ballerina-platform/module-ballerinax-googleapis.gmail/master/docs/setup/resources/oauth-playground.png)

2. Authorize the Gmail APIs (Select all except the metadata scope).

    ![Authorize APIs](https://raw.githubusercontent.com/ballerina-platform/module-ballerinax-googleapis.gmail/master/docs/setup/resources/authorize-apis.png)

3. Exchange the authorization code for tokens.

    ![Exchange Tokens](https://raw.githubusercontent.com/ballerina-platform/module-ballerinax-googleapis.gmail/master/docs/setup/resources/exchange-tokens.png)

#### Quickstart

To use the `gmail` connector in your Ballerina application, modify the `.bal` file as follows:

##### Step 1: Import the module

Import the `gmail` module.

```ballerina
import ballerinax/googleapis.gmail;
```

##### Step 2: Instantiate a new connector

Create a `gmail:ConnectionConfig` with the obtained OAuth2.0 tokens and initialize the connector with it.

```ballerina
configurable string refreshToken = ?;
configurable string clientId = ?;
configurable string clientSecret = ?;

gmail:Client gmail = check new gmail:Client(
    config = {
        auth: {
            refreshToken,
            clientId,
            clientSecret
        }
    }
);
```

##### Step 3: Invoke the connector operation

Now, utilize the available connector operations.

###### Get unread emails in INBOX

```ballerina
gmail:MessageListPage messageList = check gmail->/users/me/messages(q = "label:INBOX is:unread");
```

###### Send email

```ballerina
gmail:MessageRequest message = {
    to: ["<recipient>"],
    subject: "Scheduled Maintenance Break Notification",
    bodyInHtml: string `<html>
                            <head>
                                <title>Scheduled Maintenance</title>
                            </head>
                        </html>`;
};

gmail:Message sendResult = check gmail->/users/me/messages/send.post(message);
```

##### Step 4: Run the Ballerina application

```bash
bal run
```

#### Examples

The `gmail` connector provides practical examples illustrating usage in various scenarios. Explore these [examples](https://github.com/ballerina-platform/module-ballerinax-googleapis.gmail/tree/master/examples), covering use cases like sending emails, retrieving messages, and managing labels.

1. [Process customer feedback emails](https://github.com/ballerina-platform/module-ballerinax-googleapis.gmail/tree/master/examples/process-mails) - Manage customer feedback emails by processing unread emails in the inbox, extracting details, and marking them as read.

2. [Send maintenance break emails](https://github.com/ballerina-platform/module-ballerinax-googleapis.gmail/tree/master/examples/send-mails) - Send emails for scheduled maintenance breaks

3. [Automated Email Responses](https://github.com/ballerina-platform/module-ballerinax-googleapis.gmail/tree/master/examples/reply-mails) - Retrieve unread emails from the Inbox and subsequently send personalized responses.

4. [Email Thread Search](https://github.com/ballerina-platform/module-ballerinax-googleapis.gmail/tree/master/examples/search-threads)
    Search for email threads based on a specified query.

#### Report Issues

To report bugs, request new features, start new discussions, view project boards, etc., go to the [Ballerina library parent repository](https://github.com/ballerina-platform/ballerina-library).

#### Useful Links

- Chat live with us via our [Discord server](https://discord.gg/ballerinalang).
- Post all technical questions on Stack Overflow with the [#ballerina](https://stackoverflow.com/questions/tagged/ballerina) tag.
