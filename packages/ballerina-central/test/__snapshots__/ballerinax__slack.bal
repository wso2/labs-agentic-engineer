// ============================================================
// Library: ballerinax/slack
// [Slack](https://slack.com/) is a collaboration platform for teams that offers real-time messaging, file sharing, and integration with various tools. it helps streamline communication and enhance productivity through organized channels and direct messaging.
// ============================================================
import ballerinax/slack;

// --- Types ---


type '200200200200AnyOf12234 record {
    '200Team3 team;
    true ok;
    '200User3 user;
};


type '200200200AnyOf1123 record {
    FileObj file;
    CommentObj comment;
    true ok;
        "file_comment"  'type;
};


type '200200200AnyOf1223 record {
    '200Team2 team;
    true ok;
    '200User2 user;
};


type '200200AnyOf112 record {
    FileObj file;
    true ok;
        "file"  'type;
};


type '200200AnyOf12 record {
    int count;
    true ok;
};


type '200200AnyOf122 record {
    '200Team1 team;
    true ok;
    '200User1 user;
};


type '200AnyOf1 record {
    true ok;
    '200Items[] items;
};


type '200AnyOf11 record {
    ChannelDef channel;
    MessageObj message;
    true ok;
        "message"  'type;
};


type '200AnyOf12 record {
    '200Team team;
    true ok;
    '200User user;
};


type '200Team record {
    TeamDef id;
};


type '200Team1 record {
    TeamDef id;
};


type '200Team2 record {
    TeamDef id;
};


type '200Team3 record {
    string image132;
    string image102;
    string image68;
    boolean imageDefault;
    string image34;
    string domain;
    string image230;
    string image44;
    string image88;
    string name;
    TeamDef id;
};


type '200User record {
    string name;
    UserIdDef id;
};


type '200User1 record {
    string name;
    UserIdDef id;
    string email;
};


type '200User2 record {
    string image32;
    string image24;
    string name;
    string image192;
    UserIdDef id;
    string image48;
    string image72;
    string image512;
};


type '200User3 record {
    string name;
    UserIdDef id;
};


type AdminAppsApproveBody record {
    string teamId?;
    # The id of the app to approve
    string appId?;
    # The id of the request to approve
    string requestId?;
};

# Represents the Queries record for the operation: admin_apps_approved_list

type AdminAppsApprovedListQueries record {
    # Set `cursor` to `next_cursor` returned by the previous call to list items in the next page
    string cursor?;
    # The maximum number of items to return. Must be between 1 - 1000 both inclusive
    int 'limit?;
    string teamId?;
    string enterpriseId?;
};

# Represents the Queries record for the operation: admin_apps_requests_list

type AdminAppsRequestsListQueries record {
    # Set `cursor` to `next_cursor` returned by the previous call to list items in the next page
    string cursor?;
    # The maximum number of items to return. Must be between 1 - 1000 both inclusive
    int 'limit?;
    string teamId?;
};


type AdminAppsRestrictBody record {
    string teamId?;
    # The id of the app to restrict
    string appId?;
    # The id of the request to restrict
    string requestId?;
};

# Represents the Queries record for the operation: admin_apps_restricted_list

type AdminAppsRestrictedListQueries record {
    # Set `cursor` to `next_cursor` returned by the previous call to list items in the next page
    string cursor?;
    # The maximum number of items to return. Must be between 1 - 1000 both inclusive
    int 'limit?;
    string teamId?;
    string enterpriseId?;
};


type AdminConversationsArchiveBody record {
    # The channel to archive
    string channelId;
};

# Schema for successful response of admin.conversations.archive

type AdminConversationsArchiveResponse record {
    true ok;
};


type AdminConversationsConvertToPrivateBody record {
    # The channel to convert to private
    string channelId;
};

# Schema for successful response of admin.conversations.convertToPrivate

type AdminConversationsConvertToPrivateResponse record {
    true ok;
};


type AdminConversationsCreateBody record {
    # When `true`, creates a private channel instead of a public channel
    boolean isPrivate;
    # When `true`, the channel will be available org-wide. Note: if the channel is not `org_wide=true`, you must specify a `team_id` for this channel
    boolean orgWide?;
    # Name of the public or private channel to create
    string name;
    # Description of the public or private channel to create
    string description?;
    # The workspace to create the channel in. Note: this argument is required unless you set `org_wide=true`
    string teamId?;
};

# Schema for successful response of admin.conversations.create

type AdminConversationsCreateResponse record {
    true ok;
    ChannelIdDef channelId?;
};


type AdminConversationsDeleteBody record {
    # The channel to delete
    string channelId;
};

# Schema for successful response of admin.conversations.delete

type AdminConversationsDeleteResponse record {
    true ok;
};


type AdminConversationsDisconnectSharedBody record {
    # The channel to be disconnected from some workspaces
    string channelId;
    # The team to be removed from the channel. Currently only a single team id can be specified
    string leavingTeamIds?;
};

# Represents the Queries record for the operation: admin_conversations_ekm_listOriginalConnectedChannelInfo

type AdminConversationsEkmListOriginalConnectedChannelInfoQueries record {
    # A comma-separated list of channels to filter to
    string channelIds?;
    # Set `cursor` to `next_cursor` returned by the previous call to list items in the next page
    string cursor?;
    # A comma-separated list of the workspaces to which the channels you would like returned belong
    string teamIds?;
    # The maximum number of items to return. Must be between 1 - 1000 both inclusive
    int 'limit?;
};

# Represents the Queries record for the operation: admin_conversations_getConversationPrefs

type AdminConversationsGetConversationPrefsQueries record {
    # The channel to get preferences for
    string channelId;
};

# Schema for successful response of admin.conversations.getConversationPrefs

type AdminConversationsGetConversationPrefsResponse record {
    true ok;
    AdminConversationsGetConversationPrefsResponsePrefs prefs?;
};


type AdminConversationsGetConversationPrefsResponsePrefs record {
    AdminConversationsGetConversationPrefsResponsePrefsCanThread canThread?;
    AdminConversationsGetConversationPrefsResponsePrefsWhoCanPost whoCanPost?;
};


type AdminConversationsGetConversationPrefsResponsePrefsCanThread record {
    string[] 'type?;
    string[] user?;
};


type AdminConversationsGetConversationPrefsResponsePrefsWhoCanPost record {
    string[] 'type?;
    string[] user?;
};

# Represents the Queries record for the operation: admin_conversations_getTeams

type AdminConversationsGetTeamsQueries record {
    # Set `cursor` to `next_cursor` returned by the previous call to list items in the next page
    string cursor?;
    # The maximum number of items to return. Must be between 1 - 1000 both inclusive
    int 'limit?;
    # The channel to determine connected workspaces within the organization for
    string channelId;
};

# Schema for successful response of admin.conversations.getTeams

type AdminConversationsGetTeamsResponse record {
    TeamDef[] teamIds;
    AdminConversationsGetTeamsResponseResponseMetadata responseMetadata?;
    true ok;
};


type AdminConversationsGetTeamsResponseResponseMetadata record {
    string nextCursor;
};


type AdminConversationsInviteBody record {
    # The users to invite
    string userIds;
    # The channel that the users will be invited to
    string channelId;
};

# Schema for successful response of admin.conversations.invite

type AdminConversationsInviteResponse record {
    true ok;
};


type AdminConversationsRenameBody record {
    string name;
    # The channel to rename
    string channelId;
};

# Schema for successful response of admin.conversations.disconnectShared

type AdminConversationsRenameResponse record {
    true ok;
};

# Schema for successful response of admin.conversations.rename

type AdminConversationsRenameResponse1 record {
    true ok;
};


type AdminConversationsRestrictAccessAddGroupBody record {
    # The [IDP Group](https://slack.com/help/articles/115001435788-Connect-identity-provider-groups-to-your-Enterprise-Grid-org) ID to be an allowlist for the private channel
    string groupId;
    # The workspace where the channel exists. This argument is required for channels only tied to one workspace, and optional for channels that are shared across an organization
    string teamId?;
    # The channel to link this group to
    string channelId;
    # Authentication token. Requires scope: `admin.conversations:write`
    string token;
};

# Represents the Queries record for the operation: admin_conversations_restrictAccess_listGroups

type AdminConversationsRestrictAccessListGroupsQueries record {
    # The workspace where the channel exists. This argument is required for channels only tied to one workspace, and optional for channels that are shared across an organization
    string teamId?;
    string channelId;
};


type AdminConversationsRestrictAccessRemoveGroupBody record {
    # The [IDP Group](https://slack.com/help/articles/115001435788-Connect-identity-provider-groups-to-your-Enterprise-Grid-org) ID to remove from the private channel
    string groupId;
    # The workspace where the channel exists. This argument is required for channels only tied to one workspace, and optional for channels that are shared across an organization
    string teamId;
    # The channel to remove the linked group from
    string channelId;
    # Authentication token. Requires scope: `admin.conversations:write`
    string token;
};

# Represents the Queries record for the operation: admin_conversations_search

type AdminConversationsSearchQueries record {
    # Set `cursor` to `next_cursor` returned by the previous call to list items in the next page
    string cursor?;
    # The type of channel to include or exclude in the search. For example `private` will search private channels, while `private_exclude` will exclude them. For a full list of types, check the [Types section](#types)
    string searchChannelTypes?;
    # Comma separated string of team IDs, signifying the workspaces to search through
    string teamIds?;
    # Name of the the channel to query by
    string query?;
    # Maximum number of items to be returned. Must be between 1 - 20 both inclusive. Default is 10
    int 'limit?;
    # Possible values are `relevant` (search ranking based on what we think is closest), `name` (alphabetical), `member_count` (number of users in the channel), and `created` (date channel was created). You can optionally pair this with the `sort_dir` arg to change how it is sorted
    string sort?;
    # Sort direction. Possible values are `asc` for ascending order like (1, 2, 3) or (a, b, c), and `desc` for descending order like (3, 2, 1) or (c, b, a)
    string sortDir?;
};

# Schema for successful response of admin.conversations.search

type AdminConversationsSearchResponse record {
    string nextCursor;
    ChannelObj[] channels;
};


type AdminConversationsSetConversationPrefsBody record {
    # The channel to set the prefs for
    string channelId;
    # The prefs for this channel in a stringified JSON format
    string prefs;
};

# Schema for successful response of admin.conversations.setConversationPrefs

type AdminConversationsSetConversationPrefsResponse record {
    true ok;
};


type AdminConversationsSetTeamsBody record {
    # True if channel has to be converted to an org channel
    boolean orgChannel?;
    # A comma-separated list of workspaces to which the channel should be shared. Not required if the channel is being shared org-wide
    string targetTeamIds?;
    # The workspace to which the channel belongs. Omit this argument if the channel is a cross-workspace shared channel
    string teamId?;
    # The encoded `channel_id` to add or remove to workspaces
    string channelId;
};


type AdminConversationsUnarchiveBody record {
    # The channel to unarchive
    string channelId;
};

# Schema for successful response of admin.conversations.unarchive

type AdminConversationsUnarchiveResponse record {
    true ok;
};


type AdminEmojiAddAliasBody record {
    # The alias of the emoji
    string aliasFor;
    # The name of the emoji to be aliased. Colons (`:myemoji:`) around the value are not required, although they may be included
    string name;
    # Authentication token. Requires scope: `admin.teams:write`
    string token;
};


type AdminEmojiAddBody record {
    # The name of the emoji to be removed. Colons (`:myemoji:`) around the value are not required, although they may be included
    string name;
    # The URL of a file to use as an image for the emoji. Square images under 128KB and with transparent backgrounds work best
    string url;
    # Authentication token. Requires scope: `admin.teams:write`
    string token;
};

# Represents the Queries record for the operation: admin_emoji_list

type AdminEmojiListQueries record {
    # Set `cursor` to `next_cursor` returned by the previous call to list items in the next page
    string cursor?;
    # The maximum number of items to return. Must be between 1 - 1000 both inclusive
    int 'limit?;
};


type AdminEmojiRemoveBody record {
    # The name of the emoji to be removed. Colons (`:myemoji:`) around the value are not required, although they may be included
    string name;
    # Authentication token. Requires scope: `admin.teams:write`
    string token;
};


type AdminEmojiRenameBody record {
    # The name of the emoji to be renamed. Colons (`:myemoji:`) around the value are not required, although they may be included
    string name;
    # The new name of the emoji
    string newName;
    # Authentication token. Requires scope: `admin.teams:write`
    string token;
};

# Represents the Queries record for the operation: admin_inviteRequests_approved_list

type AdminInviteRequestsApprovedListQueries record {
    # Value of the `next_cursor` field sent as part of the previous API response
    string cursor?;
    # The number of results that will be returned by the API on each invocation. Must be between 1 - 1000, both inclusive
    int 'limit?;
    # ID for the workspace where the invite requests were made
    string teamId?;
};

# Represents the Queries record for the operation: admin_inviteRequests_denied_list

type AdminInviteRequestsDeniedListQueries record {
    # Value of the `next_cursor` field sent as part of the previous api response
    string cursor?;
    # The number of results that will be returned by the API on each invocation. Must be between 1 - 1000 both inclusive
    int 'limit?;
    # ID for the workspace where the invite requests were made
    string teamId?;
};

# Represents the Queries record for the operation: admin_inviteRequests_list

type AdminInviteRequestsListQueries record {
    # Value of the `next_cursor` field sent as part of the previous API response
    string cursor?;
    # The number of results that will be returned by the API on each invocation. Must be between 1 - 1000, both inclusive
    int 'limit?;
    # ID for the workspace where the invite requests were made
    string teamId?;
};

# Represents the Queries record for the operation: admin_teams_admins_list

type AdminTeamsAdminsListQueries record {
    # Set `cursor` to `next_cursor` returned by the previous call to list items in the next page
    string cursor?;
    # The maximum number of items to return
    int 'limit?;
    string teamId;
};


type AdminTeamsCreateBody record {
    # Description for the team
    string teamDescription?;
    # Team domain (for example, slacksoftballteam)
    string teamDomain;
    # Team name (for example, Slack Softball Team)
    string teamName;
    # Who can join the team. A team's discoverability can be `open`, `closed`, `invite_only`, or `unlisted`
    string teamDiscoverability?;
};

# Represents the Queries record for the operation: admin_teams_list

type AdminTeamsListQueries record {
    # Set `cursor` to `next_cursor` returned by the previous call to list items in the next page
    string cursor?;
    # The maximum number of items to return. Must be between 1 - 100 both inclusive
    int 'limit?;
};

# Represents the Queries record for the operation: admin_teams_owners_list

type AdminTeamsOwnersListQueries record {
    # Set `cursor` to `next_cursor` returned by the previous call to list items in the next page
    string cursor?;
    # The maximum number of items to return. Must be between 1 - 1000 both inclusive
    int 'limit?;
    string teamId;
};

# Represents the Queries record for the operation: admin_teams_settings_info

type AdminTeamsSettingsInfoQueries record {
    string teamId;
};


type AdminTeamsSettingsSetDefaultChannelsBody record {
    # An array of channel IDs
    string channelIds;
    # ID for the workspace to set the default channel for
    string teamId;
    # Authentication token. Requires scope: `admin.teams:write`
    string token;
};


type AdminTeamsSettingsSetDescriptionBody record {
    # The new description for the workspace
    string description;
    # ID for the workspace to set the description for
    string teamId;
};


type AdminTeamsSettingsSetDiscoverabilityBody record {
    # This workspace's discovery setting. It must be set to one of `open`, `invite_only`, `closed`, or `unlisted`
    string discoverability;
    # The ID of the workspace to set discoverability on
    string teamId;
};


type AdminTeamsSettingsSetIconBody record {
    # Image URL for the icon
    string imageUrl;
    # ID for the workspace to set the icon for
    string teamId;
    # Authentication token. Requires scope: `admin.teams:write`
    string token;
};


type AdminTeamsSettingsSetNameBody record {
    # The new name of the workspace
    string name;
    # ID for the workspace to set the name for
    string teamId;
};


type AdminUsergroupsAddChannelsBody record {
    # Comma separated string of channel IDs
    string channelIds;
    # ID of the IDP group to add default channels for
    string usergroupId;
    # The workspace to add default channels in
    string teamId?;
};


type AdminUsergroupsAddTeamsBody record {
    # A comma separated list of encoded team (workspace) IDs. Each workspace *MUST* belong to the organization associated with the token
    string teamIds;
    # An encoded usergroup (IDP Group) ID
    string usergroupId;
    # When `true`, this method automatically creates new workspace accounts for the IDP group members
    boolean autoProvision?;
};

# Represents the Queries record for the operation: admin_usergroups_listChannels

type AdminUsergroupsListChannelsQueries record {
    # Flag to include or exclude the count of members per channel
    boolean includeNumMembers?;
    # ID of the IDP group to list default channels for
    string usergroupId;
    # ID of the the workspace
    string teamId?;
};


type AdminUsergroupsRemoveChannelsBody record {
    # Comma-separated string of channel IDs
    string channelIds;
    # ID of the IDP Group
    string usergroupId;
};


