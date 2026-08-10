<!-- bal-library overview v1 -->
# ballerinax/slack 0.0.0-fixture

| | |
|---|---|
| Source | central |
| Clients | `Client` |
| Module functions | none |
| Errors | none declared; operations return the language-level `error` |
| Types | 490 declarations (436 records, 24 unions, 30 other), not listed here — read one with `type` |

## Next

- `bal-library ops ballerinax/slack <path>` — navigate a client's operations
- `bal-library type ballerinax/slack <Name> [--deps]` — read a declaration whole
- `bal-library api ballerinax/slack` — every declaration, when nothing above answered

## Client `Client`

One way to interact with the Slack platform is its HTTP RPC-based Web API, a collection of methods requiring OAuth 2.0-based user, bot, or workspace tokens blessed with related OAuth scopes.

### Constructor

```ballerina
function init(ConnectionConfig config, string serviceUrl = "https://slack.com/api") returns error?;
```

### Resource functions — 174, call with `->` and a path

**Not listed here** — 174 operations, 34,525 bytes of signatures, over the 100-operation limit. Top-level path segments, with the number of operations under each:

```
admin.apps.approve 1                                        admin.apps.approved.list 1                                  admin.apps.requests.list 1                                  admin.apps.restrict 1
admin.apps.restricted.list 1                                admin.conversations.archive 1                               admin.conversations.convertToPrivate 1                      admin.conversations.create 1
admin.conversations.delete 1                                admin.conversations.disconnectShared 1                      admin.conversations.ekm.listOriginalConnectedChannelInfo 1  admin.conversations.getConversationPrefs 1
admin.conversations.getTeams 1                              admin.conversations.invite 1                                admin.conversations.rename 1                                admin.conversations.restrictAccess.addGroup 1
admin.conversations.restrictAccess.listGroups 1             admin.conversations.restrictAccess.removeGroup 1            admin.conversations.search 1                                admin.conversations.setConversationPrefs 1
admin.conversations.setTeams 1                              admin.conversations.unarchive 1                             admin.emoji.add 1                                           admin.emoji.addAlias 1
admin.emoji.list 1                                          admin.emoji.remove 1                                        admin.emoji.rename 1                                        admin.inviteRequests.approve 1
admin.inviteRequests.approved.list 1                        admin.inviteRequests.denied.list 1                          admin.inviteRequests.deny 1                                 admin.inviteRequests.list 1
admin.teams.admins.list 1                                   admin.teams.create 1                                        admin.teams.list 1                                          admin.teams.owners.list 1
admin.teams.settings.info 1                                 admin.teams.settings.setDefaultChannels 1                   admin.teams.settings.setDescription 1                       admin.teams.settings.setDiscoverability 1
admin.teams.settings.setIcon 1                              admin.teams.settings.setName 1                              admin.usergroups.addChannels 1                              admin.usergroups.addTeams 1
admin.usergroups.listChannels 1                             admin.usergroups.removeChannels 1                           admin.users.assign 1                                        admin.users.invite 1
admin.users.list 1                                          admin.users.remove 1                                        admin.users.session.invalidate 1                            admin.users.session.reset 1
admin.users.setAdmin 1                                      admin.users.setExpiration 1                                 admin.users.setOwner 1                                      admin.users.setRegular 1
api.test 1                                                  apps.event.authorizations.list 1                            apps.permissions.info 1                                     apps.permissions.request 1
apps.permissions.resources.list 1                           apps.permissions.scopes.list 1                              apps.permissions.users.list 1                               apps.permissions.users.request 1
apps.uninstall 1                                            auth.revoke 1                                               auth.test 1                                                 bots.info 1
calls.add 1                                                 calls.end 1                                                 calls.info 1                                                calls.participants.add 1
calls.participants.remove 1                                 calls.update 1                                              chat.delete 1                                               chat.deleteScheduledMessage 1
chat.getPermalink 1                                         chat.meMessage 1                                            chat.postEphemeral 1                                        chat.postMessage 1
chat.scheduledMessages.list 1                               chat.scheduleMessage 1                                      chat.unfurl 1                                               chat.update 1
conversations.archive 1                                     conversations.close 1                                       conversations.create 1                                      conversations.history 1
conversations.info 1                                        conversations.invite 1                                      conversations.join 1                                        conversations.kick 1
conversations.leave 1                                       conversations.list 1                                        conversations.mark 1                                        conversations.members 1
conversations.open 1                                        conversations.rename 1                                      conversations.replies 1                                     conversations.setPurpose 1
conversations.setTopic 1                                    conversations.unarchive 1                                   dialog.open 1                                               dnd.endDnd 1
dnd.endSnooze 1                                             dnd.info 1                                                  dnd.setSnooze 1                                             dnd.teamInfo 1
emoji.list 1                                                files.comments.delete 1                                     files.delete 1                                              files.info 1
files.list 1                                                files.remote.add 1                                          files.remote.info 1                                         files.remote.list 1
files.remote.remove 1                                       files.remote.share 1                                        files.remote.update 1                                       files.revokePublicURL 1
files.sharedPublicURL 1                                     files.upload 1                                              migration.exchange 1                                        oauth.access 1
oauth.token 1                                               oauth.v2.access 1                                           pins.add 1                                                  pins.list 1
pins.remove 1                                               reactions.add 1                                             reactions.get 1                                             reactions.list 1
reactions.remove 1                                          reminders.add 1                                             reminders.complete 1                                        reminders.delete 1
reminders.info 1                                            reminders.list 1                                            rtm.connect 1                                               search.messages 1
stars.add 1                                                 stars.list 1                                                stars.remove 1                                              team.accessLogs 1
team.billableInfo 1                                         team.info 1                                                 team.integrationLogs 1                                      team.profile.get 1
usergroups.create 1                                         usergroups.disable 1                                        usergroups.enable 1                                         usergroups.list 1
usergroups.update 1                                         usergroups.users.list 1                                     usergroups.users.update 1                                   users.conversations 1
users.deletePhoto 1                                         users.getPresence 1                                         users.identity 1                                            users.info 1
users.list 1                                                users.lookupByEmail 1                                       users.profile.get 1                                         users.profile.set 1
users.setActive 1                                           users.setPhoto 1                                            users.setPresence 1                                         views.open 1
views.publish 1                                             views.push 1                                                views.update 1                                              workflows.stepCompleted 1
workflows.stepFailed 1                                      workflows.updateStep 1
```

