// ============================================================
// Library: ballerina/xlsx
// This module provides functionality to read and write Microsoft Excel files in the XLSX format with type-safe data binding to Ballerina records. It exposes a simple file-based API (`parseSheet` / `writeSheet`) for single-sheet ETL and an object-based Workbook API for multi-sheet operations, byte-array I/O, and Excel Tables.
// ============================================================
import ballerina/xlsx;

// --- Types ---

# A rectangular cell range in a sheet, with all indices 0-based.
public type CellRange record {|
    # First row index (0-based)
    int firstRowIndex;
    # Last row index (0-based)
    int lastRowIndex;
    # First column index (0-based)
    int firstColumnIndex;
    # Last column index (0-based)
    int lastColumnIndex;
|};

# Options for reading a single column (`Sheet.getColumn`).
public type ColumnParseOptions record {|
    *CommonSheetParseOptions;
    # Maximum number of cells to read (default: all)
    int rowCount?;
|};

# Read options shared by every read operation; the common base of all read-option records.
public type CommonParseOptions record {|
    # How to read formula cells (default: CACHED)
    FormulaMode formulaMode = CACHED;
    # Match headers case-insensitively, so "Name" matches field `name` (default: false)
    boolean caseInsensitiveHeaders = false;
|};

# Read options for sheets, which address rows by absolute position.
public type CommonSheetParseOptions record {|
    *CommonParseOptions;
    # 0-based row holding the column headers (default: 0). Set to `null` for no headers, which
    # names columns `col0`, `col1`, and so on. Ignored when reading into `string[][]`.
    int? headerRowIndex = 0;
    # 0-based row where data begins (default: `headerRowIndex` + 1, or 0 when there are no headers)
    int dataStartRowIndex?;
|};

# Data projection for record and map reads.
# 
# As `{}` (default) extra columns are ignored; as `false` every record field must have a column.
public type DataProjection record {|
    # Treat nil cells as absent for optional fields (default: false)
    boolean nilAsOptionalField = false;
    # Allow missing columns for nilable fields (default: false)
    boolean absentAsNilableType = false;
|};

# Details attached to an XLSX error.
public type ErrorDetails record {|
    # Sheet where the error occurred
    string sheetName?;
    # Table where the error occurred
    string tableName?;
    # Cell address where the error occurred
    string cellAddress?;
    # Row number where the error occurred
    int rowNumber?;
    # Column number where the error occurred
    int columnNumber?;
    # Record field involved in the error
    string fieldName?;
|};

# Fail-safe parsing: skip and log row-level errors instead of failing the read.
# Structural errors (corrupted file, missing sheet, header errors) always fail immediately.
public type FailSafeOptions record {|
    # Log errors to the console (default: true)
    boolean enableConsoleLogs = true;
    # Include the offending row data in console output (default: false)
    boolean includeSourceDataInConsole = false;
    # Also write errors to a file
    FileOutputMode fileOutputMode?;
|};

# File-based error logging, written to when set in `FailSafeOptions`.
public type FileOutputMode record {|
    # Path to the error log file (required)
    string filePath;
    # What to include in each log entry (default: METADATA)
    ErrorLogContentType contentType = METADATA;
    # How to write the log file (default: APPEND)
    FileWriteOption fileWriteOption = APPEND;
|};

# Location within an XLSX file where an error occurred.
public type Location record {|
    # Row number (1-based, as displayed in Excel)
    int row;
    # Column number (1-based)
    int column;
|};

# The structured JSON written to error log files.
public type LogOutput record {|
    # ISO 8601 timestamp of the error
    string time?;
    # Row and column where the error occurred
    Location location?;
    # The error message
    string message?;
    # The offending row data (only with RAW_AND_METADATA)
    string offendingRow?;
|};

# Maps a record field to an Excel column header when the two names differ.
# Applies on both read and write; fields without it use the field name as the header.
public type NameConfig record {|
    # The Excel column header to map to this field
    string value;
|};

