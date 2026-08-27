<!-- bal library client v1 -->
# Clients — ballerinax/redis `Client`

| | |
|---|---|
| Container | `Client` — 111 remote, 1 normal |
| Showing | 113 signatures |

## Next

- one call and every type it needs: `bal library client ballerinax/redis Client init -r`

## Constructor — 1

```ballerina
# Initialize the Redis client.
isolated function init(*ConnectionConfig config) returns Error?;
```

## Remote functions — 111, call with `->`

```ballerina
# Append a value to a key.
isolated remote function append(string key, string value) returns int|Error;

# Count set bits in a string.
isolated remote function bitCount(string key) returns int|Error;

# Perform bitwise AND between strings.
isolated remote function bitOpAnd(string destination, string[] keys) returns int|Error;

# Perform bitwise OR between strings.
isolated remote function bitOpOr(string destination, string[] keys) returns int|Error;

# Perform bitwise NOT on a string.
isolated remote function bitOpNot(string destination, string key) returns int|Error;

# Perform bitwise XOR between strings.
isolated remote function bitOpXor(string destination, string[] keys) returns int|Error;

# Decrement integer value of a key by one.
isolated remote function decr(string key) returns int|Error;

# Decrement integer value of a key by the given number.
isolated remote function decrBy(string key, int value) returns int|Error;

# Returns bit value at offset in the string value stored at key.
isolated remote function getBit(string key, int offset) returns int|Error;

# Get substring of string stored at a key.
isolated remote function getRange(string key, int startPos, int end) returns string|Error;

# Set string value of key and return its existing value.
isolated remote function getSet(string key, string value) returns string|Error?;

# Get value of key.
isolated remote function get(string key) returns string|Error?;

# Increment integer value of a key by one.
isolated remote function incr(string key) returns int|Error;

# Increment integer value of key by the given amount.
isolated remote function incrBy(string key, int value) returns int|Error;

# Increment integer value of key by the given float.
isolated remote function incrByFloat(string key, float value) returns float|Error;

# Get values of all given keys. Fails with an `Error` if any of the given keys does not
# exist, since the returned type cannot represent a missing value. Use `mGetOptional` instead
# if any of the given keys might not exist.
isolated remote function mGet(string[] keys) returns string[]|Error;

# Get values of all given keys. Unlike `mGet`, a key that does not exist is represented as
# `()` in the returned array.
isolated remote function mGetOptional(string[] keys) returns string?[]|Error;

# Set multiple keys to multiple values.
isolated remote function mSet(map<any> keyValueMap) returns string|Error;

# Set multiple keys to multiple values, only if none of the keys exist.
isolated remote function mSetNx(map<any> keyValueMap) returns boolean|Error;

# Set value and expiration in milliseconds of a key.
isolated remote function pSetEx(string key, string value, int expirationTime) returns string|Error;

# Set the value of a key.
isolated remote function set(string key, string value) returns string|Error;

# Sets or clears the bit at offset in the string value stored at key.
isolated remote function setBit(string key, int value, int offset) returns int|Error;

# Set the value and expiration of a key.
isolated remote function setEx(string key, string value, int expirationTime) returns string|Error;

# Set value of a key, only if key does not exist.
isolated remote function setNx(string key, string value) returns boolean|Error;

# Set the value and expiration of a key, only if the key does not exist. Combines `setNx` and `setEx`
# into a single atomic operation (`SET key value NX EX expirationTime`).
isolated remote function setNxEx(string key, string value, int expirationTime) returns boolean|Error;

# Overwrite part of string at key starting at the specified offset.
isolated remote function setRange(string key, int offset, string value) returns int|Error;

# Get length of value stored in a key.
isolated remote function strLen(string key) returns int|Error;

# Prepend one or multiple values to list.
isolated remote function lPush(string key, string[] values) returns int|Error;

# Remove and get the first element in a list.
isolated remote function lPop(string key) returns string|Error?;

# Prepend one or multiple values to a list, only if the list exists.
isolated remote function lPushX(string key, string[] values) returns int|Error;

# Remove and get the first element in a list, or block until one is available.
isolated remote function bLPop(int timeOut, string[] keys) returns map<any>|Error;

# Remove and get the last element in a list, or block until one is available.
isolated remote function bRPop(int timeout, string[] keys) returns map<any>|Error;

# Get an element from list by its index.
isolated remote function lIndex(string key, int index) returns string|Error?;

# Insert an element before or after another element in a list.
isolated remote function lInsert(string key, boolean before, string pivot, string value) returns int|Error;

# Get length of a list.
isolated remote function lLen(string key) returns int|Error;

# Get a range of elements from a list.
isolated remote function lRange(string key, int startPos, int stopPos) returns string[]|Error;

# Remove elements from list.
isolated remote function lRem(string key, int count, string value) returns int|Error;

# Set the value of an element in a list by its index.
isolated remote function lSet(string key, int index, string value) returns string|Error;

# Trim list to the specified range.
isolated remote function lTrim(string key, int startPos, int stopPos) returns string|Error;

# Remove and get the last element in a list.
isolated remote function rPop(string key) returns string|Error?;

# Remove the last element in a list, append it to another list and return it.
isolated remote function rPopLPush(string src, string destination) returns string|Error;

# Append one or multiple values to a list.
isolated remote function rPush(string key, string[] values) returns int|Error;

# Append one or multiple values to a list, only if the list exists.
isolated remote function rPushX(string key, string[] values) returns int|Error;

# Add one or more members to a set.
isolated remote function sAdd(string key, string[] values) returns int|Error;

# Get the number of members in a set
isolated remote function sCard(string key) returns int|Error;

# Return set resulting from the difference between the first set and all the successive sets
isolated remote function sDiff(string[] keys) returns string[]|Error;

# Obtain the set resulting from the difference between the first set and all the successive.
# sets and store at the provided destination.
isolated remote function sDiffStore(string destination, string[] keys) returns int|Error;

# Return the intersection of the provided sets.
isolated remote function sInter(string[] keys) returns string[]|Error;

# Obtain the intersection of the provided sets and store at the provided destination.
isolated remote function sInterStore(string destination, string[] keys) returns int|Error;

# Determine if a given value is a member of a set.
isolated remote function sIsMember(string key, string value) returns boolean|Error;

# Get all members in a set.
isolated remote function sMembers(string key) returns string[]|Error;

# Move a member from one set to another.
isolated remote function sMove(string src, string destination, string member) returns boolean|Error;

# Remove and return a random member from a set.
isolated remote function sPop(string key, int count) returns string[]|Error?;

# Get one or multiple random members from a set.
isolated remote function sRandMember(string key, int count) returns string[]|Error;

# Remove one or more members from a set.
isolated remote function sRem(string key, string[] members) returns int|Error;

# Return the union of multiple sets.
isolated remote function sUnion(string[] keys) returns string[]|Error;

# Return the union of multiple sets.
isolated remote function sUnionStore(string destination, string[] keys) returns int|Error;

# Add one or more members to a sorted set, or update its score if it already exist.
isolated remote function zAdd(string key, map<any> memberScoreMap) returns int|Error;

# Get the number of members in a sorted set.
isolated remote function zCard(string key) returns int|Error;

# Count the members in a sorted set with scores within the given range.
isolated remote function zCount(string key, float min, float max) returns int|Error;

# Increment the score of a member in a sorted set.
isolated remote function zIncrBy(string key, float amount, string member) returns float|Error;

# Intersect multiple sorted sets and store the resulting sorted set in a new key.
isolated remote function zInterStore(string destination, string[] keys) returns int|Error;

# Count the members in a sorted set within the given lexicographical range.
isolated remote function zLexCount(string key, string min, string max) returns int|Error;

# Return a range of members in a sorted set, by index.
isolated remote function zRange(string key, int min, int max) returns string[]|Error;

# Return a range of members in a sorted set, by lexicographical range from lowest to highest.
isolated remote function zRangeByLex(string key, string min, string max) returns string[]|Error;

# Return a range of members in a sorted set, by lexicographical range ordered from highest to
# lowest.
isolated remote function zRevRangeByLex(string key, string min, string max) returns string[]|Error;

# Return a range of members in a sorted set, by score from lowest to highest.
isolated remote function zRangeByScore(string key, float min, float max) returns string[]|Error;

# Determine index of a member in a sorted set.
isolated remote function zRank(string key, string member) returns int|Error;

# Remove one or more members from a sorted set
isolated remote function zRem(string key, string[] members) returns int|Error;

# Remove all members in a sorted set between the given lexicographical range.
isolated remote function zRemRangeByLex(string key, string min, string max) returns int|Error;

# Remove all members in a sorted set within the given indices.
isolated remote function zRemRangeByRank(string key, int min, int max) returns int|Error;

# Remove all members in a sorted set within the given scores.
isolated remote function zRemRangeByScore(string key, float min, float max) returns int|Error;

# Return a range of members in a sorted set, by index, ordered highest to lowest.
isolated remote function zRevRange(string key, int min, int max) returns string[]|Error;

# Return a range of members in a sorted set, by score from highest to lowest.
isolated remote function zRevRangeByScore(string key, float min, float max) returns string[]|Error;

# Determine the index of a member in a sorted set
isolated remote function zRevRank(string key, string member) returns int|Error;

# Determine the score of a member in a sorted set
isolated remote function zScore(string key, string member) returns float|Error;

# Return the union of multiple sorted sets
isolated remote function zUnionStore(string destination, string[] keys) returns int|Error;

# Delete one or more hash fields.
isolated remote function hDel(string key, string[] fields) returns int|Error;

# Determine if a hash field exists.
isolated remote function hExists(string key, string 'field) returns boolean|Error;

# Get the value of a hash field.
isolated remote function hGet(string key, string 'field) returns string|Error;

# Get the all values of a hash.
isolated remote function hGetAll(string key) returns map<any>|Error;

# Increment the integer value of a hash field by the given number.
isolated remote function hIncrBy(string key, string 'field, int amount) returns int|Error;

# Increment the float value of a hash field by the given number.
isolated remote function hIncrByFloat(string key, string 'field, float amount) returns float|Error;

# Get all the fields in a hash.
isolated remote function hKeys(string key) returns string[]|Error;

# Get the number of fields in a hash.
isolated remote function hLen(string key) returns int|Error;

# Get the values of all the given hash fields.
isolated remote function hMGet(string key, string[] fields) returns map<any>|Error;

# Set multiple hash fields to multiple values.
isolated remote function hMSet(string key, map<any> fieldValueMap) returns string|Error;

# Set the string value of a hash field.
isolated remote function hSet(string key, string 'field, string value) returns boolean|Error;

# Set the string value of a hash field, only if the field does not exist.
isolated remote function hSetNx(string key, string 'field, string value) returns boolean|Error;

# Get the string length of the field value in a hash.
isolated remote function hStrLen(string key, string 'field) returns int|Error;

# Get all the values in a hash.
isolated remote function hVals(string key) returns string[]|Error;

# Delete one or more keys.
isolated remote function del(string[] keys) returns int|Error;

# Determine how many keys exist.
isolated remote function exists(string[] keys) returns int|Error;

# Set a key's time to live in seconds.
isolated remote function expire(string key, int seconds) returns boolean|Error;

# Find all keys matching the given pattern.
isolated remote function keys(string pattern) returns string[]|Error;

# Move a key to another database.
isolated remote function move(string key, int database) returns boolean|Error;

# Remove the expiration from a key.
isolated remote function persist(string key) returns boolean|Error;

# Set a key's time to live in milliseconds.
isolated remote function pExpire(string key, int expirationTime) returns boolean|Error;

# Get the time to live for a key in milliseconds.
isolated remote function pTtl(string key) returns int|Error;

# Return a random key from the keyspace.
isolated remote function randomKey() returns string|Error?;

# Rename a key.
isolated remote function rename(string key, string newName) returns string|Error;

# Rename a key, only if the new key does not exist.
isolated remote function renameNx(string key, string newName) returns boolean|Error;

# Sort elements in a list, set or sorted set.
isolated remote function sort(string key) returns string[]|Error;

# Get the time to live for a key.
isolated remote function ttl(string key) returns int|Error;

# Determine the type stored at key.
isolated remote function redisType(string key) returns string|Error;

# Retrieve information and statistics about the cluster observed by the current node.
# This command is exclusively available in cluster mode. If the connection is in a non-clustered mode,
# the API will return a `redis:Error`. Other errors will also be appropriately handled.
isolated remote function clusterInfo() returns string[]|Error;

# Ping the server.
isolated remote function ping() returns string|Error;

# Authenticate to the server.
isolated remote function auth(string password) returns string|Error;

# Echo the given string.
isolated remote function echo(string message) returns string|Error;

# Remove all the keys from the currently selected database.
isolated remote function flushDb() returns string|Error;

# Remove all the keys from all the databases.
isolated remote function flushAll() returns string|Error;
```

## Normal functions — 1, call with `.`

```ballerina
# Close the connection.
isolated function close() returns Error?;
```