174 operations across 174 top-level segments, and none of them nests — the list above is every operation. Nothing to descend into.

- navigate: `bal-library ops ballerinax/slack <segment>`
- signatures under a path: `bal-library ops ballerinax/slack '<path>' --sigs`

## Guide

*The package's own readme, verbatim, with its headings demoted two levels.*

#### Overview

[Slack](https://slack.com/) is a collaboration platform for teams that offers real-time messaging, file sharing, and integration with various tools. it helps streamline communication and enhance productivity through organized channels and direct messaging.

The Slack connector offers APIs to connect and interact with Slack REST API endpoints, enabling seamless management of messages, channels, and other workspace resources.

##### Key Features

- Send and manage messages in channels and direct messages
- Create and manage Slack channels and conversations
- Support for Slack REST API endpoints
- Secure integration with Slack workspace via OAuth2

#### Setup guide

##### Step 1: Sign in to Slack

1. To use the Slack Connector you need to be signed in to [Slack](https://slack.com/). If you haven't created an account already, you can create it [here](https://slack.com/get-started#/createnew).

    <img src=https://raw.githubusercontent.com/ballerina-platform/module-ballerinax-slack/master/docs/setup/resources/sign-in.png alt="Sign-In Page" style="width: 70%;">

##### Step 2: Create a new Slack application

1. Navigate to your apps in [Slack API](https://api.slack.com/) and create a new Slack app.

    <img src=https://raw.githubusercontent.com/ballerina-platform/module-ballerinax-slack/master/docs/setup/resources/create-slack-app.png alt="Create Slack App" style="width: 70%;">

2. Provide an app name and choose a workspace of your choice.

    <img src=https://raw.githubusercontent.com/ballerina-platform/module-ballerinax-slack/master/docs/setup/resources/create-slack-app-2.png alt="Create Slack App Popup" style="width: 70%;">

3. Click on the "Create App" button.

##### Step 3: Add scopes to the token

1. Once the application is created, go to the "Add Features and Functionality" section and click on "Permissions" to set the token scopes.

    <img src=https://raw.githubusercontent.com/ballerina-platform/module-ballerinax-slack/master/docs/setup/resources/add-features.png alt="Add features and functionality" style="width: 70%;">

2. In the **User Token Scopes** section set the following token scopes.

    <img src=https://raw.githubusercontent.com/ballerina-platform/module-ballerinax-slack/master/docs/setup/resources/token-permissions.png alt="User Token Scopes" style="width: 70%;">

3. Install the application to workspace.

    <img src=https://raw.githubusercontent.com/ballerina-platform/module-ballerinax-slack/master/docs/setup/resources/install-workspace.jpg alt="Install to workspace" style="width: 70%;">

4. Copy the OAuth token that is generated upon installation.

    <img src=https://raw.githubusercontent.com/ballerina-platform/module-ballerinax-slack/master/docs/setup/resources/copy-token.jpg alt="Copy token" style="width: 70%;">


#### Quickstart

To use the `slack` connector in your Ballerina application, modify the `.bal` file as follows:

##### Step 1: Import the module

Import the `slack` module.

```ballerina
import ballerinax/slack;
```

##### Step 2: Instantiate a new connector

Assign the OAuth token obtained to the variable **token**, and then initialize a new instance of the slack client by passing the token.

```ballerina
configurable string token = ?;

slack:Client slack = check new({
    auth: {
        token
    }
});
```

##### Step 3: Invoke the connector operation

Now, utilize the available connector operations.

###### Send a Text Message to General Channel

```ballerina
slack:ChatPostMessageResponse postMessageResponse = check slack->/chat\.postMessage.post({channel: "general", text: "hello"});
```

##### Step 4: Run the Ballerina application

```bash
bal run
```

#### Examples

The `Slack` connector provides practical examples illustrating usage in various scenarios. Explore these [examples](https://github.com/ballerina-platform/module-ballerinax-slack/tree/master/examples), covering the following use cases:

1. [Automated Summary Report](https://github.com/ballerina-platform/module-ballerinax-slack/tree/master/examples/automated-summary-report) - This use case demonstrates how the Slack API can be utilized to generate a summarized report of daily stand up chats in the general channel.

2. [Survey Feedback Analysis](https://github.com/ballerina-platform/module-ballerinax-slack/tree/master/examples/survey-feedback-analysis) - This use case demonstrates how the Slack API can be utilized to perform a company-wide survey by creating a dedicated channel to receive and track feedback replies.
