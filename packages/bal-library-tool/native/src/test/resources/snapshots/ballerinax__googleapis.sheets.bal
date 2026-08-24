// ============================================================
// Library: ballerinax/googleapis.sheets
// The [Google Sheets](https://developers.google.com/sheets/api), developed by Google LLC, allows users to programmatically interact with Google Sheets, facilitating tasks such as data manipulation, analysis, and automation.
// ============================================================
import ballerinax/googleapis.sheets;

// --- Types ---

# A1 Notation of a ValueRange
public type A1Range record {
    # Sheet name in A1 notation
    string sheetName;
    # Starting cell of the range
    string startIndex?;
    # Ending cell of the range
    string endIndex?;
};

# Single cell in a sheet.
public type Cell record {
    # The column letter followed by the row number.
    # For example for a single cell "A1" refers to the intersection of column "A" with row "1"
    string a1Notation;
    # Value of the given cell
    (int|string|decimal) value;
};

# Provides settings related to HTTP/1.x protocol.
public type ClientHttp1Settings record {|
    # Specifies whether to reuse a connection for multiple requests
    http:KeepAlive keepAlive = http:KEEPALIVE_AUTO; // Special Agent Note: KeepAlive FROM ballerina/http module
    # The chunking behaviour of the request
    http:Chunking chunking = http:CHUNKING_AUTO; // Special Agent Note: Chunking FROM ballerina/http module
    # Proxy server related options
    ProxyConfig? proxy = ();
|};

# Single column in a sheet.
public type Column record {
    # The column letter
    string columnPosition;
    # Values of the given column
    (int|string|decimal)[] values;
};

# Provides a set of configurations for controlling the behaviours when communicating with a remote HTTP endpoint.
public type ConnectionConfig record {|
    # Configurations related to client authentication
    http:BearerTokenConfig|OAuth2RefreshTokenGrantConfig auth; // Special Agent Note: BearerTokenConfig FROM ballerina/http module
    # The HTTP version understood by the client
    http:HttpVersion httpVersion = http:HTTP_2_0; // Special Agent Note: HttpVersion FROM ballerina/http module
    # Configurations related to HTTP/1.x protocol
    ClientHttp1Settings http1Settings = {};
    # Configurations related to HTTP/2 protocol
    http:ClientHttp2Settings http2Settings = {}; // Special Agent Note: ClientHttp2Settings FROM ballerina/http module
    # The maximum time to wait (in seconds) for a response before closing the connection
    decimal timeout = 60;
    # The choice of setting `forwarded`/`x-forwarded` header
    string forwarded = "disable";
    # Configurations associated with request pooling
    http:PoolConfiguration? poolConfig = (); // Special Agent Note: PoolConfiguration FROM ballerina/http module
    # HTTP caching related configurations
    http:CacheConfig cache = {}; // Special Agent Note: CacheConfig FROM ballerina/http module
    # Specifies the way of handling compression (`accept-encoding`) header
    http:Compression compression = http:COMPRESSION_AUTO; // Special Agent Note: Compression FROM ballerina/http module
    # Configurations associated with the behaviour of the Circuit Breaker
    http:CircuitBreakerConfig? circuitBreaker = (); // Special Agent Note: CircuitBreakerConfig FROM ballerina/http module
    # Configurations associated with retrying
    http:RetryConfig? retryConfig = (); // Special Agent Note: RetryConfig FROM ballerina/http module
    # Configurations associated with inbound response size limits
    http:ResponseLimitConfigs responseLimits = {}; // Special Agent Note: ResponseLimitConfigs FROM ballerina/http module
    # SSL/TLS-related options
    http:ClientSecureSocket secureSocket = {}; // Special Agent Note: ClientSecureSocket FROM ballerina/http module
    # Proxy server related options
    http:ProxyConfig? proxy = (); // Special Agent Note: ProxyConfig FROM ballerina/http module
    # Enables the inbound payload validation functionality which provided by the constraint package. Enabled by default
    boolean validation = true;
|};