type AdminUsersAssignBody record {
    # Comma separated values of channel IDs to add user in the new workspace
    string channelIds?;
    # True if user should be added to the workspace as a single-channel guest
    boolean isUltraRestricted?;
    # The ID of the user to add to the workspace
    string userId;
    # True if user should be added to the workspace as a guest
    boolean isRestricted?;
    # The ID (`T1234`) of the workspace
    string teamId;
};


type AdminUsersInviteBody record {
    # A comma-separated list of `channel_id`s for this user to join. At least one channel is required
    string channelIds;
    # Is this user a single channel guest user? (default: false)
    boolean isUltraRestricted?;
    # An optional message to send to the user in the invite email
    string customMessage?;
    # Timestamp when guest account should be disabled. Only include this timestamp if you are inviting a guest user and you want their account to expire on a certain date
    string guestExpirationTs?;
    # Is this user a multi-channel guest user? (default: false)
    boolean isRestricted?;
    # Allow this invite to be resent in the future if a user has not signed up yet. (default: false)
    boolean resend?;
    # Full name of the user
    string realName?;
    # The ID (`T1234`) of the workspace
    string teamId;
    # The email address of the person to invite
    string email;
};

# Represents the Queries record for the operation: admin_users_list

type AdminUsersListQueries record {
    # Set `cursor` to `next_cursor` returned by the previous call to list items in the next page
    string cursor?;
    # Limit for how many users to be retrieved per page
    int 'limit?;
    # The ID (`T1234`) of the workspace
    string teamId;
};


type AdminUsersRemoveBody record {
    # The ID of the user to remove
    string userId;
    # The ID (`T1234`) of the workspace
    string teamId;
};


type AdminUsersSessionInvalidateBody record {
    int sessionId;
    # ID of the team that the session belongs to
    string teamId;
};


type AdminUsersSessionResetBody record {
    # The ID of the user to wipe sessions for
    string userId;
    # Only expire mobile sessions (default: false)
    boolean mobileOnly?;
    # Only expire web sessions (default: false)
    boolean webOnly?;
};


type AdminUsersSetAdminBody record {
    # The ID of the user to designate as an admin
    string userId;
    # The ID (`T1234`) of the workspace
    string teamId;
};


type AdminUsersSetExpirationBody record {
    # Timestamp when guest account should be disabled
    int expirationTs;
    # The ID of the user to set an expiration for
    string userId;
    # The ID (`T1234`) of the workspace
    string teamId;
};


type AdminUsersSetOwnerBody record {
    # Id of the user to promote to owner
    string userId;
    # The ID (`T1234`) of the workspace
    string teamId;
};

# Represents the Headers record for the operation: admin_users_setOwner

type AdminUsersSetOwnerHeaders record {
    # Authentication token. Requires scope: `admin.users:write`
    string token;
};


type AdminUsersSetRegularBody record {
    # The ID of the user to designate as a regular user
    string userId;
    # The ID (`T1234`) of the workspace
    string teamId;
};

# Generated from users.getPresence with shasum e7251aec575d8863f9e0eb38663ae9dc26655f65

type APIMethodUsersGetPresence record {
    boolean auto_away?;
    int connection_count?;
    int last_activity?;
    boolean manual_away?;
    true ok;
    boolean online?;
    string presence;
};

# Schema for successful response api.permissions.scopes.list method

type ApiPermissionsScopesListResponse record {
    true ok;
    ApiPermissionsScopesListResponseScopes scopes;
};


type ApiPermissionsScopesListResponseScopes record {
    ScopesObj app_home?;
    ScopesObj channel?;
    ScopesObj group?;
    ScopesObj im?;
    ScopesObj mpim?;
    ScopesObj team?;
    ScopesObj user?;
};

# Represents the Queries record for the operation: api_test

type ApiTestQueries record {
    # example property to return
    string foo?;
};

# Schema for successful response api.test method

type ApiTestResponse record {
    true ok;
    # Rest field
    record {} ;
};

# Represents the Queries record for the operation: apps_event_authorizations_list

type AppsEventAuthorizationsListQueries record {
    string cursor?;
    int 'limit?;
    string eventContext;
};

# Schema for successful response from apps.permissions.info method

type AppsPermissionsInfoResponse record {
    true ok;
    AppsPermissionsInfoResponseInfo info;
};


type AppsPermissionsInfoResponseInfo record {
    AppsPermissionsInfoResponseInfoIm im;
    AppsPermissionsInfoResponseInfoMpim mpim;
    AppsPermissionsInfoResponseInfoChannel channel;
    AppsPermissionsInfoResponseInfoAppHome appHome;
    AppsPermissionsInfoResponseInfoTeam team;
    AppsPermissionsInfoResponseInfoGroup group;
};


type AppsPermissionsInfoResponseInfoAppHome record {
    ResourcesObj resources?;
    ScopesObj scopes?;
};


type AppsPermissionsInfoResponseInfoChannel record {
    ResourcesObj resources?;
    ScopesObj scopes?;
};


type AppsPermissionsInfoResponseInfoGroup record {
    ResourcesObj resources?;
    ScopesObj scopes?;
};


type AppsPermissionsInfoResponseInfoIm record {
    ResourcesObj resources?;
    ScopesObj scopes?;
};


type AppsPermissionsInfoResponseInfoMpim record {
    ResourcesObj resources?;
    ScopesObj scopes?;
};


type AppsPermissionsInfoResponseInfoTeam record {
    ResourcesObj resources;
    ScopesObj scopes;
};

# Represents the Queries record for the operation: apps_permissions_request

type AppsPermissionsRequestQueries record {
    # Token used to trigger the permissions API
    string triggerId;
    # A comma separated list of scopes to request for
    string scopes;
};

# Schema for successful response from apps.permissions.request method

type AppsPermissionsRequestResponse record {
    true ok;
};

# Represents the Queries record for the operation: apps_permissions_resources_list

type AppsPermissionsResourcesListQueries record {
    # Paginate through collections of data by setting the `cursor` parameter to a `next_cursor` attribute returned by a previous request's `response_metadata`. Default value fetches the first "page" of the collection. See [pagination](/docs/pagination) for more detail
    string cursor?;
    # The maximum number of items to return
    int 'limit?;
};

# Schema for successful response apps.permissions.resources.list method

type AppsPermissionsResourcesListResponse record {
    true ok;
    AppsPermissionsResourcesListResponseResources[] resources;
    AppsPermissionsResourcesListResponseResponseMetadata response_metadata?;
};


type AppsPermissionsResourcesListResponseResources record {
    string id?;
    string 'type?;
};


type AppsPermissionsResourcesListResponseResponseMetadata record {
    string nextCursor;
};

# Represents the Queries record for the operation: apps_permissions_users_list

type AppsPermissionsUsersListQueries record {
    # Paginate through collections of data by setting the `cursor` parameter to a `next_cursor` attribute returned by a previous request's `response_metadata`. Default value fetches the first "page" of the collection. See [pagination](/docs/pagination) for more detail
    string cursor?;
    # The maximum number of items to return
    int 'limit?;
};

# Represents the Queries record for the operation: apps_permissions_users_request

type AppsPermissionsUsersRequestQueries record {
    # Token used to trigger the request
    string triggerId;
    # A comma separated list of user scopes to request for
    string scopes;
    # The user this scope is being requested for
    string user;
};

# Represents the Queries record for the operation: apps_uninstall

type AppsUninstallQueries record {
    # Issued when you created your application
    string clientSecret?;
    # Issued when you created your application
    string clientId?;
};

# Schema for successful response from apps.uninstall method

type AppsUninstallResponse record {
    true ok;
};

# Represents the Queries record for the operation: auth_revoke

type AuthRevokeQueries record {
    # Setting this parameter to `1` triggers a _testing mode_ where the specified token will not actually be revoked
    boolean test?;
};

# Schema for successful response from auth.revoke method

type AuthRevokeResponse record {
    true ok;
    boolean revoked;
};

# Schema for successful response auth.test method

type AuthTestResponse record {
    UserIdDef userId;
    boolean isEnterpriseInstall?;
    string team;
    TeamDef teamId;
    true ok;
    string user;
    BotIdDef botId?;
    string url;
};


type BlocksInner record {
    string 'type;
};


type BotProfileObj record {
    boolean deleted;
    string name;
    BotIdDef id;
    TeamDef teamId;
    BotProfileObjIcons icons;
    AppIdDef appId;
    int updated;
};


type BotProfileObjIcons record {
    string image36;
    string image48;
    string image72;
};

# Represents the Queries record for the operation: bots_info

type BotsInfoQueries record {
    # Bot user to get info on
    string bot?;
};

# Schema for successful response from bots.info method

type BotsInfoResponse record {
    BotsInfoResponseBot bot;
    true ok;
};


type BotsInfoResponseBot record {
    boolean deleted;
    UserIdDef userId?;
    string name;
    BotIdDef id;
    BotsInfoResponseBotIcons icons;
    AppIdDef appId;
    int updated;
};


type BotsInfoResponseBotIcons record {
    string image36;
    string image48;
    string image72;
};


type CallsAddBody record {
    # Call start time in UTC UNIX timestamp format
    int dateStart?;
    # The URL required for a client to join the Call
    string joinUrl;
    # When supplied, available Slack clients will attempt to directly launch the 3rd-party Call with this URL
    string desktopAppJoinUrl?;
    # An optional, human-readable ID supplied by the 3rd-party Call provider. If supplied, this ID will be displayed in the Call object
    string externalDisplayId?;
    # An ID supplied by the 3rd-party Call provider. It must be unique across all Calls from that service
    string externalUniqueId;
    # The name of the Call
    string title?;
    # The valid Slack user ID of the user who created this Call. When this method is called with a user token, the `created_by` field is optional and defaults to the authed user of the token. Otherwise, the field is required
    string createdBy?;
    # The list of users to register as participants in the Call. [Read more on how to specify users here](/apis/calls#users)
    string users?;
};


type CallsEndBody record {
    # Call duration in seconds
    int duration?;
    # `id` returned when registering the call using the [`calls.add`](/methods/calls.add) method
    string id;
};

# Represents the Queries record for the operation: calls_info

type CallsInfoQueries record {
    # `id` of the Call returned by the [`calls.add`](/methods/calls.add) method
    string id;
};


type CallsParticipantsAddBody record {
    # `id` returned by the [`calls.add`](/methods/calls.add) method
    string id;
    # The list of users to add as participants in the Call. [Read more on how to specify users here](/apis/calls#users)
    string users;
};


type CallsParticipantsRemoveBody record {
    # `id` returned by the [`calls.add`](/methods/calls.add) method
    string id;
    # The list of users to remove as participants in the Call. [Read more on how to specify users here](/apis/calls#users)
    string users;
};


type CallsUpdateBody record {
    # The URL required for a client to join the Call
    string joinUrl?;
    # When supplied, available Slack clients will attempt to directly launch the 3rd-party Call with this URL
    string desktopAppJoinUrl?;
    # `id` returned by the [`calls.add`](/methods/calls.add) method
    string id;
    # The name of the Call
    string title?;
};


type ChannelAnyOf2 record {
    TsDef lastRead?;
    decimal unreadCount?;
    boolean isIm?;
    boolean isOpen?;
    string created?;
    DmIdDef id;
    decimal unreadCountDisplay?;
    UserIdDef user?;
    MessageObj latest?;
};


type ChannelObj record {
    boolean isPrivate;
    UserIdDef acceptedUser?;
    ChannelObjPurpose purpose;
    int isMoved?;
    boolean isPendingExtShared?;
    int unreadCount?;
    TeamDef[] pendingShared?;
    boolean isChannel;
    boolean isShared;
    UserIdDef[] members;
    boolean isNonThreadable?;
    boolean isReadOnly?;
    ChannelIdDef id;
    ChannelNameDef[] previousNames?;
    ChannelObjLatest[] latest?;
    TsDef lastRead?;
    UserIdDef creator;
    boolean isFrozen?;
    boolean isMember?;
    boolean isMpim;
    int created;
    string nameNormalized;
    decimal priority?;
    int unreadCountDisplay?;
    int unlinked?;
    boolean isArchived?;
    boolean isGeneral?;
    int numMembers?;
    string name;
    ChannelObjTopic topic;
    boolean isThreadOnly?;
    boolean isOrgShared;
};


type ChannelObjPurpose record {
    int lastSet;
    TopicPurposeCreatorDef creator;
    string value;
};


type ChannelObjTopic record {
    int lastSet;
    TopicPurposeCreatorDef creator;
    string value;
};


type ChatDeleteBody record {
    # Pass true to delete the message as the authed user with `chat:write:user` scope. [Bot users](/bot-users) in this context are considered authed users. If unused or false, the message will be deleted with `chat:write:bot` scope
    boolean asUser?;
    # Channel containing the message to be deleted
    string channel?;
    # Timestamp of the message to be deleted
    decimal ts?;
};

# Schema for successful response of chat.delete method

type ChatDeleteResponse record {
    ChannelDef channel;
    true ok;
    TsDef ts;
};


type ChatDeleteScheduledMessageBody record {
    # Pass true to delete the message as the authed user with `chat:write:user` scope. [Bot users](/bot-users) in this context are considered authed users. If unused or false, the message will be deleted with `chat:write:bot` scope
    boolean asUser?;
    # `scheduled_message_id` returned from call to chat.scheduleMessage
    string scheduledMessageId;
    # The channel the scheduled_message is posting to
    string channel;
};

# Schema for successful response from chat.deleteScheduledMessage method

type ChatDeleteScheduledMessageResponse record {
    true ok;
};

# Represents the Queries record for the operation: chat_getPermalink

type ChatGetPermalinkQueries record {
    # The ID of the conversation or channel containing the message
    string channel;
    # A message's `ts` value, uniquely identifying it within a channel
    string messageTs;
};

# Schema for successful response chat.getPermalink

type ChatGetPermalinkResponse record {
    ChannelDef channel;
    true ok;
    string permalink;
};


type ChatMeMessageBody record {
    # Channel to send message to. Can be a public channel, private group or IM channel. Can be an encoded ID, or a name
    string channel?;
    # Text of the message to send
    string text?;
};

# Schema for successful response from chat.meMessage method

type ChatMeMessageResponse record {
    ChannelDef channel?;
    true ok;
    TsDef ts?;
};


type ChatPostEphemeralBody record {
    # URL to an image to use as the icon for this message. Must be used in conjunction with `as_user` set to false, otherwise ignored. See [authorship](#authorship) below
    string iconUrl?;
    # Find and link channel names and usernames
    boolean linkNames?;
    # Pass true to post the message as the authed user. Defaults to true if the chat:write:bot scope is not included. Otherwise, defaults to false
    boolean asUser?;
    # A JSON-based array of structured attachments, presented as a URL-encoded string
    string attachments?;
    # Emoji to use as the icon for this message. Overrides `icon_url`. Must be used in conjunction with `as_user` set to `false`, otherwise ignored. See [authorship](#authorship) below
    string iconEmoji?;
    # A JSON-based array of structured blocks, presented as a URL-encoded string
    string blocks?;
    # Provide another message's `ts` value to post this message in a thread. Avoid using a reply's `ts` value; use its parent's value instead. Ephemeral messages in threads are only shown if there is already an active thread
    string threadTs?;
    # Channel, private group, or IM channel to send message to. Can be an encoded ID, or a name
    string channel;
    # Change how messages are treated. Defaults to `none`. See [below](#formatting)
    string parse?;
    # How this field works and whether it is required depends on other fields you use in your API call. [See below](#text_usage) for more detail
    string text?;
    # `id` of the user who will receive the ephemeral message. The user should be in the channel specified by the `channel` argument
    string user;
    # Set your bot's user name. Must be used in conjunction with `as_user` set to false, otherwise ignored. See [authorship](#authorship) below
    string username?;
};

# Schema for successful response from chat.postEphemeral method

type ChatPostEphemeralResponse record {
    true ok;
    TsDef messageTs;
};


type ChatPostMessageBody record {
    # URL to an image to use as the icon for this message. Must be used in conjunction with `as_user` set to false, otherwise ignored. See [authorship](#authorship) below
    string iconUrl?;
    # Find and link channel names and usernames
    boolean linkNames?;
    # A JSON-based array of structured attachments, presented as a URL-encoded string
    string attachments?;
    # Emoji to use as the icon for this message. Overrides `icon_url`. Must be used in conjunction with `as_user` set to `false`, otherwise ignored. See [authorship](#authorship) below
    string iconEmoji?;
    # A JSON-based array of structured blocks, presented as a URL-encoded string
    string blocks?;
    # Channel, private group, or IM channel to send message to. Can be an encoded ID, or a name. See [below](#channels) for more details
    string channel;
    # Change how messages are treated. Defaults to `none`. See [below](#formatting)
    string parse?;
    # Disable Slack markup parsing by setting to `false`. Enabled by default
    boolean mrkdwn?;
    # Pass true to post the message as the authed user, instead of as a bot. Defaults to false. See [authorship](#authorship) below
    string asUser?;
    # Provide another message's `ts` value to make this message a reply. Avoid using a reply's `ts` value; use its parent instead
    string threadTs?;
    # Pass false to disable unfurling of media content
    boolean unfurlMedia?;
    # Used in conjunction with `thread_ts` and indicates whether reply should be made visible to everyone in the channel or conversation. Defaults to `false`
    boolean replyBroadcast?;
    # Pass true to enable unfurling of primarily text-based content
    boolean unfurlLinks?;
    # How this field works and whether it is required depends on other fields you use in your API call. [See below](#text_usage) for more detail
    string text?;
    # Set your bot's user name. Must be used in conjunction with `as_user` set to false, otherwise ignored. See [authorship](#authorship) below
    string username?;
};

