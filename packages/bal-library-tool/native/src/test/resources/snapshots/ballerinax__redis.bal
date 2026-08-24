// ============================================================
// Library: ballerinax/redis
// [Redis](https://redis.io/) is an open-source, in-memory data structure store that can be used as a database, cache, and message broker. It supports various data structures such as strings, hashes, lists, sets, and more.
// ============================================================
import ballerinax/redis;

// --- Types ---

# Represents a combination of certificate, private key, and private key password if encrypted.
public type CertKey record {|
    # File containing the certificate
    string certFile;
    # File containing the private key in PKCS8 format
    string keyFile;
    # Password of the private key if it is encrypted
    string keyPassword?;
|};

# The client endpoint configuration for Redis.
public type ConnectionConfig record {|
    # Connection configurations of the Redis server. This can be either a single URI or a set of parameters
    ConnectionUri|ConnectionParams connection = "redis://localhost:6379";
    # Flag to indicate whether connection pooling is enabled
    boolean connectionPooling = false;
    # Flag to indicate whether the connection is a cluster connection
    boolean isClusterConnection = false;
    # Configurations related to SSL/TLS encryption
    SecureSocket secureSocket?;
|};

# The connection parameters based configurations.
public type ConnectionParams record {|
    # Host address of the Redis database
    string host = "localhost";
    # Port of the Redis database
    int port = 6379;
    # The username for the Redis database
    string username?;
    # The password for the Redis database
    string password?;
    # Other connection options of the connection configuration
    Options options = {};
|};

# TCP keep-alive configuration for detecting stale connections.
public type KeepAliveConfig record {|
    # Time in seconds the connection must be idle before the first keep-alive probe is sent
    int idle = 7200;
    # Time in seconds between individual keep-alive probes
    int interval = 75;
    # Maximum number of keep-alive probes before the connection is considered dead
    int count = 9;
|};

# Connection options for Redis client endpoint.
public type Options record {|
    # Name of the client
    string clientName?;
    # Database index which the client should interact with. Not applicable for cluster connections
    int database = 0;
    # Connection timeout in seconds
    int connectionTimeout = 60;
    # TCP keep-alive configuration for detecting stale connections.
    # Set to `()` (nil) to disable. Default is `()` (disabled).
    KeepAliveConfig? keepAlive = ();
|};

# Configurations for secure communication with the Redis server.
public type SecureSocket record {|
    # Configurations associated with `crypto:TrustStore` or single certificate file that the client trusts
    crypto:TrustStore|string cert?; // Special Agent Note: TrustStore FROM ballerina/crypto module
    # Configurations associated with `crypto:KeyStore` or combination of certificate and private key of the client
    crypto:KeyStore|CertKey key?; // Special Agent Note: KeyStore FROM ballerina/crypto module
    # List of protocols used for the connection established to Redis Server, such as TLSv1.2, TLSv1.1, TLSv1.
    string[] protocols?;
    # List of ciphers to be used for SSL connections
    string[] ciphers?;
    # The SSL/TLS verification mode. This can be either NONE, CA, or FULL.
    SslVerifyMode verifyMode = FULL;
    # Whether StartTLS is enabled
    boolean startTls = false;
|};

# The redis Connection URI based configurations. This can become useful when working with 
# managed Redis databases, where the cloud provider usually provides a connection URI.
public type ConnectionUri string;

# Represents a redis generic error
public type Error distinct error;

# Represents the SSL/TLS verification mode.
public enum SslVerifyMode {
    # No verification
    NONE,
    # Verify the server's certificate against the provided CA certificates
    CA,
    # Verify the server's certificate against the provided CA certificates and also verify the server's hostname
    FULL
}

// --- Client ---

