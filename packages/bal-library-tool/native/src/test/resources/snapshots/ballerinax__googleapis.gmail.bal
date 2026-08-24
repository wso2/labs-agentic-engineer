// ============================================================
// Library: ballerinax/googleapis.gmail
// [Gmail](https://blog.google/products/gmail/) is a widely-used email service provided by Google LLC, enabling users to send and receive emails over the internet.
// ============================================================
import ballerinax/googleapis.gmail;

// --- Types ---

# An Attachment.
public type Attachment record {
    # Id of the attachment.
    string attachmentId?;
    # The attachment data decoded as a UTF-8 string. Populated only when the content is valid UTF-8 text.
    string data?;
    # The attachment data as raw decoded bytes. Populated for binary content that cannot be represented as a UTF-8 string (e.g. PDF, images).
    byte[] rawData?;
    # Number of bytes for the message part data (encoding notwithstanding).
    int:Signed32 size?;
};

# A file record to indicate attachment
public type AttachmentFile record {|
    # The mime type of the file (ex. application/pdf, text/plain)
    string mimeType;
    # The file name with extension. This will be used name the attachment/image in the mail.
    string name;
    # The file path
    string path;
|};

public type BatchDeleteMessagesRequest record {|
    # The IDs of the messages to delete.
    string[] ids?;
|};

public type BatchModifyMessagesRequest record {|
    # A list of label IDs to add to messages.
    string[] addLabelIds?;
    # The IDs of the messages to modify. There is a limit of 1000 ids per request.
    string[] ids?;
    # A list of label IDs to remove from messages.
    string[] removeLabelIds?;
|};

# Provides settings related to HTTP/1.x protocol.
public type ClientHttp1Settings record {|
    # Specifies whether to reuse a connection for multiple requests
    http:KeepAlive keepAlive = http:KEEPALIVE_AUTO; // Special Agent Note: KeepAlive FROM ballerina/http module
    # The chunking behaviour of the request
    http:Chunking chunking = http:CHUNKING_AUTO; // Special Agent Note: Chunking FROM ballerina/http module
    # Proxy server related options
    ProxyConfig proxy?;
|};

# Provides a set of configurations for controlling the behaviours when communicating with a remote HTTP endpoint.
public type ConnectionConfig record {|
    # Configurations related to client authentication
    http:BearerTokenConfig|OAuth2RefreshTokenGrantConfig auth; // Special Agent Note: BearerTokenConfig FROM ballerina/http module
    # The HTTP version understood by the client
    http:HttpVersion httpVersion = http:HTTP_2_0; // Special Agent Note: HttpVersion FROM ballerina/http module
    # Configurations related to HTTP/1.x protocol
    ClientHttp1Settings http1Settings?;
    # Configurations related to HTTP/2 protocol
    http:ClientHttp2Settings http2Settings?; // Special Agent Note: ClientHttp2Settings FROM ballerina/http module
    # The maximum time to wait (in seconds) for a response before closing the connection
    decimal timeout = 60;
    # The choice of setting `forwarded`/`x-forwarded` header
    string forwarded = "disable";
    # Configurations associated with request pooling
    http:PoolConfiguration poolConfig?; // Special Agent Note: PoolConfiguration FROM ballerina/http module
    # HTTP caching related configurations
    http:CacheConfig cache?; // Special Agent Note: CacheConfig FROM ballerina/http module
    # Specifies the way of handling compression (`accept-encoding`) header
    http:Compression compression = http:COMPRESSION_AUTO; // Special Agent Note: Compression FROM ballerina/http module
    # Configurations associated with the behaviour of the Circuit Breaker
    http:CircuitBreakerConfig circuitBreaker?; // Special Agent Note: CircuitBreakerConfig FROM ballerina/http module
    # Configurations associated with retrying
    http:RetryConfig retryConfig?; // Special Agent Note: RetryConfig FROM ballerina/http module
    # Configurations associated with inbound response size limits
    http:ResponseLimitConfigs responseLimits?; // Special Agent Note: ResponseLimitConfigs FROM ballerina/http module
    # SSL/TLS-related options
    http:ClientSecureSocket secureSocket?; // Special Agent Note: ClientSecureSocket FROM ballerina/http module
    # Proxy server related options
    http:ProxyConfig proxy?; // Special Agent Note: ProxyConfig FROM ballerina/http module
    # Enables the inbound payload validation functionality which provided by the constraint package. Enabled by default
    boolean validation = true;
|};

# A draft email in the user's mailbox.
public type Draft record {
    # The immutable ID of the draft.
    string id?;
    # An email message.
    Message message?;
};

# Request payload to create a draft.
public type DraftRequest record {|
    # The immutable ID of the draft.
    string id?;
    # An email message.
    MessageRequest message?;
|};

# A record of a change to the user's mailbox. Each history change may affect multiple messages in multiple ways.
public type History record {
    # The mailbox sequence ID.
    string id?;
    # Labels added to messages in this history record.
    HistoryLabelAdded[] labelsAdded?;
    # Labels removed from messages in this history record.
    HistoryLabelRemoved[] labelsRemoved?;
    # List of messages changed in this history record. The fields for specific change types, such as `messagesAdded` may duplicate messages in this field. We recommend using the specific change-type fields instead of this.
    Message[] messages?;
    # Messages added to the mailbox in this history record.
    HistoryMessageAdded[] messagesAdded?;
    # Messages deleted (not Trashed) from the mailbox in this history record.
    HistoryMessageDeleted[] messagesDeleted?;
};

public type HistoryLabelAdded record {
    # Label IDs added to the message.
    string[] labelIds?;
    # An email message.
    Message message?;
};

public type HistoryLabelRemoved record {
    # Label IDs removed from the message.
    string[] labelIds?;
    # An email message.
    Message message?;
};

public type HistoryMessageAdded record {
    # An email message.
    Message message?;
};

public type HistoryMessageDeleted record {
    # An email message.
    Message message?;
};

# A file record to indicate inline image
public type ImageFile record {|
    *AttachmentFile;
    # The content id of the image. This will be used to refer the image in the mail body.
    string contentId;
|};

# Labels are used to categorize messages and threads within the user's mailbox. The maximum number of labels supported for a user's mailbox is 10,000.
public type Label record {
    # The color to assign to the label. Color is only available for labels that have their `type` set to `user`.
    LabelColor color?;
    # The immutable ID of the label.
    string id?;
    # The visibility of the label in the label list in the Gmail web interface.
    "labelShow"|"labelShowIfUnread"|"labelHide" labelListVisibility?;
    # The visibility of messages with this label in the message list in the Gmail web interface.
    "show"|"hide" messageListVisibility?;
    # The total number of messages with the label.
    int:Signed32 messagesTotal?;
    # The number of unread messages with the label.
    int:Signed32 messagesUnread?;
    # The display name of the label.
    string name?;
    # The total number of threads with the label.
    int:Signed32 threadsTotal?;
    # The number of unread threads with the label.
    int:Signed32 threadsUnread?;
    # The owner type for the label. User labels are created by the user and can be modified and deleted by the user and can be applied to any message or thread. System labels are internally created and cannot be added, modified, or deleted. System labels may be able to be applied to or removed from messages and threads under some circumstances but this is not guaranteed. For example, users can apply and remove the `INBOX` and `UNREAD` labels from messages and threads, but cannot apply or remove the `DRAFTS` or `SENT` labels from messages or threads.
    "system"|"user" 'type?;
};

