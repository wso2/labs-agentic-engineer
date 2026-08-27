<!-- bal library client v1 -->
# Clients — ballerinax/slack `Client`

| | |
|---|---|
| Container | `Client` — 174 resource |
| Showing | 175 by name, no signatures — over the byte budget |

## Next

- narrow it: `bal library client ballerinax/slack Client -s "<what it does>"` — matches names, paths, parameter and type names, and documentation
- one call and every type it needs: `bal library client ballerinax/slack Client init -r`
- last resort — every signature, unbudgeted: `bal library client ballerinax/slack Client --all` — 175 signatures, 36,493 bytes

## 175 by name

```
init                                                          post admin.apps.approve                                       get admin.apps.approved.list                                  get admin.apps.requests.list
post admin.apps.restrict                                      get admin.apps.restricted.list                                post admin.conversations.archive                              post admin.conversations.convertToPrivate
post admin.conversations.create                               post admin.conversations.delete                               post admin.conversations.disconnectShared                     get admin.conversations.ekm.listOriginalConnectedChannelInfo
get admin.conversations.getConversationPrefs                  get admin.conversations.getTeams                              post admin.conversations.invite                               post admin.conversations.rename
post admin.conversations.restrictAccess.addGroup              get admin.conversations.restrictAccess.listGroups             post admin.conversations.restrictAccess.removeGroup           get admin.conversations.search
post admin.conversations.setConversationPrefs                 post admin.conversations.setTeams                             post admin.conversations.unarchive                            post admin.emoji.add
post admin.emoji.addAlias                                     get admin.emoji.list                                          post admin.emoji.remove                                       post admin.emoji.rename
post admin.inviteRequests.approve                             get admin.inviteRequests.approved.list                        get admin.inviteRequests.denied.list                          post admin.inviteRequests.deny
get admin.inviteRequests.list                                 get admin.teams.admins.list                                   post admin.teams.create                                       get admin.teams.list
get admin.teams.owners.list                                   get admin.teams.settings.info                                 post admin.teams.settings.setDefaultChannels                  post admin.teams.settings.setDescription
post admin.teams.settings.setDiscoverability                  post admin.teams.settings.setIcon                             post admin.teams.settings.setName                             post admin.usergroups.addChannels
post admin.usergroups.addTeams                                get admin.usergroups.listChannels                             post admin.usergroups.removeChannels                          post admin.users.assign
post admin.users.invite                                       get admin.users.list                                          post admin.users.remove                                       post admin.users.session.invalidate
post admin.users.session.reset                                post admin.users.setAdmin                                     post admin.users.setExpiration                                post admin.users.setOwner
post admin.users.setRegular                                   get api.test                                                  get apps.event.authorizations.list                            get apps.permissions.info
get apps.permissions.request                                  get apps.permissions.resources.list                           get apps.permissions.scopes.list                              get apps.permissions.users.list
get apps.permissions.users.request                            get apps.uninstall                                            get auth.revoke                                               get auth.test
get bots.info                                                 post calls.add                                                post calls.end                                                get calls.info
post calls.participants.add                                   post calls.participants.remove                                post calls.update                                             post chat.delete
post chat.deleteScheduledMessage                              get chat.getPermalink                                         post chat.meMessage                                           post chat.postEphemeral
post chat.postMessage                                         post chat.scheduleMessage                                     get chat.scheduledMessages.list                               post chat.unfurl
post chat.update                                              post conversations.archive                                    post conversations.close                                      post conversations.create
get conversations.history                                     get conversations.info                                        post conversations.invite                                     post conversations.join
post conversations.kick                                       post conversations.leave                                      get conversations.list                                        post conversations.mark
get conversations.members                                     post conversations.open                                       post conversations.rename                                     get conversations.replies
post conversations.setPurpose                                 post conversations.setTopic                                   post conversations.unarchive                                  get dialog.open
post dnd.endDnd                                               post dnd.endSnooze                                            get dnd.info                                                  post dnd.setSnooze
get dnd.teamInfo                                              get emoji.list                                                post files.comments.delete                                    post files.delete
get files.info                                                get files.list                                                post files.remote.add                                         get files.remote.info
get files.remote.list                                         post files.remote.remove                                      get files.remote.share                                        post files.remote.update
post files.revokePublicURL                                    post files.sharedPublicURL                                    post files.upload                                             get migration.exchange
get oauth.access                                              get oauth.token                                               get oauth.v2.access                                           post pins.add
get pins.list                                                 post pins.remove                                              post reactions.add                                            get reactions.get
get reactions.list                                            post reactions.remove                                         post reminders.add                                            post reminders.complete
post reminders.delete                                         get reminders.info                                            get reminders.list                                            get rtm.connect
get search.messages                                           post stars.add                                                get stars.list                                                post stars.remove
get team.accessLogs                                           get team.billableInfo                                         get team.info                                                 get team.integrationLogs
get team.profile.get                                          post usergroups.create                                        post usergroups.disable                                       post usergroups.enable
get usergroups.list                                           post usergroups.update                                        get usergroups.users.list                                     post usergroups.users.update
get users.conversations                                       post users.deletePhoto                                        get users.getPresence                                         get users.identity
get users.info                                                get users.list                                                get users.lookupByEmail                                       get users.profile.get
post users.profile.set                                        post users.setActive                                          post users.setPhoto                                           post users.setPresence
get views.open                                                get views.publish                                             get views.push                                                get views.update
get workflows.stepCompleted                                   get workflows.stepFailed                                      get workflows.updateStep
```
