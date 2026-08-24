<!-- bal library client v1 -->
# Clients — ballerinax/googleapis.sheets `Client`

| | |
|---|---|
| Container | `Client` — 43 remote |
| Showing | 44 signatures |

## Next

- one call and every type it needs: `bal library client ballerinax/googleapis.sheets Client init -r`

## Constructor — 1

```ballerina
# Gets invoked to initialize the `connector`.
isolated function init(ConnectionConfig config, string serviceUrl = BASE_URL, string driveServiceUrl = DRIVE_BASE_URL) returns error?; // Special Agent Note: the defaults BASE_URL, DRIVE_BASE_URL are not exported by this package; omit the arguments rather than repeating them
```

## Remote functions — 43, call with `->`

```ballerina
# Creates a new spreadsheet.
isolated remote function createSpreadsheet(string name) returns Spreadsheet|error;

# Deletes a spreadsheet by the given ID.
# 
# **Note**: This operation uses the Google Drive API and requires the Google Drive API to be enabled in
# your Google Cloud project. The OAuth token must include the
# `https://www.googleapis.com/auth/drive.file` scope (or broader Drive scope).
isolated remote function deleteSpreadsheet(string spreadsheetId) returns error?;

# Opens a spreadsheet by the given ID.
isolated remote function openSpreadsheetById(string spreadsheetId) returns Spreadsheet|error;

# Opens a spreadsheet by the given Url.
isolated remote function openSpreadsheetByUrl(string url) returns Spreadsheet|error;

# Renames the spreadsheet with the given name.
isolated remote function renameSpreadsheet(string spreadsheetId, string name) returns error?;

# Get worksheets of the spreadsheet.
isolated remote function getSheets(string spreadsheetId) returns Sheet[]|error;

# Get a worksheet of the spreadsheet.
isolated remote function getSheetByName(string spreadsheetId, string sheetName) returns Sheet|error;

# Add a new worksheet.
isolated remote function addSheet(string spreadsheetId, string sheetName) returns Sheet|error;

# Delete specified worksheet by worksheet ID.
isolated remote function removeSheet(string spreadsheetId, int sheetId) returns error?;

# Delete specified worksheet by worksheet name.
isolated remote function removeSheetByName(string spreadsheetId, string sheetName) returns error?;

# Renames the worksheet of a given spreadsheet with the given name.
isolated remote function renameSheet(string spreadsheetId, string sheetName, string name) returns error?;

# Sets the values of the given range of cells of the worksheet.
isolated remote function setRange(string spreadsheetId, string sheetName, Range range, string? valueInputOption = ()) returns error?;

# Gets the given range of the worksheet.
isolated remote function getRange(string spreadsheetId, string sheetName, string a1Notation, string? valueRenderOption = ()) returns Range|error;

# Clears the range of contents, formats, and data validation rules.
isolated remote function clearRange(string spreadsheetId, string sheetName, string a1Notation) returns error?;

# Inserts the given number of columns before the given column position by worksheet ID.
isolated remote function addColumnsBefore(string spreadsheetId, int sheetId, int index, int numberOfColumns) returns error?;

# Inserts the given number of columns before the given column position by worksheet name.
isolated remote function addColumnsBeforeBySheetName(string spreadsheetId, string sheetName, int index, int numberOfColumns) returns error?;

# Inserts the given number of columns after the given column position by worksheet ID.
isolated remote function addColumnsAfter(string spreadsheetId, int sheetId, int index, int numberOfColumns) returns error?;

# Inserts the given number of columns after the given column position by worksheet name.
isolated remote function addColumnsAfterBySheetName(string spreadsheetId, string sheetName, int index, int numberOfColumns) returns error?;

# Create or Update a Column.
isolated remote function createOrUpdateColumn(string spreadsheetId, string sheetName, string column, (int|string|decimal)[] values, string? valueInputOption = ()) returns error?;

# Gets the values in the given column of the worksheet.
isolated remote function getColumn(string spreadsheetId, string sheetName, string column, string? valueRenderOption = ()) returns Column|error;

# Deletes the given number of columns starting at the given column position by worksheet ID.
isolated remote function deleteColumns(string spreadsheetId, int sheetId, int column, int numberOfColumns) returns error?;

# Deletes the given number of columns starting at the given column position by worksheet name.
isolated remote function deleteColumnsBySheetName(string spreadsheetId, string sheetName, int column, int numberOfColumns) returns error?;

