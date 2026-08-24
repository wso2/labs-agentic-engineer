// ============================================================
// Library: ballerina/log
// This module provides APIs to log information when running applications, with support for contextual logging, configurable log levels, formats, destinations, and key-value context.
// ============================================================
import ballerina/log;

// --- Types ---

# Anydata key-value pairs to be displayed in the log.
public type AnydataKeyValues record {
    # msg which cannot be a key
    never msg?;
    # message which cannot be a key
    never message?;
    # time which cannot be a key
    never time?;
    # level which cannot be a key
    never level?;
    # 'error which cannot be a key
    never 'error?;
    # stackTrace which cannot be a key
    never stackTrace?;
    # module name which cannot be a key
    never module?;
    # icp.runtimeId which cannot be a key
    never icp\.runtimeId?;
};

# Configuration for the Ballerina logger
public type Config record {|
    # Optional unique identifier for this logger.
    # All loggers are registered and can be discovered via `getLoggerRegistry()`.
    # If provided, this value is used as the logger's runtime ID (prefixed with the module name);
    # otherwise, an ID is auto-generated from the calling context.
    string id?;
    # Log format to use. Default is the logger format configured in the module level
    LogFormat format = format;
    # Log level to use. Default is the logger level configured in the module level
    Level level = level;
    # List of destinations to log to. Default is the logger destinations configured in the module level
    readonly & OutputDestination[] destinations = destinations;
    # Additional key-value pairs to include in the log messages. Default is the key-values configured in the module level
    readonly & AnydataKeyValues keyValues = {...keyValues};
    # Enable sensitive data masking. Default is the module level configuration
    boolean enableSensitiveDataMasking = enableSensitiveDataMasking;
|};

# A file output destination.
public type FileOutputDestination record {
    # Type of the file destination. Allowed value is "file".
    readonly FILE 'type = FILE;
    # File path(only files with .log extension are supported)
    string path;
    # File output mode
    FileOutputMode mode = APPEND;
    # Log rotation configuration
    RotationConfig rotation?;
};

# Key-value pairs to be displayed in the log.
public type KeyValues record {|
    # msg which cannot be a key
    never msg?;
    # message which cannot be a key
    never message?;
    # time which cannot be a key
    never time?;
    # level which cannot be a key
    never level?;
    # 'error which cannot be a key
    never 'error?;
    # stackTrace which cannot be a key
    never stackTrace?;
    # icp.runtimeId which cannot be a key
    never icp\.runtimeId?;
    Value...;
|};

# Replacement strategy for sensitive data
public type Replacement record {|
    # The replacement value. This can be a string which will be used to replace the
    # entire value, or a function that takes the original value and returns a masked version.
    string|ReplacementFunction replacement;
|};

# Log rotation configuration for file destinations.
public type RotationConfig record {|
    # Rotation policy to use
    RotationPolicy policy = BOTH;
    # Maximum file size in bytes before rotation (used with SIZE_BASED or BOTH policies)
    # Default: 10MB (10 * 1024 * 1024 bytes)
    int maxFileSize = 10485760;
    # Maximum age in seconds before rotation (used with TIME_BASED or BOTH policies)
    # Default: 24 hours (24 * 60 * 60 seconds)
    int maxAge = 86400;
    # Maximum number of backup files to retain. Older files are deleted.
    # Default: 10 backup files
    int maxBackupFiles = 10;
|};

# Represents sensitive data with a masking strategy
public type SensitiveConfig record {|
    # The masking strategy to apply
    MaskingStrategy strategy = EXCLUDE;
|};

# A standard destination.
public type StandardDestination record {|
    # Type of the standard destination. Allowed values are "stderr" and "stdout"
    readonly STDERR|STDOUT 'type = STDERR;
|};

# Represents errors specific to the Log module.
public type Error distinct error;

# Exclude the field from log output
public const string EXCLUDE = "EXCLUDE";

# Output destination types.
public enum DestinationType {
    # Standard error output as the destination
    STDERR,
    # Standard output as the destination
    STDOUT,
    # File output as the destination
    FILE
}

# File output modes.
public enum FileOutputMode {
    # Truncates the file before writing. This mode creates a new file if one doesn't exist.
    # If the file already exists, its contents are cleared, and new data is written
    # from the beginning.
    TRUNCATE,
    # Appends to the existing content. This mode creates a new file if one doesn't exist.
    # If the file already exists, new data is appended to the end of its current contents.
    APPEND
}

