// ============================================================
// Library: ballerinax/googleapis.sheets
// The [Google Sheets](https://developers.google.com/sheets/api), developed by Google LLC, allows users to programmatically interact with Google Sheets, facilitating tasks such as data manipulation, analysis, and automation.
// ============================================================
import ballerinax/googleapis.sheets;

// --- Types ---

# A1 Notation of a ValueRange

type A1Range record {
    # Sheet name in A1 notation
    string sheetName;
    # Starting cell of the range
    string startIndex?;
    # Ending cell of the range
    string endIndex?;
};

# Single cell in a sheet.

type Cell record {
    # The column letter followed by the row number.
For example for a single cell "A1" refers to the intersection of column "A" with row "1"
    string a1Notation;
    # Value of the given cell
    (int|string|decimal) value;
};

# Provides settings related to HTTP/1.x protocol.

type ClientHttp1Settings record {
    # Specifies whether to reuse a connection for multiple requests
    http:KeepAlive keepAlive = http:KEEPALIVE_AUTO; // Special Agent Note: KeepAlive FROM ballerina/http package
    # The chunking behaviour of the request
    http:Chunking chunking = http:CHUNKING_AUTO; // Special Agent Note: Chunking FROM ballerina/http package
    # Proxy server related options
    ProxyConfig? proxy = ();
};

# Single column in a sheet.

type Column record {
    # The column letter
    string columnPosition;
    # Values of the given column
    (int|string|decimal)[] values;
};

# Provides a set of configurations for controlling the behaviours when communicating with a remote HTTP endpoint.

type ConnectionConfig record {
    # Configurations related to client authentication
    http:BearerTokenConfig|OAuth2RefreshTokenGrantConfig auth; // Special Agent Note: BearerTokenConfig FROM ballerina/http package
    # The HTTP version understood by the client
    http:HttpVersion httpVersion = http:HTTP_2_0; // Special Agent Note: HttpVersion FROM ballerina/http package
    # Configurations related to HTTP/1.x protocol
    ClientHttp1Settings http1Settings = {};
    # Configurations related to HTTP/2 protocol
    http:ClientHttp2Settings http2Settings = {}; // Special Agent Note: ClientHttp2Settings FROM ballerina/http package
    # The maximum time to wait (in seconds) for a response before closing the connection
    decimal timeout = 60;
    # The choice of setting `forwarded`/`x-forwarded` header
    string forwarded = "disable";
    # Configurations associated with request pooling
    http:PoolConfiguration? poolConfig = (); // Special Agent Note: PoolConfiguration FROM ballerina/http package
    # HTTP caching related configurations
    http:CacheConfig cache = {}; // Special Agent Note: CacheConfig FROM ballerina/http package
    # Specifies the way of handling compression (`accept-encoding`) header
    http:Compression compression = http:COMPRESSION_AUTO; // Special Agent Note: Compression FROM ballerina/http package
    # Configurations associated with the behaviour of the Circuit Breaker
    http:CircuitBreakerConfig? circuitBreaker = (); // Special Agent Note: CircuitBreakerConfig FROM ballerina/http package
    # Configurations associated with retrying
    http:RetryConfig? retryConfig = (); // Special Agent Note: RetryConfig FROM ballerina/http package
    # Configurations associated with inbound response size limits
    http:ResponseLimitConfigs responseLimits = {}; // Special Agent Note: ResponseLimitConfigs FROM ballerina/http package
    # SSL/TLS-related options
    http:ClientSecureSocket secureSocket = {}; // Special Agent Note: ClientSecureSocket FROM ballerina/http package
    # Proxy server related options
    http:ProxyConfig? proxy = (); // Special Agent Note: ProxyConfig FROM ballerina/http package
    # Enables the inbound payload validation functionality which provided by the constraint package. Enabled by default
    boolean validation = true;
};

# The DeveloperMetadataLookup filter