# Options for bulk sheet reads (`parseSheet`, `Sheet.getRows`).
public type ParseOptions record {|
    *CommonSheetParseOptions;
    # Maximum number of data rows to read (default: all)
    int rowCount?;
    # Validate parsed records against their `@constraint` annotations (default: true)
    boolean enableConstraintValidation = true;
    # Data projection: `{}` ignores extra columns (default), `false` requires an exact match
    DataProjection|false allowDataProjection = {};
    # Skip and log row-level errors instead of failing the read
    FailSafeOptions failSafe?;
|};

# Options for reading a single sheet row (`Sheet.getRow`). Fail-fast: no `failSafe` or `rowCount`.
public type RowParseOptions record {|
    *CommonSheetParseOptions;
    # Validate the parsed record against its `@constraint` annotations (default: true)
    boolean enableConstraintValidation = true;
    # Data projection: `{}` ignores extra columns (default), `false` requires an exact match
    DataProjection|false allowDataProjection = {};
|};

# Options for writing a single row (`Sheet.setRow`).
public type RowWriteOptions record {|
    # 0-based row holding the headers, used to align a record or map by name (default: 0).
    # Ignored for `string[]` data, which is written positionally.
    int headerRowIndex = 0;
    # How to treat existing content at the target row (default: `REPLACE`)
    SheetWriteMode sheetWriteMode = REPLACE;
|};

# Options for `writeSheet`.
public type SheetWriteOptions record {|
    # Write a header row from field names or map keys (default: true; ignored for `string[][]`)
    boolean writeHeaders = true;
    # 0-based row where a fresh write starts (default: 0). Used by `FAIL_IF_EXISTS` and `REPLACE`;
    # ignored by `APPEND`, which writes below the existing data.
    int startRowIndex = 0;
    # How to treat the sheet when it already exists (default: `FAIL_IF_EXISTS`). `REPLACE` drops
    # and recreates the sheet, discarding its tables and formatting.
    SheetWriteMode sheetWriteMode = FAIL_IF_EXISTS;
|};

# Options for bulk table reads (`parseTable`, `Table.getRows`).
public type TableParseOptions record {|
    *CommonParseOptions;
    # Maximum number of data rows to read (default: all)
    int rowCount?;
    # Validate parsed records against their `@constraint` annotations (default: true)
    boolean enableConstraintValidation = true;
    # Data projection: `{}` ignores extra columns (default), `false` requires an exact match
    DataProjection|false allowDataProjection = {};
    # Skip and log row-level errors instead of failing the read
    FailSafeOptions failSafe?;
|};

# Options for reading a single table row (`Table.getRow`). Fail-fast: no `failSafe` or `rowCount`.
public type TableRowParseOptions record {|
    *CommonParseOptions;
    # Validate the parsed record against its `@constraint` annotations (default: true)
    boolean enableConstraintValidation = true;
    # Data projection: `{}` ignores extra columns (default), `false` requires an exact match
    DataProjection|false allowDataProjection = {};
|};

# Options for `writeTable` / `Table.putRows`.
public type TableWriteOptions record {|
    # How to treat the table's existing data (default: `REPLACE`)
    TableWriteMode tableWriteMode = REPLACE;
    # For `APPEND`, the 0-based data-row index to insert at; omitted, appends at the end.
    # Ignored by `REPLACE`.
    int insertAt?;
|};

# Options for `Sheet.putRows`.
public type WriteOptions record {|
    # Write a header row from field names or map keys (default: true; ignored for `string[][]`)
    boolean writeHeaders = true;
    # 0-based target row. Omitted, uses the mode's natural point: the end of the data for
    # `APPEND`, row 0 otherwise.
    int startRowIndex?;
    # How to treat existing content at the target (default: `APPEND`)
    SheetWriteMode sheetWriteMode = APPEND;
|};

# A parsed record failed a `@constraint` rule.
public type ConstraintValidationError distinct Error;

# The base type for all `xlsx` module errors.
public type Error distinct error<ErrorDetails>;

# The XLSX file path does not exist or could not be accessed.
public type FileNotFoundError distinct Error;