# Inserts the given number of rows before the given row position by worksheet ID.
isolated remote function addRowsBefore(string spreadsheetId, int sheetId, int index, int numberOfRows) returns error?;

# Inserts the given number of rows before the given row position by worksheet name.
isolated remote function addRowsBeforeBySheetName(string spreadsheetId, string sheetName, int index, int numberOfRows) returns error?;

# Inserts a number of rows after the given row position by worksheet ID.
isolated remote function addRowsAfter(string spreadsheetId, int sheetId, int index, int numberOfRows) returns error?;

# Inserts a number of rows after the given row position by worksheet name.
isolated remote function addRowsAfterBySheetName(string spreadsheetId, string sheetName, int index, int numberOfRows) returns error?;

# Create or update a row.
isolated remote function createOrUpdateRow(string spreadsheetId, string sheetName, int row, (int|string|decimal)[] values, string? valueInputOption = ()) returns error?;

# Gets the values in the given row of the worksheet.
isolated remote function getRow(string spreadsheetId, string sheetName, int row, string? valueRenderOption = ()) returns Row|error;

# Deletes the given number of rows starting at the given row position by worksheet ID.
isolated remote function deleteRows(string spreadsheetId, int sheetId, int row, int numberOfRows) returns error?;

# Deletes the given number of rows starting at the given row position by worksheet name.
isolated remote function deleteRowsBySheetName(string spreadsheetId, string sheetName, int row, int numberOfRows) returns error?;

# Sets the value of the given cell of the worksheet.
isolated remote function setCell(string spreadsheetId, string sheetName, string a1Notation, int|string|decimal value, string? valueInputOption = ()) returns error?;

# Gets the value of the given cell of the sheet.
isolated remote function getCell(string spreadsheetId, string sheetName, string a1Notation, string? valueRenderOption = ()) returns Cell|error;

# Clears the given cell of contents, formats, and data validation rules.
isolated remote function clearCell(string spreadsheetId, string sheetName, string a1Notation) returns error?;

# Adds the given values to a row at the bottom of the worksheet. The input range is used to search
# for existing data and find a "table" within that range. Values will be appended to the next row of
# the table, starting with the first column of the table.
isolated remote function appendValue(string spreadsheetId, (int|string|decimal|boolean|float)[] values, A1Range a1Range, string? valueInputOption = ()) returns error|ValueRange;

# Adds the given values to number of rows at the bottom of the worksheet. The input range is used to search
# for existing data and find a "table" within that range. Values will be appended to the next rows of
# the table, starting with the first column of the table.
isolated remote function appendValues(string spreadsheetId, (int|string|decimal|boolean|float)[][] values, A1Range a1Range, string? valueInputOption = ()) returns error|ValuesRange;

# Copies the sheet to a given spreadsheet by worksheet ID.
isolated remote function copyTo(string spreadsheetId, int sheetId, string destinationId) returns error?;

# Copies the sheet to a given spreadsheet by worksheet name.
isolated remote function copyToBySheetName(string spreadsheetId, string sheetName, string destinationId) returns error?;

# Clears the worksheet content and formatting rules by worksheet ID.
isolated remote function clearAll(string spreadsheetId, int sheetId) returns error?;

# Clears the worksheet content and formatting rules by worksheet name.
isolated remote function clearAllBySheetName(string spreadsheetId, string sheetName) returns error?;

# Add developer metadata to the given row.
isolated remote function setRowMetaData(string spreadsheetId, int sheetId, int rowIndex, Visibility visibility, string key, string value) returns error?;

# Fetch rows matching to the given criteria in the filter.
# Supports A1Range, GridRange and DeveloperMetadataLookup filters.
isolated remote function getRowByDataFilter(string spreadsheetId, int sheetId, Filter filter) returns error|ValueRange[];

# Update rows matching the user provided data filter.
# Supports a1Range, gridRange and Developer metadata lookup filters.
isolated remote function updateRowByDataFilter(string spreadsheetId, int sheetId, Filter filter, (int|string|decimal|boolean|float)[] values, string valueInputOption) returns error?;

# Delete rows matching the user provided data filter
# Supports a1Range, gridRange and Developer metadata lookup filters
isolated remote function deleteRowByDataFilter(string spreadsheetId, int sheetId, Filter filter) returns error?;
```