type DeveloperMetadataLookupFilter record {
    # Specified type which the metadata ara associated.
For more information, see [LocationType](https://developers.google.com/sheets/api/reference/rest/v4/spreadsheets.developerMetadata#DeveloperMetadata.DeveloperMetadataLocationType)
    LocationType locationType;
    # An enumeration of strategies for matching developer metadata locations.
For more information, see [locationMatchingStrategy](https://developers.google.com/sheets/api/reference/rest/v4/DataFilter#DeveloperMetadataLocationMatchingStrategy).
    LocationMatchingStrategy locationMatchingStrategy?;
    # The spreadsheet-scoped unique ID that identifies the metadata.
    int metadataId?;
    # Key used to identify metadata.
    string metadataKey?;
    # Data associated with the metadata's key.
    string metadataValue;
    # Visibility scope of the associated metadata
For more information, see [Visibility](https://developers.google.com/sheets/api/reference/rest/v4/spreadsheets.developerMetadata#DeveloperMetadata.DeveloperMetadataVisibility).
    Visibility visibility?;
    # Location of association for metadata
    MetadataLocation metadataLocation?;
};

# The Dimension Range

type DimensionRange record {
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

type GridProperties record {
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

type GridRangeFilter record {
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

type MetadataLocation record {
    # Specified type which the metadata ara associated.
For more information, see [LocationType](https://developers.google.com/sheets/api/reference/rest/v4/spreadsheets.developerMetadata#DeveloperMetadata.DeveloperMetadataLocationType)
    LocationType locationType;
    # Whether metadata is associated with an entire spreadsheet.
    boolean spreadsheet;
    # The ID of the worksheet
    int sheetId;
    # Dimension when the metadata is associated with them
    DimensionRange dimensionRange;
};

# OAuth2 Refresh Token Grant Configs

type OAuth2RefreshTokenGrantConfig record {
    # Refresh URL
    string refreshUrl = "https://accounts.google.com/o/oauth2/token";
    string refreshToken;
    string clientId;
    string clientSecret;
    string|string[] scopes;
    decimal defaultTokenExpTime;
    decimal clockSkew;
    map<string> optionalParams;
    oauth2:CredentialBearer credentialBearer; // Special Agent Note: CredentialBearer FROM ballerina/oauth2 package
    oauth2:ClientConfiguration clientConfig; // Special Agent Note: ClientConfiguration FROM ballerina/oauth2 package
};

# Proxy server configurations to be used with the HTTP client endpoint.

type ProxyConfig record {
    # Host name of the proxy server
    string host = "";
    # Proxy server port
    int port = 0;
    # Proxy server username
    string userName = "";
    # Proxy server password
    string password = "";
};

# Single cell or a group of adjacent cells in a sheet.

type Range record {
    # The column letter followed by the row number.
For example for a single cell "A1" refers to the intersection of column "A" with row "1",
and for a range of cells "A1:D5" refers to the top left cell and the bottom right cell of a range
    string a1Notation;
    # Values of the given range
    (int|string|decimal)[][] values;
};

# Single row in a sheet.

type Row record {
    # The row number
    int rowPosition;
    # Values of the given row
    (int|string|decimal)[] values;
};

# Worksheet information.

type Sheet record {
    # Properties of a worksheet
    SheetProperties properties = {};
};

# Worksheet properties.

type SheetProperties record {
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

type Spreadsheet record {
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

type SpreadsheetProperties record {
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

type ValueRange record {
    # The row number
    int rowPosition;
    # Values of the given row
    (int|string|decimal|boolean|float)[] values;
    # A1Notation of the range
    A1Range a1Range;
};

# Values related to a multiple rows.

type ValuesRange record {
    # The row number
    int rowStartPosition;
    # Values of the given rows
    (int|string|decimal|boolean|float)[] values;
    # A1Notation of the range
    A1Range a1Range;
};

# Defines the generic error type for the `googleapis.sheets` module.
type Error error;

# Error that occurs when an invalid cell range is provided. This could be due to malformed A1 notation or a range
# that falls outside the bounds of the sheet.
type InvalidRangeError error;

# Error that occurs when a spreadsheet or sheet operation fails. This could be due to the resource not being found,
# insufficient permissions, or an API-level rejection.
type SpreadsheetError error;

const string REFRESH_URL = ""https://accounts.google.com/o/oauth2/token"";

# Dimension
enum Dimension {
    UNSPECIFIED_DIMENSION,
    COLUMNS,
    ROWS
}

# The location matching strategy for filters
enum LocationMatchingStrategy {
    UNSPECIFIED_STRATEGY,
    EXACT_LOCATION,
    INTERSECTING_LOCATION
}

# The location type for filters
enum LocationType {
    UNSPECIFIED_LOCATION,
    COLUMN,
    SPREADSHEET,
    SHEET,
    ROW
}

enum ValueInputOption {
    RAW,
    USER_ENTERED
}

enum ValueRenderOption {
    FORMATTED_VALUE,
    UNFORMATTED_VALUE,
    FORMULA
}

# The metadata visibility
enum Visibility {
    UNSPECIFIED_VISIBILITY,
    DOCUMENT,
    PROJECT
}

# Type of filter used to match data.
type Filter A1Range|DeveloperMetadataLookupFilter|GridRangeFilter;

// --- Client ---

# Ballerina Google Sheets connector provides the capability to access Google Sheets API.
# The connector let you perform spreadsheet management operations, worksheet management operations and
# the capability to handle Google Sheets data level operations.
client class Client {
    function init(ConnectionConfig config, string serviceUrl = BASE_URL, string driveServiceUrl = DRIVE_BASE_URL) returns error?;

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
}
