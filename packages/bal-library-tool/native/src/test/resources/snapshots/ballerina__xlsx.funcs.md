<!-- bal library funcs v1 -->
# Module functions — ballerina/xlsx

| | |
|---|---|
| Showing | 6 signatures |

## Next

- one call and every type it needs: `bal library funcs ballerina/xlsx fromBytes -r`

## Module-level functions — 6, call with `.`

```ballerina
# Open an XLSX workbook from an in-memory byte array.
# 
# The workbook has no associated file; use `saveAs(path)` to persist it.
# + sourceBytes - XLSX content as a byte array
# + return - The opened workbook, or an error if the bytes are invalid
public isolated function fromBytes(byte[] sourceBytes) returns Workbook|Error;

# Open an XLSX workbook from a file path.
# 
# To create a new file, use `new` and then `saveAs(path)`.
# + path - Path to the XLSX file
# + return - The opened workbook, or an error if the path is missing or the file is invalid
public isolated function fromFile(string path) returns Workbook|Error;

# Parse a sheet from an XLSX file into records, maps, or a string grid.
# 
# ```ballerina
# Employee[] employees = check xlsx:parseSheet("employees.xlsx");
# ```
# + path - Path to the XLSX file
# + sheet - Sheet name or 0-based index (default: 0, the first sheet)
# + options - Parse options
# + t - Target row type
# + return - Parsed rows or an error
public isolated function parseSheet(string path, string|int sheet = 0, ParseOptions options = {}, typedesc<Row> t = <>) returns t[]|Error;

# Parse an Excel table by name into records, maps, or a string grid.
# 
# Table names are unique across the workbook, so no sheet is needed. Headers and any totals
# row are excluded.
# 
# ```ballerina
# Employee[] employees = check xlsx:parseTable("sales.xlsx", "EmployeeTable");
# ```
# + path - Path to the XLSX file
# + tableName - Name of the table to parse
# + options - Table parse options
# + t - Target row type
# + return - Parsed rows or an error such as `TableNotFoundError`
public isolated function parseTable(string path, string tableName, TableParseOptions options = {}, typedesc<Row> t = <>) returns t[]|Error;

# Write rows to a sheet in an XLSX file, creating the file if it does not exist.
# 
# Only the named sheet is affected; other sheets, their tables, and formulas are preserved.
# By default the write fails if the sheet already exists.
# 
# ```ballerina
# Employee[] employees = [{name: "John", age: 30}];
# check xlsx:writeSheet(employees, "staff.xlsx", "Employees");
# ```
# + data - Rows to write (records, maps, or string arrays)
# + path - Path to the XLSX file
# + sheetName - Target sheet name (default: "Sheet1")
# + options - Write options
# + return - An error if the write fails, or if the sheet exists under FAIL_IF_EXISTS
public isolated function writeSheet(Row[] data, string path, string sheetName = "Sheet1", *SheetWriteOptions options) returns Error?;

# Write rows to an existing Excel table, resizing its data range to fit.
# 
# By default the table's data is replaced; `tableWriteMode = APPEND` adds rows below it instead.
# 
# ```ballerina
# check xlsx:writeTable(employees, "sales.xlsx", "EmployeeTable");
# ```
# + data - Rows to write (records, maps, or string arrays)
# + path - Path to the XLSX file containing the table
# + tableName - Name of the table to write to
# + options - Table write options
# + return - A `TableNotFoundError`, a `TableOverlapError` if a resize collides, or another error
public isolated function writeTable(Row[] data, string path, string tableName, *TableWriteOptions options) returns Error?;
```
