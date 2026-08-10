<!-- bal-library overview v1 -->
# ballerinax/googleapis.sheets 0.0.0-fixture

| | |
|---|---|
| Source | central |
| Clients | `Client` |
| Module functions | none |
| Errors | 3, listed below |
| Types | 28 declarations (20 records, 1 unions, 7 other), not listed here — read one with `type` |

## Next

- `bal-library ops ballerinax/googleapis.sheets <path>` — navigate a client's operations
- `bal-library type ballerinax/googleapis.sheets <Name> [--deps]` — read a declaration whole
- `bal-library api ballerinax/googleapis.sheets` — every declaration, when nothing above answered

## Client `Client`

Ballerina Google Sheets connector provides the capability to access Google Sheets API.

### Constructor

```ballerina
function init(ConnectionConfig config, string serviceUrl = BASE_URL, string driveServiceUrl = DRIVE_BASE_URL) returns error?;
```

### Remote functions — 43, call with `->`

```ballerina
# Creates a new spreadsheet.
remote function createSpreadsheet(string name) returns Spreadsheet|error;

# Deletes a spreadsheet by the given ID.
# 
# **Note**: This operation uses the Google Drive API and requires the Google Drive API to be enabled in
# your Google Cloud project. The OAuth token must include the
# `https://www.googleapis.com/auth/drive.file` scope (or broader Drive scope).
remote function deleteSpreadsheet(string spreadsheetId) returns error?;

# Opens a spreadsheet by the given ID.
remote function openSpreadsheetById(string spreadsheetId) returns Spreadsheet|error;

# Opens a spreadsheet by the given Url.
remote function openSpreadsheetByUrl(string url) returns Spreadsheet|error;

# Renames the spreadsheet with the given name.
remote function renameSpreadsheet(string spreadsheetId, string name) returns error?;

# Get worksheets of the spreadsheet.
remote function getSheets(string spreadsheetId) returns Sheet[]|error;

# Get a worksheet of the spreadsheet.
remote function getSheetByName(string spreadsheetId, string sheetName) returns Sheet|error;

# Add a new worksheet.
remote function addSheet(string spreadsheetId, string sheetName) returns Sheet|error;

# Delete specified worksheet by worksheet ID.
remote function removeSheet(string spreadsheetId, int sheetId) returns error?;

# Delete specified worksheet by worksheet name.
remote function removeSheetByName(string spreadsheetId, string sheetName) returns error?;

# Renames the worksheet of a given spreadsheet with the given name.
remote function renameSheet(string spreadsheetId, string sheetName, string name) returns error?;

# Sets the values of the given range of cells of the worksheet.
remote function setRange(string spreadsheetId, string sheetName, Range range, string? valueInputOption = ()) returns error?;

# Gets the given range of the worksheet.
remote function getRange(string spreadsheetId, string sheetName, string a1Notation, string? valueRenderOption = ()) returns Range|error;

# Clears the range of contents, formats, and data validation rules.
remote function clearRange(string spreadsheetId, string sheetName, string a1Notation) returns error?;

# Inserts the given number of columns before the given column position by worksheet ID.
remote function addColumnsBefore(string spreadsheetId, int sheetId, int index, int numberOfColumns) returns error?;

# Inserts the given number of columns before the given column position by worksheet name.
remote function addColumnsBeforeBySheetName(string spreadsheetId, string sheetName, int index, int numberOfColumns) returns error?;

# Inserts the given number of columns after the given column position by worksheet ID.
remote function addColumnsAfter(string spreadsheetId, int sheetId, int index, int numberOfColumns) returns error?;

# Inserts the given number of columns after the given column position by worksheet name.
remote function addColumnsAfterBySheetName(string spreadsheetId, string sheetName, int index, int numberOfColumns) returns error?;

# Create or Update a Column.
remote function createOrUpdateColumn(string spreadsheetId, string sheetName, string column, (int|string|decimal)[] values, string? valueInputOption = ()) returns error?;