# The color to assign to the label. Color is only available for labels that have their `type` set to `user`.
public type LabelColor record {
    # The background color represented as hex string #RRGGBB (ex #000000). This field is required in order to set the color of a label. Only the following predefined set of color values are allowed: \#000000, #434343, #666666, #999999, #cccccc, #efefef, #f3f3f3, #ffffff, \#fb4c2f, #ffad47, #fad165, #16a766, #43d692, #4a86e8, #a479e2, #f691b3, \#f6c5be, #ffe6c7, #fef1d1, #b9e4d0, #c6f3de, #c9daf8, #e4d7f5, #fcdee8, \#efa093, #ffd6a2, #fce8b3, #89d3b2, #a0eac9, #a4c2f4, #d0bcf1, #fbc8d9, \#e66550, #ffbc6b, #fcda83, #44b984, #68dfa9, #6d9eeb, #b694e8, #f7a7c0, \#cc3a21, #eaa041, #f2c960, #149e60, #3dc789, #3c78d8, #8e63ce, #e07798, \#ac2b16, #cf8933, #d5ae49, #0b804b, #2a9c68, #285bac, #653e9b, #b65775, \#822111, #a46a21, #aa8831, #076239, #1a764d, #1c4587, #41236d, #83334c \#464646, #e7e7e7, #0d3472, #b6cff5, #0d3b44, #98d7e4, #3d188e, #e3d7ff, \#711a36, #fbd3e0, #8a1c0a, #f2b2a8, #7a2e0b, #ffc8af, #7a4706, #ffdeb5, \#594c05, #fbe983, #684e07, #fdedc1, #0b4f30, #b3efd3, #04502e, #a2dcc1, \#c2c2c2, #4986e7, #2da2bb, #b99aff, #994a64, #f691b2, #ff7537, #ffad46, \#662e37, #ebdbde, #cca6ac, #094228, #42d692, #16a765
    string backgroundColor?;
    # The text color of the label, represented as hex string. This field is required in order to set the color of a label. Only the following predefined set of color values are allowed: \#000000, #434343, #666666, #999999, #cccccc, #efefef, #f3f3f3, #ffffff, \#fb4c2f, #ffad47, #fad165, #16a766, #43d692, #4a86e8, #a479e2, #f691b3, \#f6c5be, #ffe6c7, #fef1d1, #b9e4d0, #c6f3de, #c9daf8, #e4d7f5, #fcdee8, \#efa093, #ffd6a2, #fce8b3, #89d3b2, #a0eac9, #a4c2f4, #d0bcf1, #fbc8d9, \#e66550, #ffbc6b, #fcda83, #44b984, #68dfa9, #6d9eeb, #b694e8, #f7a7c0, \#cc3a21, #eaa041, #f2c960, #149e60, #3dc789, #3c78d8, #8e63ce, #e07798, \#ac2b16, #cf8933, #d5ae49, #0b804b, #2a9c68, #285bac, #653e9b, #b65775, \#822111, #a46a21, #aa8831, #076239, #1a764d, #1c4587, #41236d, #83334c \#464646, #e7e7e7, #0d3472, #b6cff5, #0d3b44, #98d7e4, #3d188e, #e3d7ff, \#711a36, #fbd3e0, #8a1c0a, #f2b2a8, #7a2e0b, #ffc8af, #7a4706, #ffdeb5, \#594c05, #fbe983, #684e07, #fdedc1, #0b4f30, #b3efd3, #04502e, #a2dcc1, \#c2c2c2, #4986e7, #2da2bb, #b99aff, #994a64, #f691b2, #ff7537, #ffad46, \#662e37, #ebdbde, #cca6ac, #094228, #42d692, #16a765
    string textColor?;
};

public type ListDraftsResponse record {
    # List of drafts. Note that the `Message` property in each `Draft` resource only contains an `id` and a `threadId`. The messages.get method can fetch additional message details.
    Draft[] drafts?;
    # Token to retrieve the next page of results in the list.
    string nextPageToken?;
    # Estimated total number of results.
    int resultSizeEstimate?;
};

public type ListHistoryResponse record {
    # List of history records. Any `messages` contained in the response will typically only have `id` and `threadId` fields populated.
    History[] history?;
    # The ID of the mailbox's current history record.
    string historyId?;
    # Page token to retrieve the next page of results in the list.
    string nextPageToken?;
};

public type ListLabelsResponse record {
    # List of labels. Note that each label resource only contains an `id`, `name`, `messageListVisibility`, `labelListVisibility`, and `type`. The labels.get method can fetch additional label details.
    Label[] labels?;
};

# List of messages.
public type ListMessagesResponse record {
    # List of messages. Note that each message resource contains only an `id` and a `threadId`. Additional message details can be fetched using the messages.get method.
    Message[] messages?;
    # Token to retrieve the next page of results in the list.
    string nextPageToken?;
    # Estimated total number of results.
    int resultSizeEstimate?;
};

public type ListThreadsResponse record {
    # Page token to retrieve the next page of results in the list.
    string nextPageToken?;
    # Estimated total number of results.
    int resultSizeEstimate?;
    # List of threads. Note that each thread resource does not contain a list of `messages`. The list of `messages` for a given thread can be fetched using the threads.get method.
    MailThread[] threads?;
};

# A collection of messages representing a conversation.
public type MailThread record {
    # The ID of the last history record that modified this thread.
    string historyId?;
    # The unique ID of the thread.
    string id?;
    # The list of messages in the thread.
    Message[] messages?;
    # A short part of the message text.
    string snippet?;
};

# Request payload used to create a collection of messages representing a conversation.
public type MailThreadRequest record {|
    # The unique ID of the thread.
    string id?;
    # The list of messages in the thread.
    Message[] messages?;
|};