# Schema for successful response of chat.postMessage method

type ChatPostMessageResponse record {
    ChannelDef channel;
    MessageObj message;
    true ok;
    TsDef ts;
};

# Represents the Queries record for the operation: chat_scheduledMessages_list

type ChatScheduledMessagesListQueries record {
    # For pagination purposes, this is the `cursor` value returned from a previous call to `chat.scheduledmessages.list` indicating where you want to start this call from
    string cursor?;
    # A UNIX timestamp of the oldest value in the time range
    decimal oldest?;
    # The channel of the scheduled messages
    string channel?;
    # Maximum number of original entries to return
    int 'limit?;
    # A UNIX timestamp of the latest value in the time range
    decimal latest?;
};

# Schema for successful response from chat.scheduledMessages.list method

type ChatScheduledMessagesListResponse record {
    ChatScheduledMessagesListResponseScheduledMessages[] scheduledMessages;
    ChatScheduledMessagesListResponseResponseMetadata responseMetadata;
    true ok;
};


type ChatScheduledMessagesListResponseResponseMetadata record {
    string nextCursor;
};


type ChatScheduledMessagesListResponseScheduledMessages record {
    int dateCreated;
    string id;
    string text?;
    int postAt;
    ChannelIdDef channelId;
};


type ChatScheduleMessageBody record {
    # Find and link channel names and usernames
    boolean linkNames?;
    # Pass true to post the message as the authed user, instead of as a bot. Defaults to false. See [chat.postMessage](chat.postMessage#authorship)
    boolean asUser?;
    # A JSON-based array of structured attachments, presented as a URL-encoded string
    string attachments?;
    # A JSON-based array of structured blocks, presented as a URL-encoded string
    string blocks?;
    # Provide another message's `ts` value to make this message a reply. Avoid using a reply's `ts` value; use its parent instead
    decimal threadTs?;
    # Pass false to disable unfurling of media content
    boolean unfurlMedia?;
    # Channel, private group, or DM channel to send message to. Can be an encoded ID, or a name. See [below](#channels) for more details
    string channel?;
    # Used in conjunction with `thread_ts` and indicates whether reply should be made visible to everyone in the channel or conversation. Defaults to `false`
    boolean replyBroadcast?;
    # Pass true to enable unfurling of primarily text-based content
    boolean unfurlLinks?;
    # Change how messages are treated. Defaults to `none`. See [chat.postMessage](chat.postMessage#formatting)
    string parse?;
    # How this field works and whether it is required depends on other fields you use in your API call. [See below](#text_usage) for more detail
    string text?;
    # Unix EPOCH timestamp of time in future to send the message
    string postAt?;
};

# Schema for successful response of chat.scheduleMessage method

type ChatScheduleMessageResponse record {
    string scheduledMessageId;
    ChannelDef channel;
    ChatScheduleMessageResponseMessage message;
    true ok;
    int postAt;
};


type ChatScheduleMessageResponseMessage record {
    BotProfileObj botProfile?;
    TeamDef team;
    string text;
    string 'type;
    UserIdDef user;
    BotIdDef botId;
    string username?;
};


type ChatUnfurlBody record {
    # URL-encoded JSON map with keys set to URLs featured in the the message, pointing to their unfurl blocks or message attachments
    string unfurls?;
    # Provide a simply-formatted string to send as an ephemeral message to the user as invitation to authenticate further and enable full unfurling behavior
    string userAuthMessage?;
    # Channel ID of the message
    string channel;
    # Send users to this custom URL where they will complete authentication in your app to fully trigger unfurling. Value should be properly URL-encoded
    string userAuthUrl?;
    # Set to `true` or `1` to indicate the user must install your Slack app to trigger unfurls for this domain
    boolean userAuthRequired?;
    # Timestamp of the message to add unfurl behavior to
    string ts;
};

# Schema for successful response from chat.unfurl method

type ChatUnfurlResponse record {
    true ok;
};


type ChatUpdateBody record {
    # Find and link channel names and usernames. Defaults to `none`. If you do not specify a value for this field, the original value set for the message will be overwritten with the default, `none`
    string linkNames?;
    # Pass true to update the message as the authed user. [Bot users](/bot-users) in this context are considered authed users
    string asUser?;
    # A JSON-based array of structured attachments, presented as a URL-encoded string. This field is required when not presenting `text`. If you don't include this field, the message's previous `attachments` will be retained. To remove previous `attachments`, include an empty array for this field
    string attachments?;
    # A JSON-based array of [structured blocks](/block-kit/building), presented as a URL-encoded string. If you don't include this field, the message's previous `blocks` will be retained. To remove previous `blocks`, include an empty array for this field
    string blocks?;
    # Channel containing the message to be updated
    string channel;
    # Change how messages are treated. Defaults to `client`, unlike `chat.postMessage`. Accepts either `none` or `full`. If you do not specify a value for this field, the original value set for the message will be overwritten with the default, `client`
    string parse?;
    # New text for the message, using the [default formatting rules](/reference/surfaces/formatting). It's not required when presenting `blocks` or `attachments`
    string text?;
    # Timestamp of the message to be updated
    string ts;
};

# Schema for successful response of chat.update method

type ChatUpdateResponse record {
    string channel;
    string text;
    MessageObject message;
    true ok;
    string ts;
};


type CommentObj record {
    boolean isStarred?;
    int created;
    int numStars?;
    boolean isIntro;
    PinnedInfoDef pinnedInfo?;
    string comment;
    ReactionObj[] reactions?;
    CommentIdDef id;
    UserIdDef user;
    ChannelDef[] pinnedTo?;
    int timestamp;
};

# Provides a set of configurations for controlling the behaviours when communicating with a remote HTTP endpoint.

type ConnectionConfig record {
    # Configurations related to client authentication
    http:BearerTokenConfig|OAuth2RefreshTokenGrantConfig auth; // Special Agent Note: BearerTokenConfig FROM ballerina/http package
    # The HTTP version understood by the client
    http:HttpVersion httpVersion = http:HTTP_2_0; // Special Agent Note: HttpVersion FROM ballerina/http package
    # Configurations related to HTTP/1.x protocol
    http:ClientHttp1Settings http1Settings = {}; // Special Agent Note: ClientHttp1Settings FROM ballerina/http package
    # Configurations related to HTTP/2 protocol
    http:ClientHttp2Settings http2Settings = {}; // Special Agent Note: ClientHttp2Settings FROM ballerina/http package
    # The maximum time to wait (in seconds) for a response before closing the connection
    decimal timeout = 30;
    # The choice of setting `forwarded`/`x-forwarded` header
    string forwarded = "disable";
    # Configurations associated with Redirection
    http:FollowRedirects followRedirects?; // Special Agent Note: FollowRedirects FROM ballerina/http package
    # Configurations associated with request pooling
    http:PoolConfiguration poolConfig?; // Special Agent Note: PoolConfiguration FROM ballerina/http package
    # HTTP caching related configurations
    http:CacheConfig cache = {}; // Special Agent Note: CacheConfig FROM ballerina/http package
    # Specifies the way of handling compression (`accept-encoding`) header
    http:Compression compression = http:COMPRESSION_AUTO; // Special Agent Note: Compression FROM ballerina/http package
    # Configurations associated with the behaviour of the Circuit Breaker
    http:CircuitBreakerConfig circuitBreaker?; // Special Agent Note: CircuitBreakerConfig FROM ballerina/http package
    # Configurations associated with retrying
    http:RetryConfig retryConfig?; // Special Agent Note: RetryConfig FROM ballerina/http package
    # Configurations associated with cookies
    http:CookieConfig cookieConfig?; // Special Agent Note: CookieConfig FROM ballerina/http package
    # Configurations associated with inbound response size limits
    http:ResponseLimitConfigs responseLimits = {}; // Special Agent Note: ResponseLimitConfigs FROM ballerina/http package
    # SSL/TLS-related options
    http:ClientSecureSocket secureSocket?; // Special Agent Note: ClientSecureSocket FROM ballerina/http package
    # Proxy server related options
    http:ProxyConfig proxy?; // Special Agent Note: ProxyConfig FROM ballerina/http package
    # Provides settings related to client socket configuration
    http:ClientSocketConfig socketConfig = {}; // Special Agent Note: ClientSocketConfig FROM ballerina/http package
    # Enables the inbound payload validation functionality which provided by the constraint package. Enabled by default
    boolean validation = true;
    # Enables relaxed data binding on the client side. When enabled, `nil` values are treated as optional, 
and absent fields are handled as `nilable` types. Enabled by default.
    boolean laxDataBinding = true;
};


type ConversationIMChannelObjectFromConversationsMethods record {
    boolean hasPins?;
    TsDef lastRead?;
    boolean isUserDeleted?;
    boolean isFrozen?;
    boolean isIm;
    boolean isOpen?;
    int created;
    boolean isExtShared?;
    decimal priority;
    int unreadCountDisplay?;
    int version?;
    ConversationObjShares2[] shares?;
    int unreadCount?;
    boolean isStarred?;
    boolean isArchived?;
    boolean isShared?;
    int pinCount?;
    DmIdDef id;
    boolean isOrgShared;
    UserIdDef user;
    ConversationObjLatest2[] latest?;
    ConversationObjParentConversation2[] parentConversation?;
};


type ConversationMPIMObject record {
    boolean isPendingExtShared?;
    TeamDef[] pendingShared?;
    TeamDef[] internalTeamIds?;
    boolean isChannel;
    UserIdDef[] members?;
    boolean isNonThreadable?;
    int pinCount?;
    boolean isReadOnly?;
    ChannelDef id;
    boolean isIm;
    boolean isMember?;
    boolean isOpen?;
    int created;
    ConversationObjDisplayCounts1 displayCounts?;
    decimal priority?;
    int version?;
    boolean isStarred?;
    boolean isArchived;
    string name;
    ConversationObjTopic1 topic;
    TeamDef[] sharedTeamIds?;
    boolean isOrgShared;
    boolean isPrivate;
    UserIdDef acceptedUser?;
    WorkspaceIdDef conversationHostId?;
    ConversationObjPurpose1 purpose;
    int isMoved?;
    ConversationObjShares1[] shares?;
    int unreadCount?;
    boolean isShared;
    ChannelNameDef[] previousNames?;
    TeamDef[] connectedTeamIds?;
    TeamDef[] pendingConnectedTeamIds?;
    ConversationObjLatest1[] latest?;
    TsDef lastRead?;
    UserIdDef creator;
    boolean isFrozen?;
        true  isMpim;
    int timezoneCount?;
    boolean isExtShared?;
    string nameNormalized;
    int unreadCountDisplay?;
    boolean isGroup;
    int unlinked?;
    boolean isGeneral;
    int numMembers?;
    boolean isThreadOnly?;
    UserIdDef user?;
    ConversationObjParentConversation1[] parentConversation?;
};


type ConversationObjDisplayCounts record {
    int displayCounts;
    int guestCounts;
};


type ConversationObjDisplayCounts1 record {
    int displayCounts;
    int guestCounts;
};


type ConversationObject record {
    boolean isGlobalShared?;
    boolean isPendingExtShared?;
    TeamDef[] pendingShared?;
    TeamDef[] internalTeamIds?;
    boolean isChannel;
    UserIdDef[] members?;
    boolean isNonThreadable?;
    int pinCount?;
    boolean isReadOnly?;
    ChannelDef id;
    boolean isOrgDefault?;
    boolean isOrgMandatory?;
    boolean isIm;
    boolean isMember?;
    boolean isOpen?;
    int created;
    ConversationObjDisplayCounts displayCounts?;
    decimal priority?;
    int version?;
    boolean isStarred?;
    boolean isArchived;
    string name;
    ConversationObjTopic topic;
    TeamDef[] sharedTeamIds?;
    boolean isOrgShared;
    boolean isPrivate;
    UserIdDef acceptedUser?;
    WorkspaceIdDef conversationHostId?;
    ConversationObjPurpose purpose;
    int isMoved?;
    ConversationObjShares[] shares?;
    int unreadCount?;
    boolean isShared;
    ChannelNameDef[] previousNames?;
    WorkspaceIdDef[] connectedTeamIds?;
    TeamDef[] pendingConnectedTeamIds?;
    ConversationObjLatest[] latest?;
    boolean hasPins?;
    TsDef lastRead?;
    UserIdDef creator;
    boolean isFrozen?;
        false  isMpim;
    int timezoneCount?;
    boolean isExtShared?;
    string nameNormalized;
    EnterpriseIdDef enterpriseId?;
    int unreadCountDisplay?;
    boolean isGroup;
    int unlinked?;
    string useCase?;
    boolean isGeneral;
    int numMembers?;
    boolean isThreadOnly?;
    UserIdDef user?;
    ConversationObjParentConversation[] parentConversation?;
};


type ConversationObjPurpose record {
    int lastSet;
    TopicPurposeCreatorDef creator;
    string value;
};


type ConversationObjPurpose1 record {
    int lastSet;
    TopicPurposeCreatorDef creator;
    string value;
};


type ConversationObjShares record {
    boolean isActive;
    UserIdDef acceptedUser?;
    TeamObj team;
    UserIdDef user;
};


type ConversationObjShares1 record {
    boolean isActive;
    UserIdDef acceptedUser?;
    TeamObj team;
    UserIdDef user;
};


type ConversationObjShares2 record {
    boolean isActive;
    int dateCreate;
    string name;
    TeamDef id;
    TeamObj team;
};


type ConversationObjTopic record {
    int lastSet;
    TopicPurposeCreatorDef creator;
    string value;
};


type ConversationObjTopic1 record {
    int lastSet;
    TopicPurposeCreatorDef creator;
    string value;
};


type ConversationsArchiveBody record {
    # ID of conversation to archive
    string channel?;
};

# Schema for successful response conversations.archive method

type ConversationsArchiveResponse record {
    true ok;
};


type ConversationsCloseBody record {
    # Conversation to close
    string channel?;
};

# Schema for successful response conversations.close method

type ConversationsCloseResponse record {
    boolean alreadyClosed?;
    true ok;
    boolean noOp?;
};


type ConversationsCreateBody record {
    # Create a private channel instead of a public one
    boolean isPrivate?;
    # Name of the public or private channel to create
    string name?;
};

# Schema for successful response conversations.create method

type ConversationsCreateResponse record {
    ConversationObj channel;
    true ok;
};

# Represents the Queries record for the operation: conversations_history

type ConversationsHistoryQueries record {
    # Paginate through collections of data by setting the `cursor` parameter to a `next_cursor` attribute returned by a previous request's `response_metadata`. Default value fetches the first "page" of the collection. See [pagination](/docs/pagination) for more detail
    string cursor?;
    # Include messages with latest or oldest timestamp in results only when either timestamp is specified
    boolean inclusive?;
    # Start of time range of messages to include in results
    decimal oldest?;
    # Conversation ID to fetch history for
    string channel?;
    # The maximum number of items to return. Fewer than the requested number of items may be returned, even if the end of the users list hasn't been reached
    int 'limit?;
    # End of time range of messages to include in results
    decimal latest?;
};

# Schema for successful response from conversations.history method

type ConversationsHistoryResponse record {
    int channelActionsCount;
    int pinCount;
    MessageObj[] messages;
    ConversationsHistoryResponseChannelActionsTs[] channelActionsTs;
    boolean hasMore;
    true ok;
};

# Represents the Queries record for the operation: conversations_info

type ConversationsInfoQueries record {
    # Set to `true` to include the member count for the specified conversation. Defaults to `false`
    boolean includeNumMembers?;
    # Conversation ID to learn more about
    string channel?;
    # Set this to `true` to receive the locale for this conversation. Defaults to `false`
    boolean includeLocale?;
};

# Schema for successful response conversations.info

type ConversationsInfoResponse record {
    ConversationObj channel;
    true ok;
};


type ConversationsInviteBody record {
    # The ID of the public or private channel to invite user(s) to
    string channel?;
    # A comma separated list of user IDs. Up to 1000 users may be listed
    string users?;
};

# Schema for successful response from conversations.invite method

type ConversationsInviteErrorResponse record {
    ConversationObj channel;
    true ok;
};


type ConversationsJoinBody record {
    # ID of conversation to join
    string channel?;
};