# Ballerina Redis connector provides the capability to access Redis cache.
# This connector lets you to perform operations to access and manipulate key-value data stored in a Redis database.
public isolated client class Client {
    # Initialize the Redis client.
    # + config - configuration for the connector
    # + return - `redis:Error` in case of failures or `nil` if successful.
    isolated function init(*ConnectionConfig config) returns Error?;

    # Append a value to a key.
    # + key - Key referring to a value
    # + value - String value to be appended
    # + return - Length of the string after the operation
    isolated remote function append(string key, string value) returns int|Error;

    # Count set bits in a string.
    # + key - Key referring to a value
    # + return - Number of bits of the value
    isolated remote function bitCount(string key) returns int|Error;

    # Perform bitwise AND between strings.
    # + destination - Result key of the operation
    # + keys - Input keys to perform AND between
    # + return - Size of the string stored in the destination key, that is equal to the size of the longest input
    # string
    isolated remote function bitOpAnd(string destination, string[] keys) returns int|Error;

    # Perform bitwise OR between strings.
    # + destination - Result key of the operation
    # + keys - Input keys to perform OR between
    # + return - Size of the string stored in the destination key, that is equal to the size of the longest input
    # string or `redis:Error` if an error occurs
    isolated remote function bitOpOr(string destination, string[] keys) returns int|Error;

    # Perform bitwise NOT on a string.
    # + destination - Result key of the operation
    # + key - Input key to perform NOT
    # + return - Size of the string stored in the destination key or `redis:Error` if an error occurs
    isolated remote function bitOpNot(string destination, string key) returns int|Error;

    # Perform bitwise XOR between strings.
    # + destination - Result key of the operation
    # + keys - Input keys to perform XOR between
    # + return - Size of the string stored in the destination key, that is equal to the size of the longest input
    # string or `redis:Error` if an error occurs
    isolated remote function bitOpXor(string destination, string[] keys) returns int|Error;

    # Decrement integer value of a key by one.
    # + key - Key referring to a value
    # + return - Value of key after the decrement
    isolated remote function decr(string key) returns int|Error;

    # Decrement integer value of a key by the given number.
    # + key - Key referring to a value
    # + value - Value to be decremented
    # + return - Value of key after decrement or `redis:Error` if an error occurs
    isolated remote function decrBy(string key, int value) returns int|Error;

    # Returns bit value at offset in the string value stored at key.
    # + key - Key referring to a value
    # + offset - Offset in string value
    # + return - Bit value stored at offset or `redis:Error` if an error occurs
    isolated remote function getBit(string key, int offset) returns int|Error;

    # Get substring of string stored at a key.
    # + key - Key referring to a value
    # + startPos - Starting point of substring
    # + end - End point of substring
    # + return - Substring or `redis:Error` if an error occurs
    isolated remote function getRange(string key, int startPos, int end) returns string|Error;

    # Set string value of key and return its existing value.
    # + key - Key referring to a value
    # + value - Value to be set
    # + return - Existing value stored at key, or nil when key does not exist or `redis:Error` if an error occurs
    isolated remote function getSet(string key, string value) returns string|Error?;

    # Get value of key.
    # + key - Key referring to a value
    # + return - Value of key, or nil when key does not exist or `redis:Error` if an error occurs
    isolated remote function get(string key) returns string|Error?;

    # Increment integer value of a key by one.
    # + key - Key referring to a value
    # + return - Value of key after increment
    isolated remote function incr(string key) returns int|Error;

    # Increment integer value of key by the given amount.
    # + key - Key referring to a value
    # + value - Amount to increment
    # + return - Value of key after increment
    isolated remote function incrBy(string key, int value) returns int|Error;

    # Increment integer value of key by the given float.
    # + key - Key referring to a value
    # + value - Amount to increment
    # + return - Value of key after increment
    isolated remote function incrByFloat(string key, float value) returns float|Error;

    # Get values of all given keys. Fails with an `Error` if any of the given keys does not
    # exist, since the returned type cannot represent a missing value. Use `mGetOptional` instead
    # if any of the given keys might not exist.
    # + keys - Keys of which values need to be retrieved
    # + return - Array of values at specified keys
    isolated remote function mGet(string[] keys) returns string[]|Error;

    # Get values of all given keys. Unlike `mGet`, a key that does not exist is represented as
    # `()` in the returned array.
    # + keys - Keys of which values need to be retrieved
    # + return - Array of values at specified keys, with `()` for a missing key
    isolated remote function mGetOptional(string[] keys) returns string?[]|Error;

    # Set multiple keys to multiple values.
    # + keyValueMap - Map of key-value pairs to be set
    # + return - String with value `OK` if the operation was successful
    isolated remote function mSet(map<any> keyValueMap) returns string|Error;

    # Set multiple keys to multiple values, only if none of the keys exist.
    # + keyValueMap - Map of key-value pairs to be set
    # + return - True if the operation was successful, false if it failed
    isolated remote function mSetNx(map<any> keyValueMap) returns boolean|Error;

    # Set value and expiration in milliseconds of a key.
    # + key - Key referring to a value
    # + value - Value to be set
    # + expirationTime - Expiration time in milli seconds
    # + return - `OK` if successful or `redis:Error` if an error occurs
    isolated remote function pSetEx(string key, string value, int expirationTime) returns string|Error;

    # Set the value of a key.
    # + key - Key referring to a value
    # + value - Values
    # + return - `OK` if successful
    isolated remote function set(string key, string value) returns string|Error;

    # Sets or clears the bit at offset in the string value stored at key.
    # + key - Key referring to a value
    # + value - Value to be set
    # + offset - Offset at which the value should be set
    # + return - Original bit value stored at offset or `redis:Error` if an error occurs
    isolated remote function setBit(string key, int value, int offset) returns int|Error;

    # Set the value and expiration of a key.
    # + key - Key referring to a value
    # + value - Value to be set
    # + expirationTime - Expiration time to be set, in seconds
    # + return - On success `OK` or `redis:Error` if an error occurs
    isolated remote function setEx(string key, string value, int expirationTime) returns string|Error;

    # Set value of a key, only if key does not exist.
    # + key - Key referring to a value
    # + value - Value to be set
    # + return - `True` if exist `False` if not or `redis:Error` if an error occurs
    isolated remote function setNx(string key, string value) returns boolean|Error;

    # Set the value and expiration of a key, only if the key does not exist. Combines `setNx` and `setEx`
    # into a single atomic operation (`SET key value NX EX expirationTime`).
    # + key - Key referring to a value
    # + value - Value to be set
    # + expirationTime - Expiration time to be set, in seconds
    # + return - `True` if the key was set, `False` if the key already existed, or `redis:Error` if an error occurs
    isolated remote function setNxEx(string key, string value, int expirationTime) returns boolean|Error;

    # Overwrite part of string at key starting at the specified offset.
    # + key - Key referring to a value
    # + offset - Offset at which the value should be set
    # + value - Value to be set
    # + return - Length of the string after it was modified or `redis:Error` if an error occurs
    isolated remote function setRange(string key, int offset, string value) returns int|Error;

    # Get length of value stored in a key.
    # + key - Key referring to a value
    # + return - Length of string at key, or 0 when key does not exist or `redis:Error` if an error occurs
    isolated remote function strLen(string key) returns int|Error;

    # Prepend one or multiple values to list.
    # + key - Key referring to a value
    # + values - Values to be prepended
    # + return - Length of list after the push operation(s) or `redis:Error` if an error occurs
    isolated remote function lPush(string key, string[] values) returns int|Error;

    # Remove and get the first element in a list.
    # + key - Key referring to a value
    # + return - Value of the first element, or nil when key does not exist or `redis:Error` if an error occurs
    isolated remote function lPop(string key) returns string|Error?;

    # Prepend one or multiple values to a list, only if the list exists.
    # + key - Key referring to a value
    # + values - Values to be prepended
    # + return - Length of the list after the push operation(s)
    isolated remote function lPushX(string key, string[] values) returns int|Error;

    # Remove and get the first element in a list, or block until one is available.
    # + timeOut - Timeout in seconds
    # + keys - Keys referring to values
    # + return - `Nil` when no element could be popped and the timeout expired. A map containing one item, with the
    # key being  the name of the key where an element was popped and the second element  being the value of the
    # popped element, or `redis:Error` if an error occurs
    isolated remote function bLPop(int timeOut, string[] keys) returns map<any>|Error;

    # Remove and get the last element in a list, or block until one is available.
    # + timeout - Timeout in seconds
    # + keys - Keys referring to values
    # + return - `nil` when no element could be popped and the timeout expired. A map containing one item, with the
    # key being  the name of the key where an element was popped and the second element being the value of the
    # popped element, or `redis:Error` if an error occurs
    isolated remote function bRPop(int timeout, string[] keys) returns map<any>|Error;

    # Get an element from list by its index.
    # + key - Key referring to a value
    # + index - Index from which the element should be retrieved
    # + return - Value at the given index
    isolated remote function lIndex(string key, int index) returns string|Error?;

    # Insert an element before or after another element in a list.
    # + key - Key referring to a value
    # + before - Boolean value representing Whether element should be inserted before or after the pivot
    # + pivot - Pivot position
    # + value - Value to insert
    # + return - Length of the list after the insert operation, or -1 when the value pivot not found, or `redis:Error` if
    # an error occurs
    isolated remote function lInsert(string key, boolean before, string pivot, string value) returns int|Error;

    # Get length of a list.
    # + key - Key referring to a value
    # + return - Length of list at key or `redis:Error` if an error occurs
    isolated remote function lLen(string key) returns int|Error;

    # Get a range of elements from a list.
    # + key - Key referring to a value
    # + startPos - Begining index of the range
    # + stopPos - Last index of the range
    # + return - Array of elements in the specified range or `redis:Error` if an error occurs
    isolated remote function lRange(string key, int startPos, int stopPos) returns string[]|Error;

    # Remove elements from list.
    # + key - Key referring to a value
    # + count - Number of elements to be removed
    # + value - Value which the elements to be removed should be equal to
    # + return - Number of elements removed or `redis:Error` if an error occurs
    isolated remote function lRem(string key, int count, string value) returns int|Error;

    # Set the value of an element in a list by its index.
    # + key - Key of the list
    # + index - Index of the element of which the value needs to be set
    # + value - Value to be set
    # + return - String with the value `OK` if the operation was successful or `redis:Error` if an error occurs
    isolated remote function lSet(string key, int index, string value) returns string|Error;

    # Trim list to the specified range.
    # + key - Key of the list
    # + startPos - Starting index of the range
    # + stopPos - End index of the range
    # + return - String with the value `OK` if the operation was successful
    isolated remote function lTrim(string key, int startPos, int stopPos) returns string|Error;

    # Remove and get the last element in a list.
    # + key - Key of the list
    # + return - Value of the last element, or `nil` when key does not exist or `redis:Error` if an error occurs
    isolated remote function rPop(string key) returns string|Error?;

    # Remove the last element in a list, append it to another list and return it.
    # + src - Source key
    # + destination - Destination key
    # + return - Element being popped and pushed or `redis:Error` if an error occurs
    isolated remote function rPopLPush(string src, string destination) returns string|Error;

    # Append one or multiple values to a list.
    # + key - Key of the list
    # + values - Array of values to be appended
    # + return - Length of the list after the push operation or `redis:Error` if an error occurs
    isolated remote function rPush(string key, string[] values) returns int|Error;

    # Append one or multiple values to a list, only if the list exists.
    # + key - Key of the list
    # + values - Array of values to be appended
    # + return - Length of the list after the push operation or `redis:Error` if an error occurs
    isolated remote function rPushX(string key, string[] values) returns int|Error;

    # Add one or more members to a set.
    # + key - Key of the set
    # + values - Array of values to be added
    # + return - Number of elements that were added to the set, not including all the elements which were
    # already present in the set, or `redis:Error` if an error occurs
    isolated remote function sAdd(string key, string[] values) returns int|Error;

    # Get the number of members in a set
    # + key - Key of the set
    # + return - Cardinality (number of elements) of the set or `redis:Error` if an error occurs
    isolated remote function sCard(string key) returns int|Error;

    # Return set resulting from the difference between the first set and all the successive sets
    # + keys - The keys of the sets
    # + return - An array of members of the resulting set or `redis:Error` if an error occurs
    isolated remote function sDiff(string[] keys) returns string[]|Error;

    # Obtain the set resulting from the difference between the first set and all the successive.
    # sets and store at the provided destination.
    # + destination - Destination key of the resulting set
    # + keys - Keys of the sets to find the difference of
    # + return - Number of members in the resulting set or `redis:Error` if an error occurs
    isolated remote function sDiffStore(string destination, string[] keys) returns int|Error;

    # Return the intersection of the provided sets.
    # + keys - Keys of the sets to be intersected
    # + return - Array of members of the resulting set or `redis:Error` if an error occurs
    isolated remote function sInter(string[] keys) returns string[]|Error;

    # Obtain the intersection of the provided sets and store at the provided destination.
    # + destination - Destination key of the resulting set
    # + keys - Keys of the sets to be intersected
    # + return - Number of members of the resulting set or `redis:Error` if an error occurs
    isolated remote function sInterStore(string destination, string[] keys) returns int|Error;

    # Determine if a given value is a member of a set.
    # + key - Key of the set
    # + value - Value of a key
    # + return - boolean true/false depending on whether the value is a member of the set or not, or `redis:Error` if an error
    # occurs
    isolated remote function sIsMember(string key, string value) returns boolean|Error;

    # Get all members in a set.
    # + key - Key of the set
    # + return - Array of all members in the set or `redis:Error` if an error occurs
    isolated remote function sMembers(string key) returns string[]|Error;

    # Move a member from one set to another.
    # + src - Source key
    # + destination - Destination key
    # + member - Member to be moved
    # + return - `True` if the element is moved. `false` if the element is not a member of source and no
    # operation was performed or `redis:Error` if an error occurs
    isolated remote function sMove(string src, string destination, string member) returns boolean|Error;

    # Remove and return a random member from a set.
    # + key - Source key
    # + count - Number of members to pop
    # + return - Array of removed elements or `nil` if key does not exist or `redis:Error` if an error occurs
    isolated remote function sPop(string key, int count) returns string[]|Error?;

    # Get one or multiple random members from a set.
    # + key - Key of the set
    # + count - Number of members to obtain
    # + return - Array of the randomly selected elements, or `nil` when key does not exist or `redis:Error` if an error occurs
    isolated remote function sRandMember(string key, int count) returns string[]|Error;

    # Remove one or more members from a set.
    # + key - Key of the set
    # + members - Array of members to remove
    # + return - Number of members that were removed from the set, not including non existing members or `redis:Error` if
    # an error occurs
    isolated remote function sRem(string key, string[] members) returns int|Error;

    # Return the union of multiple sets.
    # + keys - Array of keys of sets
    # + return - Array of members of the resulting set or `redis:Error` if an error occurs
    isolated remote function sUnion(string[] keys) returns string[]|Error;

    # Return the union of multiple sets.
    # + destination - Destination key of the resulting set
    # + keys - Array of keys of sets
    # + return - Number of members of the resulting set or `redis:Error` if an error occurs
    isolated remote function sUnionStore(string destination, string[] keys) returns int|Error;

    # Add one or more members to a sorted set, or update its score if it already exist.
    # + key - Key of the sorted set
    # + memberScoreMap - Map of members and corresponding scores
    # + return - Number of elements that were added to the sorted set, not including all the elements which were
    # already present in the set for which the score was updated, or `redis:Error` if an error occurs
    isolated remote function zAdd(string key, map<any> memberScoreMap) returns int|Error;

    # Get the number of members in a sorted set.
    # + key - Key of the sorted set
    # + return - Cardinality (number of elements) of the sorted set or `redis:Error` if an error occurs
    isolated remote function zCard(string key) returns int|Error;

    # Count the members in a sorted set with scores within the given range.
    # + key - Key of the sorted set
    # + min - Minimum score of the range
    # + max - Maximum score of the range
    # + return - Number of elements in the specified score range or `redis:Error` if an error occurs
    isolated remote function zCount(string key, float min, float max) returns int|Error;

    # Increment the score of a member in a sorted set.
    # + key - Key of the sorted set
    # + amount - Amount to increment
    # + member - Member whose score to be incremented
    # + return - New score of the member or `redis:Error` if an error occurs
    isolated remote function zIncrBy(string key, float amount, string member) returns float|Error;

    # Intersect multiple sorted sets and store the resulting sorted set in a new key.
    # + destination - Destination key of the resulting sorted set
    # + keys - Keys of the sorted sets to be intersected
    # + return - Number of elements in the resulting sorted set or `redis:Error` if an error occurs
    isolated remote function zInterStore(string destination, string[] keys) returns int|Error;

    # Count the members in a sorted set within the given lexicographical range.
    # + key - Key of the sorted set
    # + min - Minimum lexicographical value of the range
    # + max - Maximum lexicographical value of the range
    # + return - Number of elements in the specified lexicographical value range or `redis:Error` if an error occurs
    isolated remote function zLexCount(string key, string min, string max) returns int|Error;

    # Return a range of members in a sorted set, by index.
    # + key - Key of the sorted set
    # + min - Minimum index of the range
    # + max - Maximum index of the range
    # + return - Range of members in a sorted set, by index, or `redis:Error` if an error occurs
    isolated remote function zRange(string key, int min, int max) returns string[]|Error;

    # Return a range of members in a sorted set, by lexicographical range from lowest to highest.
    # + key - Key of the sorted set
    # + min - Minimum lexicographical value of the range
    # + max - Maximum lexicographical value of the range
    # + return - Array of members in the specified lexicographical value range ordered from lowest to highest or `redis:Error`
    # if an error occurs
    isolated remote function zRangeByLex(string key, string min, string max) returns string[]|Error;

    # Return a range of members in a sorted set, by lexicographical range ordered from highest to
    # lowest.
    # + key - Key of the sorted set
    # + min - Lexicographical value of the range
    # + max - Maximum lexicographical value of the range
    # + return - Array of members in the specified lexicographical value range ordered from highest to lowest or `redis:Error`
    # if an error occurs
    isolated remote function zRevRangeByLex(string key, string min, string max) returns string[]|Error;

    # Return a range of members in a sorted set, by score from lowest to highest.
    # + key - Key of sorted set
    # + min - Minimum score of range
    # + max - Maximum score of range
    # + return - Array of members in the specified score range ordered from lowest to highest or `redis:Error` if an error
    # occurs
    isolated remote function zRangeByScore(string key, float min, float max) returns string[]|Error;

    # Determine index of a member in a sorted set.
    # + key - Key of the sorted set
    # + member - Member of which the index needs to be obtained
    # + return - Index of the member or `redis:Error` if an error occurs
    isolated remote function zRank(string key, string member) returns int|Error;

    # Remove one or more members from a sorted set
    # + key - Key of the sorted set
    # + members - Members to be removed
    # + return - Number of members removed from the sorted set, not including non existing members or `redis:Error` if an
    # error occurs
    isolated remote function zRem(string key, string[] members) returns int|Error;

    # Remove all members in a sorted set between the given lexicographical range.
    # + key - Key of the sorted set
    # + min - Minimum lexicographical value of the range
    # + max - Maximum lexicographical value of the range
    # + return - Number of members removed from the sorted set or `redis:Error` if an error occurs
    isolated remote function zRemRangeByLex(string key, string min, string max) returns int|Error;

    # Remove all members in a sorted set within the given indices.
    # + key - Key of the sorted set
    # + min - Minimum index of the range
    # + max - Maximum index of the range
    # + return - Number of members removed from the sorted set or `redis:Error` if an error occurs
    isolated remote function zRemRangeByRank(string key, int min, int max) returns int|Error;

    # Remove all members in a sorted set within the given scores.
    # + key - Key of the sorted set
    # + min - Minimum score of the range
    # + max - Maximum score of the range
    # + return - Number of members removed from the sorted set or `redis:Error` if an error occurs
    isolated remote function zRemRangeByScore(string key, float min, float max) returns int|Error;

    # Return a range of members in a sorted set, by index, ordered highest to lowest.
    # + key - Key of the sorted set
    # + min - Minimum index of the range
    # + max - Maximum index of the range
    # + return - Number of elements in the specified index range or `redis:Error` if an error occurs
    isolated remote function zRevRange(string key, int min, int max) returns string[]|Error;

    # Return a range of members in a sorted set, by score from highest to lowest.
    # + key - Key of the sorted set
    # + min - Minimum score of the range
    # + max - Maximum score of the range
    # + return - Array of members in the specified score range ordered from highest to lowest or `redis:Error` if an error
    # occurs
    isolated remote function zRevRangeByScore(string key, float min, float max) returns string[]|Error;

    # Determine the index of a member in a sorted set
    # + key - Key of the sorted set
    # + member - Member of which the index needs to be obtained
    # + return - Index of the member or `redis:Error` if an error occurs
    isolated remote function zRevRank(string key, string member) returns int|Error;

    # Determine the score of a member in a sorted set
    # + key - Key of the sorted set
    # + member - Member of which the score needs to be obtained
    # + return - Score of the member or `redis:Error` if an error occurs
    isolated remote function zScore(string key, string member) returns float|Error;

    # Return the union of multiple sorted sets
    # + destination - Destination key of the resulting set
    # + keys - Array of keys of sorted sets
    # + return - Number of members of the resulting sorted set or `redis:Error` if an error occurs
    isolated remote function zUnionStore(string destination, string[] keys) returns int|Error;

    # Delete one or more hash fields.
    # + key - Key of the hash
    # + fields - Array of fields to be deleted
    # + return - Number of fields that were removed from the hash, not including specified but non existing fields or
    # `redis:Error` if an error occurs
    isolated remote function hDel(string key, string[] fields) returns int|Error;

    # Determine if a hash field exists.
    # + key - Key of the hash
    # + return - Boolean `true` if the hash contains the field. boolean false if the hash does not contain
    # field or key does not exist or `redis:Error` if an error occurs
    isolated remote function hExists(string key, string 'field) returns boolean|Error;

    # Get the value of a hash field.
    # + key - Key of the hash
    # + return - Value of the field or `redis:Error` if an error occurs
    isolated remote function hGet(string key, string 'field) returns string|Error;

    # Get the all values of a hash.
    # + key - Key of the hash
    # + return - Map of field-value pairs or `redis:Error` if an error occurs
    isolated remote function hGetAll(string key) returns map<any>|Error;

    # Increment the integer value of a hash field by the given number.
    # + key - Key of the hash
    # + amount - Amount to increment
    # + return - Value of the field or `redis:Error` if an error occurs
    isolated remote function hIncrBy(string key, string 'field, int amount) returns int|Error;

    # Increment the float value of a hash field by the given number.
    # + key - Key of the hash
    # + amount - Amount to increment
    # + return - Value of the field or `redis:Error` if an error occurs
    isolated remote function hIncrByFloat(string key, string 'field, float amount) returns float|Error;

    # Get all the fields in a hash.
    # + key - Key of the hash
    # + return - Array of hash fields or `redis:Error` if an error occurs
    isolated remote function hKeys(string key) returns string[]|Error;

    # Get the number of fields in a hash.
    # + key - Key of the hash
    # + return - Number of fields or `redis:Error` if an error occurs
    isolated remote function hLen(string key) returns int|Error;

    # Get the values of all the given hash fields.
    # + key - Key of the hash
    # + fields - Array of hash fields
    # + return - Map of field-value pairs or `redis:Error` if an error occurs
    isolated remote function hMGet(string key, string[] fields) returns map<any>|Error;

    # Set multiple hash fields to multiple values.
    # + key - Key of the hash
    # + fieldValueMap - Map of field-value pairs
    # + return - String with the value `OK` if the operation was successful, or `redis:Error` if an error occurs
    isolated remote function hMSet(string key, map<any> fieldValueMap) returns string|Error;

    # Set the string value of a hash field.
    # + key - Key of the hash
    # + value - Value to be set to the field
    # + return - Boolean `true` if field is a new field in the hash and value was set. boolean false if
    # field already exists in the hash and the value was updated, or `redis:Error` if an error occurs
    isolated remote function hSet(string key, string 'field, string value) returns boolean|Error;

    # Set the string value of a hash field, only if the field does not exist.
    # + key - Key of the hash
    # + value - Value to be set to the field
    # + return - Boolean `true` if field is a new field in the hash and value was set. boolean false if
    # field already exists in the hash and no operation was performed, or `redis:Error` if an error occurs
    isolated remote function hSetNx(string key, string 'field, string value) returns boolean|Error;

    # Get the string length of the field value in a hash.
    # + key - Key of the hash
    # + return - Length of the field value, or 0 when field is not present in the hash or key does
    # not exist at all, or `redis:Error` if an error occurs
    isolated remote function hStrLen(string key, string 'field) returns int|Error;

    # Get all the values in a hash.
    # + key - Key of the hash
    # + return - Array of values in the hash, or an empty array when key does not exist or `redis:Error` if an error occurs
    isolated remote function hVals(string key) returns string[]|Error;

    # Delete one or more keys.
    # + keys - Key to be deleted
    # + return - Number of keys that were removed
    isolated remote function del(string[] keys) returns int|Error;

    # Determine how many keys exist.
    # + keys - Keys of which existence to be found out
    # + return - Number of existing keys or `redis:Error` if an error occurs
    isolated remote function exists(string[] keys) returns int|Error;

    # Set a key's time to live in seconds.
    # + key - Keys of which expiry time to be set
    # + seconds - Expiry in seconds
    # + return - Boolean `true` if the timeout was set. false if key does not exist or the timeout could not be set or
    # `redis:Error` if an error occurs
    isolated remote function expire(string key, int seconds) returns boolean|Error;

    # Find all keys matching the given pattern.
    # + pattern - Pattern to match
    # + return - Array of keys matching the given pattern or `redis:Error` if an error occurs
    isolated remote function keys(string pattern) returns string[]|Error;

    # Move a key to another database.
    # + key - Key to be moved
    # + database - Database to which the key needs to be moved
    # + return - Boolean true if key was succesfully moved, boolean false otherwise or `redis:Error` if an error occurs
    isolated remote function move(string key, int database) returns boolean|Error;

    # Remove the expiration from a key.
    # + key - Key of which expiry time should be removed
    # + return - Boolean `true` if the timeout was removed. boolean `false` if key does not exist or does not have
    # an associated timeout, or `redis:Error` if an error occurs
    isolated remote function persist(string key) returns boolean|Error;

    # Set a key's time to live in milliseconds.
    # + key - Key of which expiry time should be removed
    # + expirationTime - Expiry time in milli seconds
    # + return - Boolean `true` if the timeout was set. boolean false if key does not exist or the timeout could not
    # be set, or `redis:Error` if an error occurs
    isolated remote function pExpire(string key, int expirationTime) returns boolean|Error;

    # Get the time to live for a key in milliseconds.
    # + key - Key of which time-to-live should be obtained
    # + return - TTL of the key, in milli seconds or `redis:Error` if an error occurs
    isolated remote function pTtl(string key) returns int|Error;

    # Return a random key from the keyspace.
    # + return - Random key, or `nil` when the database is empty or `redis:Error` if an error occurs
    isolated remote function randomKey() returns string|Error?;

    # Rename a key.
    # + key - Key to be renamed
    # + newName - New name of the key
    # + return - String with the value `OK` if the operation was successful or `redis:Error` if an error occurs
    isolated remote function rename(string key, string newName) returns string|Error;

    # Rename a key, only if the new key does not exist.
    # + key - Key to be renamed
    # + newName - New name of the key
    # + return - Boolean `true` if key was renamed to newkey. boolean `false` if newkey already exists. Or `redis:Error` if an
    # error occurs
    isolated remote function renameNx(string key, string newName) returns boolean|Error;

    # Sort elements in a list, set or sorted set.
    # + key - Key of the data type to be sorted
    # + return - Sorted array containing the members of the sorted data type or `redis:Error` if an error occurs
    isolated remote function sort(string key) returns string[]|Error;

    # Get the time to live for a key.
    # + key - Key of which the time to live needs to be obtained
    # + return - Time to live in seconds or a negative value/`redis:Error` in order to signal an error in evaluating ttl.
    # Whether it is a negative value of an `redis:Error` would differ depending on whether the error occurs at DB
    # level or the driver level
    isolated remote function ttl(string key) returns int|Error;

    # Determine the type stored at key.
    # + key - Key of which the type needs to be obtained
    # + return - Type stored at key
    isolated remote function redisType(string key) returns string|Error;

    # Retrieve information and statistics about the cluster observed by the current node.
    # This command is exclusively available in cluster mode. If the connection is in a non-clustered mode,
    # the API will return a `redis:Error`. Other errors will also be appropriately handled.
    # + return - a bulk-string-reply as a string array or, 
    # a `redis:Error` if the connection is non-clustered or encounters any other errors.
    isolated remote function clusterInfo() returns string[]|Error;

    # Ping the server.
    # + return - String with the value `PONG` if the operation was successful
    isolated remote function ping() returns string|Error;

    # Authenticate to the server.
    # + password - Password to authenticate
    # + return - String with the value `OK` if the operation was successful or `redis:Error` if an error occurs
    isolated remote function auth(string password) returns string|Error;

    # Echo the given string.
    # + message - Message to be echo-ed
    # + return - Message itself if the operation was successful or `redis:Error` if an error occurs
    isolated remote function echo(string message) returns string|Error;

    # Remove all the keys from the currently selected database.
    # + return - String with the value `OK` if the operation was successful or `redis:Error` if an error occurs
    isolated remote function flushDb() returns string|Error;

    # Remove all the keys from all the databases.
    # + return - String with the value `OK` if the operation was successful or `redis:Error` if an error occurs
    isolated remote function flushAll() returns string|Error;

    # Close the connection.
    # + return - `nil` if the operation was successful or an `redis:Error` if an error occurs
    isolated function close() returns Error?;
}