# An email message.
public type Message record {
    # The ID of the thread the message belongs to.
    string threadId;
    # The immutable ID of the message.
    string id;
    # List of IDs of labels applied to this message.
    string[] labelIds?;
    # The entire email message in an RFC 2822 formatted. Returned in `messages.get` and `drafts.get` responses when the `format=RAW` parameter is supplied.
    string raw?;
    # A short part of the message text.
    string snippet?;
    # The ID of the last history record that modified this message.
    string historyId?;
    # The internal message creation timestamp (epoch ms), which determines ordering in the inbox. For normal SMTP-received email, this represents the time the message was originally accepted by Google, which is more reliable than the `Date` header. However, for API-migrated mail, it can be configured by client to be based on the `Date` header.
    string internalDate?;
    # Estimated size in bytes of the message.
    int:Signed32 sizeEstimate?;
    # Email header **To**
    string[] to?;
    # Email header **From**
    string 'from?;
    # Email header **Bcc**
    string[] bcc?;
    # Email header **Cc**
    string[] cc?;
    # Email header **Subject**
    string subject?;
    # Email header **Date**
    string date?;
    # Email header **Message-ID**
    string messageId?;
    # Email header **ContentType**
    string contentType?;
    # MIME type of the top level message part. Values in `multipart/alternative` such as `text/plain` and `text/html` and in `multipart/*` including `multipart/mixed` and `multipart/related` indicate that the message contains a structured body with MIME parts. Values in `message/rfc822` indicate that the message is a container for the message parts that follow after the header.
    string mimeType?;
    # Body of the message.
    MessagePart payload?;
};

# A single MIME message part.
public type MessagePart record {
    # The filename of the attachment. Only present if this message part represents an attachment.
    string filename?;
    # List of headers on this message part. For the top-level message part, representing the entire message payload, it will contain the standard RFC 2822 email headers such as `To`, `From`, and `Subject`.
    map<string> headers?;
    # The MIME type of the message part.
    string mimeType?;
    # The immutable ID of the message part.
    string partId;
    # When present, contains the ID of an external attachment that can be retrieved in a separate `messages.attachments.get` request. When not present, the entire content of the message part body is contained in the data field.
    string attachmentId?;
    # The body data of a MIME message part decoded as a UTF-8 string. May be empty for MIME container types that have no message body or when the body data is sent as a separate attachment. An attachment ID is present if the body data is contained in a separate attachment.
    string data?;
    # The body data of a MIME message part as raw decoded bytes. May be empty for MIME container types that have no message body or when the body data is sent as a separate attachment. An attachment ID is present if the body data is contained in a separate attachment.
    byte[] rawData?;
    # Number of bytes for the message part data.
    int:Signed32 size?;
    # The child MIME message parts of this part. This only applies to container MIME message parts, for example `multipart/*`. For non- container MIME message part types, such as `text/plain`, this field is empty. For more information, see RFC 1521.
    MessagePart[] parts?;
};

# Message Send Request-Payload (Charset UTF-8 will be used to encode the message body).
public type MessageRequest record {|
    # The recipients of the mail
    string[] to?;
    # The sender of the mail
    string 'from?;
    # The subject of the mail
    string subject?;
    # The cc recipients of the mail.
    string[] cc?;
    # The bcc recipients of the mail.
    string[] bcc?;
    # Message body of content type ```text/plain```.
    string bodyInText?;
    # Message body of content type ```text/html```.
    string bodyInHtml?;
    # The file array consisting the inline images.
    ImageFile[] inlineImages?;
    # The file array consisting the attachments.
    AttachmentFile[] attachments?;
    # ID of the thread the message must be replied to.
    string threadId?;
    # **Message-ID** header of the message being replied to.
    string initialMessageId?;
    # List of **Message-ID** headers identifying ancestors of the message being replied to.
    string[] references?;
|};

public type ModifyMessageRequest record {|
    # A list of IDs of labels to add to this message. You can add up to 100 labels with each update.
    string[] addLabelIds?;
    # A list IDs of labels to remove from this message. You can remove up to 100 labels with each update.
    string[] removeLabelIds?;
|};

public type ModifyThreadRequest record {|
    # A list of IDs of labels to add to this thread. You can add up to 100 labels with each update.
    string[] addLabelIds?;
    # A list of IDs of labels to remove from this thread. You can remove up to 100 labels with each update.
    string[] removeLabelIds?;
|};

# OAuth2 Refresh Token Grant Configs
public type OAuth2RefreshTokenGrantConfig record {|
    *http:OAuth2RefreshTokenGrantConfig; // Special Agent Note: OAuth2RefreshTokenGrantConfig FROM ballerina/http module
    # Refresh URL
    string refreshUrl = "https://accounts.google.com/o/oauth2/token";
|};

# Profile for a Gmail user.
public type Profile record {
    # The user's email address.
    string emailAddress?;
    # The ID of the mailbox's current history record.
    string historyId?;
    # The total number of messages in the mailbox.
    int:Signed32 messagesTotal?;
    # The total number of threads in the mailbox.
    int:Signed32 threadsTotal?;
};

# Proxy server configurations to be used with the HTTP client endpoint.
public type ProxyConfig record {|
    # Host name of the proxy server
    string host = "";
    # Proxy server port
    int port = 0;
    # Proxy server username
    string userName = "";
    # Proxy server password
    string password = "";
|};

# Defines the generic error type for the `gmail` module.
public type Error distinct error;

# Error that occurs when there is an issue with inline images or attachments. This could be due to issues like file not found, unsupported file type, etc.
public type FileGenericError distinct Error;

# Error that occurs when an invalid encoded value is provided for the `data` fields.
public type ValueEncodeError distinct Error;

# Holds value for message type **text/html**.
public const string CONTENT_TYPE_TEXT_HTML = "text/html";

# Holds value for message type **text/plain**.
public const string CONTENT_TYPE_TEXT_PLAIN = "text/plain";

# Data format for response.
public type Alt "json"|"media"|"proto";

# V1 error format.
public type Xgafv "1"|"2";

// --- Client ---