# Schema for successful response from conversations.join method

type ConversationsJoinResponse record {
    ConversationObj channel;
    string warning?;
    ResponseMetadata responseMetadata?;
    true ok;
};


type ConversationsKickBody record {
    # ID of conversation to remove user from
    string channel?;
    # User ID to be removed
    string user?;
};

# Schema for successful response conversations.kick method

type ConversationsKickResponse record {
    true ok;
};


type ConversationsLeaveBody record {
    # Conversation to leave
    string channel?;
};

# Schema for successful response from conversations.leave method

type ConversationsLeaveResponse record {
    true ok;
        true  notInChannel?;
};

# Represents the Queries record for the operation: conversations_list

type ConversationsListQueries record {
    # Paginate through collections of data by setting the `cursor` parameter to a `next_cursor` attribute returned by a previous request's `response_metadata`. Default value fetches the first "page" of the collection. See [pagination](/docs/pagination) for more detail
    string cursor?;
    # Mix and match channel types by providing a comma-separated list of any combination of `public_channel`, `private_channel`, `mpim`, `im`
    string types?;
    # The maximum number of items to return. Fewer than the requested number of items may be returned, even if the end of the list hasn't been reached. Must be an integer no larger than 1000
    int 'limit?;
    # Set to `true` to exclude archived channels from the list
    boolean excludeArchived?;
};

# Schema for successful response from conversations.list method

type ConversationsListResponse record {
    ConversationObj[] channels;
    ConversationsListResponseResponseMetadata responseMetadata?;
    true ok;
};


type ConversationsListResponseResponseMetadata record {
    string nextCursor;
};


type ConversationsMarkBody record {
    # Channel or conversation to set the read cursor for
    string channel?;
    # Unique identifier of message you want marked as most recently seen in this conversation
    decimal ts?;
};

# Schema for successful response conversations.mark method

type ConversationsMarkResponse record {
    true ok;
};

# Represents the Queries record for the operation: conversations_members

type ConversationsMembersQueries record {
    # Paginate through collections of data by setting the `cursor` parameter to a `next_cursor` attribute returned by a previous request's `response_metadata`. Default value fetches the first "page" of the collection. See [pagination](/docs/pagination) for more detail
    string cursor?;
    # ID of the conversation to retrieve members for
    string channel?;
    # The maximum number of items to return. Fewer than the requested number of items may be returned, even if the end of the users list hasn't been reached
    int 'limit?;
};

# Schema for successful response conversations.members method

type ConversationsMembersResponse record {
    UserIdDef[] members;
    ConversationsMembersResponseResponseMetadata responseMetadata;
    true ok;
};


type ConversationsMembersResponseResponseMetadata record {
    string nextCursor;
};


type ConversationsOpenBody record {
    # Boolean, indicates you want the full IM channel definition in the response
    boolean returnIm?;
    # Resume a conversation by supplying an `im` or `mpim`'s ID. Or provide the `users` field instead
    string channel?;
    # Comma separated lists of users. If only one user is included, this creates a 1:1 DM.  The ordering of the users is preserved whenever a multi-person direct message is returned. Supply a `channel` when not supplying `users`
    string users?;
};

# Schema for successful response from conversations.open method when opening channels, ims, mpims

type ConversationsOpenResponse record {
    boolean alreadyOpen?;
    ConversationsOpenResponseChannel[] channel;
    true ok;
    boolean noOp?;
};


type ConversationsRenameBody record {
    # ID of conversation to rename
    string channel?;
    # New name for conversation
    string name?;
};

# Schema for successful response from conversations.rename method

type ConversationsRenameResponse record {
    ConversationObj channel;
    true ok;
};

# Represents the Queries record for the operation: conversations_replies

type ConversationsRepliesQueries record {
    # Paginate through collections of data by setting the `cursor` parameter to a `next_cursor` attribute returned by a previous request's `response_metadata`. Default value fetches the first "page" of the collection. See [pagination](/docs/pagination) for more detail
    string cursor?;
    # Include messages with latest or oldest timestamp in results only when either timestamp is specified
    boolean inclusive?;
    # Start of time range of messages to include in results
    decimal oldest?;
    # Conversation ID to fetch thread from
    string channel?;
    # The maximum number of items to return. Fewer than the requested number of items may be returned, even if the end of the users list hasn't been reached
    int 'limit?;
    # Unique identifier of a thread's parent message. `ts` must be the timestamp of an existing message with 0 or more replies. If there are no replies then just the single message referenced by `ts` will return - it is just an ordinary, unthreaded message
    decimal ts?;
    # End of time range of messages to include in results
    decimal latest?;
};

# Schema for successful response from conversations.replies method

type ConversationsRepliesResponse record {
    (record {TsDef last_read; TsDef latest_reply; int reply_count; UserIdDef[] reply_users; int reply_users_count; TeamDef source_team; boolean subscribed; TeamDef team; string text; TsDef thread_ts; TsDef ts; string 'type; int unread_count; UserIdDef user; UserProfileShortObj user_profile; TeamDef user_team; }|record {boolean is_starred; UserIdDef parent_user_id; TeamDef source_team; TeamDef team; string text; TsDef thread_ts; TsDef ts; string 'type; UserIdDef user; UserProfileShortObj user_profile; TeamDef user_team; })[] messages;
    boolean hasMore?;
    true ok;
};


type ConversationsSetPurposeBody record {
    # A new, specialer purpose
    string purpose?;
    # Conversation to set the purpose of
    string channel?;
};

# Schema for successful response from conversations.setPurpose method

type ConversationsSetPurposeResponse record {
    ConversationObj channel;
    true ok;
};


type ConversationsSetTopicBody record {
    # Conversation to set the topic of
    string channel?;
    # The new topic string. Does not support formatting or linkification
    string topic?;
};

# Schema for successful response from conversations.setTopic method

type ConversationsSetTopicResponse record {
    ConversationObj channel;
    true ok;
};


type ConversationsUnarchiveBody record {
    # ID of conversation to unarchive
    string channel?;
};

# Schema for successful response from conversations.unarchive method

type ConversationsUnarchiveResponse record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse1 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse10 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse11 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse12 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse13 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse14 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse15 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse16 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse17 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse18 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse19 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse2 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse20 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse21 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse22 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse23 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse24 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse25 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse26 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse27 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse28 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse29 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse3 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse30 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse31 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse32 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse33 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse34 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse35 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse36 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse37 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse38 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse39 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse4 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse40 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse41 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse42 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse43 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse44 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse45 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse46 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse47 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse48 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse49 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse5 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse50 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse51 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse52 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse53 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse54 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse55 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse56 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse57 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse58 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse59 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse6 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse60 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse61 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse62 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse63 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse64 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse65 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse66 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse67 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse68 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse69 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse7 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse70 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse71 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse72 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse8 record {
    true ok;
};

# This method either only returns a brief _OK_ response or a verbose schema is not available for this method

type DefaultSuccessResponse9 record {
    true ok;
};


type DeprecationWarning record {
    ("method_deprecated")[] warnings;
    string[] messages;
};


type DeprecationWarningAndPagingStyleTogether record {
    string nextCursor;
    ("method_deprecated")[] warnings;
    string[] messages;
};

# Represents the Queries record for the operation: dialog_open

type DialogOpenQueries record {
    # The dialog definition. This must be a JSON-encoded string
    string dialog;
    # Exchange a trigger to post to the user
    string triggerId;
};

# Schema for successful response from dialog.open method

type DialogOpenResponse record {
    true ok;
};

# Schema for successful response from dnd.endDnd method

type DndEndDndResponse record {
    true ok;
};

# Schema for successful response from dnd.endSnooze method

type DndEndSnoozeResponse record {
    int nextDndEndTs;
    boolean dndEnabled;
    int nextDndStartTs;
    boolean snoozeEnabled;
    true ok;
};

# Represents the Queries record for the operation: dnd_info

type DndInfoQueries record {
    # User to fetch status for (defaults to current user)
    string user?;
};

# Schema for successful response from dnd.info method

type DndInfoResponse record {
    int nextDndEndTs;
    int snoozeEndtime?;
    boolean dndEnabled;
    int nextDndStartTs;
    boolean snoozeEnabled?;
    int snoozeRemaining?;
    true ok;
};


type DndSetSnoozeBody record {
    # Number of minutes, from now, to snooze until
    string numMinutes;
    # Authentication token. Requires scope: `dnd:write`
    string token;
};

# Schema for successful response from dnd.setSnooze method

type DndSetSnoozeResponse record {
    int snoozeEndtime;
    boolean snoozeEnabled;
    int snoozeRemaining;
    true ok;
};

# Represents the Queries record for the operation: dnd_teamInfo

type DndTeamInfoQueries record {
    # Comma-separated list of users to fetch Do Not Disturb status for
    string users?;
};


type EnterpriseUserObj record {
    boolean isAdmin;
    TeamDef[] teams;
    boolean isOwner;
    EnterpriseUserIdDef id;
    EnterpriseIdDef enterpriseId;
    EnterpriseNameDef enterpriseName;
};


type ExternalOrgMigrationsObj record {
    ExternalOrgMigrationsObjCurrent[] current;
    int dateUpdated;
};


type ExternalOrgMigrationsObjCurrent record {
    string teamId;
    int dateStarted;
};


type FileObj record {
    string filetype?;
    string thumb360?;
    string thumb160?;
    int dateDelete?;
    string thumb480?;
    PinnedInfoDef pinnedInfo?;
    string thumb800?;
    string thumb720?;
    boolean nonOwnerEditable?;
    string thumb960?;
    int thumb800W?;
    string mode?;
    string externalUrl?;
    boolean isTombstoned?;
    int numStars?;
    int imageExifRotation?;
    FileIdDef id?;
    string state?;
    string thumb64?;
    int created?;
    UserIdDef lastEditor?;
    int thumb480W?;
    int thumb960H?;
    string urlPrivateDownload?;
    string permalinkPublic?;
    boolean hasRichPreview?;
    boolean isStarred?;
    ChannelIdDef[] channels?;
    int size?;
    int commentsCount?;
    string name?;
    string permalink?;
    boolean publicUrlShared?;
    int updated?;
    int originalW?;
    int thumb480H?;
    int thumb720W?;
    string preview?;
    string externalId?;
    int thumb1024H?;
    string title?;
    int originalH?;
    DmIdDef[] ims?;
    int thumb720H?;
    FileObjShares shares?;
    string urlPrivate?;
    int thumb960W?;
    boolean displayAsBot?;
    int timestamp?;
    UserIdDef editor?;
    string thumb80?;
    boolean editable?;
    GroupIdDef[] groups?;
    boolean isExternal?;
    int thumb360H?;
    string prettyType?;
    string externalType?;
    TeamDef userTeam?;
    ChannelDef[] pinnedTo?;
    int thumb800H?;
    TeamDef sourceTeam?;
    boolean isPublic?;
    int thumb360W?;
    string thumbTiny?;
    string mimetype?;
    ReactionObj[] reactions?;
    int thumb1024W?;
    string thumb1024?;
    string user?;
    string username?;
};


type FileObjShares record {
    record {} 'private?;
    record {} 'public?;
};


type FilePin record {
    FileObj file?;
    int created?;
        "file"  'type?;
    UserIdDef createdBy?;
};


type FilesCommentsDeleteBody record {
    # File to delete a comment from
    string file?;
    # The comment to delete
    string id?;
};

# Schema for successful response files.comments.delete method

type FilesCommentsDeleteResponse record {
    true ok;
};


type FilesDeleteBody record {
    # ID of file to delete
    string file?;
};

# Schema for successful response files.delete method

type FilesDeleteResponse record {
    true ok;
};

# Represents the Queries record for the operation: files_info

type FilesInfoQueries record {
    # Parameter for pagination. File comments are paginated for a single file. Set `cursor` equal to the `next_cursor` attribute returned by the previous request's `response_metadata`. This parameter is optional, but pagination is mandatory: the default value simply fetches the first "page" of the collection of comments. See [pagination](/docs/pagination) for more details
    string cursor?;
    # Specify a file by providing its ID
    string file?;
    string count?;
    # The maximum number of items to return. Fewer than the requested number of items may be returned, even if the end of the list hasn't been reached
    int 'limit?;
    string page?;
};

# Schema for successful response from files.info method

type FilesInfoResponse record {
    UserIdDef editor?;
    CommentsObj comments;
    FileObj file;
    PagingObj paging?;
    ResponseMetadataObj responseMetadata?;
    true ok;
    anydata? contentHtml?;
};

# Represents the Queries record for the operation: files_list

type FilesListQueries record {
    # Filter files created after this timestamp (inclusive)
    decimal tsFrom?;
    # Show truncated file info for files hidden due to being too old, and the team who owns the file being over the file limit
    boolean showFilesHiddenByLimit?;
    # Filter files by type ([see below](#file_types)). You can pass multiple values in the types argument, like `types=spaces,snippets`.The default value is `all`, which does not filter the list
    string types?;
    # Filter files created before this timestamp (inclusive)
    decimal tsTo?;
    # Filter files appearing in a specific channel, indicated by its ID
    string channel?;
    string count?;
    string page?;
    # Filter files created by a single user
    string user?;
};

# Schema for successful response from files.list method

type FilesListResponse record {
    FileObj[] files;
    PagingObj paging;
    true ok;
};


type FilesRemoteAddBody record {
    # type of file
    string filetype?;
    # URL of the remote file
    string externalUrl?;
    # Preview of the document via `multipart/form-data`
    string previewImage?;
    # Creator defined GUID for the file
    string externalId?;
    # A text file (txt, pdf, doc, etc.) containing textual search terms that are used to improve discovery of the remote file
    string indexableFileContents?;
    # Title of the file being shared
    string title?;
    # Authentication token. Requires scope: `remote_files:write`
    string token?;
};

# Represents the Queries record for the operation: files_remote_info

type FilesRemoteInfoQueries record {
    # Specify a file by providing its ID
    string file?;
    # Creator defined GUID for the file
    string externalId?;
};

# Represents the Queries record for the operation: files_remote_list

type FilesRemoteListQueries record {
    # Filter files created after this timestamp (inclusive)
    decimal tsFrom?;
    # Paginate through collections of data by setting the `cursor` parameter to a `next_cursor` attribute returned by a previous request's `response_metadata`. Default value fetches the first "page" of the collection. See [pagination](/docs/pagination) for more detail
    string cursor?;
    # Filter files created before this timestamp (inclusive)
    decimal tsTo?;
    # Filter files appearing in a specific channel, indicated by its ID
    string channel?;
    # The maximum number of items to return
    int 'limit?;
};


type FilesRemoteRemoveBody record {
    # Specify a file by providing its ID
    string file?;
    # Creator defined GUID for the file
    string externalId?;
    # Authentication token. Requires scope: `remote_files:write`
    string token?;
};

# Represents the Queries record for the operation: files_remote_share

type FilesRemoteShareQueries record {
    # Specify a file registered with Slack by providing its ID. Either this field or `external_id` or both are required
    string file?;
    # Comma-separated list of channel IDs where the file will be shared
    string channels?;
    # The globally unique identifier (GUID) for the file, as set by the app registering the file with Slack.  Either this field or `file` or both are required
    string externalId?;
};


type FilesRemoteUpdateBody record {
    # type of file
    string filetype?;
    # URL of the remote file
    string externalUrl?;
    # Specify a file by providing its ID
    string file?;
    # Preview of the document via `multipart/form-data`
    string previewImage?;
    # Creator defined GUID for the file
    string externalId?;
    # File containing contents that can be used to improve searchability for the remote file
    string indexableFileContents?;
    # Title of the file being shared
    string title?;
    # Authentication token. Requires scope: `remote_files:write`
    string token?;
};


type FilesRevokePublicURLBody record {
    # File to revoke
    string file?;
};

# Schema for successful response from files.revokePublicURL method

type FilesRevokePublicURLResponse record {
    FileObj file;
    true ok;
};


type FilesSharedPublicURLBody record {
    # File to share
    string file?;
};

# Schema for successful response from files.sharedPublicURL method

type FilesSharedPublicURLResponse record {
    FileObj file;
    true ok;
};


type FilesUploadBody record {
    # A [file type](/types/file#file_types) identifier
    string filetype?;
    # File contents via `multipart/form-data`. If omitting this parameter, you must submit `content`
    string file?;
    # Filename of file
    string filename?;
    # Comma-separated list of channel names or IDs where the file will be shared
    string channels?;
    # Provide another message's `ts` value to upload this file as a reply. Never use a reply's `ts` value; use its parent instead
    decimal threadTs?;
    # The message text introducing the file in specified `channels`
    string initialComment?;
    # Title of file
    string title?;
    # File contents via a POST variable. If omitting this parameter, you must provide a `file`
    string content?;
    # Authentication token. Requires scope: `files:write:user`
    string token?;
};

# Schema for successful response files.upload method

type FilesUploadResponse record {
    FileObj file;
    true ok;
};