# Gets the values in the given column of the worksheet.
remote function getColumn(string spreadsheetId, string sheetName, string column, string? valueRenderOption = ()) returns Column|error;

# Deletes the given number of columns starting at the given column position by worksheet ID.
remote function deleteColumns(string spreadsheetId, int sheetId, int column, int numberOfColumns) returns error?;

# Deletes the given number of columns starting at the given column position by worksheet name.
remote function deleteColumnsBySheetName(string spreadsheetId, string sheetName, int column, int numberOfColumns) returns error?;

# Inserts the given number of rows before the given row position by worksheet ID.
remote function addRowsBefore(string spreadsheetId, int sheetId, int index, int numberOfRows) returns error?;

# Inserts the given number of rows before the given row position by worksheet name.
remote function addRowsBeforeBySheetName(string spreadsheetId, string sheetName, int index, int numberOfRows) returns error?;

# Inserts a number of rows after the given row position by worksheet ID.
remote function addRowsAfter(string spreadsheetId, int sheetId, int index, int numberOfRows) returns error?;

# Inserts a number of rows after the given row position by worksheet name.
remote function addRowsAfterBySheetName(string spreadsheetId, string sheetName, int index, int numberOfRows) returns error?;

# Create or update a row.
remote function createOrUpdateRow(string spreadsheetId, string sheetName, int row, (int|string|decimal)[] values, string? valueInputOption = ()) returns error?;

# Gets the values in the given row of the worksheet.
remote function getRow(string spreadsheetId, string sheetName, int row, string? valueRenderOption = ()) returns Row|error;

# Deletes the given number of rows starting at the given row position by worksheet ID.
remote function deleteRows(string spreadsheetId, int sheetId, int row, int numberOfRows) returns error?;

# Deletes the given number of rows starting at the given row position by worksheet name.
remote function deleteRowsBySheetName(string spreadsheetId, string sheetName, int row, int numberOfRows) returns error?;

# Sets the value of the given cell of the worksheet.
remote function setCell(string spreadsheetId, string sheetName, string a1Notation, int|string|decimal value, string? valueInputOption = ()) returns error?;

# Gets the value of the given cell of the sheet.
remote function getCell(string spreadsheetId, string sheetName, string a1Notation, string? valueRenderOption = ()) returns Cell|error;

# Clears the given cell of contents, formats, and data validation rules.
remote function clearCell(string spreadsheetId, string sheetName, string a1Notation) returns error?;

# Adds the given values to a row at the bottom of the worksheet. The input range is used to search
# for existing data and find a "table" within that range. Values will be appended to the next row of
# the table, starting with the first column of the table.
remote function appendValue(string spreadsheetId, (int|string|decimal|boolean|float)[] values, A1Range a1Range, string? valueInputOption = ()) returns error|ValueRange;

# Adds the given values to number of rows at the bottom of the worksheet. The input range is used to search
# for existing data and find a "table" within that range. Values will be appended to the next rows of
# the table, starting with the first column of the table.
remote function appendValues(string spreadsheetId, (int|string|decimal|boolean|float)[] values, A1Range a1Range, string? valueInputOption = ()) returns error|ValuesRange;

# Copies the sheet to a given spreadsheet by worksheet ID.
remote function copyTo(string spreadsheetId, int sheetId, string destinationId) returns error?;

# Copies the sheet to a given spreadsheet by worksheet name.
remote function copyToBySheetName(string spreadsheetId, string sheetName, string destinationId) returns error?;

# Clears the worksheet content and formatting rules by worksheet ID.
remote function clearAll(string spreadsheetId, int sheetId) returns error?;

# Clears the worksheet content and formatting rules by worksheet name.
remote function clearAllBySheetName(string spreadsheetId, string sheetName) returns error?;

# Add developer metadata to the given row.
remote function setRowMetaData(string spreadsheetId, int sheetId, int rowIndex, Visibility visibility, string key, string value) returns error?;

# Fetch rows matching to the given criteria in the filter.
# Supports A1Range, GridRange and DeveloperMetadataLookup filters.
remote function getRowByDataFilter(string spreadsheetId, int sheetId, Filter filter) returns error|ValueRange[];