# The DeveloperMetadataLookup filter
public type DeveloperMetadataLookupFilter record {
    # Specified type which the metadata ara associated.
    # For more information, see [LocationType](https://developers.google.com/sheets/api/reference/rest/v4/spreadsheets.developerMetadata#DeveloperMetadata.DeveloperMetadataLocationType)
    LocationType locationType;
    # An enumeration of strategies for matching developer metadata locations.
    # For more information, see [locationMatchingStrategy](https://developers.google.com/sheets/api/reference/rest/v4/DataFilter#DeveloperMetadataLocationMatchingStrategy).
    LocationMatchingStrategy locationMatchingStrategy?;
    # The spreadsheet-scoped unique ID that identifies the metadata.
    int metadataId?;
    # Key used to identify metadata.
    string metadataKey?;
    # Data associated with the metadata's key.
    string metadataValue;
    # Visibility scope of the associated metadata
    # For more information, see [Visibility](https://developers.google.com/sheets/api/reference/rest/v4/spreadsheets.developerMetadata#DeveloperMetadata.DeveloperMetadataVisibility).
    Visibility visibility?;
    # Location of association for metadata
    MetadataLocation metadataLocation?;
};

# The Dimension Range
public type DimensionRange record {
    # The ID of the worksheet
    int sheetId;
    # The dimension of the span
    Dimension dimension;
    # The start (inclusive) of the span, or not set if unbounded
    int startIndex;
    # The end (exclusive) of the span, or not set if unbounded.
    int endIndex;
};

# Grid properties.
public type GridProperties record {
    # The number of rows in the grid
    int rowCount = 0;
    # The number of columns in the grid
    int columnCount = 0;
    # The number of rows that are frozen in the grid
    int frozenRowCount = 0;
    # The number of columns that are frozen in the grid
    int frozenColumnCount = 0;
    # True if the grid is not showing gridlines in the UI
    boolean hideGridlines = false;
};

# The GridRange filters
public type GridRangeFilter record {
    # The ID of the worksheet
    int sheetId;
    # The start row (inclusive) of the range, or not set if unbounded.
    int startRowIndex?;
    # The end row (exclusive) of the range, or not set if unbounded.
    int endRowIndex?;
    # The start column (inclusive) of the range, or not set if unbounded.
    int startColumnIndex?;
    # The end column (exclusive) of the range, or not set if unbounded.
    int endColumnIndex?;
};

# The Metadata Location
public type MetadataLocation record {
    # Specified type which the metadata ara associated.
    # For more information, see [LocationType](https://developers.google.com/sheets/api/reference/rest/v4/spreadsheets.developerMetadata#DeveloperMetadata.DeveloperMetadataLocationType)
    LocationType locationType;
    # Whether metadata is associated with an entire spreadsheet.
    boolean spreadsheet;
    # The ID of the worksheet
    int sheetId;
    # Dimension when the metadata is associated with them
    DimensionRange dimensionRange;
};

# OAuth2 Refresh Token Grant Configs
public type OAuth2RefreshTokenGrantConfig record {|
    *http:OAuth2RefreshTokenGrantConfig; // Special Agent Note: OAuth2RefreshTokenGrantConfig FROM ballerina/http module
    # Refresh URL
    string refreshUrl = "https://accounts.google.com/o/oauth2/token";
|};

# Proxy server configurations to be used with the HTTP client endpoint.
public type ProxyConfig record {|
    # Host name of the proxy server
    string host = "";
    # Proxy server port
    int port = 0;
    # Proxy server username
    string userName = "";
    # Proxy server password
    string password = "";
|};

# Single cell or a group of adjacent cells in a sheet.
public type Range record {
    # The column letter followed by the row number.
    # For example for a single cell "A1" refers to the intersection of column "A" with row "1",
    # and for a range of cells "A1:D5" refers to the top left cell and the bottom right cell of a range
    string a1Notation;
    # Values of the given range
    (int|string|decimal)[][] values;
};

# Single row in a sheet.
public type Row record {
    # The row number
    int rowPosition;
    # Values of the given row
    (int|string|decimal)[] values;
};