# File opening options for writing.
public enum FileWriteOption {
    # Overwrite(truncate the existing content)
    OVERWRITE,
    # Append to the existing content
    APPEND
}

# Log level types.
public enum Level {
    DEBUG,
    ERROR,
    INFO,
    WARN
}

# Supported log formats.
public enum LogFormat {
    # The JSON log format.
    JSON_FORMAT,
    # The Logfmt log format.
    LOGFMT
}

# Log rotation policies.
public enum RotationPolicy {
    # Rotate logs based on file size only
    SIZE_BASED,
    # Rotate logs based on time only
    TIME_BASED,
    # Rotate logs based on both file size and time (whichever condition is met first)
    BOTH
}

# Logger object type defines an interface for logging messages
public type Logger object {
    # Prints debug logs.
    # + msg - The message to be logged
    # + 'error - The error struct to be logged
    # + stackTrace - The error stack trace to be logged
    # + keyValues - The key-value pairs to be logged
    isolated function printDebug(string|PrintableRawTemplate msg, error? 'error = (), error:StackFrame[]? stackTrace = (), *KeyValues keyValues);

    # Prints info logs.
    # + msg - The message to be logged
    # + 'error - The error struct to be logged
    # + stackTrace - The error stack trace to be logged
    # + keyValues - The key-value pairs to be logged
    isolated function printInfo(string|PrintableRawTemplate msg, error? 'error = (), error:StackFrame[]? stackTrace = (), *KeyValues keyValues);

    # Prints warn logs.
    # + msg - The message to be logged
    # + 'error - The error struct to be logged
    # + stackTrace - The error stack trace to be logged
    # + keyValues - The key-value pairs to be logged
    isolated function printWarn(string|PrintableRawTemplate msg, error? 'error = (), error:StackFrame[]? stackTrace = (), *KeyValues keyValues);

    # Prints error logs.
    # + msg - The message to be logged
    # + 'error - The error struct to be logged
    # + stackTrace - The error stack trace to be logged
    # + keyValues - The key-value pairs to be logged
    isolated function printError(string|PrintableRawTemplate msg, error? 'error = (), error:StackFrame[]? stackTrace = (), *KeyValues keyValues);

    # Creates a new child/derived logger with the given key-values.
    # + keyValues - The key-value pairs to be added to the logger context
    # + return - A new Logger instance with the given key-values added to its context
    isolated function withContext(*KeyValues keyValues) returns Logger|error;

    # Returns the effective log level of this logger.
    # For root and custom loggers, returns the explicitly set level.
    # For child loggers (created via `withContext`), returns the inherited level from the parent logger.
    # + return - The effective log level
    isolated function getLevel() returns Level;

    # Sets the log level of this logger at runtime.
    # This is supported on root loggers, module loggers, and loggers created via `fromConfig`.
    # Child loggers (created via `withContext`) do not support this operation and will return an error.
    # To change a child logger's effective level, set the level on its parent logger instead.
    # + level - The new log level to set
    # + return - An error if the operation is not supported, nil on success
    isolated function setLevel(Level level) returns error?;
};

# Provides access to the logger registry for discovering and managing registered loggers.
public type LoggerRegistry object {
    # Returns the IDs of all registered loggers.
    # + return - An array of logger IDs
    isolated function getIds() returns string[];

    # Returns a logger by its registered ID.
    # + id - The logger ID to look up
    # + return - The Logger instance if found, nil otherwise
    isolated function getById(string id) returns Logger?;
};

# Raw templates for logging.
# 
# e.g: `The input value is ${val}`
public type PrintableRawTemplate readonly object {
    *object:RawTemplate;
    # String values of the template as an array
    public string[] & readonly strings;
    # Parameterized values/expressions after evaluations as an array
    public Value[] insertions;
};

# A value that can be of type `anydata`, a function pointer, or a raw template.
public type Value anydata|Valuer|PrintableRawTemplate;

# Log output destination.
public type OutputDestination StandardDestination|FileOutputDestination;

# Masking strategy for sensitive data
public type MaskingStrategy EXCLUDE|Replacement;

# A function that returns a value of type `anydata`.
# Useful in scenarios where computation is required to retrieve the value.
# This function is executed only if the specific log level is enabled.
public type Valuer isolated function () returns anydata;

# Replacement function type for sensitive data masking
public type ReplacementFunction isolated function (string) returns string;

// --- Functions ---

# Evaluates the raw template and returns the evaluated string.
# + template - The raw template to be evaluated
# + enableSensitiveDataMasking - Flag to indicate if sensitive data masking is enabled
# + return - The evaluated string
public isolated function evaluateTemplate(PrintableRawTemplate template, boolean enableSensitiveDataMasking = false) returns string;

# Creates a new logger with the given configuration.
# + config - The configuration to use for the new logger
# + return - The newly created logger
public isolated function fromConfig(*Config config) returns Logger|Error;

# Returns the logger registry for discovering and managing registered loggers.
# + return - The LoggerRegistry instance
public isolated function getLoggerRegistry() returns LoggerRegistry;

# Prints debug logs.
# ```ballerina
# log:printDebug("debug message", id = 845315)
# ```
# + msg - The message to be logged
# + 'error - The error struct to be logged
# + stackTrace - The error stack trace to be logged
# + keyValues - The key-value pairs to be logged
public isolated function printDebug(string|PrintableRawTemplate msg, error? 'error = (), error:StackFrame[]? stackTrace = (), *KeyValues keyValues);

# Prints error logs.
# ```ballerina
# error e = error("error occurred");
# log:printError("error log with cause", 'error = e, id = 845315);
# ```
# + msg - The message to be logged
# + 'error - The error struct to be logged
# + stackTrace - The error stack trace to be logged
# + keyValues - The key-value pairs to be logged
public isolated function printError(string|PrintableRawTemplate msg, error? 'error = (), error:StackFrame[]? stackTrace = (), *KeyValues keyValues);

# Prints info logs.
# ```ballerina
# log:printInfo("info message", id = 845315)
# ```
# + msg - The message to be logged
# + 'error - The error struct to be logged
# + stackTrace - The error stack trace to be logged
# + keyValues - The key-value pairs to be logged
public isolated function printInfo(string|PrintableRawTemplate msg, error? 'error = (), error:StackFrame[]? stackTrace = (), *KeyValues keyValues);

# Prints warn logs.
# ```ballerina
# log:printWarn("warn message", id = 845315)
# ```
# + msg - The message to be logged
# + 'error - The error struct to be logged
# + stackTrace - The error stack trace to be logged
# + keyValues - The key-value pairs to be logged
public isolated function printWarn(string|PrintableRawTemplate msg, error? 'error = (), error:StackFrame[]? stackTrace = (), *KeyValues keyValues);

# Process the raw template and return the processed string.
# + template - The raw template to be processed
# + return - The processed string
@deprecated
public isolated function processTemplate(PrintableRawTemplate template) returns string;

# Returns the root logger instance.
# + return - The root logger instance
public isolated function root() returns Logger;

# Sets the log output to a file. All subsequent logs of the entire application will be written to this file.
# ```ballerina
# var result = log:setOutputFile("./resources/myfile.log");
# var result = log:setOutputFile("./resources/myfile.log", log:OVERWRITE);
# ```
# + path - The file path to write the logs. Should be a file with `.log` extension
# + option - The file write option. Default is `APPEND`
# + return - A `log:Error` if an invalid file path was provided
# # Deprecated
# Setting output file destination using this method is deprecated. 
# Add the output file path as part of the `destinations` configurable instead.
@deprecated
public isolated function setOutputFile(string path, FileWriteOption option = APPEND) returns Error?;

# Returns a masked string representation of the given data based on the sensitive data masking annotation.
# This method panics if a cyclic value reference is encountered.
# + data - The data to be masked
# + return - The masked string representation of the data
public isolated function toMaskedString(anydata data) returns string;

// --- Annotations ---

# Marks a record field or type as sensitive, excluding it from log output
# The default strategy is to exclude the field from log output
public annotation SensitiveConfig Sensitive on record field;

// --- Configurables ---

// Set in the CALLER's Config.toml, under a [ballerina.log] table. These are
// module-private, so they cannot be referenced from code — a default above that a signature
// also names is one you set here rather than pass.

// format = LOGFMT    # LogFormat — Root logger default log format.

// level = INFO    # Level — Root logger default log level.

// modules = table []    # table<Module> & readonly — Modules with their log levels.

// keyValues = {}    # AnydataKeyValues & readonly — Default key-values to add to the root logger.

// destinations = [{'type: STDERR}]    # readonly & OutputDestination[] — A list of file destinations or standard output/error.

// enableSensitiveDataMasking = false    # boolean