# A table range or insert position is invalid.
public type InvalidTableRangeError distinct Error;

# The workbook content is malformed or could not be read.
public type ParseError distinct Error;

# A sheet with the target name already exists.
public type SheetExistsError distinct Error;

# No sheet matches the given name or index.
public type SheetNotFoundError distinct Error;

# A table with the target name already exists.
public type TableExistsError distinct Error;

# No table matches the given name.
public type TableNotFoundError distinct Error;

# A table write would overlap with another table.
public type TableOverlapError distinct Error;

# A cell value could not be converted to the target type.
public type TypeConversionError distinct Error;

# What to include when logging parsing errors.
public enum ErrorLogContentType {
    # Log only the metadata: timestamp, location, and message
    METADATA,
    # Log only the offending row data
    RAW,
    # Log both the row data and the metadata
    RAW_AND_METADATA
}

# How to write the error log file (default: `APPEND`, which adds to the existing file).
public enum FileWriteOption {
    APPEND,
    # Overwrite the log on the first error, then append the rest
    OVERWRITE
}

# How to read cells that contain a formula.
public enum FormulaMode {
    # Use the last cached value (default)
    CACHED,
    # Return the formula text, such as "=SUM(A1:A10)"
    TEXT
}

# How a sheet write treats content already at the target.
public enum SheetWriteMode {
    # Fail instead of overwriting existing content
    FAIL_IF_EXISTS,
    # Overwrite existing content in place
    REPLACE,
    # Add new content, shifting existing content down to make room
    APPEND
}

# How `writeTable` / `Table.putRows` treats the table's existing data.
# 
# `REPLACE` (default) replaces the data and resizes the range to fit; `APPEND` adds rows below it.
# A table always has a data region, so there is no `FAIL_IF_EXISTS`.
public enum TableWriteMode {
    REPLACE,
    APPEND
}

# An Excel workbook: a set of sheets, with methods to read, create, delete, and save them.
# 
# Create an empty workbook with `new`, or open one with `xlsx:fromFile` / `xlsx:fromBytes`.
# A workbook and the sheets and tables obtained from it are not safe for concurrent mutation.
# 
# ```ballerina
# xlsx:Workbook wb = check xlsx:fromFile("report.xlsx");
# xlsx:Sheet sheet = check wb.getSheet("Sales");
# check wb.save();
# check wb.close();
# ```
public isolated class Workbook {
    # Create an empty in-memory workbook. Persist it with `saveAs(path)`, since `save()` has no
    # source path yet. To open an existing workbook, use `xlsx:fromFile` or `xlsx:fromBytes`.
    isolated function init();

    # Get all sheet names in the workbook.
    # + return - Array of sheet names
    isolated function getSheetNames() returns string[]|Error;

    # Get the number of sheets in the workbook.
    # + return - Sheet count
    isolated function getSheetCount() returns int|Error;

    # Check whether a sheet with the given name exists.
    # + name - Sheet name
    # + return - Whether the sheet exists, or an error if the workbook is closed
    isolated function hasSheet(string name) returns boolean|Error;

    # Get a sheet by name or 0-based index.
    # + target - Sheet name, or 0-based index
    # + return - The sheet, or a `SheetNotFoundError` if it does not exist
    isolated function getSheet(string|int target) returns Sheet|Error;

    # Create a new sheet in the workbook.
    # + name - Name for the new sheet
    # + return - The new sheet, or a `SheetExistsError` if the name is taken
    isolated function createSheet(string name) returns Sheet|Error;

    # Delete a sheet by name or 0-based index.
    # + target - Sheet name, or 0-based index
    # + return - A `SheetNotFoundError` if the sheet is missing, or an error if it is the last sheet
    isolated function deleteSheet(string|int target) returns Error?;

    # Save the workbook, overwriting the file it was opened from or last saved to with `saveAs`.
    # + return - An error if the workbook has no source path (created with `new`), or if the save fails
    isolated function save() returns Error?;

    # Save the workbook to a new path, which then becomes the target of later `save()` calls.
    # + path - Path to write the XLSX file to
    # + return - An error if the save fails
    isolated function saveAs(string path) returns Error?;

    # Serialize the workbook to a byte array, for example to send as an HTTP response.
    # + return - The XLSX bytes, or an error if serialization fails
    isolated function toBytes() returns byte[]|Error;

    # Close the workbook and release its resources. Call this when done to free memory.
    # + return - An error if the close fails
    isolated function close() returns Error?;

    # Get a table by name from anywhere in the workbook. Table names are unique workbook-wide.
    # + name - Table name
    # + return - The table, or a `TableNotFoundError` if it does not exist
    isolated function getTable(string name) returns Table|Error;

    # Get all tables across every sheet in the workbook.
    # + return - All tables (may be empty), or an error on failure
    isolated function getAllTables() returns Table[]|Error;
}