type IconObj record {
    string image132?;
    string image102?;
    string image68?;
    boolean imageDefault?;
    string image34?;
    string image230?;
    string image44?;
    string image88?;
};


type MessageObj record {
    MessageObjAttachments[] attachments?;
    string clientMsgId?;
    string purpose?;
    boolean upload?;
    boolean isIntro?;
    UserProfileShortObj userProfile?;
    string 'type;
    boolean isDelayedMessage?;
    boolean subscribed?;
    int unreadCount?;
    FileObj file?;
    string subtype?;
    int replyUsersCount?;
    UserIdDef inviter?;
    string text;
    boolean displayAsBot?;
    MessageObjBotId[] botId?;
    TsDef latestReply?;
    TsDef lastRead?;
    UserIdDef parentUserId?;
    # This is a very loose definition, in the future, we'll populate this with deeper schema in this definition namespace
    Blocks blocks?;
    UserIdDef[] replyUsers?;
    WorkspaceIdDef team?;
    MessageObjIcons icons?;
    int replyCount?;
    WorkspaceIdDef userTeam?;
    ChannelDef[] pinnedTo?;
    boolean isStarred?;
    BotProfileObj botProfile?;
    string oldName?;
    WorkspaceIdDef sourceTeam?;
    TsDef threadTs?;
    string name?;
    FileObj[] files?;
    string topic?;
    CommentObj comment?;
    ReactionObj[] reactions?;
    string permalink?;
    UserIdDef user?;
    TsDef ts;
    string username?;
};


type MessageObjAttachments record {
    int imageHeight?;
    string imageUrl?;
    int id;
    int imageWidth?;
    string fallback?;
    int imageBytes?;
};


type MessageObject record {
    record {}[] attachments?;
    record {} blocks?;
    string text;
};


type MessageObjIcons record {
    string image64?;
    string emoji?;
};


type MessagePin record {
    int created?;
    ChannelDef channel?;
    MessageObj message?;
        "message"  'type?;
    UserIdDef createdBy?;
};

# Represents the Queries record for the operation: migration_exchange

type MigrationExchangeQueries record {
    # Specify `true` to convert `W` global user IDs to workspace-specific `U` IDs. Defaults to `false`
    boolean toOld?;
    # Specify team_id starts with `T` in case of Org Token
    string teamId?;
    # A comma-separated list of user ids, up to 400 per request
    string users;
};

# Schema for successful response from migration.exchange method

type MigrationExchangeResponse record {
    string enterprise_id;
    string[] invalid_user_ids?;
    true ok;
    TeamDef team_id;
    record {} user_id_map?;
};


type NewPagingStyle record {
    string nextCursor;
};

# OAuth2 Refresh Token Grant Configs

type OAuth2RefreshTokenGrantConfig record {
    # Refresh URL
    string refreshUrl = "https://slack.com/api/oauth.access";
    string refreshToken;
    string clientId;
    string clientSecret;
    string|string[] scopes;
    decimal defaultTokenExpTime;
    decimal clockSkew;
    map<string> optionalParams;
    oauth2:CredentialBearer credentialBearer; // Special Agent Note: CredentialBearer FROM ballerina/oauth2 package
    oauth2:ClientConfiguration clientConfig; // Special Agent Note: ClientConfiguration FROM ballerina/oauth2 package
};

# Represents the Queries record for the operation: oauth_access

type OauthAccessQueries record {
    # Request the user to add your app only to a single channel. Only valid with a [legacy workspace app](https://api.slack.com/legacy-workspace-apps)
    boolean singleChannel?;
    # The `code` param returned via the OAuth callback
    string code?;
    # Issued when you created your application
    string clientSecret?;
    # This must match the originally submitted URI (if one was sent)
    string redirectUri?;
    # Issued when you created your application
    string clientId?;
};

# Represents the Queries record for the operation: oauth_token

type OauthTokenQueries record {
    # Request the user to add your app only to a single channel
    boolean singleChannel?;
    # The `code` param returned via the OAuth callback
    string code?;
    # Issued when you created your application
    string clientSecret?;
    # This must match the originally submitted URI (if one was sent)
    string redirectUri?;
    # Issued when you created your application
    string clientId?;
};

# Represents the Queries record for the operation: oauth_v2_access

type OauthV2AccessQueries record {
    # The `code` param returned via the OAuth callback
    string code;
    # Issued when you created your application
    string clientSecret?;
    # This must match the originally submitted URI (if one was sent)
    string redirectUri?;
    # Issued when you created your application
    string clientId?;
};


type PagingObj record {
    int perPage?;
    int total;
    int pages?;
    int spill?;
    int count?;
    int page;
};


type PinnedInfoDef record {
};


type PinsAddBody record {
    # Channel to pin the item in
    string channel;
    # Timestamp of the message to pin
    string timestamp?;
};

# Schema for successful response from pins.add method

type PinsAddResponse record {
    true ok;
};

# Represents the Queries record for the operation: pins_list

type PinsListQueries record {
    # Channel to get pinned items for
    string channel;
};


type PinsRemoveBody record {
    # Channel where the item is pinned to
    string channel;
    # Timestamp of the message to un-pin
    string timestamp?;
};

# Schema for successful response from pins.remove method

type PinsRemoveResponse record {
    true ok;
};


type PrimaryOwnerObj record {
    string id;
    string email;
};


type ReactionObj record {
    int count;
    string name;
    UserIdDef[] users;
};


type ReactionsAddBody record {
    # Channel where the message to add reaction to was posted
    string channel;
    # Reaction (emoji) name
    string name;
    # Timestamp of the message to add reaction to
    string timestamp;
};

# Schema for successful response from reactions.add method

type ReactionsAddResponse record {
    true ok;
};

# Represents the Queries record for the operation: reactions_get

type ReactionsGetQueries record {
    # File to get reactions for
    string file?;
    # Channel where the message to get reactions for was posted
    string channel?;
    # File comment to get reactions for
    string fileComment?;
    # If true always return the complete reaction list
    boolean full?;
    # Timestamp of the message to get reactions for
    string timestamp?;
};

# Represents the Queries record for the operation: reactions_list

type ReactionsListQueries record {
    # Parameter for pagination. Set `cursor` equal to the `next_cursor` attribute returned by the previous request's `response_metadata`. This parameter is optional, but pagination is mandatory: the default value simply fetches the first "page" of the collection. See [pagination](/docs/pagination) for more details
    string cursor?;
    int count?;
    # The maximum number of items to return. Fewer than the requested number of items may be returned, even if the end of the list hasn't been reached
    int 'limit?;
    int page?;
    # Show reactions made by this user. Defaults to the authed user
    string user?;
    # If true always return the complete reaction list
    boolean full?;
};

# Schema for successful response from reactions.list method