# The Gmail API lets you view and manage Gmail mailbox data like threads, messages, and labels.
public isolated client class Client {
    # Gets invoked to initialize the `connector`.
    # + config - The configurations to be used when initializing the `connector`
    # + serviceUrl - URL of the target service
    # + return - An error if connector initialization failed
    isolated function init(ConnectionConfig config, string serviceUrl = "https://gmail.googleapis.com/gmail/v1") returns error?;

    # Lists the drafts in the user's mailbox.
    # + xgafv - V1 error format.
    # + access_token - OAuth access token.
    # + alt - Data format for response.
    # + callback - JSONP
    # + fields - Selector specifying which fields to include in a partial response.
    # + 'key - API key. Your API key identifies your project and provides you with API access, quota, and reports. Required unless you provide an OAuth 2.0 token.
    # + oauth_token - OAuth 2.0 token for the current user.
    # + prettyPrint - Returns response with indentations and line breaks.
    # + quotaUser - Available to use for quota purposes for server-side applications. Can be any arbitrary string assigned to a user, but should not exceed 40 characters.
    # + upload_protocol - Upload protocol for media (e.g. "raw", "multipart").
    # + uploadType - Legacy upload protocol for media (e.g. "media", "multipart").
    # + includeSpamTrash - Include drafts from `SPAM` and `TRASH` in the results.
    # + maxResults - Maximum number of drafts to return. This field defaults to 100. The maximum allowed value for this field is 500.
    # + pageToken - Page token to retrieve a specific page of results in the list.
    # + q - Only return draft messages matching the specified query. Supports the same query format as the Gmail search box. For example, `"from:someuser@example.com rfc822msgid: is:unread"`.
    # + return - Successful response
    isolated resource function get users/[string userId]/drafts(Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = (), boolean? includeSpamTrash = (), int? maxResults = (), string? pageToken = (), string? q = ()) returns ListDraftsResponse|error;

    # Creates a new draft with the `DRAFT` label.
    # + payload - The draft to create.
    # + xgafv - V1 error format.
    # + access_token - OAuth access token.
    # + alt - Data format for response.
    # + callback - JSONP
    # + fields - Selector specifying which fields to include in a partial response.
    # + 'key - API key. Your API key identifies your project and provides you with API access, quota, and reports. Required unless you provide an OAuth 2.0 token.
    # + oauth_token - OAuth 2.0 token for the current user.
    # + prettyPrint - Returns response with indentations and line breaks.
    # + quotaUser - Available to use for quota purposes for server-side applications. Can be any arbitrary string assigned to a user, but should not exceed 40 characters.
    # + upload_protocol - Upload protocol for media (e.g. "raw", "multipart").
    # + uploadType - Legacy upload protocol for media (e.g. "media", "multipart").
    # + return - Successful response
    isolated resource function post users/[string userId]/drafts(DraftRequest payload, Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns Draft|error;

    # Sends the specified, existing draft to the recipients in the `To`, `Cc`, and `Bcc` headers.
    # + payload - The ID of the existing draft to send. (Optional) Updated draft if necessary.
    # + xgafv - V1 error format.
    # + access_token - OAuth access token.
    # + alt - Data format for response.
    # + callback - JSONP
    # + fields - Selector specifying which fields to include in a partial response.
    # + 'key - API key. Your API key identifies your project and provides you with API access, quota, and reports. Required unless you provide an OAuth 2.0 token.
    # + oauth_token - OAuth 2.0 token for the current user.
    # + prettyPrint - Returns response with indentations and line breaks.
    # + quotaUser - Available to use for quota purposes for server-side applications. Can be any arbitrary string assigned to a user, but should not exceed 40 characters.
    # + upload_protocol - Upload protocol for media (e.g. "raw", "multipart").
    # + uploadType - Legacy upload protocol for media (e.g. "media", "multipart").
    # + return - Successful response
    isolated resource function post users/[string userId]/drafts/send(DraftRequest payload, Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns Message|error;

    # Gets the specified draft.
    # + xgafv - V1 error format.
    # + access_token - OAuth access token.
    # + alt - Data format for response.
    # + callback - JSONP
    # + fields - Selector specifying which fields to include in a partial response.
    # + 'key - API key. Your API key identifies your project and provides you with API access, quota, and reports. Required unless you provide an OAuth 2.0 token.
    # + oauth_token - OAuth 2.0 token for the current user.
    # + prettyPrint - Returns response with indentations and line breaks.
    # + quotaUser - Available to use for quota purposes for server-side applications. Can be any arbitrary string assigned to a user, but should not exceed 40 characters.
    # + upload_protocol - Upload protocol for media (e.g. "raw", "multipart").
    # + uploadType - Legacy upload protocol for media (e.g. "media", "multipart").
    # + format - The format to return the draft in.
    # + return - Successful response
    isolated resource function get users/[string userId]/drafts/[string id](Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = (), "minimal"|"full"|"raw"|"metadata"? format = ()) returns Draft|error;

    # Replaces a draft's content.
    # + payload - The updated draft to update.
    # + xgafv - V1 error format.
    # + access_token - OAuth access token.
    # + alt - Data format for response.
    # + callback - JSONP
    # + fields - Selector specifying which fields to include in a partial response.
    # + 'key - API key. Your API key identifies your project and provides you with API access, quota, and reports. Required unless you provide an OAuth 2.0 token.
    # + oauth_token - OAuth 2.0 token for the current user.
    # + prettyPrint - Returns response with indentations and line breaks.
    # + quotaUser - Available to use for quota purposes for server-side applications. Can be any arbitrary string assigned to a user, but should not exceed 40 characters.
    # + upload_protocol - Upload protocol for media (e.g. "raw", "multipart").
    # + uploadType - Legacy upload protocol for media (e.g. "media", "multipart").
    # + return - Successful response
    isolated resource function put users/[string userId]/drafts/[string id](DraftRequest payload, Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns Draft|error;

    # Immediately and permanently deletes the specified draft. Does not simply trash it.
    # + xgafv - V1 error format.
    # + access_token - OAuth access token.
    # + alt - Data format for response.
    # + callback - JSONP
    # + fields - Selector specifying which fields to include in a partial response.
    # + 'key - API key. Your API key identifies your project and provides you with API access, quota, and reports. Required unless you provide an OAuth 2.0 token.
    # + oauth_token - OAuth 2.0 token for the current user.
    # + prettyPrint - Returns response with indentations and line breaks.
    # + quotaUser - Available to use for quota purposes for server-side applications. Can be any arbitrary string assigned to a user, but should not exceed 40 characters.
    # + upload_protocol - Upload protocol for media (e.g. "raw", "multipart").
    # + uploadType - Legacy upload protocol for media (e.g. "media", "multipart").
    # + return - Successful response
    isolated resource function delete users/[string userId]/drafts/[string id](Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns error?;

    # Lists the history of all changes to the given mailbox. History results are returned in chronological order (increasing `historyId`).
    # + xgafv - V1 error format.
    # + access_token - OAuth access token.
    # + alt - Data format for response.
    # + callback - JSONP
    # + fields - Selector specifying which fields to include in a partial response.
    # + 'key - API key. Your API key identifies your project and provides you with API access, quota, and reports. Required unless you provide an OAuth 2.0 token.
    # + oauth_token - OAuth 2.0 token for the current user.
    # + prettyPrint - Returns response with indentations and line breaks.
    # + quotaUser - Available to use for quota purposes for server-side applications. Can be any arbitrary string assigned to a user, but should not exceed 40 characters.
    # + upload_protocol - Upload protocol for media (e.g. "raw", "multipart").
    # + uploadType - Legacy upload protocol for media (e.g. "media", "multipart").
    # + historyTypes - History types to be returned by the function
    # + labelId - Only return messages with a label matching the ID.
    # + maxResults - Maximum number of history records to return. This field defaults to 100. The maximum allowed value for this field is 500.
    # + pageToken - Page token to retrieve a specific page of results in the list.
    # + startHistoryId - Required. Returns history records after the specified `startHistoryId`. The supplied `startHistoryId` should be obtained from the `historyId` of a message, thread, or previous `list` response. History IDs increase chronologically but are not contiguous with random gaps in between valid IDs. Supplying an invalid or out of date `startHistoryId` typically returns an `HTTP 404` error code. A `historyId` is typically valid for at least a week, but in some rare circumstances may be valid for only a few hours. If you receive an `HTTP 404` error response, your application should perform a full sync. If you receive no `nextPageToken` in the response, there are no updates to retrieve and you can store the returned `historyId` for a future request.
    # + return - Successful response
    isolated resource function get users/[string userId]/history(Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = (), ("messageAdded"|"messageDeleted"|"labelAdded"|"labelRemoved")[]? historyTypes = (), string? labelId = (), int? maxResults = (), string? pageToken = (), string? startHistoryId = ()) returns ListHistoryResponse|error;

    # Lists all labels in the user's mailbox.
    # + xgafv - V1 error format.
    # + access_token - OAuth access token.
    # + alt - Data format for response.
    # + callback - JSONP
    # + fields - Selector specifying which fields to include in a partial response.
    # + 'key - API key. Your API key identifies your project and provides you with API access, quota, and reports. Required unless you provide an OAuth 2.0 token.
    # + oauth_token - OAuth 2.0 token for the current user.
    # + prettyPrint - Returns response with indentations and line breaks.
    # + quotaUser - Available to use for quota purposes for server-side applications. Can be any arbitrary string assigned to a user, but should not exceed 40 characters.
    # + upload_protocol - Upload protocol for media (e.g. "raw", "multipart").
    # + uploadType - Legacy upload protocol for media (e.g. "media", "multipart").
    # + return - Successful response
    isolated resource function get users/[string userId]/labels(Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns ListLabelsResponse|error;

    # Creates a new label.
    # + payload - The label to create.
    # + xgafv - V1 error format.
    # + access_token - OAuth access token.
    # + alt - Data format for response.
    # + callback - JSONP
    # + fields - Selector specifying which fields to include in a partial response.
    # + 'key - API key. Your API key identifies your project and provides you with API access, quota, and reports. Required unless you provide an OAuth 2.0 token.
    # + oauth_token - OAuth 2.0 token for the current user.
    # + prettyPrint - Returns response with indentations and line breaks.
    # + quotaUser - Available to use for quota purposes for server-side applications. Can be any arbitrary string assigned to a user, but should not exceed 40 characters.
    # + upload_protocol - Upload protocol for media (e.g. "raw", "multipart").
    # + uploadType - Legacy upload protocol for media (e.g. "media", "multipart").
    # + return - Successful response
    isolated resource function post users/[string userId]/labels(Label payload, Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns Label|error;

    # Gets the specified label.
    # + xgafv - V1 error format.
    # + access_token - OAuth access token.
    # + alt - Data format for response.
    # + callback - JSONP
    # + fields - Selector specifying which fields to include in a partial response.
    # + 'key - API key. Your API key identifies your project and provides you with API access, quota, and reports. Required unless you provide an OAuth 2.0 token.
    # + oauth_token - OAuth 2.0 token for the current user.
    # + prettyPrint - Returns response with indentations and line breaks.
    # + quotaUser - Available to use for quota purposes for server-side applications. Can be any arbitrary string assigned to a user, but should not exceed 40 characters.
    # + upload_protocol - Upload protocol for media (e.g. "raw", "multipart").
    # + uploadType - Legacy upload protocol for media (e.g. "media", "multipart").
    # + return - Successful response
    isolated resource function get users/[string userId]/labels/[string id](Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns Label|error;

    # Updates the specified label.
    # + payload - The updated label to update.
    # + xgafv - V1 error format.
    # + access_token - OAuth access token.
    # + alt - Data format for response.
    # + callback - JSONP
    # + fields - Selector specifying which fields to include in a partial response.
    # + 'key - API key. Your API key identifies your project and provides you with API access, quota, and reports. Required unless you provide an OAuth 2.0 token.
    # + oauth_token - OAuth 2.0 token for the current user.
    # + prettyPrint - Returns response with indentations and line breaks.
    # + quotaUser - Available to use for quota purposes for server-side applications. Can be any arbitrary string assigned to a user, but should not exceed 40 characters.
    # + upload_protocol - Upload protocol for media (e.g. "raw", "multipart").
    # + uploadType - Legacy upload protocol for media (e.g. "media", "multipart").
    # + return - Successful response
    isolated resource function put users/[string userId]/labels/[string id](Label payload, Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns Label|error;

    # Immediately and permanently deletes the specified label and removes it from any messages and threads that it is applied to.
    # + xgafv - V1 error format.
    # + access_token - OAuth access token.
    # + alt - Data format for response.
    # + callback - JSONP
    # + fields - Selector specifying which fields to include in a partial response.
    # + 'key - API key. Your API key identifies your project and provides you with API access, quota, and reports. Required unless you provide an OAuth 2.0 token.
    # + oauth_token - OAuth 2.0 token for the current user.
    # + prettyPrint - Returns response with indentations and line breaks.
    # + quotaUser - Available to use for quota purposes for server-side applications. Can be any arbitrary string assigned to a user, but should not exceed 40 characters.
    # + upload_protocol - Upload protocol for media (e.g. "raw", "multipart").
    # + uploadType - Legacy upload protocol for media (e.g. "media", "multipart").
    # + return - Successful response
    isolated resource function delete users/[string userId]/labels/[string id](Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns error?;

    # Patch the specified label.
    # + payload - The updated label to update.
    # + xgafv - V1 error format.
    # + access_token - OAuth access token.
    # + alt - Data format for response.
    # + callback - JSONP
    # + fields - Selector specifying which fields to include in a partial response.
    # + 'key - API key. Your API key identifies your project and provides you with API access, quota, and reports. Required unless you provide an OAuth 2.0 token.
    # + oauth_token - OAuth 2.0 token for the current user.
    # + prettyPrint - Returns response with indentations and line breaks.
    # + quotaUser - Available to use for quota purposes for server-side applications. Can be any arbitrary string assigned to a user, but should not exceed 40 characters.
    # + upload_protocol - Upload protocol for media (e.g. "raw", "multipart").
    # + uploadType - Legacy upload protocol for media (e.g. "media", "multipart").
    # + return - Successful response
    isolated resource function patch users/[string userId]/labels/[string id](Label payload, Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns Label|error;

    # Lists the messages in the user's mailbox.
    # + xgafv - V1 error format.
    # + access_token - OAuth access token.
    # + alt - Data format for response.
    # + callback - JSONP
    # + fields - Selector specifying which fields to include in a partial response.
    # + 'key - API key. Your API key identifies your project and provides you with API access, quota, and reports. Required unless you provide an OAuth 2.0 token.
    # + oauth_token - OAuth 2.0 token for the current user.
    # + prettyPrint - Returns response with indentations and line breaks.
    # + quotaUser - Available to use for quota purposes for server-side applications. Can be any arbitrary string assigned to a user, but should not exceed 40 characters.
    # + upload_protocol - Upload protocol for media (e.g. "raw", "multipart").
    # + uploadType - Legacy upload protocol for media (e.g. "media", "multipart").
    # + includeSpamTrash - Include messages from `SPAM` and `TRASH` in the results.
    # + labelIds - Only return messages with labels that match all of the specified label IDs. Messages in a thread might have labels that other messages in the same thread don't have. To learn more, see [Manage labels on messages and threads](https://developers.google.com/gmail/api/guides/labels#manage_labels_on_messages_threads).
    # + maxResults - Maximum number of messages to return. This field defaults to 100. The maximum allowed value for this field is 500.
    # + pageToken - Page token to retrieve a specific page of results in the list.
    # + q - Only return messages matching the specified query. Supports the same query format as the Gmail search box. For example, `"from:someuser@example.com rfc822msgid: is:unread"`. Parameter cannot be used when accessing the api using the gmail.metadata scope.
    # + return - Successful response
    isolated resource function get users/[string userId]/messages(Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = (), boolean? includeSpamTrash = (), string[]? labelIds = (), int? maxResults = (), string? pageToken = (), string? q = ()) returns ListMessagesResponse|error;

    # Directly inserts a message into only this user's mailbox similar to `IMAP APPEND`, bypassing most scanning and classification. Does not send a message.
    # + payload - The message to be inserted.
    # + xgafv - V1 error format.
    # + access_token - OAuth access token.
    # + alt - Data format for response.
    # + callback - JSONP
    # + fields - Selector specifying which fields to include in a partial response.
    # + 'key - API key. Your API key identifies your project and provides you with API access, quota, and reports. Required unless you provide an OAuth 2.0 token.
    # + oauth_token - OAuth 2.0 token for the current user.
    # + prettyPrint - Returns response with indentations and line breaks.
    # + quotaUser - Available to use for quota purposes for server-side applications. Can be any arbitrary string assigned to a user, but should not exceed 40 characters.
    # + upload_protocol - Upload protocol for media (e.g. "raw", "multipart").
    # + uploadType - Legacy upload protocol for media (e.g. "media", "multipart").
    # + deleted - Mark the email as permanently deleted (not TRASH) and only visible in Google Vault to a Vault administrator. Only used for Google Workspace accounts.
    # + internalDateSource - Source for Gmail's internal date of the message.
    # + return - Successful response
    isolated resource function post users/[string userId]/messages(MessageRequest payload, Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = (), boolean? deleted = (), "receivedTime"|"dateHeader"? internalDateSource = ()) returns Message|error;

    # Deletes many messages by message ID. Provides no guarantees that messages were not already deleted or even existed at all.
    # + payload - The IDs of the messages to delete.
    # + xgafv - V1 error format.
    # + access_token - OAuth access token.
    # + alt - Data format for response.
    # + callback - JSONP
    # + fields - Selector specifying which fields to include in a partial response.
    # + 'key - API key. Your API key identifies your project and provides you with API access, quota, and reports. Required unless you provide an OAuth 2.0 token.
    # + oauth_token - OAuth 2.0 token for the current user.
    # + prettyPrint - Returns response with indentations and line breaks.
    # + quotaUser - Available to use for quota purposes for server-side applications. Can be any arbitrary string assigned to a user, but should not exceed 40 characters.
    # + upload_protocol - Upload protocol for media (e.g. "raw", "multipart").
    # + uploadType - Legacy upload protocol for media (e.g. "media", "multipart").
    # + return - Successful response
    isolated resource function post users/[string userId]/messages/batchDelete(BatchDeleteMessagesRequest payload, Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns error?;

    # Modifies the labels on the specified messages.
    # + payload - A list of labels to add/remove in messages.
    # + xgafv - V1 error format.
    # + access_token - OAuth access token.
    # + alt - Data format for response.
    # + callback - JSONP
    # + fields - Selector specifying which fields to include in a partial response.
    # + 'key - API key. Your API key identifies your project and provides you with API access, quota, and reports. Required unless you provide an OAuth 2.0 token.
    # + oauth_token - OAuth 2.0 token for the current user.
    # + prettyPrint - Returns response with indentations and line breaks.
    # + quotaUser - Available to use for quota purposes for server-side applications. Can be any arbitrary string assigned to a user, but should not exceed 40 characters.
    # + upload_protocol - Upload protocol for media (e.g. "raw", "multipart").
    # + uploadType - Legacy upload protocol for media (e.g. "media", "multipart").
    # + return - Successful response
    isolated resource function post users/[string userId]/messages/batchModify(BatchModifyMessagesRequest payload, Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns error?;

    # Imports a message into only this user's mailbox, with standard email delivery scanning and classification similar to receiving via SMTP. This method doesn't perform SPF checks, so it might not work for some spam messages, such as those attempting to perform domain spoofing. This method does not send a message.
    # + payload - The message to be imported.
    # + xgafv - V1 error format.
    # + access_token - OAuth access token.
    # + alt - Data format for response.
    # + callback - JSONP
    # + fields - Selector specifying which fields to include in a partial response.
    # + 'key - API key. Your API key identifies your project and provides you with API access, quota, and reports. Required unless you provide an OAuth 2.0 token.
    # + oauth_token - OAuth 2.0 token for the current user.
    # + prettyPrint - Returns response with indentations and line breaks.
    # + quotaUser - Available to use for quota purposes for server-side applications. Can be any arbitrary string assigned to a user, but should not exceed 40 characters.
    # + upload_protocol - Upload protocol for media (e.g. "raw", "multipart").
    # + uploadType - Legacy upload protocol for media (e.g. "media", "multipart").
    # + deleted - Mark the email as permanently deleted (not TRASH) and only visible in Google Vault to a Vault administrator. Only used for Google Workspace accounts.
    # + internalDateSource - Source for Gmail's internal date of the message.
    # + neverMarkSpam - Ignore the Gmail spam classifier decision and never mark this email as SPAM in the mailbox.
    # + processForCalendar - Process calendar invites in the email and add any extracted meetings to the Google Calendar for this user.
    # + return - Successful response
    isolated resource function post users/[string userId]/messages/'import(MessageRequest payload, Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = (), boolean? deleted = (), "receivedTime"|"dateHeader"? internalDateSource = (), boolean? neverMarkSpam = (), boolean? processForCalendar = ()) returns Message|error;

    # Sends the specified message to the recipients in the `To`, `Cc`, and `Bcc` headers. For example usage, see [Sending email](https://developers.google.com/gmail/api/guides/sending).
    # + payload - The message to be sent.
    # + xgafv - V1 error format.
    # + access_token - OAuth access token.
    # + alt - Data format for response.
    # + callback - JSONP
    # + fields - Selector specifying which fields to include in a partial response.
    # + 'key - API key. Your API key identifies your project and provides you with API access, quota, and reports. Required unless you provide an OAuth 2.0 token.
    # + oauth_token - OAuth 2.0 token for the current user.
    # + prettyPrint - Returns response with indentations and line breaks.
    # + quotaUser - Available to use for quota purposes for server-side applications. Can be any arbitrary string assigned to a user, but should not exceed 40 characters.
    # + upload_protocol - Upload protocol for media (e.g. "raw", "multipart").
    # + uploadType - Legacy upload protocol for media (e.g. "media", "multipart").
    # + return - Successful response
    isolated resource function post users/[string userId]/messages/send(MessageRequest payload, Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns Message|error;

    # Gets the specified message.
    # + xgafv - V1 error format.
    # + access_token - OAuth access token.
    # + alt - Data format for response.
    # + callback - JSONP
    # + fields - Selector specifying which fields to include in a partial response.
    # + 'key - API key. Your API key identifies your project and provides you with API access, quota, and reports. Required unless you provide an OAuth 2.0 token.
    # + oauth_token - OAuth 2.0 token for the current user.
    # + prettyPrint - Returns response with indentations and line breaks.
    # + quotaUser - Available to use for quota purposes for server-side applications. Can be any arbitrary string assigned to a user, but should not exceed 40 characters.
    # + upload_protocol - Upload protocol for media (e.g. "raw", "multipart").
    # + uploadType - Legacy upload protocol for media (e.g. "media", "multipart").
    # + format - The format to return the message in.
    # + metadataHeaders - When given and format is `METADATA`, only include headers specified.
    # + return - Successful response
    isolated resource function get users/[string userId]/messages/[string id](Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = (), "minimal"|"full"|"raw"|"metadata"? format = (), string[]? metadataHeaders = ()) returns Message|error;

    # Immediately and permanently deletes the specified message. This operation cannot be undone. Prefer `messages.trash` instead.
    # + xgafv - V1 error format.
    # + access_token - OAuth access token.
    # + alt - Data format for response.
    # + callback - JSONP
    # + fields - Selector specifying which fields to include in a partial response.
    # + 'key - API key. Your API key identifies your project and provides you with API access, quota, and reports. Required unless you provide an OAuth 2.0 token.
    # + oauth_token - OAuth 2.0 token for the current user.
    # + prettyPrint - Returns response with indentations and line breaks.
    # + quotaUser - Available to use for quota purposes for server-side applications. Can be any arbitrary string assigned to a user, but should not exceed 40 characters.
    # + upload_protocol - Upload protocol for media (e.g. "raw", "multipart").
    # + uploadType - Legacy upload protocol for media (e.g. "media", "multipart").
    # + return - Successful response
    isolated resource function delete users/[string userId]/messages/[string id](Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns error?;

    # Modifies the labels on the specified message.
    # + payload - A list of labels to add/remove on the message.
    # + xgafv - V1 error format.
    # + access_token - OAuth access token.
    # + alt - Data format for response.
    # + callback - JSONP
    # + fields - Selector specifying which fields to include in a partial response.
    # + 'key - API key. Your API key identifies your project and provides you with API access, quota, and reports. Required unless you provide an OAuth 2.0 token.
    # + oauth_token - OAuth 2.0 token for the current user.
    # + prettyPrint - Returns response with indentations and line breaks.
    # + quotaUser - Available to use for quota purposes for server-side applications. Can be any arbitrary string assigned to a user, but should not exceed 40 characters.
    # + upload_protocol - Upload protocol for media (e.g. "raw", "multipart").
    # + uploadType - Legacy upload protocol for media (e.g. "media", "multipart").
    # + return - Successful response
    isolated resource function post users/[string userId]/messages/[string id]/modify(ModifyMessageRequest payload, Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns Message|error;

    # Moves the specified message to the trash.
    # + xgafv - V1 error format.
    # + access_token - OAuth access token.
    # + alt - Data format for response.
    # + callback - JSONP
    # + fields - Selector specifying which fields to include in a partial response.
    # + 'key - API key. Your API key identifies your project and provides you with API access, quota, and reports. Required unless you provide an OAuth 2.0 token.
    # + oauth_token - OAuth 2.0 token for the current user.
    # + prettyPrint - Returns response with indentations and line breaks.
    # + quotaUser - Available to use for quota purposes for server-side applications. Can be any arbitrary string assigned to a user, but should not exceed 40 characters.
    # + upload_protocol - Upload protocol for media (e.g. "raw", "multipart").
    # + uploadType - Legacy upload protocol for media (e.g. "media", "multipart").
    # + return - Successful response
    isolated resource function post users/[string userId]/messages/[string id]/trash(Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns Message|error;

    # Removes the specified message from the trash.
    # + xgafv - V1 error format.
    # + access_token - OAuth access token.
    # + alt - Data format for response.
    # + callback - JSONP
    # + fields - Selector specifying which fields to include in a partial response.
    # + 'key - API key. Your API key identifies your project and provides you with API access, quota, and reports. Required unless you provide an OAuth 2.0 token.
    # + oauth_token - OAuth 2.0 token for the current user.
    # + prettyPrint - Returns response with indentations and line breaks.
    # + quotaUser - Available to use for quota purposes for server-side applications. Can be any arbitrary string assigned to a user, but should not exceed 40 characters.
    # + upload_protocol - Upload protocol for media (e.g. "raw", "multipart").
    # + uploadType - Legacy upload protocol for media (e.g. "media", "multipart").
    # + return - Successful response
    isolated resource function post users/[string userId]/messages/[string id]/untrash(Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns Message|error;

    # Gets the specified message attachment.
    # + xgafv - V1 error format.
    # + access_token - OAuth access token.
    # + alt - Data format for response.
    # + callback - JSONP
    # + fields - Selector specifying which fields to include in a partial response.
    # + 'key - API key. Your API key identifies your project and provides you with API access, quota, and reports. Required unless you provide an OAuth 2.0 token.
    # + oauth_token - OAuth 2.0 token for the current user.
    # + prettyPrint - Returns response with indentations and line breaks.
    # + quotaUser - Available to use for quota purposes for server-side applications. Can be any arbitrary string assigned to a user, but should not exceed 40 characters.
    # + upload_protocol - Upload protocol for media (e.g. "raw", "multipart").
    # + uploadType - Legacy upload protocol for media (e.g. "media", "multipart").
    # + return - Successful response
    isolated resource function get users/[string userId]/messages/[string messageId]/attachments/[string id](Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns Attachment|error;

    # Gets the current user's Gmail profile.
    # + xgafv - V1 error format.
    # + access_token - OAuth access token.
    # + alt - Data format for response.
    # + callback - JSONP
    # + fields - Selector specifying which fields to include in a partial response.
    # + 'key - API key. Your API key identifies your project and provides you with API access, quota, and reports. Required unless you provide an OAuth 2.0 token.
    # + oauth_token - OAuth 2.0 token for the current user.
    # + prettyPrint - Returns response with indentations and line breaks.
    # + quotaUser - Available to use for quota purposes for server-side applications. Can be any arbitrary string assigned to a user, but should not exceed 40 characters.
    # + upload_protocol - Upload protocol for media (e.g. "raw", "multipart").
    # + uploadType - Legacy upload protocol for media (e.g. "media", "multipart").
    # + return - Successful response
    isolated resource function get users/[string userId]/profile(Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns Profile|error;

    # Lists the threads in the user's mailbox.
    # + xgafv - V1 error format.
    # + access_token - OAuth access token.
    # + alt - Data format for response.
    # + callback - JSONP
    # + fields - Selector specifying which fields to include in a partial response.
    # + 'key - API key. Your API key identifies your project and provides you with API access, quota, and reports. Required unless you provide an OAuth 2.0 token.
    # + oauth_token - OAuth 2.0 token for the current user.
    # + prettyPrint - Returns response with indentations and line breaks.
    # + quotaUser - Available to use for quota purposes for server-side applications. Can be any arbitrary string assigned to a user, but should not exceed 40 characters.
    # + upload_protocol - Upload protocol for media (e.g. "raw", "multipart").
    # + uploadType - Legacy upload protocol for media (e.g. "media", "multipart").
    # + includeSpamTrash - Include threads from `SPAM` and `TRASH` in the results.
    # + labelIds - Only return threads with labels that match all of the specified label IDs.
    # + maxResults - Maximum number of threads to return. This field defaults to 100. The maximum allowed value for this field is 500.
    # + pageToken - Page token to retrieve a specific page of results in the list.
    # + q - Only return threads matching the specified query. Supports the same query format as the Gmail search box. For example, `"from:someuser@example.com rfc822msgid: is:unread"`. Parameter cannot be used when accessing the api using the gmail.metadata scope.
    # + return - Successful response
    isolated resource function get users/[string userId]/threads(Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = (), boolean? includeSpamTrash = (), string[]? labelIds = (), int? maxResults = (), string? pageToken = (), string? q = ()) returns ListThreadsResponse|error;

    # Gets the specified thread.
    # + xgafv - V1 error format.
    # + access_token - OAuth access token.
    # + alt - Data format for response.
    # + callback - JSONP
    # + fields - Selector specifying which fields to include in a partial response.
    # + 'key - API key. Your API key identifies your project and provides you with API access, quota, and reports. Required unless you provide an OAuth 2.0 token.
    # + oauth_token - OAuth 2.0 token for the current user.
    # + prettyPrint - Returns response with indentations and line breaks.
    # + quotaUser - Available to use for quota purposes for server-side applications. Can be any arbitrary string assigned to a user, but should not exceed 40 characters.
    # + upload_protocol - Upload protocol for media (e.g. "raw", "multipart").
    # + uploadType - Legacy upload protocol for media (e.g. "media", "multipart").
    # + format - The format to return the messages in.
    # + metadataHeaders - When given and format is METADATA, only include headers specified.
    # + return - Successful response
    isolated resource function get users/[string userId]/threads/[string id](Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = (), "full"|"metadata"|"minimal"? format = (), string[]? metadataHeaders = ()) returns MailThread|error;

    # Immediately and permanently deletes the specified thread. Any messages that belong to the thread are also deleted. This operation cannot be undone. Prefer `threads.trash` instead.
    # + xgafv - V1 error format.
    # + access_token - OAuth access token.
    # + alt - Data format for response.
    # + callback - JSONP
    # + fields - Selector specifying which fields to include in a partial response.
    # + 'key - API key. Your API key identifies your project and provides you with API access, quota, and reports. Required unless you provide an OAuth 2.0 token.
    # + oauth_token - OAuth 2.0 token for the current user.
    # + prettyPrint - Returns response with indentations and line breaks.
    # + quotaUser - Available to use for quota purposes for server-side applications. Can be any arbitrary string assigned to a user, but should not exceed 40 characters.
    # + upload_protocol - Upload protocol for media (e.g. "raw", "multipart").
    # + uploadType - Legacy upload protocol for media (e.g. "media", "multipart").
    # + return - Successful response
    isolated resource function delete users/[string userId]/threads/[string id](Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns error?;

    # Modifies the labels applied to the thread. This applies to all messages in the thread.
    # + payload - A list labels to add/remove on the thread.
    # + xgafv - V1 error format.
    # + access_token - OAuth access token.
    # + alt - Data format for response.
    # + callback - JSONP
    # + fields - Selector specifying which fields to include in a partial response.
    # + 'key - API key. Your API key identifies your project and provides you with API access, quota, and reports. Required unless you provide an OAuth 2.0 token.
    # + oauth_token - OAuth 2.0 token for the current user.
    # + prettyPrint - Returns response with indentations and line breaks.
    # + quotaUser - Available to use for quota purposes for server-side applications. Can be any arbitrary string assigned to a user, but should not exceed 40 characters.
    # + upload_protocol - Upload protocol for media (e.g. "raw", "multipart").
    # + uploadType - Legacy upload protocol for media (e.g. "media", "multipart").
    # + return - Successful response
    isolated resource function post users/[string userId]/threads/[string id]/modify(ModifyThreadRequest payload, Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns MailThread|error;

    # Moves the specified thread to the trash. Any messages that belong to the thread are also moved to the trash.
    # + xgafv - V1 error format.
    # + access_token - OAuth access token.
    # + alt - Data format for response.
    # + callback - JSONP
    # + fields - Selector specifying which fields to include in a partial response.
    # + 'key - API key. Your API key identifies your project and provides you with API access, quota, and reports. Required unless you provide an OAuth 2.0 token.
    # + oauth_token - OAuth 2.0 token for the current user.
    # + prettyPrint - Returns response with indentations and line breaks.
    # + quotaUser - Available to use for quota purposes for server-side applications. Can be any arbitrary string assigned to a user, but should not exceed 40 characters.
    # + upload_protocol - Upload protocol for media (e.g. "raw", "multipart").
    # + uploadType - Legacy upload protocol for media (e.g. "media", "multipart").
    # + return - Successful response
    isolated resource function post users/[string userId]/threads/[string id]/trash(Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns MailThread|error;

    # Removes the specified thread from the trash. Any messages that belong to the thread are also removed from the trash.
    # + xgafv - V1 error format.
    # + access_token - OAuth access token.
    # + alt - Data format for response.
    # + callback - JSONP
    # + fields - Selector specifying which fields to include in a partial response.
    # + 'key - API key. Your API key identifies your project and provides you with API access, quota, and reports. Required unless you provide an OAuth 2.0 token.
    # + oauth_token - OAuth 2.0 token for the current user.
    # + prettyPrint - Returns response with indentations and line breaks.
    # + quotaUser - Available to use for quota purposes for server-side applications. Can be any arbitrary string assigned to a user, but should not exceed 40 characters.
    # + upload_protocol - Upload protocol for media (e.g. "raw", "multipart").
    # + uploadType - Legacy upload protocol for media (e.g. "media", "multipart").
    # + return - Successful response
    isolated resource function post users/[string userId]/threads/[string id]/untrash(Xgafv? xgafv = (), string? access_token = (), Alt? alt = (), string? callback = (), string? fields = (), string? 'key = (), string? oauth_token = (), boolean? prettyPrint = (), string? quotaUser = (), string? upload_protocol = (), string? uploadType = ()) returns MailThread|error;
}