# Worksheet information.
public type Sheet record {
    # Properties of a worksheet
    SheetProperties properties = {};
};

# Worksheet properties.
public type SheetProperties record {
    # The ID of the worksheet
    int sheetId = 0;
    # The name of the worksheet
    string title = "";
    # The index of the worksheet within the spreadsheet
    int index = 0;
    # The type of worksheet
    string sheetType = "";
    # Additional properties of the worksheet if this worksheet is a grid
    GridProperties gridProperties = {};
    # True if the worksheet is hidden in the UI, false if it is visible
    boolean hidden = false;
    # True if the worksheet is an RTL worksheet instead of an LTR worksheet
    boolean rightToLeft = false;
};

# Spreadsheet information.
public type Spreadsheet record {
    # Id of the spreadsheet
    string spreadsheetId = "";
    # Properties of a spreadsheet
    SpreadsheetProperties properties = {};
    # The sheets that are part of a spreadsheet
    Sheet[] sheets = [];
    # The Url of the spreadsheet
    string spreadsheetUrl = "";
};

# Spreadsheet properties.
public type SpreadsheetProperties record {
    # The title of the spreadsheet
    string title = "";
    # The locale of the spreadsheet
    string locale = "";
    # The amount of time to wait before volatile functions are recalculated
    string autoRecalc = "";
    # The time zone of the spreadsheet
    string timeZone = "";
};

# Values related to a single row.
public type ValueRange record {
    # The row number
    int rowPosition;
    # Values of the given row
    (int|string|decimal|boolean|float)[] values;
    # A1Notation of the range
    A1Range a1Range;
};

# Values related to a multiple rows.
public type ValuesRange record {|
    # The row number
    int rowStartPosition;
    # Values of the given rows
    (int|string|decimal|boolean|float)[][] values;
    # A1Notation of the range
    A1Range a1Range;
|};

# Defines the generic error type for the `googleapis.sheets` module.
public type Error distinct error;

# Error that occurs when an invalid cell range is provided. This could be due to malformed A1 notation or a range
# that falls outside the bounds of the sheet.
public type InvalidRangeError distinct Error;

# Error that occurs when a spreadsheet or sheet operation fails. This could be due to the resource not being found,
# insufficient permissions, or an API-level rejection.
public type SpreadsheetError distinct Error;

public const string REFRESH_URL = "https://accounts.google.com/o/oauth2/token";

# Dimension
public enum Dimension {
    UNSPECIFIED_DIMENSION,
    COLUMNS,
    ROWS
}

# The location matching strategy for filters
public enum LocationMatchingStrategy {
    UNSPECIFIED_STRATEGY,
    EXACT_LOCATION,
    INTERSECTING_LOCATION
}

# The location type for filters
public enum LocationType {
    UNSPECIFIED_LOCATION,
    COLUMN,
    SPREADSHEET,
    SHEET,
    ROW
}

public enum ValueInputOption {
    RAW,
    USER_ENTERED
}

public enum ValueRenderOption {
    FORMATTED_VALUE,
    UNFORMATTED_VALUE,
    FORMULA
}

# The metadata visibility
public enum Visibility {
    UNSPECIFIED_VISIBILITY,
    DOCUMENT,
    PROJECT
}

# Type of filter used to match data.
public type Filter A1Range|DeveloperMetadataLookupFilter|GridRangeFilter;

// --- Client ---