type ReactionsListResponse record {
    PagingObj paging?;
    ResponseMetadataObj responseMetadata?;
    true ok;
    (record {ChannelDef channel; MessageObj message; "message"  'type; }|record {FileObj file; "file"  'type; }|record {CommentObj comment; FileObj file; "file_comment"  'type; })[] items;
};


type ReactionsRemoveBody record {
    # File to remove reaction from
    string file?;
    # Channel where the message to remove reaction from was posted
    string channel?;
    # Reaction (emoji) name
    string name;
    # File comment to remove reaction from
    string fileComment?;
    # Timestamp of the message to remove reaction from
    string timestamp?;
};

# Schema for successful response from reactions.remove method

type ReactionsRemoveResponse record {
    true ok;
};


type ReminderObj record {
    UserIdDef creator;
    boolean recurring;
    int completeTs?;
    ReminderIdDef id;
    string text;
    int time?;
    UserIdDef user;
};


type RemindersAddBody record {
    # The content of the reminder
    string text;
    # When this reminder should happen: the Unix timestamp (up to five years from now), the number of seconds until the reminder (if within 24 hours), or a natural language description (Ex. "in 15 minutes," or "every Thursday")
    string time;
    # The user who will receive the reminder. If no user is specified, the reminder will go to user who created it
    string user?;
};

# Schema for successful response from reminders.add method

type RemindersAddResponse record {
    ReminderObj reminder;
    true ok;
};


type RemindersCompleteBody record {
    # The ID of the reminder to be marked as complete
    string reminder?;
};

# Schema for successful response from reminders.complete method

type RemindersCompleteResponse record {
    true ok;
};


type RemindersDeleteBody record {
    # The ID of the reminder
    string reminder?;
};

# Schema for successful response from reminders.delete method

type RemindersDeleteResponse record {
    true ok;
};

# Represents the Queries record for the operation: reminders_info

type RemindersInfoQueries record {
    # The ID of the reminder
    string reminder?;
};

# Schema for successful response from reminders.info method

type RemindersInfoResponse record {
    ReminderObj reminder;
    true ok;
};

# Schema for successful response from reminders.list method

type RemindersListResponse record {
    ReminderObj[] reminders;
    true ok;
};


type ResourcesObj record {
    (ChannelDef|TeamDef)[] ids;
    (ChannelDef|TeamDef)[] excludedIds?;
    boolean wildcard?;
};


type ResponseMetadata record {
    string[] warnings?;
};

# Represents the Queries record for the operation: rtm_connect

type RtmConnectQueries record {
    # Batch presence deliveries via subscription. Enabling changes the shape of `presence_change` events. See [batch presence](/docs/presence-and-status#batching)
    boolean batchPresenceAware?;
    # Only deliver presence events when requested by subscription. See [presence subscriptions](/docs/presence-and-status#subscriptions)
    boolean presenceSub?;
};

# Schema for successful response from rtm.connect method

type RtmConnectResponse record {
    RtmConnectResponseSelf self;
    RtmConnectResponseTeam team;
    true ok;
    string url;
};


type RtmConnectResponseSelf record {
    string name;
    UserIdDef id;
};


type RtmConnectResponseTeam record {
    string domain;
    string name;
    TeamDef id;
};

# Represents the Queries record for the operation: search_messages

type SearchMessagesQueries record {
    # Pass a value of `true` to enable query highlight markers (see below)
    boolean highlight?;
    # Search query
    string query;
    # Pass the number of results you want per "page". Maximum of `100`
    int count?;
    int page?;
    # Return matches sorted by either `score` or `timestamp`
    string sort?;
    # Change sort direction to ascending (`asc`) or descending (`desc`)
    string sortDir?;
};


type StarsAddBody record {
    # File to add star to
    string file?;
    # Channel to add star to, or channel where the message to add star to was posted (used with `timestamp`)
    string channel?;
    # File comment to add star to
    string fileComment?;
    # Timestamp of the message to add star to
    string timestamp?;
};

# Schema for successful response from stars.add method

type StarsAddResponse record {
    true ok;
};

# Represents the Queries record for the operation: stars_list

type StarsListQueries record {
    # Parameter for pagination. Set `cursor` equal to the `next_cursor` attribute returned by the previous request's `response_metadata`. This parameter is optional, but pagination is mandatory: the default value simply fetches the first "page" of the collection. See [pagination](/docs/pagination) for more details
    string cursor?;
    string count?;
    # The maximum number of items to return. Fewer than the requested number of items may be returned, even if the end of the list hasn't been reached
    int 'limit?;
    string page?;
};

# Schema for successful response from stars.list method

type StarsListResponse record {
    PagingObj paging?;
    true ok;
    (record {ChannelDef channel; int date_create; MessageObj message; "message"  'type; }|record {int date_create; FileObj file; "file"  'type; }|record {CommentObj comment; int date_create; FileObj file; "file_comment"  'type; }|record {ChannelDef channel; int date_create; "channel"  'type; }|record {DmIdDef channel; int date_create; "im"  'type; }|record {GroupIdDef channel; int date_create; "group"  'type; })[] items;
};


type StarsRemoveBody record {
    # File to remove star from
    string file?;
    # Channel to remove star from, or channel where the message to remove star from was posted (used with `timestamp`)
    string channel?;
    # File comment to remove star from
    string fileComment?;
    # Timestamp of the message to remove star from
    string timestamp?;
};

# Schema for successful response from stars.remove method

type StarsRemoveResponse record {
    true ok;
};


type SubteamObj record {
    int channelCount?;
    int dateDelete;
    int dateUpdate;
    SubteamObjDeletedBy[] deletedBy;
    string description;
    boolean isExternal;
    string 'handle;
    TeamDef teamId;
    UserIdDef createdBy;
    UserIdDef[] users?;
    SubteamObjAutoType[] autoType;
    SubteamObjPrefs prefs;
    boolean isSubteam;
    int userCount?;
    int dateCreate;
    string name;
    UserIdDef updatedBy;
    boolean isUsergroup;
    SubteamIdDef id;
    boolean autoProvision;
    string enterpriseSubteamId;
};


type SubteamObjPrefs record {
    ChannelIdDef[] channels;
    GroupIdDef[] groups;
};

# Represents the Queries record for the operation: team_accessLogs

type TeamAccessLogsQueries record {
    # End of time range of logs to include in results (inclusive)
    string before?;
    string count?;
    string page?;
};

# Schema for successful response from team.accessLogs method

type TeamAccessLogsResponse record {
    PagingObj paging;
    true ok;
    TeamAccessLogsResponseLogins[] logins;
};


type TeamAccessLogsResponseLogins record {
    string? country;
    int dateLast;
    UserIdDef userId;
    string? ip;
    string? isp;
    int count;
    int dateFirst;
    string? region;
    string userAgent;
    string username;
};

# Represents the Queries record for the operation: team_billableInfo

type TeamBillableInfoQueries record {
    # A user to retrieve the billable information for. Defaults to all users
    string user?;
};

# Represents the Queries record for the operation: team_info

type TeamInfoQueries record {
    # Team to get info on, if omitted, will return information about the current team. Will only return team that the authenticated token is allowed to see through external shared channels
    string team?;
};

# Schema for successful response from team.info method

type TeamInfoResponse record {
    TeamObj team;
    true ok;
};

# Represents the Queries record for the operation: team_integrationLogs

type TeamIntegrationLogsQueries record {
    # Filter logs to this service. Defaults to all logs
    string serviceId?;
    string count?;
    # Filter logs with this change type. Defaults to all logs
    string changeType?;
    string page?;
    # Filter logs to this Slack app. Defaults to all logs
    string appId?;
    # Filter logs generated by this user’s actions. Defaults to all logs
    string user?;
};

# Schema for successful response from team.integrationLogs method

type TeamIntegrationLogsResponse record {
    PagingObj paging;
    true ok;
    TeamIntegrationLogsResponseLogs[] logs;
};


type TeamIntegrationLogsResponseLogs record {
    string date;
    string serviceType?;
    string appType;
    UserIdDef userId;
    string userName;
    string scope;
    string serviceId?;
    ChannelDef channel?;
    string changeType;
    AppIdDef adminAppId?;
    AppIdDef appId;
};


type TeamObj record {
    PrimaryOwnerObj primaryOwner?;
    int isEnterprise?;
    IconObj icon;
    string? description?;
    int msgEditWindowMins?;
    string avatarBaseUrl?;
    TeamObjSsoProvider ssoProvider?;
    string locale?;
    boolean archived?;
    int messagesCount?;
    string payProdCur?;
    WorkspaceIdDef id;
    boolean isOverStorageLimit?;
        ""|"std"|"plus"|"compliance"|"enterprise"  plan?;
    ExternalOrgMigrationsObj externalOrgMigrations?;
    boolean overIntegrationsLimit?;
    int created?;
    boolean isAssigned?;
    int limitTs?;
    EnterpriseIdDef enterpriseId?;
    boolean deleted?;
    boolean hasComplianceExport?;
    int dateCreate?;
    TeamObjDiscoverable[] discoverable?;
    string domain;
    string name;
    string emailDomain;
    EnterpriseNameDef enterpriseName?;
    boolean overStorageLimit?;
};


type TeamObjSsoProvider record {
    string name?;
    string label?;
    string 'type?;
};


type TeamProfileFieldObj record {
    decimal ordering;
    string hint;
    string[]? possibleValues?;
    boolean isHidden?;
    TeamProfileFieldObjOptions[] options?;
    string id;
    string label;
        "text"|"date"|"link"|"mailto"|"options_list"|"user"  'type;
    string? fieldName?;
};


type TeamProfileFieldOptionObj record {
    boolean? isScim?;
    boolean? isCustom?;
    boolean? isProtected?;
    boolean? isMultipleEntry?;
};

# Represents the Queries record for the operation: team_profile_get

type TeamProfileGetQueries record {
    # Filter by visibility
    string visibility?;
};

# Schema for successful response from team.profile.get method

type TeamProfileGetResponse record {
    TeamProfileGetResponseProfile profile;
    true ok;
};


type TeamProfileGetResponseProfile record {
    TeamProfileFieldObj[] fields;
};


type UsergroupsCreateBody record {
    # A comma separated string of encoded channel IDs for which the User Group uses as a default
    string channels?;
    # A name for the User Group. Must be unique among User Groups
    string name;
    # A short description of the User Group
    string description?;
    # A mention handle. Must be unique among channels, users and User Groups
    string 'handle?;
    # Include the number of users in each User Group
    boolean includeCount?;
};

# Schema for successful response from usergroups.create method

type UsergroupsCreateResponse record {
    SubteamObj usergroup;
    true ok;
};


type UsergroupsDisableBody record {
    # The encoded ID of the User Group to disable
    string usergroup;
    # Include the number of users in the User Group
    boolean includeCount?;
};

# Schema for successful response from usergroups.disable method

type UsergroupsDisableResponse record {
    SubteamObj usergroup;
    true ok;
};


type UsergroupsEnableBody record {
    # The encoded ID of the User Group to enable
    string usergroup;
    # Include the number of users in the User Group
    boolean includeCount?;
};

# Schema for successful response from usergroups.enable method

type UsergroupsEnableResponse record {
    SubteamObj usergroup;
    true ok;
};

# Represents the Queries record for the operation: usergroups_list

type UsergroupsListQueries record {
    # Include disabled User Groups
    boolean includeDisabled?;
    # Include the list of users for each User Group
    boolean includeUsers?;
    # Include the number of users in each User Group
    boolean includeCount?;
};

# Schema for successful response from usergroups.list method

type UsergroupsListResponse record {
    SubteamObj[] usergroups;
    true ok;
};


type UsergroupsUpdateBody record {
    # A comma separated string of encoded channel IDs for which the User Group uses as a default
    string channels?;
    # A name for the User Group. Must be unique among User Groups
    string name?;
    # The encoded ID of the User Group to update
    string usergroup;
    # A short description of the User Group
    string description?;
    # A mention handle. Must be unique among channels, users and User Groups
    string 'handle?;
    # Include the number of users in the User Group
    boolean includeCount?;
};

# Schema for successful response from usergroups.update method

type UsergroupsUpdateResponse record {
    SubteamObj usergroup;
    true ok;
};

# Represents the Queries record for the operation: usergroups_users_list

type UsergroupsUsersListQueries record {
    # Allow results that involve disabled User Groups
    boolean includeDisabled?;
    # The encoded ID of the User Group to update
    string usergroup;
};

# Schema for successful response from usergroups.users.list method

type UsergroupsUsersListResponse record {
    true ok;
    UserIdDef[] users;
};


type UsergroupsUsersUpdateBody record {
    # The encoded ID of the User Group to update
    string usergroup;
    # Include the number of users in the User Group
    boolean includeCount?;
    # A comma separated string of encoded user IDs that represent the entire list of users for the User Group
    string users;
};

# Schema for successful response from usergroups.users.update method

type UsergroupsUsersUpdateResponse record {
    SubteamObj usergroup;
    true ok;
};

# user object for non enterprise type

type UserObjAnyOf1 record {
    string color?;
    boolean isInvitedUser?;
    boolean has2fa?;
    boolean isRestricted?;
    UserObjTz[] tz?;
    string tzLabel?;
    boolean isPrimaryOwner?;
    UserObjTeamProfile teamProfile?;
    string realName?;
    WorkspaceIdDef teamId?;
    string locale?;
    boolean isAdmin?;
    boolean isAppUser;
    decimal tzOffset?;
    boolean isStranger?;
    boolean isForgotten?;
    UserIdDef id;
    boolean isBot;
    string presence?;
    boolean isUltraRestricted?;
    boolean isOwner?;
    UserProfileObj profile;
    boolean isExternal?;
    WorkspaceIdDef team?;
    EnterpriseUserObj enterpriseUser?;
    boolean deleted?;
    string twoFactorType?;
    string name;
    decimal updated;
};


type UserObjTeamProfile record {
    TeamProfileFieldObj[] fields;
};


type UserObjTeamProfile1 record {
    TeamProfileFieldObj[] fields;
};

# enterprise user

type UserObjUserObjAnyOf12 record {
    # refercing to bug: https://jira.tinyspeck.com/browse/EVALUE-1559
    string color?;
    boolean has2fa?;
    boolean isRestricted?;
    UserObjTz1[] tz?;
    string tzLabel?;
    boolean isPrimaryOwner?;
    UserObjTeamProfile1 teamProfile?;
    string realName?;
    WorkspaceIdDef teamId?;
    string locale?;
    boolean isAdmin?;
    boolean isAppUser;
    decimal tzOffset?;
    boolean isStranger?;
    boolean isForgotten?;
    UserIdDef id;
    boolean isBot;
    string presence?;
    boolean isUltraRestricted?;
    WorkspaceIdDef[] teams?;
    boolean isOwner?;
    UserProfileObj profile;
    boolean isExternal?;
    EnterpriseUserObj enterpriseUser?;
    boolean deleted?;
    string twoFactorType?;
    string name;
    decimal updated;
};


type UserProfileObj record {
    string? image32?;
    string statusEmoji;
    string? guestInvitedBy?;
    boolean? isRestricted?;
    OptionalAppIdDef apiAppId?;
    string? image192?;
    string realName;
    string title;
    string? statusTextCanonical?;
    string skype;
    boolean isAppUser?;
    string? imageOriginal?;
    int? guestExpirationTs?;
    string realNameNormalized;
    string avatarHash;
    string? firstName?;
    BotIdDef botId?;
    string? email?;
    string? image512?;
    string? statusDefaultTextCanonical?;
    boolean? isUltraRestricted?;
    string? image1024?;
    string statusDefaultEmoji?;
    string? image24?;
    string? lastName?;
    string? image48?;
    WorkspaceIdDef team?;
    string displayName;
    string lastAvatarImageHash?;
    boolean alwaysActive?;
    int statusExpiration?;
    int membershipsCount?;
    string phone;
    string userId?;
    string? name?;
    string statusDefaultText?;
    string pronouns?;
    boolean isCustomImage?;
    string statusText;
    record {}[]? fields;
    string? image72?;
    int updated?;
    string displayNameNormalized;
    string? username?;
};


type UserProfileShortObj record {
    boolean isUltraRestricted;
    boolean isRestricted;
    string realNameNormalized?;
    string name;
    string realName;
    WorkspaceIdDef team;
    string avatarHash;
    string displayName;
    string image72;
    string? firstName;
    string displayNameNormalized?;
};

# Represents the Queries record for the operation: users_conversations

type UsersConversationsQueries record {
    # Paginate through collections of data by setting the `cursor` parameter to a `next_cursor` attribute returned by a previous request's `response_metadata`. Default value fetches the first "page" of the collection. See [pagination](/docs/pagination) for more detail
    string cursor?;
    # Mix and match channel types by providing a comma-separated list of any combination of `public_channel`, `private_channel`, `mpim`, `im`
    string types?;
    # The maximum number of items to return. Fewer than the requested number of items may be returned, even if the end of the list hasn't been reached. Must be an integer no larger than 1000
    int 'limit?;
    # Browse conversations by a specific user ID's membership. Non-public channels are restricted to those where the calling user shares membership
    string user?;
    # Set to `true` to exclude archived channels from the list
    boolean excludeArchived?;
};

# Schema for successful response from users.conversations method. Returned conversation objects do not include `num_members` or `is_member`

type UsersConversationsResponse record {
    ConversationObj[] channels;
    true ok;
    UsersConversationsResponseResponseMetadata response_metadata?;
};


type UsersConversationsResponseResponseMetadata record {
    string nextCursor;
};


type UsersDeletePhotoBody record {
    # Authentication token. Requires scope: `users.profile:write`
    string token;
};

# Schema for successful response from users.deletePhoto method

type UsersDeletePhotoResponse record {
    true ok;
};

# Represents the Queries record for the operation: users_getPresence

type UsersGetPresenceQueries record {
    # User to get presence info on. Defaults to the authed user
    string user?;
};

# Represents the Queries record for the operation: users_info

type UsersInfoQueries record {
    # Set this to `true` to receive the locale for this user. Defaults to `false`
    boolean includeLocale?;
    # User to get info on
    string user?;
};

# Schema for successful response from users.info method

type UsersInfoResponse record {
    true ok;
    UserObj user;
};

# Represents the Queries record for the operation: users_list

type UsersListQueries record {
    # Paginate through collections of data by setting the `cursor` parameter to a `next_cursor` attribute returned by a previous request's `response_metadata`. Default value fetches the first "page" of the collection. See [pagination](/docs/pagination) for more detail
    string cursor?;
    # The maximum number of items to return. Fewer than the requested number of items may be returned, even if the end of the users list hasn't been reached. Providing no `limit` value will result in Slack attempting to deliver you the entire result set. If the collection is too large you may experience `limit_required` or HTTP 500 errors
    int 'limit?;
    # Set this to `true` to receive the locale for users. Defaults to `false`
    boolean includeLocale?;
};

# Schema for successful response from users.list method

type UsersListResponse record {
    int cacheTs;
    UserObj[] members;
    ResponseMetadataObj responseMetadata?;
    true ok;
};

# Represents the Queries record for the operation: users_lookupByEmail

type UsersLookupByEmailQueries record {
    # An email address belonging to a user in the workspace
    string email;
};

# Schema for successful response from users.lookupByEmail method

type UsersLookupByEmailResponse record {
    true ok;
    UserObj user;
};

# Represents the Queries record for the operation: users_profile_get

type UsersProfileGetQueries record {
    # Include labels for each ID in custom profile fields
    boolean includeLabels?;
    # User to retrieve profile info for
    string user?;
};

# Schema for successful response from users.profile.get method

type UsersProfileGetResponse record {
    UserProfileObj profile;
    true ok;
};


type UsersProfileSetBody record {
    # Collection of key:value pairs presented as a URL-encoded JSON hash. At most 50 fields may be set. Each field name is limited to 255 characters
    string profile?;
    # Name of a single key to set. Usable only if `profile` is not passed
    string name?;
    # ID of user to change. This argument may only be specified by team admins on paid teams
    string user?;
    # Value to set a single key to. Usable only if `profile` is not passed
    string value?;
};

# Schema for successful response from users.profile.set method

type UsersProfileSetResponse record {
    UserProfileObj profile;
    true ok;
    string emailPending?;
    string username;
};

# Schema for successful response from users.setActive method

type UsersSetActiveResponse record {
    true ok;
};


type UsersSetPhotoBody record {
    # Y coordinate of top-left corner of crop box
    string cropY?;
    # File contents via `multipart/form-data`
    string image?;
    # Width/height of crop box (always square)
    string cropW?;
    # X coordinate of top-left corner of crop box
    string cropX?;
    # Authentication token. Requires scope: `users.profile:write`
    string token;
};

# Schema for successful response from users.setPhoto method

type UsersSetPhotoResponse record {
    UsersSetPhotoResponseProfile profile;
    true ok;
};


type UsersSetPhotoResponseProfile record {
    string image32;
    string imageOriginal;
    string image1024;
    string image24;
    string image192;
    string image48;
    string avatarHash;
    string image72;
    string image512;
};


type UsersSetPresenceBody record {
    # Either `auto` or `away`
    string presence;
};

# Schema for successful response from users.setPresence method

type UsersSetPresenceResponse record {
    true ok;
};

# Represents the Queries record for the operation: views_open

type ViewsOpenQueries record {
    # A [view payload](/reference/surfaces/views). This must be a JSON-encoded string
    string view;
    # Exchange a trigger to post to the user
    string triggerId;
};

# Represents the Queries record for the operation: views_publish

type ViewsPublishQueries record {
    # A [view payload](/reference/surfaces/views). This must be a JSON-encoded string
    string view;
    # `id` of the user you want publish a view to
    string userId;
    # A string that represents view state to protect against possible race conditions
    string hash?;
};

# Represents the Queries record for the operation: views_push

type ViewsPushQueries record {
    # A [view payload](/reference/surfaces/views). This must be a JSON-encoded string
    string view;
    # Exchange a trigger to post to the user
    string triggerId;
};

# Represents the Queries record for the operation: views_update

type ViewsUpdateQueries record {
    # A [view object](/reference/surfaces/views). This must be a JSON-encoded string
    string view?;
    # A unique identifier of the view to be updated. Either `view_id` or `external_id` is required
    string viewId?;
    # A unique identifier of the view set by the developer. Must be unique for all views on a team. Max length of 255 characters. Either `view_id` or `external_id` is required
    string externalId?;
    # A string that represents view state to protect against possible race conditions
    string hash?;
};

# Represents the Queries record for the operation: workflows_stepCompleted

type WorkflowsStepCompletedQueries record {
    # Key-value object of outputs from your step. Keys of this object reflect the configured `key` properties of your [`outputs`](/reference/workflows/workflow_step#output) array from your `workflow_step` object
    string outputs?;
    # Context identifier that maps to the correct workflow step execution
    string workflowStepExecuteId;
};

# Represents the Queries record for the operation: workflows_stepFailed

type WorkflowsStepFailedQueries record {
    # Context identifier that maps to the correct workflow step execution
    string workflowStepExecuteId;
    # A JSON-based object with a `message` property that should contain a human readable error message
    string 'error;
};

# Represents the Queries record for the operation: workflows_updateStep

type WorkflowsUpdateStepQueries record {
    # An JSON array of output objects used during step execution. This is the data your app agrees to provide when your workflow step was executed
    string outputs?;
    # A JSON key-value map of inputs required from a user during configuration. This is the data your app expects to receive when the workflow step starts. **Please note**: the embedded variable format is set and replaced by the workflow system. You cannot create custom variables that will be replaced at runtime. [Read more about variables in workflow steps here](/workflows/steps#variables)
    string inputs?;
    # An optional field that can be used to override the step name that is shown in the Workflow Builder
    string stepName?;
    # An optional field that can be used to override app image that is shown in the Workflow Builder
    string stepImageUrl?;
    # A context identifier provided with `view_submission` payloads used to call back to `workflows.updateStep`
    string workflowStepEditId;
};

// Unknown type: DiscoverableDiscoverableAnyOf12

// Unknown type: TzTzAnyOf112

// Unknown type: TsDef

// Unknown type: EnterpriseIdDef

// Unknown type: EnterpriseNameDef

// Unknown type: TeamDef

// Unknown type: ChannelNameDef

// Unknown type: BotIdDef

// Unknown type: UserIdDef

// Unknown type: SubteamIdDef

// Unknown type: TopicPurposeCreatorDef

// Unknown type: ReminderIdDef

// Unknown type: CommentIdDef

// Unknown type: TzTzAnyOf12

// Unknown type: ChannelIdDef

// Unknown type: DmIdDef

// Unknown type: FileIdDef

// Unknown type: AppIdDef

// Unknown type: EnterpriseUserIdDef

// Unknown type: ChannelDef

// Unknown type: OptionalAppIdDef

// Unknown type: WorkspaceIdDef

// Unknown type: GroupIdDef

// Unknown type: ChannelActionsTsAnyOf1

// Unknown type: ScopesObj

// Unknown type: CommentsObj

// Unknown type: ConversationObj

// Unknown type: ResponseMetadataObj

// Unknown type: Blocks

// Unknown type: UserObj

type ConversationsOpenResponseChannel ConversationObj|ChannelAnyOf2;

type MessageObjBotId BotIdDef|NilBotIdSetWhenDisplayAsBotIsFalse?;

type ConversationObjParentConversation ChannelDef|ParentConversationAnyOf2?;

type UserObjTz1 TzAnyOf11|TzTzAnyOf112?;

type ConversationObjParentConversation2 ChannelDef|ParentConversationAnyOf22?;

type ConversationObjParentConversation1 ChannelDef|ParentConversationAnyOf21?;

type ConversationObjLatest MessageObj|LatestAnyOf21?;

type ConversationsHistoryResponseChannelActionsTs ChannelActionsTsAnyOf1|ChannelActionsTsChannelActionsTsAnyOf12?;

type SubteamObjDeletedBy DeletedByAnyOf1|UserIdDef?;

type UserObjTz TzAnyOf1|TzTzAnyOf12?;

type ChannelObjLatest MessageObj|LatestAnyOf2?;

type InlineResponseItems2002 '200AnyOf12|'200200AnyOf122|'200200200AnyOf1223|'200200200200AnyOf12234;

type InlineResponseItems2001 '200AnyOf11|'200200AnyOf112|'200200200AnyOf1123;

type InlineArrayItemsConversationObj ConversationObject|ConversationMPIMObject|ConversationIMChannelObjectFromConversationsMethods;

type AutoTypeAutoTypeAnyOf12 "owner"|"admin";

type InlineResponseItems200 '200AnyOf1|'200200AnyOf12;

type SubteamObjAutoType AutoTypeAnyOf1|AutoTypeAutoTypeAnyOf12?;

type InlineArrayItemsUserObj UserObjAnyOf1|UserObjUserObjAnyOf12;

type ConversationObjLatest2 MessageObj|LatestAnyOf23?;

type ConversationObjLatest1 MessageObj|LatestAnyOf22?;

type TeamObjDiscoverable DiscoverableAnyOf1|DiscoverableDiscoverableAnyOf12?;

type '200Items FilePin|MessagePin;

type TeamProfileFieldObjOptions OptionsAnyOf1|TeamProfileFieldOptionObj?;

type InlineArrayItemsResponseMetadataObj NewPagingStyle|DeprecationWarning|DeprecationWarningAndPagingStyleTogether;

// --- Client ---

# One way to interact with the Slack platform is its HTTP RPC-based Web API, a collection of methods requiring OAuth 2.0-based user, bot, or workspace tokens blessed with related OAuth scopes.
client class Client {
    function init(ConnectionConfig config, string serviceUrl = "https://slack.com/api") returns error?;

    # Approve an app for installation on a workspace.
    resource function post admin\.apps\.approve(AdminAppsApproveBody payload, map<string|string[]> headers = {}) returns DefaultSuccessResponse|error;

    # List approved apps for an org or workspace.
    resource function get admin\.apps\.approved\.list(map<string|string[]> headers = {}, AdminAppsApprovedListQueries queries) returns DefaultSuccessResponse1|error;

    # List app requests for a team/workspace.
    resource function get admin\.apps\.requests\.list(map<string|string[]> headers = {}, AdminAppsRequestsListQueries queries) returns DefaultSuccessResponse2|error;

    # Restrict an app for installation on a workspace.
    resource function post admin\.apps\.restrict(AdminAppsRestrictBody payload, map<string|string[]> headers = {}) returns DefaultSuccessResponse3|error;

    # List restricted apps for an org or workspace.
    resource function get admin\.apps\.restricted\.list(map<string|string[]> headers = {}, AdminAppsRestrictedListQueries queries) returns DefaultSuccessResponse4|error;

    # Archive a public or private channel.
    resource function post admin\.conversations\.archive(AdminConversationsArchiveBody payload, map<string|string[]> headers = {}) returns AdminConversationsArchiveResponse|error;

    # Convert a public channel to a private channel.
    resource function post admin\.conversations\.convertToPrivate(AdminConversationsConvertToPrivateBody payload, map<string|string[]> headers = {}) returns AdminConversationsConvertToPrivateResponse|error;

    # Create a public or private channel-based conversation.
    resource function post admin\.conversations\.create(AdminConversationsCreateBody payload, map<string|string[]> headers = {}) returns AdminConversationsCreateResponse|error;

    # Delete a public or private channel.
    resource function post admin\.conversations\.delete(AdminConversationsDeleteBody payload, map<string|string[]> headers = {}) returns AdminConversationsDeleteResponse|error;

    # Disconnect a connected channel from one or more workspaces.
    resource function post admin\.conversations\.disconnectShared(AdminConversationsDisconnectSharedBody payload, map<string|string[]> headers = {}) returns AdminConversationsRenameResponse|error;

    # List all disconnected channels—i.e., channels that were once connected to other workspaces and then disconnected—and the corresponding original channel IDs for key revocation with EKM.
    resource function get admin\.conversations\.ekm\.listOriginalConnectedChannelInfo(map<string|string[]> headers = {}, AdminConversationsEkmListOriginalConnectedChannelInfoQueries queries) returns DefaultSuccessResponse5|error;

    # Get conversation preferences for a public or private channel.
    resource function get admin\.conversations\.getConversationPrefs(map<string|string[]> headers = {}, AdminConversationsGetConversationPrefsQueries queries) returns AdminConversationsGetConversationPrefsResponse|error;

    # Get all the workspaces a given public or private channel is connected to within this Enterprise org.
    resource function get admin\.conversations\.getTeams(map<string|string[]> headers = {}, AdminConversationsGetTeamsQueries queries) returns AdminConversationsGetTeamsResponse|error;

    # Invite a user to a public or private channel.
    resource function post admin\.conversations\.invite(AdminConversationsInviteBody payload, map<string|string[]> headers = {}) returns AdminConversationsInviteResponse|error;

    # Rename a public or private channel.
    resource function post admin\.conversations\.rename(AdminConversationsRenameBody payload, map<string|string[]> headers = {}) returns AdminConversationsRenameResponse1|error;

    # Add an allowlist of IDP groups for accessing a channel
    resource function post admin\.conversations\.restrictAccess\.addGroup(AdminConversationsRestrictAccessAddGroupBody payload, map<string|string[]> headers = {}) returns DefaultSuccessResponse6|error;

    # List all IDP Groups linked to a channel
    resource function get admin\.conversations\.restrictAccess\.listGroups(map<string|string[]> headers = {}, AdminConversationsRestrictAccessListGroupsQueries queries) returns DefaultSuccessResponse7|error;

    # Remove a linked IDP group linked from a private channel
    resource function post admin\.conversations\.restrictAccess\.removeGroup(AdminConversationsRestrictAccessRemoveGroupBody payload, map<string|string[]> headers = {}) returns DefaultSuccessResponse8|error;

    # Search for public or private channels in an Enterprise organization.
    resource function get admin\.conversations\.search(map<string|string[]> headers = {}, AdminConversationsSearchQueries queries) returns AdminConversationsSearchResponse|error;

    # Set the posting permissions for a public or private channel.
    resource function post admin\.conversations\.setConversationPrefs(AdminConversationsSetConversationPrefsBody payload, map<string|string[]> headers = {}) returns AdminConversationsSetConversationPrefsResponse|error;

    # Set the workspaces in an Enterprise grid org that connect to a public or private channel.
    resource function post admin\.conversations\.setTeams(AdminConversationsSetTeamsBody payload, map<string|string[]> headers = {}) returns DefaultSuccessResponse9|error;

    # Unarchive a public or private channel.
    resource function post admin\.conversations\.unarchive(AdminConversationsUnarchiveBody payload, map<string|string[]> headers = {}) returns AdminConversationsUnarchiveResponse|error;

    # Add an emoji.
    resource function post admin\.emoji\.add(AdminEmojiAddBody payload, map<string|string[]> headers = {}) returns DefaultSuccessResponse10|error;

    # Add an emoji alias.
    resource function post admin\.emoji\.addAlias(AdminEmojiAddAliasBody payload, map<string|string[]> headers = {}) returns DefaultSuccessResponse11|error;

    # List emoji for an Enterprise Grid organization.
    resource function get admin\.emoji\.list(map<string|string[]> headers = {}, AdminEmojiListQueries queries) returns DefaultSuccessResponse12|error;

    # Remove an emoji across an Enterprise Grid organization
    resource function post admin\.emoji\.remove(AdminEmojiRemoveBody payload, map<string|string[]> headers = {}) returns DefaultSuccessResponse13|error;

    # Rename an emoji.
    resource function post admin\.emoji\.rename(AdminEmojiRenameBody payload, map<string|string[]> headers = {}) returns DefaultSuccessResponse14|error;

    # Approve a workspace invite request.
    resource function post admin\.inviteRequests\.approve(record {string invite_request_id; string team_id; } payload, map<string|string[]> headers = {}) returns DefaultSuccessResponse15|error;

    # List all approved workspace invite requests.
    resource function get admin\.inviteRequests\.approved\.list(map<string|string[]> headers = {}, AdminInviteRequestsApprovedListQueries queries) returns DefaultSuccessResponse16|error;

    # List all denied workspace invite requests.
    resource function get admin\.inviteRequests\.denied\.list(map<string|string[]> headers = {}, AdminInviteRequestsDeniedListQueries queries) returns DefaultSuccessResponse17|error;

    # Deny a workspace invite request.
    resource function post admin\.inviteRequests\.deny(record {string invite_request_id; string team_id; } payload, map<string|string[]> headers = {}) returns DefaultSuccessResponse18|error;

    # List all pending workspace invite requests.
    resource function get admin\.inviteRequests\.list(map<string|string[]> headers = {}, AdminInviteRequestsListQueries queries) returns DefaultSuccessResponse19|error;

    # List all of the admins on a given workspace.
    resource function get admin\.teams\.admins\.list(map<string|string[]> headers = {}, AdminTeamsAdminsListQueries queries) returns DefaultSuccessResponse20|error;

    # Create an Enterprise team.
    resource function post admin\.teams\.create(AdminTeamsCreateBody payload, map<string|string[]> headers = {}) returns DefaultSuccessResponse21|error;

    # List all teams on an Enterprise organization
    resource function get admin\.teams\.list(map<string|string[]> headers = {}, AdminTeamsListQueries queries) returns DefaultSuccessResponse22|error;

    # List all of the owners on a given workspace.
    resource function get admin\.teams\.owners\.list(map<string|string[]> headers = {}, AdminTeamsOwnersListQueries queries) returns DefaultSuccessResponse23|error;

    # Fetch information about settings in a workspace
    resource function get admin\.teams\.settings\.info(map<string|string[]> headers = {}, AdminTeamsSettingsInfoQueries queries) returns DefaultSuccessResponse24|error;

    # Set the default channels of a workspace.
    resource function post admin\.teams\.settings\.setDefaultChannels(AdminTeamsSettingsSetDefaultChannelsBody payload, map<string|string[]> headers = {}) returns DefaultSuccessResponse25|error;

    # Set the description of a given workspace.
    resource function post admin\.teams\.settings\.setDescription(AdminTeamsSettingsSetDescriptionBody payload, map<string|string[]> headers = {}) returns DefaultSuccessResponse26|error;

    # An API method that allows admins to set the discoverability of a given workspace
    resource function post admin\.teams\.settings\.setDiscoverability(AdminTeamsSettingsSetDiscoverabilityBody payload, map<string|string[]> headers = {}) returns DefaultSuccessResponse27|error;

    # Sets the icon of a workspace.
    resource function post admin\.teams\.settings\.setIcon(AdminTeamsSettingsSetIconBody payload, map<string|string[]> headers = {}) returns DefaultSuccessResponse28|error;

    # Set the name of a given workspace.
    resource function post admin\.teams\.settings\.setName(AdminTeamsSettingsSetNameBody payload, map<string|string[]> headers = {}) returns DefaultSuccessResponse29|error;

    # Add one or more default channels to an IDP group.
    resource function post admin\.usergroups\.addChannels(AdminUsergroupsAddChannelsBody payload, map<string|string[]> headers = {}) returns DefaultSuccessResponse30|error;

    # Associate one or more default workspaces with an organization-wide IDP group.
    resource function post admin\.usergroups\.addTeams(AdminUsergroupsAddTeamsBody payload, map<string|string[]> headers = {}) returns DefaultSuccessResponse31|error;

    # List the channels linked to an org-level IDP group (user group).
    resource function get admin\.usergroups\.listChannels(map<string|string[]> headers = {}, AdminUsergroupsListChannelsQueries queries) returns DefaultSuccessResponse32|error;

    # Remove one or more default channels from an org-level IDP group (user group).
    resource function post admin\.usergroups\.removeChannels(AdminUsergroupsRemoveChannelsBody payload, map<string|string[]> headers = {}) returns DefaultSuccessResponse33|error;

    # Add an Enterprise user to a workspace.
    resource function post admin\.users\.assign(AdminUsersAssignBody payload, map<string|string[]> headers = {}) returns DefaultSuccessResponse34|error;

    # Invite a user to a workspace.
    resource function post admin\.users\.invite(AdminUsersInviteBody payload, map<string|string[]> headers = {}) returns DefaultSuccessResponse35|error;

    # List users on a workspace
    resource function get admin\.users\.list(map<string|string[]> headers = {}, AdminUsersListQueries queries) returns DefaultSuccessResponse36|error;

    # Remove a user from a workspace.
    resource function post admin\.users\.remove(AdminUsersRemoveBody payload, map<string|string[]> headers = {}) returns DefaultSuccessResponse37|error;

    # Invalidate a single session for a user by session_id
    resource function post admin\.users\.session\.invalidate(AdminUsersSessionInvalidateBody payload, map<string|string[]> headers = {}) returns DefaultSuccessResponse38|error;

    # Wipes all valid sessions on all devices for a given user
    resource function post admin\.users\.session\.reset(AdminUsersSessionResetBody payload, map<string|string[]> headers = {}) returns DefaultSuccessResponse39|error;

    # Set an existing guest, regular user, or owner to be an admin user.
    resource function post admin\.users\.setAdmin(AdminUsersSetAdminBody payload, map<string|string[]> headers = {}) returns DefaultSuccessResponse40|error;

    # Set an expiration for a guest user
    resource function post admin\.users\.setExpiration(AdminUsersSetExpirationBody payload, map<string|string[]> headers = {}) returns DefaultSuccessResponse41|error;

    # Set an existing guest, regular user, or admin user to be a workspace owner.
    resource function post admin\.users\.setOwner(AdminUsersSetOwnerHeaders headers, AdminUsersSetOwnerBody payload) returns DefaultSuccessResponse42|error;

    # Set an existing guest user, admin user, or owner to be a regular user.
    resource function post admin\.users\.setRegular(AdminUsersSetRegularBody payload, map<string|string[]> headers = {}) returns DefaultSuccessResponse43|error;

    # Checks API calling code.
    resource function get api\.test(map<string|string[]> headers = {}, ApiTestQueries queries) returns ApiTestResponse|error;

    # Get a list of authorizations for the given event context. Each authorization represents an app installation that the event is visible to.
    resource function get apps\.event\.authorizations\.list(map<string|string[]> headers = {}, AppsEventAuthorizationsListQueries queries) returns DefaultSuccessResponse44|error;

    # Returns list of permissions this app has on a team.
    resource function get apps\.permissions\.info(map<string|string[]> headers = {}) returns AppsPermissionsInfoResponse|error;

    # Allows an app to request additional scopes
    resource function get apps\.permissions\.request(map<string|string[]> headers = {}, AppsPermissionsRequestQueries queries) returns AppsPermissionsRequestResponse|error;

    # Returns list of resource grants this app has on a team.
    resource function get apps\.permissions\.resources\.list(map<string|string[]> headers = {}, AppsPermissionsResourcesListQueries queries) returns AppsPermissionsResourcesListResponse|error;

    # Returns list of scopes this app has on a team.
    resource function get apps\.permissions\.scopes\.list(map<string|string[]> headers = {}) returns ApiPermissionsScopesListResponse|error;

    # Returns list of user grants and corresponding scopes this app has on a team.
    resource function get apps\.permissions\.users\.list(map<string|string[]> headers = {}, AppsPermissionsUsersListQueries queries) returns DefaultSuccessResponse45|error;

    # Enables an app to trigger a permissions modal to grant an app access to a user access scope.
    resource function get apps\.permissions\.users\.request(map<string|string[]> headers = {}, AppsPermissionsUsersRequestQueries queries) returns DefaultSuccessResponse46|error;

    # Uninstalls your app from a workspace.
    resource function get apps\.uninstall(map<string|string[]> headers = {}, AppsUninstallQueries queries) returns AppsUninstallResponse|error;

    # Revokes a token.
    resource function get auth\.revoke(map<string|string[]> headers = {}, AuthRevokeQueries queries) returns AuthRevokeResponse|error;

    # Checks authentication & identity.
    resource function get auth\.test(map<string|string[]> headers = {}) returns AuthTestResponse|error;

    # Gets information about a bot user.
    resource function get bots\.info(map<string|string[]> headers = {}, BotsInfoQueries queries) returns BotsInfoResponse|error;

    # Registers a new Call.
    resource function post calls\.add(CallsAddBody payload, map<string|string[]> headers = {}) returns DefaultSuccessResponse47|error;

    # Ends a Call.
    resource function post calls\.end(CallsEndBody payload, map<string|string[]> headers = {}) returns DefaultSuccessResponse48|error;

    # Returns information about a Call.
    resource function get calls\.info(map<string|string[]> headers = {}, CallsInfoQueries queries) returns DefaultSuccessResponse49|error;

    # Registers new participants added to a Call.
    resource function post calls\.participants\.add(CallsParticipantsAddBody payload, map<string|string[]> headers = {}) returns DefaultSuccessResponse50|error;

    # Registers participants removed from a Call.
    resource function post calls\.participants\.remove(CallsParticipantsRemoveBody payload, map<string|string[]> headers = {}) returns DefaultSuccessResponse51|error;

    # Updates information about a Call.
    resource function post calls\.update(CallsUpdateBody payload, map<string|string[]> headers = {}) returns DefaultSuccessResponse52|error;

    # Deletes a message.
    resource function post chat\.delete(ChatDeleteBody payload, map<string|string[]> headers = {}) returns ChatDeleteResponse|error;

    # Deletes a pending scheduled message from the queue.
    resource function post chat\.deleteScheduledMessage(ChatDeleteScheduledMessageBody payload, map<string|string[]> headers = {}) returns ChatDeleteScheduledMessageResponse|error;

    # Retrieve a permalink URL for a specific extant message
    resource function get chat\.getPermalink(map<string|string[]> headers = {}, ChatGetPermalinkQueries queries) returns ChatGetPermalinkResponse|error;

    # Share a me message into a channel.
    resource function post chat\.meMessage(ChatMeMessageBody payload, map<string|string[]> headers = {}) returns ChatMeMessageResponse|error;

    # Sends an ephemeral message to a user in a channel.
    resource function post chat\.postEphemeral(ChatPostEphemeralBody payload, map<string|string[]> headers = {}) returns ChatPostEphemeralResponse|error;

    # Sends a message to a channel.
    resource function post chat\.postMessage(ChatPostMessageBody payload, map<string|string[]> headers = {}) returns ChatPostMessageResponse|error;

    # Schedules a message to be sent to a channel.
    resource function post chat\.scheduleMessage(ChatScheduleMessageBody payload, map<string|string[]> headers = {}) returns ChatScheduleMessageResponse|error;

    # Returns a list of scheduled messages.
    resource function get chat\.scheduledMessages\.list(map<string|string[]> headers = {}, ChatScheduledMessagesListQueries queries) returns ChatScheduledMessagesListResponse|error;

    # Provide custom unfurl behavior for user-posted URLs
    resource function post chat\.unfurl(ChatUnfurlBody payload, map<string|string[]> headers = {}) returns ChatUnfurlResponse|error;

    # Updates a message.
    resource function post chat\.update(ChatUpdateBody payload, map<string|string[]> headers = {}) returns ChatUpdateResponse|error;

    # Archives a conversation.
    resource function post conversations\.archive(ConversationsArchiveBody payload, map<string|string[]> headers = {}) returns ConversationsArchiveResponse|error;

    # Closes a direct message or multi-person direct message.
    resource function post conversations\.close(ConversationsCloseBody payload, map<string|string[]> headers = {}) returns ConversationsCloseResponse|error;

    # Initiates a public or private channel-based conversation
    resource function post conversations\.create(ConversationsCreateBody payload, map<string|string[]> headers = {}) returns ConversationsCreateResponse|error;

    # Fetches a conversation's history of messages and events.
    resource function get conversations\.history(map<string|string[]> headers = {}, ConversationsHistoryQueries queries) returns ConversationsHistoryResponse|error;

    # Retrieve information about a conversation.
    resource function get conversations\.info(map<string|string[]> headers = {}, ConversationsInfoQueries queries) returns ConversationsInfoResponse|error;

    # Invites users to a channel.
    resource function post conversations\.invite(ConversationsInviteBody payload, map<string|string[]> headers = {}) returns ConversationsInviteErrorResponse|error;

    # Joins an existing conversation.
    resource function post conversations\.join(ConversationsJoinBody payload, map<string|string[]> headers = {}) returns ConversationsJoinResponse|error;

    # Removes a user from a conversation.
    resource function post conversations\.kick(ConversationsKickBody payload, map<string|string[]> headers = {}) returns ConversationsKickResponse|error;

    # Leaves a conversation.
    resource function post conversations\.leave(ConversationsLeaveBody payload, map<string|string[]> headers = {}) returns ConversationsLeaveResponse|error;

    # Lists all channels in a Slack team.
    resource function get conversations\.list(map<string|string[]> headers = {}, ConversationsListQueries queries) returns ConversationsListResponse|error;

    # Sets the read cursor in a channel.
    resource function post conversations\.mark(ConversationsMarkBody payload, map<string|string[]> headers = {}) returns ConversationsMarkResponse|error;

    # Retrieve members of a conversation.
    resource function get conversations\.members(map<string|string[]> headers = {}, ConversationsMembersQueries queries) returns ConversationsMembersResponse|error;

    # Opens or resumes a direct message or multi-person direct message.
    resource function post conversations\.open(ConversationsOpenBody payload, map<string|string[]> headers = {}) returns ConversationsOpenResponse|error;

    # Renames a conversation.
    resource function post conversations\.rename(ConversationsRenameBody payload, map<string|string[]> headers = {}) returns ConversationsRenameResponse|error;

    # Retrieve a thread of messages posted to a conversation
    resource function get conversations\.replies(map<string|string[]> headers = {}, ConversationsRepliesQueries queries) returns ConversationsRepliesResponse|error;

    # Sets the purpose for a conversation.
    resource function post conversations\.setPurpose(ConversationsSetPurposeBody payload, map<string|string[]> headers = {}) returns ConversationsSetPurposeResponse|error;

    # Sets the topic for a conversation.
    resource function post conversations\.setTopic(ConversationsSetTopicBody payload, map<string|string[]> headers = {}) returns ConversationsSetTopicResponse|error;

    # Reverses conversation archival.
    resource function post conversations\.unarchive(ConversationsUnarchiveBody payload, map<string|string[]> headers = {}) returns ConversationsUnarchiveResponse|error;

    # Open a dialog with a user
    resource function get dialog\.open(map<string|string[]> headers = {}, DialogOpenQueries queries) returns DialogOpenResponse|error;

    # Ends the current user's Do Not Disturb session immediately.
    resource function post dnd\.endDnd(map<string|string[]> headers = {}) returns DndEndDndResponse|error;

    # Ends the current user's snooze mode immediately.
    resource function post dnd\.endSnooze(map<string|string[]> headers = {}) returns DndEndSnoozeResponse|error;

    # Retrieves a user's current Do Not Disturb status.
    resource function get dnd\.info(map<string|string[]> headers = {}, DndInfoQueries queries) returns DndInfoResponse|error;

    # Turns on Do Not Disturb mode for the current user, or changes its duration.
    resource function post dnd\.setSnooze(DndSetSnoozeBody payload, map<string|string[]> headers = {}) returns DndSetSnoozeResponse|error;

    # Retrieves the Do Not Disturb status for up to 50 users on a team.
    resource function get dnd\.teamInfo(map<string|string[]> headers = {}, DndTeamInfoQueries queries) returns DefaultSuccessResponse53|error;

    # Lists custom emoji for a team.
    resource function get emoji\.list(map<string|string[]> headers = {}) returns DefaultSuccessResponse54|error;

    # Deletes an existing comment on a file.
    resource function post files\.comments\.delete(FilesCommentsDeleteBody payload, map<string|string[]> headers = {}) returns FilesCommentsDeleteResponse|error;

    # Deletes a file.
    resource function post files\.delete(FilesDeleteBody payload, map<string|string[]> headers = {}) returns FilesDeleteResponse|error;

    # Gets information about a file.
    resource function get files\.info(map<string|string[]> headers = {}, FilesInfoQueries queries) returns FilesInfoResponse|error;

    # List for a team, in a channel, or from a user with applied filters.
    resource function get files\.list(map<string|string[]> headers = {}, FilesListQueries queries) returns FilesListResponse|error;

    # Adds a file from a remote service
    resource function post files\.remote\.add(FilesRemoteAddBody payload, map<string|string[]> headers = {}) returns DefaultSuccessResponse55|error;

    # Retrieve information about a remote file added to Slack
    resource function get files\.remote\.info(map<string|string[]> headers = {}, FilesRemoteInfoQueries queries) returns DefaultSuccessResponse56|error;

    # Retrieve information about a remote file added to Slack
    resource function get files\.remote\.list(map<string|string[]> headers = {}, FilesRemoteListQueries queries) returns DefaultSuccessResponse57|error;

    # Remove a remote file.
    resource function post files\.remote\.remove(FilesRemoteRemoveBody payload, map<string|string[]> headers = {}) returns DefaultSuccessResponse58|error;

    # Share a remote file into a channel.
    resource function get files\.remote\.share(map<string|string[]> headers = {}, FilesRemoteShareQueries queries) returns DefaultSuccessResponse59|error;

    # Updates an existing remote file.
    resource function post files\.remote\.update(FilesRemoteUpdateBody payload, map<string|string[]> headers = {}) returns DefaultSuccessResponse60|error;

    # Revokes public/external sharing access for a file
    resource function post files\.revokePublicURL(FilesRevokePublicURLBody payload, map<string|string[]> headers = {}) returns FilesRevokePublicURLResponse|error;

    # Enables a file for public/external sharing.
    resource function post files\.sharedPublicURL(FilesSharedPublicURLBody payload, map<string|string[]> headers = {}) returns FilesSharedPublicURLResponse|error;

    # Uploads or creates a file.
    resource function post files\.upload(FilesUploadBody payload, map<string|string[]> headers = {}) returns FilesUploadResponse|error;

    # For Enterprise Grid workspaces, map local user IDs to global user IDs
    resource function get migration\.exchange(map<string|string[]> headers = {}, MigrationExchangeQueries queries) returns MigrationExchangeResponse|error;

    # Exchanges a temporary OAuth verifier code for an access token.
    resource function get oauth\.access(map<string|string[]> headers = {}, OauthAccessQueries queries) returns DefaultSuccessResponse61|error;

    # Exchanges a temporary OAuth verifier code for a workspace token.
    resource function get oauth\.token(map<string|string[]> headers = {}, OauthTokenQueries queries) returns DefaultSuccessResponse62|error;

    # Exchanges a temporary OAuth verifier code for an access token.
    resource function get oauth\.v2\.access(map<string|string[]> headers = {}, OauthV2AccessQueries queries) returns DefaultSuccessResponse63|error;

    # Pins an item to a channel.
    resource function post pins\.add(PinsAddBody payload, map<string|string[]> headers = {}) returns PinsAddResponse|error;

    # Lists items pinned to a channel.
    resource function get pins\.list(map<string|string[]> headers = {}, PinsListQueries queries) returns InlineResponseItems200[]|error;

    # Un-pins an item from a channel.
    resource function post pins\.remove(PinsRemoveBody payload, map<string|string[]> headers = {}) returns PinsRemoveResponse|error;

    # Adds a reaction to an item.
    resource function post reactions\.add(ReactionsAddBody payload, map<string|string[]> headers = {}) returns ReactionsAddResponse|error;

    # Gets reactions for an item.
    resource function get reactions\.get(map<string|string[]> headers = {}, ReactionsGetQueries queries) returns InlineResponseItems2001[]|error;

    # Lists reactions made by a user.
    resource function get reactions\.list(map<string|string[]> headers = {}, ReactionsListQueries queries) returns ReactionsListResponse|error;

    # Removes a reaction from an item.
    resource function post reactions\.remove(ReactionsRemoveBody payload, map<string|string[]> headers = {}) returns ReactionsRemoveResponse|error;

    # Creates a reminder.
    resource function post reminders\.add(RemindersAddBody payload, map<string|string[]> headers = {}) returns RemindersAddResponse|error;

    # Marks a reminder as complete.
    resource function post reminders\.complete(RemindersCompleteBody payload, map<string|string[]> headers = {}) returns RemindersCompleteResponse|error;

    # Deletes a reminder.
    resource function post reminders\.delete(RemindersDeleteBody payload, map<string|string[]> headers = {}) returns RemindersDeleteResponse|error;

    # Gets information about a reminder.
    resource function get reminders\.info(map<string|string[]> headers = {}, RemindersInfoQueries queries) returns RemindersInfoResponse|error;

    # Lists all reminders created by or for a given user.
    resource function get reminders\.list(map<string|string[]> headers = {}) returns RemindersListResponse|error;

    # Starts a Real Time Messaging session.
    resource function get rtm\.connect(map<string|string[]> headers = {}, RtmConnectQueries queries) returns RtmConnectResponse|error;

    # Searches for messages matching a query.
    resource function get search\.messages(map<string|string[]> headers = {}, SearchMessagesQueries queries) returns DefaultSuccessResponse64|error;

    # Adds a star to an item.
    resource function post stars\.add(StarsAddBody payload, map<string|string[]> headers = {}) returns StarsAddResponse|error;

    # Lists stars for a user.
    resource function get stars\.list(map<string|string[]> headers = {}, StarsListQueries queries) returns StarsListResponse|error;

    # Removes a star from an item.
    resource function post stars\.remove(StarsRemoveBody payload, map<string|string[]> headers = {}) returns StarsRemoveResponse|error;

    # Gets the access logs for the current team.
    resource function get team\.accessLogs(map<string|string[]> headers = {}, TeamAccessLogsQueries queries) returns TeamAccessLogsResponse|error;

    # Gets billable users information for the current team.
    resource function get team\.billableInfo(map<string|string[]> headers = {}, TeamBillableInfoQueries queries) returns DefaultSuccessResponse65|error;

    # Gets information about the current team.
    resource function get team\.info(map<string|string[]> headers = {}, TeamInfoQueries queries) returns TeamInfoResponse|error;

    # Gets the integration logs for the current team.
    resource function get team\.integrationLogs(map<string|string[]> headers = {}, TeamIntegrationLogsQueries queries) returns TeamIntegrationLogsResponse|error;

    # Retrieve a team's profile.
    resource function get team\.profile\.get(map<string|string[]> headers = {}, TeamProfileGetQueries queries) returns TeamProfileGetResponse|error;

    # Create a User Group
    resource function post usergroups\.create(UsergroupsCreateBody payload, map<string|string[]> headers = {}) returns UsergroupsCreateResponse|error;

    # Disable an existing User Group
    resource function post usergroups\.disable(UsergroupsDisableBody payload, map<string|string[]> headers = {}) returns UsergroupsDisableResponse|error;

    # Enable a User Group
    resource function post usergroups\.enable(UsergroupsEnableBody payload, map<string|string[]> headers = {}) returns UsergroupsEnableResponse|error;

    # List all User Groups for a team
    resource function get usergroups\.list(map<string|string[]> headers = {}, UsergroupsListQueries queries) returns UsergroupsListResponse|error;

    # Update an existing User Group
    resource function post usergroups\.update(UsergroupsUpdateBody payload, map<string|string[]> headers = {}) returns UsergroupsUpdateResponse|error;

    # List all users in a User Group
    resource function get usergroups\.users\.list(map<string|string[]> headers = {}, UsergroupsUsersListQueries queries) returns UsergroupsUsersListResponse|error;

    # Update the list of users for a User Group
    resource function post usergroups\.users\.update(UsergroupsUsersUpdateBody payload, map<string|string[]> headers = {}) returns UsergroupsUsersUpdateResponse|error;

    # List conversations the calling user may access.
    resource function get users\.conversations(map<string|string[]> headers = {}, UsersConversationsQueries queries) returns UsersConversationsResponse|error;

    # Delete the user profile photo
    resource function post users\.deletePhoto(UsersDeletePhotoBody payload, map<string|string[]> headers = {}) returns UsersDeletePhotoResponse|error;

    # Gets user presence information.
    resource function get users\.getPresence(map<string|string[]> headers = {}, UsersGetPresenceQueries queries) returns APIMethodUsersGetPresence|error;

    # Get a user's identity.
    resource function get users\.identity(map<string|string[]> headers = {}) returns InlineResponseItems2002[]|error;

    # Gets information about a user.
    resource function get users\.info(map<string|string[]> headers = {}, UsersInfoQueries queries) returns UsersInfoResponse|error;

    # Lists all users in a Slack team.
    resource function get users\.list(map<string|string[]> headers = {}, UsersListQueries queries) returns UsersListResponse|error;

    # Find a user with an email address.
    resource function get users\.lookupByEmail(map<string|string[]> headers = {}, UsersLookupByEmailQueries queries) returns UsersLookupByEmailResponse|error;

    # Retrieves a user's profile information.
    resource function get users\.profile\.get(map<string|string[]> headers = {}, UsersProfileGetQueries queries) returns UsersProfileGetResponse|error;

    # Set the profile information for a user.
    resource function post users\.profile\.set(UsersProfileSetBody payload, map<string|string[]> headers = {}) returns UsersProfileSetResponse|error;

    # Marked a user as active. Deprecated and non-functional.
    resource function post users\.setActive(map<string|string[]> headers = {}) returns UsersSetActiveResponse|error;

    # Set the user profile photo
    resource function post users\.setPhoto(UsersSetPhotoBody payload, map<string|string[]> headers = {}) returns UsersSetPhotoResponse|error;

    # Manually sets user presence.
    resource function post users\.setPresence(UsersSetPresenceBody payload, map<string|string[]> headers = {}) returns UsersSetPresenceResponse|error;

    # Open a view for a user.
    resource function get views\.open(map<string|string[]> headers = {}, ViewsOpenQueries queries) returns DefaultSuccessResponse66|error;

    # Publish a static view for a User.
    resource function get views\.publish(map<string|string[]> headers = {}, ViewsPublishQueries queries) returns DefaultSuccessResponse67|error;

    # Push a view onto the stack of a root view.
    resource function get views\.push(map<string|string[]> headers = {}, ViewsPushQueries queries) returns DefaultSuccessResponse68|error;

    # Update an existing view.
    resource function get views\.update(map<string|string[]> headers = {}, ViewsUpdateQueries queries) returns DefaultSuccessResponse69|error;

    # Indicate that an app's step in a workflow completed execution.
    resource function get workflows\.stepCompleted(map<string|string[]> headers = {}, WorkflowsStepCompletedQueries queries) returns DefaultSuccessResponse70|error;

    # Indicate that an app's step in a workflow failed to execute.
    resource function get workflows\.stepFailed(map<string|string[]> headers = {}, WorkflowsStepFailedQueries queries) returns DefaultSuccessResponse71|error;

    # Update the configuration for a workflow extension step.
    resource function get workflows\.updateStep(map<string|string[]> headers = {}, WorkflowsUpdateStepQueries queries) returns DefaultSuccessResponse72|error;
}