# Update rows matching the user provided data filter.
# Supports a1Range, gridRange and Developer metadata lookup filters.
remote function updateRowByDataFilter(string spreadsheetId, int sheetId, Filter filter, (int|string|decimal|boolean|float)[] values, string valueInputOption) returns error?;

# Delete rows matching the user provided data filter
# Supports a1Range, gridRange and Developer metadata lookup filters
remote function deleteRowByDataFilter(string spreadsheetId, int sheetId, Filter filter) returns error?;
```

## Errors — 3

The subtype chain is what `is` tests against, so `e is <Name>` works off these lines directly.

```ballerina
# Defines the generic error type for the `googleapis.sheets` module.
type Error distinct error;

# Error that occurs when an invalid cell range is provided. This could be due to malformed A1 notation or a range
# that falls outside the bounds of the sheet.
type InvalidRangeError distinct Error;

# Error that occurs when a spreadsheet or sheet operation fails. This could be due to the resource not being found,
# insufficient permissions, or an API-level rejection.
type SpreadsheetError distinct Error;
```

## Guide

*The package's own readme, verbatim, with its headings demoted two levels.*

#### Overview

The [Google Sheets](https://developers.google.com/sheets/api), developed by Google LLC, allows users to programmatically interact with Google Sheets, facilitating tasks such as data manipulation, analysis, and automation.

The Google Sheets connector offers APIs to connect and interact with [Sheets API](https://developers.google.com/sheets/api/guides) endpoints, specifically based on [Google Sheets API v4](https://developers.google.com/sheets/api).

##### Key Features

- Programmatically interact with Google Sheets
- Support for data manipulation and analysis
- Automate tasks related to spreadsheets
- Compatible with Google Sheets API v4

#### Setup guide

To use the Google Sheets connector, you must have access to the Google Sheets API through a [Google Cloud Platform (GCP)](https://console.cloud.google.com/) account and a project under it. If you do not have a GCP account, you can sign up for one [here](https://cloud.google.com/).

##### Step 1: Create a Google Cloud Platform project

1. Open the [Google Cloud Platform Console](https://console.cloud.google.com/).

2. Click on **Select a project** in the drop-down menu and either select an existing project or create a new one.

   ![Enable Google Sheets API](https://raw.githubusercontent.com/ballerina-platform/module-ballerinax-googleapis.sheets/master/docs/setup/resources/gcp-console-project-view.png)

##### Step 2: Enabling Google Sheets API

1. Select the created project.

2. Navigate to **APIs & Services** > **Library**.

3. Search and select `Google Sheets API`. Then click **ENABLE**.

   ![Enable Sheets Api](https://raw.githubusercontent.com/ballerina-platform/module-ballerinax-googleapis.sheets/master/docs/setup/resources/enable-sheets-api.png)

> **Note**: If you intend to use the `deleteSpreadsheet` operation, you must also enable the **Google Drive API** in the same project. Search for `Google Drive API` in the library and click **ENABLE**.

##### Step 3: Creating an OAuth consent app

1. Click on the **OAuth Consent Screen** in the sidebar.

2. Select `External` and click **CREATE**.

3. Fill in the app information and add the necessary scopes for Google Sheets API.

   ![OAuth Consent Screen](https://raw.githubusercontent.com/ballerina-platform/module-ballerinax-googleapis.sheets/master/docs/setup/resources/oauth-consent.png)

##### Step 4: Generating client ID & client secret

1. In the left sidebar, click on **Credentials**.

2. Click on **+ CREATE CREDENTIALS** and choose **OAuth Client ID**.

   ![Create Credentials](https://raw.githubusercontent.com/ballerina-platform/module-ballerinax-googleapis.sheets/master/docs/setup/resources/create-credentials.png)

3. You will be directed to the OAuth consent screen, in which you need to fill in the necessary information below.

   | Field                    | Value                                           |
   | ------------------------ | ----------------------------------------------- |
   | Application type         | Web Application                                 |
   | Name                     | Sheets Client                                   |
   | Authorized Redirect URIs | <https://developers.google.com/oauthplayground> |

   ![Create Client](https://raw.githubusercontent.com/ballerina-platform/module-ballerinax-googleapis.sheets/master/docs/setup/resources/create-client.png)

##### Step 5: Obtain the access and refresh tokens

Follow these steps to generate the access and refresh tokens.

**Note**: It is recommended to use the [OAuth 2.0 playground](https://developers.google.com/oauthplayground) to acquire the tokens.

1. Configure the [OAuth playground](https://developers.google.com/oauthplayground) with the OAuth client ID and client secret.

   ![OAuth Playground](https://raw.githubusercontent.com/ballerina-platform/module-ballerinax-googleapis.sheets/master/docs/setup/resources/oauth-playground-config.png)

2. Authorize the Google Sheets APIs. If you intend to use the `deleteSpreadsheet` operation, also add the `https://www.googleapis.com/auth/drive.file` scope.

   ![Authorize APIs](https://raw.githubusercontent.com/ballerina-platform/module-ballerinax-googleapis.sheets/master/docs/setup/resources/auhtorize-apis.png)

3. Exchange the authorization code for tokens.

   ![Exchange Tokens](https://raw.githubusercontent.com/ballerina-platform/module-ballerinax-googleapis.sheets/master/docs/setup/resources/exchange-tokens.png)

#### Quickstart

To use the Google Sheets connector in your Ballerina project, modify the `.bal` file as follows:

##### Step 1: Import connector

Import the `ballerinax/googleapis.sheets` module.

```ballerina
import ballerinax/googleapis.sheets;
```

##### Step 2: Create a new connector instance

Create a `sheets:ConnectionConfig` with the obtained OAuth2.0 tokens and initialize the connector with it.

```ballerina
configurable string clientId = ?;
configurable string clientSecret = ?;
configurable string refreshToken = ?;
configurable string refreshUrl = ?;

sheets:Client spreadsheetClient = check new ({
    auth: {
        clientId,
        clientSecret,
        refreshToken,
        refreshUrl
    }
});
```

##### Step 3: Invoke connector operation

Now, utilize the available connector operations.

##### Create a spreadsheet with a given name

```ballerina
public function main() returns error? {

    // create a spreadsheet
    sheets:Spreadsheet response = check spreadsheetClient->createSpreadsheet("NewSpreadsheet");

    // Add a new worksheet with given name to the Spreadsheet
    string spreadsheetId = response.spreadsheetId;
    sheets:Sheet sheet = check spreadsheetClient->addSheet(spreadsheetId, "NewWorksheet");
}
```

##### Step 4: Run the Ballerina application

```bash
bal run
```

#### Examples

The `Google Sheets` connector provides practical examples illustrating usage in various scenarios. Explore these [examples](https://github.com/ballerina-platform/module-ballerinax-googleapis.sheets/tree/master/examples), covering use cases like creating, reading, and appending rows.

1. [Cell operations](https://github.com/ballerina-platform/module-ballerinax-googleapis.sheets/tree/master/examples/cell-operations) - Operations associated with a cell, such as clearing, setting, and deleting cell values.

2. [Grid filtering](https://github.com/ballerina-platform/module-ballerinax-googleapis.sheets/tree/master/examples/grid-filtering) - Demonstrate filtering sheet values using a grid range.

3. [Sheet modifying](https://github.com/ballerina-platform/module-ballerinax-googleapis.sheets/tree/master/examples/sheet-modifying) - Basic operations associated with sheets such as creating, reading, and appending rows.

#### Report issues

To report bugs, request new features, start new discussions, view project boards, etc., go to the [Ballerina library parent repository](https://github.com/ballerina-platform/ballerina-library).

#### Useful links

- Chat live with us via our [Discord server](https://discord.gg/ballerinalang).
- Post all technical questions on Stack Overflow with the [#ballerina](https://stackoverflow.com/questions/tagged/ballerina) tag.
