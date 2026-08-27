<!-- bal library client v1 -->
# Clients — ballerinax/twilio `Client`

| | |
|---|---|
| Container | `Client` — 199 remote |
| Showing | 200 by name, no signatures — over the byte budget |

## Next

- narrow it: `bal library client ballerinax/twilio Client -s "<what it does>"` — matches names, paths, parameter and type names, and documentation
- one call and every type it needs: `bal library client ballerinax/twilio Client init -r`
- last resort — every signature, unbudgeted: `bal library client ballerinax/twilio Client --all` — 200 signatures, 51,608 bytes

## 200 by name

```
init                                             listAccount                                      createAccount                                    fetchAccount
updateAccount                                    listAddress                                      createAddress                                    fetchAddress
updateAddress                                    deleteAddress                                    listApplication                                  createApplication
fetchApplication                                 updateApplication                                deleteApplication                                fetchAuthorizedConnectApp
listAuthorizedConnectApp                         listAvailablePhoneNumberCountry                  fetchAvailablePhoneNumberCountry                 listAvailablePhoneNumberLocal
listAvailablePhoneNumberMachineToMachine         listAvailablePhoneNumberMobile                   listAvailablePhoneNumberNational                 listAvailablePhoneNumberSharedCost
listAvailablePhoneNumberTollFree                 listAvailablePhoneNumberVoip                     fetchBalance                                     listCall
createCall                                       fetchCall                                        updateCall                                       deleteCall
listCallEvent                                    fetchCallFeedback                                updateCallFeedback                               createCallFeedbackSummary
fetchCallFeedbackSummary                         deleteCallFeedbackSummary                        fetchCallNotification                            listCallNotification
listCallRecording                                createCallRecording                              fetchCallRecording                               updateCallRecording
deleteCallRecording                              fetchConference                                  updateConference                                 listConference
fetchConferenceRecording                         updateConferenceRecording                        deleteConferenceRecording                        listConferenceRecording
fetchConnectApp                                  updateConnectApp                                 deleteConnectApp                                 listConnectApp
listDependentPhoneNumber                         fetchIncomingPhoneNumber                         updateIncomingPhoneNumber                        deleteIncomingPhoneNumber
listIncomingPhoneNumber                          createIncomingPhoneNumber                        fetchIncomingPhoneNumberAssignedAddOn            deleteIncomingPhoneNumberAssignedAddOn
listIncomingPhoneNumberAssignedAddOn             createIncomingPhoneNumberAssignedAddOn           fetchIncomingPhoneNumberAssignedAddOnExtension   listIncomingPhoneNumberAssignedAddOnExtension
listIncomingPhoneNumberLocal                     createIncomingPhoneNumberLocal                   listIncomingPhoneNumberMobile                    createIncomingPhoneNumberMobile
listIncomingPhoneNumberTollFree                  createIncomingPhoneNumberTollFree                fetchKey                                         updateKey
deleteKey                                        listKey                                          createNewKey                                     fetchMedia
deleteMedia                                      listMedia                                        fetchMember                                      updateMember
listMember                                       listMessage                                      createMessage                                    fetchMessage
updateMessage                                    deleteMessage                                    createMessageFeedback                            listSigningKey
createNewSigningKey                              fetchNotification                                listNotification                                 fetchOutgoingCallerId
updateOutgoingCallerId                           deleteOutgoingCallerId                           listOutgoingCallerId                             createValidationRequest
fetchParticipant                                 updateParticipant                                deleteParticipant                                listParticipant
createParticipant                                createPayments                                   updatePayments                                   fetchQueue
updateQueue                                      deleteQueue                                      listQueue                                        createQueue
fetchRecording                                   deleteRecording                                  listRecording                                    fetchRecordingAddOnResult
deleteRecordingAddOnResult                       listRecordingAddOnResult                         fetchRecordingAddOnResultPayload                 deleteRecordingAddOnResultPayload
listRecordingAddOnResultPayload                  fetchRecordingTranscription                      deleteRecordingTranscription                     listRecordingTranscription
fetchShortCode                                   updateShortCode                                  listShortCode                                    fetchSigningKey
updateSigningKey                                 deleteSigningKey                                 listSipAuthCallsCredentialListMapping            createSipAuthCallsCredentialListMapping
fetchSipAuthCallsCredentialListMapping           deleteSipAuthCallsCredentialListMapping          listSipAuthCallsIpAccessControlListMapping       createSipAuthCallsIpAccessControlListMapping
fetchSipAuthCallsIpAccessControlListMapping      deleteSipAuthCallsIpAccessControlListMapping     listSipAuthRegistrationsCredentialListMapping    createSipAuthRegistrationsCredentialListMapping
fetchSipAuthRegistrationsCredentialListMapping   deleteSipAuthRegistrationsCredentialListMapping  listSipCredential                                createSipCredential
fetchSipCredential                               updateSipCredential                              deleteSipCredential                              listSipCredentialList
createSipCredentialList                          fetchSipCredentialList                           updateSipCredentialList                          deleteSipCredentialList
listSipCredentialListMapping                     createSipCredentialListMapping                   fetchSipCredentialListMapping                    deleteSipCredentialListMapping
listSipDomain                                    createSipDomain                                  fetchSipDomain                                   updateSipDomain
deleteSipDomain                                  listSipIpAccessControlList                       createSipIpAccessControlList                     fetchSipIpAccessControlList
updateSipIpAccessControlList                     deleteSipIpAccessControlList                     fetchSipIpAccessControlListMapping               deleteSipIpAccessControlListMapping
listSipIpAccessControlListMapping                createSipIpAccessControlListMapping              listSipIpAddress                                 createSipIpAddress
fetchSipIpAddress                                updateSipIpAddress                               deleteSipIpAddress                               createSiprec
updateSiprec                                     createStream                                     updateStream                                     createToken
fetchTranscription                               deleteTranscription                              listTranscription                                listUsageRecord
listUsageRecordAllTime                           listUsageRecordDaily                             listUsageRecordLastMonth                         listUsageRecordMonthly
listUsageRecordThisMonth                         listUsageRecordToday                             listUsageRecordYearly                            listUsageRecordYesterday
fetchUsageTrigger                                updateUsageTrigger                               deleteUsageTrigger                               listUsageTrigger
createUsageTrigger                               createUserDefinedMessage                         createUserDefinedMessageSubscription             deleteUserDefinedMessageSubscription
```
