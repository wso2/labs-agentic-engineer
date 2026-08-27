<!-- bal library client v1 -->
# Clients — ballerinax/googleapis.gmail `Client`

| | |
|---|---|
| Container | `Client` — 32 resource |
| Showing | 33 signatures |

## Next

- one call and every type it needs: `bal library client ballerinax/googleapis.gmail Client init -r`

## Constructor — 1

```ballerina
# Gets invoked to initialize the `connector`.
isolated function init(ConnectionConfig config, string serviceUrl = "https://gmail.googleapis.com/gmail/v1") returns error?;
```

## Resource functions — 32, call with `-> and a path`

```ballerina
# Lists the drafts in the user's mailbox.
isolated resource function get users/[string userId]/drafts(Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = (), boolean? includeSpamTrash = (), int? maxResults = (), string? pageToken = (), string? q = ()) returns ListDraftsResponse|error;

# Creates a new draft with the `DRAFT` label.
isolated resource function post users/[string userId]/drafts(DraftRequest payload, Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns Draft|error;

# Sends the specified, existing draft to the recipients in the `To`, `Cc`, and `Bcc` headers.
isolated resource function post users/[string userId]/drafts/send(DraftRequest payload, Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns Message|error;

# Gets the specified draft.
isolated resource function get users/[string userId]/drafts/[string id](Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = (), "minimal"|"full"|"raw"|"metadata"? format = ()) returns Draft|error;

# Replaces a draft's content.
isolated resource function put users/[string userId]/drafts/[string id](DraftRequest payload, Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns Draft|error;

# Immediately and permanently deletes the specified draft. Does not simply trash it.
isolated resource function delete users/[string userId]/drafts/[string id](Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns error?;

# Lists the history of all changes to the given mailbox. History results are returned in chronological order (increasing `historyId`).
isolated resource function get users/[string userId]/history(Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = (), ("messageAdded"|"messageDeleted"|"labelAdded"|"labelRemoved")[]? historyTypes = (), string? labelId = (), int? maxResults = (), string? pageToken = (), string? startHistoryId = ()) returns ListHistoryResponse|error;

# Lists all labels in the user's mailbox.
isolated resource function get users/[string userId]/labels(Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns ListLabelsResponse|error;

# Creates a new label.
isolated resource function post users/[string userId]/labels(Label payload, Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns Label|error;

# Gets the specified label.
isolated resource function get users/[string userId]/labels/[string id](Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns Label|error;

# Updates the specified label.
isolated resource function put users/[string userId]/labels/[string id](Label payload, Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns Label|error;

# Immediately and permanently deletes the specified label and removes it from any messages and threads that it is applied to.
isolated resource function delete users/[string userId]/labels/[string id](Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns error?;

# Patch the specified label.
isolated resource function patch users/[string userId]/labels/[string id](Label payload, Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns Label|error;

# Lists the messages in the user's mailbox.
isolated resource function get users/[string userId]/messages(Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = (), boolean? includeSpamTrash = (), string[]? labelIds = (), int? maxResults = (), string? pageToken = (), string? q = ()) returns ListMessagesResponse|error;

# Directly inserts a message into only this user's mailbox similar to `IMAP APPEND`, bypassing most scanning and classification. Does not send a message.
isolated resource function post users/[string userId]/messages(MessageRequest payload, Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = (), boolean? deleted = (), "receivedTime"|"dateHeader"? internalDateSource = ()) returns Message|error;

# Deletes many messages by message ID. Provides no guarantees that messages were not already deleted or even existed at all.
isolated resource function post users/[string userId]/messages/batchDelete(BatchDeleteMessagesRequest payload, Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns error?;

# Modifies the labels on the specified messages.
isolated resource function post users/[string userId]/messages/batchModify(BatchModifyMessagesRequest payload, Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns error?;

# Imports a message into only this user's mailbox, with standard email delivery scanning and classification similar to receiving via SMTP. This method doesn't perform SPF checks, so it might not work for some spam messages, such as those attempting to perform domain spoofing. This method does not send a message.
isolated resource function post users/[string userId]/messages/'import(MessageRequest payload, Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = (), boolean? deleted = (), "receivedTime"|"dateHeader"? internalDateSource = (), boolean? neverMarkSpam = (), boolean? processForCalendar = ()) returns Message|error;

# Sends the specified message to the recipients in the `To`, `Cc`, and `Bcc` headers. For example usage, see [Sending email](https://developers.google.com/gmail/api/guides/sending).
isolated resource function post users/[string userId]/messages/send(MessageRequest payload, Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns Message|error;

# Gets the specified message.
isolated resource function get users/[string userId]/messages/[string id](Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = (), "minimal"|"full"|"raw"|"metadata"? format = (), string[]? metadataHeaders = ()) returns Message|error;

# Immediately and permanently deletes the specified message. This operation cannot be undone. Prefer `messages.trash` instead.
isolated resource function delete users/[string userId]/messages/[string id](Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns error?;

# Modifies the labels on the specified message.
isolated resource function post users/[string userId]/messages/[string id]/modify(ModifyMessageRequest payload, Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns Message|error;

# Moves the specified message to the trash.
isolated resource function post users/[string userId]/messages/[string id]/trash(Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns Message|error;

# Removes the specified message from the trash.
isolated resource function post users/[string userId]/messages/[string id]/untrash(Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns Message|error;

# Gets the specified message attachment.
isolated resource function get users/[string userId]/messages/[string messageId]/attachments/[string id](Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns Attachment|error;

# Gets the current user's Gmail profile.
isolated resource function get users/[string userId]/profile(Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns Profile|error;

# Lists the threads in the user's mailbox.
isolated resource function get users/[string userId]/threads(Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = (), boolean? includeSpamTrash = (), string[]? labelIds = (), int? maxResults = (), string? pageToken = (), string? q = ()) returns ListThreadsResponse|error;

# Gets the specified thread.
isolated resource function get users/[string userId]/threads/[string id](Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = (), "full"|"metadata"|"minimal"? format = (), string[]? metadataHeaders = ()) returns MailThread|error;

# Immediately and permanently deletes the specified thread. Any messages that belong to the thread are also deleted. This operation cannot be undone. Prefer `threads.trash` instead.
isolated resource function delete users/[string userId]/threads/[string id](Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns error?;

# Modifies the labels applied to the thread. This applies to all messages in the thread.
isolated resource function post users/[string userId]/threads/[string id]/modify(ModifyThreadRequest payload, Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns MailThread|error;

# Moves the specified thread to the trash. Any messages that belong to the thread are also moved to the trash.
isolated resource function post users/[string userId]/threads/[string id]/trash(Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns MailThread|error;

# Removes the specified thread from the trash. Any messages that belong to the thread are also removed from the trash.
isolated resource function post users/[string userId]/threads/[string id]/untrash(Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns MailThread|error;
```