# A worksheet in a workbook, with methods to read and write rows, columns, cells, and tables.
# Obtained from a `Workbook` (for example `getSheet` or `createSheet`); not constructed directly.
public type Sheet object {
    # Get the name of the sheet.
    # + return - The sheet name, or an error
    isolated function getName() returns string|Error;

    # Get the used range of the sheet in A1 notation, such as "A1:D50".
    # + return - The used range, or an error
    isolated function getUsedRange() returns string|Error;

    # Get the used cell range as a structured record, or nil if the sheet is empty.
    # + return - The used range as 0-based indices, nil if the sheet is empty, or an error
    isolated function getUsedCellRange() returns CellRange?|Error;

    # Get the number of rows with data.
    # + return - The row count, or an error
    isolated function getRowCount() returns int|Error;

    # Get the number of columns with data.
    # + return - The column count, or an error
    isolated function getColumnCount() returns int|Error;

    # Read all rows from the sheet as records, maps, or a string grid.
    # + options - Read options
    # + t - Target row type
    # + return - The rows, or an error
    isolated function getRows(ParseOptions options = {}, typedesc<Row> t = <>) returns t[]|Error;

    # Read a single row by index as a record, map, or string array.
    # + index - 0-based index within the data window, so `getRow(0)` is the first data row
    # + options - Read options
    # + t - Target row type
    # + return - The row, or an error
    isolated function getRow(int index, RowParseOptions options = {}, typedesc<Row> t = <>) returns t|Error;

    # Write rows to the sheet (records, maps, or string arrays).
    # 
    # By default rows are appended below the existing data; `sheetWriteMode` selects another
    # disposition.
    # + data - Rows to write
    # + options - Write options
    # + return - An error if the write fails
    isolated function putRows(Row[] data, *WriteOptions options) returns Error?;

    # Get a column of values by header name or 0-based index.
    # + columnRef - Column header name, or 0-based index
    # + options - Read options
    # + t - Target cell type (`CellValue`, which includes `()` for blank cells)
    # + return - The column values, or an error
    isolated function getColumn(string|int columnRef, ColumnParseOptions options = {}, typedesc<CellValue> t = <>) returns t[]|Error;

    # Read a single cell, bound to the target type.
    # 
    # The target type drives the binding: the default `CellValue` yields the cell's natural value,
    # while a `time:Civil` / `time:Date` / `time:TimeOfDay` or scalar target yields that type.
    # + rowIndex - 0-based row index
    # + columnIndex - 0-based column index
    # + t - Target cell type (default: `CellValue`)
    # + return - The cell value, `()` for a blank cell when the target allows it, or an error
    isolated function getCell(int rowIndex, int columnIndex, typedesc<CellValue> t = <>) returns t|Error;

    # Write a single row at the given 0-based row index.
    # 
    # By default the row is overwritten; `sheetWriteMode` selects another disposition. For a record
    # or map, values align to columns by header name, using the header at `options.headerRowIndex`.
    # + rowIndex - 0-based row index
    # + data - Row data (`string[]`, record, or `map<CellValue>`)
    # + options - Single-row write options
    # + return - An error if the write fails
    isolated function setRow(int rowIndex, Row data, *RowWriteOptions options) returns Error?;

    # Write a column of values by header name or 0-based index.
    # 
    # Values are written into successive rows below the header row.
    # + columnRef - Column header name, or 0-based index
    # + data - Column values
    # + return - An error if the write fails
    isolated function setColumn(string|int columnRef, CellValue[] data) returns Error?;

    # Write a single cell by 0-based row and column index.
    # + rowIndex - 0-based row index
    # + columnIndex - 0-based column index
    # + value - Cell value
    # + return - An error if the write fails
    isolated function setCell(int rowIndex, int columnIndex, CellValue value) returns Error?;

    # Write a single cell by A1-notation address.
    # + cellAddress - Cell address in A1 notation, such as "A1" or "B12"
    # + value - Cell value
    # + return - An error if the address is invalid or the write fails
    isolated function setCellByAddress(string cellAddress, CellValue value) returns Error?;

    # Delete a row from the sheet; subsequent rows shift up by one.
    # + index - 0-based row index to delete
    # + return - An error if the delete fails
    isolated function deleteRow(int index) returns Error?;

    # Rename the sheet.
    # 
    # The new name must follow Excel rules (at most 31 characters, none of `\ / ? * [ ] :`) and be
    # unique in the workbook.
    # + newName - New sheet name
    # + return - An error if the name is invalid or already taken
    isolated function rename(string newName) returns Error?;

    # Get a table on this sheet by name.
    # + name - Table name
    # + return - The table, or a `TableNotFoundError` if it does not exist
    isolated function getTable(string name) returns Table|Error;

    # Get all tables on this sheet.
    # + return - The tables (may be empty), or an error on failure
    isolated function getTables() returns Table[]|Error;

    # Create a table over an existing range.
    # 
    # The range must include a header row. If `headers` is not given, the first row is used.
    # + name - Unique table name (across the workbook)
    # + range - Table range as a `CellRange` or A1-notation string
    # + headers - Optional custom headers; if omitted, the first row is used
    # + return - The created table, or an error
    isolated function createTable(string name, CellRange|string range, string[]? headers = ()) returns Table|Error;

    # Write data and wrap it in a new table, computing the range automatically.
    # 
    # The table always has a header row: field names (or `@xlsx:Name`) for records, keys for maps,
    # or the first row for `string[][]`.
    # + name - Unique table name (across the workbook)
    # + data - Data to write (records, maps, or string arrays)
    # + startRowIndex - Starting row for the table (default: 0)
    # + startColumnIndex - Starting column for the table (default: 0)
    # + return - The created table, or an error
    isolated function createTableFromData(string name, Row[] data, int startRowIndex = 0, int startColumnIndex = 0) returns Table|Error;

    # Delete a table from this sheet. The underlying data is preserved.
    # + name - Table name to delete
    # + return - A `TableNotFoundError` if the table does not exist, or another error
    isolated function deleteTable(string name) returns Error?;
};