# Ballerina Google Sheets connector provides the capability to access Google Sheets API.
# The connector let you perform spreadsheet management operations, worksheet management operations and
# the capability to handle Google Sheets data level operations.
public isolated client class Client {
    # Gets invoked to initialize the `connector`.
    # + config - Configuration for the connector
    # + serviceUrl - URL of the Google Sheets API
    # + driveServiceUrl - URL of the Google Drive API
    # + return - `http:Error` in case of failure to initialize or `null` if successfully initialized
    isolated function init(ConnectionConfig config, string serviceUrl = BASE_URL, string driveServiceUrl = DRIVE_BASE_URL) returns error?; // Special Agent Note: the defaults BASE_URL, DRIVE_BASE_URL are not exported by this package; omit the arguments rather than repeating them

    # Creates a new spreadsheet.
    # + name - Name of the spreadsheet
    # + return - `sheets:Spreadsheet` record on success, or else an error
    isolated remote function createSpreadsheet(string name) returns Spreadsheet|error;

    # Deletes a spreadsheet by the given ID.
    # 
    # **Note**: This operation uses the Google Drive API and requires the Google Drive API to be enabled in
    # your Google Cloud project. The OAuth token must include the
    # `https://www.googleapis.com/auth/drive.file` scope (or broader Drive scope).
    # + spreadsheetId - ID of the spreadsheet to delete
    # + return - Nil on success, or else an error
    isolated remote function deleteSpreadsheet(string spreadsheetId) returns error?;

    # Opens a spreadsheet by the given ID.
    # + spreadsheetId - ID of the spreadsheet
    # + return - `sheets:Spreadsheet` record on success, or else an error
    isolated remote function openSpreadsheetById(string spreadsheetId) returns Spreadsheet|error;

    # Opens a spreadsheet by the given Url.
    # + url - Url of the spreadsheet
    # + return - `sheets:Spreadsheet` record on success, or else an error
    isolated remote function openSpreadsheetByUrl(string url) returns Spreadsheet|error;

    # Renames the spreadsheet with the given name.
    # + spreadsheetId - ID of the spreadsheet
    # + name - New name for the spreadsheet
    # + return - Nil on success, or else an error
    isolated remote function renameSpreadsheet(string spreadsheetId, string name) returns error?;

    # Get worksheets of the spreadsheet.
    # + spreadsheetId - ID of the spreadsheet
    # + return - Array of `sheets:Sheet` records on success, or else an error
    isolated remote function getSheets(string spreadsheetId) returns Sheet[]|error;

    # Get a worksheet of the spreadsheet.
    # + spreadsheetId - ID of the spreadsheet
    # + sheetName - Name of the worksheet to retrieve
    # + return - `sheets:Sheet` record on success, or else an error
    isolated remote function getSheetByName(string spreadsheetId, string sheetName) returns Sheet|error;

    # Add a new worksheet.
    # + spreadsheetId - ID of the spreadsheet
    # + sheetName - The name of the worksheet
    # + return - `sheets:Sheet` record on success, or else an error
    isolated remote function addSheet(string spreadsheetId, string sheetName) returns Sheet|error;

    # Delete specified worksheet by worksheet ID.
    # + spreadsheetId - ID of the spreadsheet
    # + sheetId - The ID of the worksheet to delete
    # + return - Nil on success, or else an error
    isolated remote function removeSheet(string spreadsheetId, int sheetId) returns error?;

    # Delete specified worksheet by worksheet name.
    # + spreadsheetId - ID of the spreadsheet
    # + sheetName - The name of the worksheet to delete
    # + return - Nil on success, or else an error
    isolated remote function removeSheetByName(string spreadsheetId, string sheetName) returns error?;

    # Renames the worksheet of a given spreadsheet with the given name.
    # + spreadsheetId - ID of the spreadsheet
    # + sheetName - The existing name of the worksheet
    # + name - New name for the worksheet
    # + return - Nil on success, or else an error
    isolated remote function renameSheet(string spreadsheetId, string sheetName, string name) returns error?;

    # Sets the values of the given range of cells of the worksheet.
    # + spreadsheetId - ID of the Spreadsheet
    # + sheetName - The name of the Worksheet
    # + range - The Range record to be set
    # + valueInputOption - Determines how input data should be interpreted.
    # It's either `RAW` or `USER_ENTERED`. Default is `RAW` (Optional).
    # For more information, see [ValueInputOption](https://developers.google.com/sheets/api/reference/rest/v4/ValueInputOption)
    # + return - Nil on success, or else an error
    isolated remote function setRange(string spreadsheetId, string sheetName, Range range, string? valueInputOption = ()) returns error?;

    # Gets the given range of the worksheet.
    # + spreadsheetId - ID of the spreadsheet
    # + sheetName - The name of the worksheet
    # + a1Notation - The required range in A1 notation
    # + valueRenderOption - Determines how values should be rendered in the output.
    # It's either `FORMATTED_VALUE`, `UNFORMATTED_VALUE` or `FORMULA`.
    # Default is `FORMATTED_VALUE` (Optional).
    # For more information, see [ValueRenderOption](https://developers.google.com/sheets/api/reference/rest/v4/ValueRenderOption)
    # + return - `sheets:Range` record on success, or else an error
    isolated remote function getRange(string spreadsheetId, string sheetName, string a1Notation, string? valueRenderOption = ()) returns Range|error;

    # Clears the range of contents, formats, and data validation rules.
    # + spreadsheetId - ID of the spreadsheet
    # + sheetName - The name of the worksheet
    # + a1Notation - The required range in A1 notation
    # + return - Nil on success, or else an error
    isolated remote function clearRange(string spreadsheetId, string sheetName, string a1Notation) returns error?;

    # Inserts the given number of columns before the given column position by worksheet ID.
    # + spreadsheetId - ID of the spreadsheet
    # + sheetId - ID of the worksheet
    # + index - The position of the column before which the new columns should be added
    # + numberOfColumns - Number of columns to be added
    # + return - Nil on success, or else an error
    isolated remote function addColumnsBefore(string spreadsheetId, int sheetId, int index, int numberOfColumns) returns error?;

    # Inserts the given number of columns before the given column position by worksheet name.
    # + spreadsheetId - ID of the Spreadsheet
    # + sheetName - The name of the Worksheet
    # + index - The position of the column before which the new columns should be added
    # + numberOfColumns - Number of columns to be added
    # + return - Nil on success, or else an error
    isolated remote function addColumnsBeforeBySheetName(string spreadsheetId, string sheetName, int index, int numberOfColumns) returns error?;

    # Inserts the given number of columns after the given column position by worksheet ID.
    # + spreadsheetId - ID of the spreadsheet
    # + sheetId - ID of the worksheet
    # + index - The position of the column after which the new columns should be added
    # + numberOfColumns - Number of columns to be added
    # + return - Nil on success, or else an error
    isolated remote function addColumnsAfter(string spreadsheetId, int sheetId, int index, int numberOfColumns) returns error?;

    # Inserts the given number of columns after the given column position by worksheet name.
    # + spreadsheetId - ID of the spreadsheet
    # + sheetName - The name of the worksheet
    # + index - The position of the column after which the new columns should be added
    # + numberOfColumns - Number of columns to be added
    # + return - Nil on success, or else an error
    isolated remote function addColumnsAfterBySheetName(string spreadsheetId, string sheetName, int index, int numberOfColumns) returns error?;

    # Create or Update a Column.
    # + spreadsheetId - ID of the spreadsheet
    # + sheetName - The name of the worksheet
    # + column - Position of column (string notation) to set the data
    # + values - Array of values of the column to be added
    # + valueInputOption - Determines how input data should be interpreted.
    # It's either `RAW` or `USER_ENTERED`. Default is `RAW` (Optional).
    # For more information, see [ValueInputOption](https://developers.google.com/sheets/api/reference/rest/v4/ValueInputOption)
    # + return - Nil on success, or else an error
    isolated remote function createOrUpdateColumn(string spreadsheetId, string sheetName, string column, (int|string|decimal)[] values, string? valueInputOption = ()) returns error?;

    # Gets the values in the given column of the worksheet.
    # + spreadsheetId - ID of the spreadsheet
    # + sheetName - The name of the worksheet
    # + column - Position of Column (string notation) to retrieve the data
    # + valueRenderOption - Determines how values should be rendered in the output.
    # It's either `FORMATTED_VALUE`, `UNFORMATTED_VALUE` or `FORMULA`.
    # Default is `FORMATTED_VALUE` (Optional).
    # For more information, see [ValueRenderOption](https://developers.google.com/sheets/api/reference/rest/v4/ValueRenderOption)
    # + return - `sheets:Column` record on success, or else an error
    isolated remote function getColumn(string spreadsheetId, string sheetName, string column, string? valueRenderOption = ()) returns Column|error;

    # Deletes the given number of columns starting at the given column position by worksheet ID.
    # + spreadsheetId - ID of the spreadsheet
    # + sheetId - ID of the worksheet
    # + column - Starting position of the columns
    # + numberOfColumns - Number of columns from the starting position
    # + return - Nil on success, or else an error
    isolated remote function deleteColumns(string spreadsheetId, int sheetId, int column, int numberOfColumns) returns error?;

    # Deletes the given number of columns starting at the given column position by worksheet name.
    # + spreadsheetId - ID of the spreadsheet
    # + sheetName - The name of the worksheet
    # + column - Starting position of the columns
    # + numberOfColumns - Number of columns from the starting position
    # + return - Nil on success, or else an error
    isolated remote function deleteColumnsBySheetName(string spreadsheetId, string sheetName, int column, int numberOfColumns) returns error?;

    # Inserts the given number of rows before the given row position by worksheet ID.
    # + spreadsheetId - ID of the spreadsheet
    # + sheetId - ID of the worksheet
    # + index - The position of the row before which the new rows should be added
    # + numberOfRows - The number of rows to be added
    # + return - Nil on success, or else an error
    isolated remote function addRowsBefore(string spreadsheetId, int sheetId, int index, int numberOfRows) returns error?;

    # Inserts the given number of rows before the given row position by worksheet name.
    # + spreadsheetId - ID of the spreadsheet
    # + sheetName - The name of the worksheet
    # + index - The position of the row before which the new rows should be added
    # + numberOfRows - The number of rows to be added
    # + return - Nil on success, or else an error
    isolated remote function addRowsBeforeBySheetName(string spreadsheetId, string sheetName, int index, int numberOfRows) returns error?;

    # Inserts a number of rows after the given row position by worksheet ID.
    # + spreadsheetId - ID of the spreadsheet
    # + sheetId - ID of the worksheet
    # + index - The row after which the new rows should be added.
    # + numberOfRows - The number of rows to be added
    # + return - Nil on success, or else an error
    isolated remote function addRowsAfter(string spreadsheetId, int sheetId, int index, int numberOfRows) returns error?;

    # Inserts a number of rows after the given row position by worksheet name.
    # + spreadsheetId - ID of the spreadsheet
    # + sheetName - The name of the worksheet
    # + index - The row after which the new rows should be added.
    # + numberOfRows - The number of rows to be added
    # + return - Nil on success, or else an error
    isolated remote function addRowsAfterBySheetName(string spreadsheetId, string sheetName, int index, int numberOfRows) returns error?;

    # Create or update a row.
    # + spreadsheetId - ID of the spreadsheet
    # + sheetName - The name of the worksheet
    # + row - Position of row (integer notation) to set the data
    # + values - Array of values of the row to be added
    # + valueInputOption - Determines how input data should be interpreted.
    # It's either `RAW` or `USER_ENTERED`. Default is `RAW` (Optional).
    # For more information, see [ValueInputOption](https://developers.google.com/sheets/api/reference/rest/v4/ValueInputOption)
    # + return - Nil on success, or else an error
    isolated remote function createOrUpdateRow(string spreadsheetId, string sheetName, int row, (int|string|decimal)[] values, string? valueInputOption = ()) returns error?;

    # Gets the values in the given row of the worksheet.
    # + spreadsheetId - ID of the spreadsheet
    # + sheetName - The name of the worksheet
    # + row - Row number to retrieve the data
    # + valueRenderOption - Determines how values should be rendered in the output.
    # It's either `FORMATTED_VALUE`, `UNFORMATTED_VALUE` or `FORMULA`.
    # Default is `FORMATTED_VALUE` (Optional).
    # For more information, see [ValueRenderOption](https://developers.google.com/sheets/api/reference/rest/v4/ValueRenderOption)
    # + return - `sheets:Row` record on success, or else an error
    isolated remote function getRow(string spreadsheetId, string sheetName, int row, string? valueRenderOption = ()) returns Row|error;

    # Deletes the given number of rows starting at the given row position by worksheet ID.
    # + spreadsheetId - ID of the spreadsheet
    # + sheetId - ID of the worksheet
    # + row - Starting position of the rows
    # + numberOfRows - Number of rows from the starting position
    # + return - Nil on success, or else an error
    isolated remote function deleteRows(string spreadsheetId, int sheetId, int row, int numberOfRows) returns error?;

    # Deletes the given number of rows starting at the given row position by worksheet name.
    # + spreadsheetId - ID of the spreadsheet
    # + sheetName - The name of the worksheet
    # + row - Starting position of the rows
    # + numberOfRows - Number of rows from the starting position
    # + return - Nil on success, or else an error
    isolated remote function deleteRowsBySheetName(string spreadsheetId, string sheetName, int row, int numberOfRows) returns error?;

    # Sets the value of the given cell of the worksheet.
    # + spreadsheetId - ID of the spreadsheet
    # + sheetName - The name of the worksheet
    # + a1Notation - The required cell in A1 notation
    # + value - Value of the cell to be set
    # + valueInputOption - Determines how input data should be interpreted.
    # It's either `RAW` or `USER_ENTERED`. Default is `RAW` (Optional).
    # For more information, see [ValueInputOption](https://developers.google.com/sheets/api/reference/rest/v4/ValueInputOption)
    # + return - Nil on success, or else an error
    isolated remote function setCell(string spreadsheetId, string sheetName, string a1Notation, int|string|decimal value, string? valueInputOption = ()) returns error?;

    # Gets the value of the given cell of the sheet.
    # + spreadsheetId - ID of the spreadsheet
    # + sheetName - The name of the worksheet
    # + a1Notation - The required cell in A1 notation
    # + valueRenderOption - Determines how values should be rendered in the output.
    # It's either `FORMATTED_VALUE`, `UNFORMATTED_VALUE` or `FORMULA`.
    # Default is `FORMATTED_VALUE` (Optional).
    # For more information, see [ValueRenderOption](https://developers.google.com/sheets/api/reference/rest/v4/ValueRenderOption)
    # + return - `sheets:Cell` record on success, or else an error
    isolated remote function getCell(string spreadsheetId, string sheetName, string a1Notation, string? valueRenderOption = ()) returns Cell|error;

    # Clears the given cell of contents, formats, and data validation rules.
    # + spreadsheetId - ID of the spreadsheet
    # + sheetName - The name of the worksheet
    # + a1Notation - The required cell in A1 notation
    # + return - Nil on success, or else an error
    isolated remote function clearCell(string spreadsheetId, string sheetName, string a1Notation) returns error?;

    # Adds the given values to a row at the bottom of the worksheet. The input range is used to search
    # for existing data and find a "table" within that range. Values will be appended to the next row of
    # the table, starting with the first column of the table.
    # + spreadsheetId - ID of the spreadsheet
    # + values - Array of values of the row to be added
    # + a1Range - The required range in A1 notation
    # + valueInputOption - Determines how input data should be interpreted.
    # It's either `RAW` or `USER_ENTERED`. Default is `RAW` (Optional).
    # For more information, see [ValueInputOption](https://developers.google.com/sheets/api/reference/rest/v4/ValueInputOption)
    # + return - ValueRange on success, or else an error
    isolated remote function appendValue(string spreadsheetId, (int|string|decimal|boolean|float)[] values, A1Range a1Range, string? valueInputOption = ()) returns error|ValueRange;

    # Adds the given values to number of rows at the bottom of the worksheet. The input range is used to search
    # for existing data and find a "table" within that range. Values will be appended to the next rows of
    # the table, starting with the first column of the table.
    # + spreadsheetId - ID of the spreadsheet
    # + values - Array of values of the rows to be added
    # + a1Range - The required range in A1 notation
    # + valueInputOption - Determines how input data should be interpreted.
    # It's either `RAW` or `USER_ENTERED`. Default is `RAW` (Optional).
    # For more information, see [ValueInputOption](https://developers.google.com/sheets/api/reference/rest/v4/ValueInputOption)
    # + return - ValueRange on success, or else an error
    isolated remote function appendValues(string spreadsheetId, (int|string|decimal|boolean|float)[][] values, A1Range a1Range, string? valueInputOption = ()) returns error|ValuesRange;

    # Copies the sheet to a given spreadsheet by worksheet ID.
    # + spreadsheetId - ID of the spreadsheet
    # + sheetId - ID of the worksheet
    # + destinationId - ID of the spreadsheet to copy the sheet to
    # + return - Nil on success, or else an error
    isolated remote function copyTo(string spreadsheetId, int sheetId, string destinationId) returns error?;

    # Copies the sheet to a given spreadsheet by worksheet name.
    # + spreadsheetId - ID of the spreadsheet
    # + sheetName - The name of the worksheet
    # + destinationId - ID of the spreadsheet to copy the sheet to
    # + return - Nil on success, or else an error
    isolated remote function copyToBySheetName(string spreadsheetId, string sheetName, string destinationId) returns error?;

    # Clears the worksheet content and formatting rules by worksheet ID.
    # + spreadsheetId - ID of the spreadsheet
    # + sheetId - ID of the worksheet
    # + return - Nil on success, or else an error
    isolated remote function clearAll(string spreadsheetId, int sheetId) returns error?;

    # Clears the worksheet content and formatting rules by worksheet name.
    # + spreadsheetId - ID of the spreadsheet
    # + sheetName - The name of the worksheet
    # + return - Nil on success, or else an error
    isolated remote function clearAllBySheetName(string spreadsheetId, string sheetName) returns error?;

    # Add developer metadata to the given row.
    # + spreadsheetId - ID of the spreadsheet
    # + sheetId - The ID of the worksheet
    # + rowIndex - ID of the target row
    # + visibility - Visibility parameter for the developer metadata. It's either `UNSPECIFIED`, `DOCUMENT` or `PROJECT`.
    # + key - Metadata key asigned to the row
    # + value - Value assigned with the key. This should be unique.
    # + return - Nil on success, or else an error
    isolated remote function setRowMetaData(string spreadsheetId, int sheetId, int rowIndex, Visibility visibility, string key, string value) returns error?;

    # Fetch rows matching to the given criteria in the filter.
    # Supports A1Range, GridRange and DeveloperMetadataLookup filters.
    # + spreadsheetId - ID of the spreadsheet
    # + sheetId - The ID of the worksheet
    # + filter - A record defining the filter used for the data filtering
    # + return - ValueRange[] on success, or else an error
    isolated remote function getRowByDataFilter(string spreadsheetId, int sheetId, Filter filter) returns error|ValueRange[];

    # Update rows matching the user provided data filter.
    # Supports a1Range, gridRange and Developer metadata lookup filters.
    # + spreadsheetId - ID of the spreadsheet
    # + sheetId - The ID of the worksheet
    # + filter - A record defining the filter used for the data filtering
    # + values - Values to assign.
    # + valueInputOption - Determines how input data should be interpreted.
    # It's either `RAW` or `USER_ENTERED`. Default is `RAW` (Optional).
    # For more information, see [ValueInputOption](https://developers.google.com/sheets/api/reference/rest/v4/ValueInputOption)
    # + return - Nil on success, or else an error
    isolated remote function updateRowByDataFilter(string spreadsheetId, int sheetId, Filter filter, (int|string|decimal|boolean|float)[] values, string valueInputOption) returns error?;

    # Delete rows matching the user provided data filter
    # Supports a1Range, gridRange and Developer metadata lookup filters
    # + spreadsheetId - ID of the spreadsheet
    # + sheetId - The ID of the worksheet
    # + filter - A record defining the filter used for the data filtering
    # + return - Nil on success, or else an error
    isolated remote function deleteRowByDataFilter(string spreadsheetId, int sheetId, Filter filter) returns error?;
}
