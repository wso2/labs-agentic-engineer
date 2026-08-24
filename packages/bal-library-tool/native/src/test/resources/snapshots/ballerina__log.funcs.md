<!-- bal library funcs v1 -->
# Module functions — ballerina/log

| | |
|---|---|
| Showing | 11 signatures |

## Next

- one call and every type it needs: `bal library funcs ballerina/log evaluateTemplate -r`

## Module-level functions — 11, call with `.`

```ballerina
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
```