# An Excel Table (ListObject) in a worksheet, with automatic header handling, an optional totals
# row, and auto-resize on write. Table names are unique across the workbook.
# 
# Obtained from a `Workbook` or `Sheet` (for example `getTable` or `createTable`); not constructed
# directly.
# 
# ```ballerina
# xlsx:Table empTable = check wb.getTable("EmployeeTable");
# Employee[] employees = check empTable.getRows();
# check empTable.putRows(newEmployees);
# ```
public type Table object {
    # Get the name of the table. Table names are unique across the workbook.
    # + return - The table name, or an error
    isolated function getName() returns string|Error;

    # Get the display name of the table, as shown in the Excel UI.
    # + return - The display name, or an error
    isolated function getDisplayName() returns string|Error;

    # Get the name of the sheet that holds this table.
    # + return - The sheet name, or an error
    isolated function getSheetName() returns string|Error;

    # Get the full table range, including the header and totals row, in A1 notation.
    # + return - The range in A1 notation, such as "A1:D10", or an error
    isolated function getRange() returns string|Error;

    # Get the full table range, including the header and totals row, as a `CellRange` (0-based).
    # + return - The full table range, or an error
    isolated function getCellRange() returns CellRange|Error;

    # Get the data range, excluding the header and totals row, in A1 notation.
    # + return - The data range in A1 notation, or an error
    isolated function getDataRange() returns string|Error;

    # Get the data range, excluding the header and totals row, as a `CellRange` (0-based).
    # + return - The data range, or an error
    isolated function getDataCellRange() returns CellRange|Error;

    # Get the number of data rows, excluding the header and totals row.
    # + return - The data row count, or an error
    isolated function getRowCount() returns int|Error;

    # Get the number of columns in the table.
    # + return - The column count, or an error
    isolated function getColumnCount() returns int|Error;

    # Get the column header names, in column order.
    # + return - The header names, or an error
    isolated function getHeaders() returns string[]|Error;

    # Read all data rows from the table as records, maps, or a string grid.
    # 
    # The header and any totals row are excluded.
    # + options - Table read options
    # + t - Target row type
    # + return - The data rows, or an error
    isolated function getRows(TableParseOptions options = {}, typedesc<Row> t = <>) returns t[]|Error;

    # Read a single data row by index as a record, map, or string array.
    # + index - 0-based index within the data range, so `getRow(0)` is the first data row
    # + options - Table read options
    # + t - Target row type
    # + return - The row, or an error
    isolated function getRow(int index, TableRowParseOptions options = {}, typedesc<Row> t = <>) returns t|Error;

    # Write rows to the table, resizing its data range to fit.
    # 
    # By default the data is replaced; `tableWriteMode = APPEND` adds rows below it instead.
    # A resize that would overlap another table fails with a `TableOverlapError`.
    # + data - Rows to write (records, maps, or string arrays)
    # + options - Table write options
    # + return - An error if the write fails
    isolated function putRows(Row[] data, *TableWriteOptions options) returns Error?;

    # Check whether the table has a totals row.
    # + return - Whether a totals row exists, or an error
    isolated function hasTotalRow() returns boolean|Error;

    # Get the totals row as a map keyed by column name.
    # 
    # Each value binds to its natural cell value, or `()` for a blank total cell.
    # + t - Result map type (default: `map<CellValue>`); a narrower type binds only if every cell fits
    # + return - The totals by column name, or an error if there is no totals row
    isolated function getTotalRow(typedesc<map<CellValue>> t = <>) returns t|Error;

    # Rename the table. The new name must be unique in the workbook.
    # + newName - New table name
    # + return - An error if the rename fails, for example if the name is taken
    isolated function rename(string newName) returns Error?;

    # Resize the table to a new range, which must include a header row and a data row.
    # For automatic resizing on write, use `putRows` instead.
    # + newRange - New table range as a `CellRange` or an A1-notation string
    # + return - An error if the range is invalid or overlaps another table
    isolated function resize(CellRange|string newRange) returns Error?;

    # Delete a data row by 0-based index; the table shrinks and rows below move up.
    # 
    # A table must keep at least one data row, so the last one cannot be deleted.
    # + index - 0-based index within the data range, so 0 is the first data row
    # + return - An error if the index is out of range, it is the last data row, or the shrink
    # would disrupt another table
    isolated function deleteRow(int index) returns Error?;
};

# A single row: either a `map<CellValue>` keyed by column header, or a `string[]` of cell text
# in column order. A typed record also binds when every field type is a subtype of `CellValue`.
public type Row map<CellValue>|string[];

# An XLSX cell value: a string, number (int/float/decimal), boolean, date/time, or `()` for a blank cell.
public type CellValue string|int|float|decimal|boolean|time:Date|time:Civil|time:TimeOfDay|(); // Special Agent Note: Date, Civil, TimeOfDay FROM ballerina/time module

// --- Functions ---

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

// --- Annotations ---

# Annotation to specify the Excel column name for a record field.
public annotation NameConfig Name on record field;
