<!-- bal library overview v1 -->
# ballerina/xlsx 0.0.0-fixture

| | |
|---|---|
| Clients | none |
| Classes | 3 — `Workbook`, `Sheet`, `Table` — `bal library class ballerina/xlsx` |
| Module functions | 6 — `bal library funcs ballerina/xlsx` |
| Errors | 11 — `ConstraintValidationError`, `Error`, `FileNotFoundError`, `InvalidTableRangeError`, `ParseError`, `SheetExistsError`, `SheetNotFoundError`, `TableExistsError`, `TableNotFoundError`, `TableOverlapError`, `TypeConversionError` — read one with `bal library type ballerina/xlsx <Name>` |
| Types | 29 declarations (19 records, 2 type aliases, 5 enums, 3 classes and object types), not listed here — read one with `type` |
| Guide | 201 lines — `bal library guide ballerina/xlsx` |

Guide chunks (10): 1. `Step 1: Import the module`  2. `Parse an XLSX file into typed records`  3. `Write records to an XLSX file`  4. `Map non-matching headers with `@xlsx:Name``  5. `Work with multiple sheets via the `Workbook` API`  6. `Bytes in, bytes out`  7. `Read and write Excel Tables`  8. `Bind dates and times to `time:Civil`, `time:Date`, or `time:TimeOfDay``  9. `Continue parsing on row-level errors with fail-safe mode`  10. `Step 3: Run the Ballerina application` — `bal library guide ballerina/xlsx <n>`

## Next

- `bal library class ballerina/xlsx` — 3 classes and object types, called with `.`
- `bal library funcs ballerina/xlsx` — 6 functions callable without a client
- `bal library overview ballerina/xlsx -s "<what you need>"` — search every kind at once when you do not know which verb holds it
- `bal library type ballerina/xlsx <Name> [-r]` — a declaration whole, with the types it names

## Classes and object types — 3

- `Workbook` — 12 normal · `bal library class ballerina/xlsx Workbook`
- `Sheet` — 21 normal · `bal library class ballerina/xlsx Sheet`
- `Table` — 18 normal · `bal library class ballerina/xlsx Table`

## Module-level functions — 6, call with `.`

```
fromBytes   fromFile    parseSheet  parseTable
writeSheet  writeTable
```

## Quickstart

*Every Ballerina block in the package's own readme, quoted verbatim and in its order. It is Central's text and can be out of date; the signatures the container verbs generate win wherever the two disagree.*

<!-- guide: begin ballerina/xlsx readme usage -->

```ballerina
import ballerina/xlsx;
```

```ballerina
type Employee record {|
    string name;
    int age;
    string department;
|};

Employee[] employees = check xlsx:parseSheet("employees.xlsx");
```

```ballerina
Employee[] sales = check xlsx:parseSheet("report.xlsx", "Sales");
map<xlsx:CellValue>[] rows = check xlsx:parseSheet("unknown.xlsx");
string[][] raw = check xlsx:parseSheet("anything.xlsx");
```

```ballerina
Employee[] employees = [
    {name: "John", age: 30, department: "IT"},
    {name: "Jane", age: 28, department: "HR"}
];

check xlsx:writeSheet(employees, "output.xlsx", "Employees");
```

```ballerina
// Replace the sheet's contents, keeping the rest of the workbook.
check xlsx:writeSheet(employees, "report.xlsx", "Employees", sheetWriteMode = xlsx:REPLACE);

// Append rows below the existing data.
check xlsx:writeSheet(employees, "report.xlsx", "Employees", sheetWriteMode = xlsx:APPEND);
```

```ballerina
type Employee record {|
    @xlsx:Name {value: "Employee Name"}
    string name;

    @xlsx:Name {value: "Years of Service"}
    int tenure;
|};

Employee[] employees = check xlsx:parseSheet("employees.xlsx");
```

```ballerina
xlsx:Workbook wb = check xlsx:fromFile("report.xlsx");

string[] sheetNames = wb.getSheetNames();
xlsx:Sheet sales = check wb.getSheet("Sales");
Employee[] salesRows = check sales.getRows();

xlsx:Sheet summary = check wb.createSheet("Summary");
check summary.putRows(salesRows);

check wb.save();
check wb.close();
```

```ballerina
xlsx:Workbook wb1 = new;                                    // empty in-memory workbook (saveAs required to persist)
xlsx:Workbook wb2 = check xlsx:fromFile("existing.xlsx");   // open an existing file (errors if missing)
xlsx:Workbook wb3 = check xlsx:fromBytes(sourceBytes);      // open from a byte array (e.g., SFTP / HTTP body)
```

```ballerina
byte[] inputBytes = check sftp->get("/in/orders.xlsx");
xlsx:Workbook wb = check xlsx:fromBytes(inputBytes);

xlsx:Sheet sheet = check wb.getSheet(0);
Order[] orders = check sheet.getRows();
// ... enrich orders ...
check sheet.putRows(orders);

byte[] outputBytes = check wb.toBytes();
check sftp->put("/out/orders-enriched.xlsx", outputBytes);
check wb.close();
```

```ballerina
Employee[] employees = check xlsx:parseTable("sales.xlsx", "EmployeeTable");

Employee[] newEmployees = [{name: "Alice", age: 31, department: "Eng"}];
check xlsx:writeTable([...employees, ...newEmployees], "sales.xlsx", "EmployeeTable");
// writeTable resizes the table's data range to fit the data (grows or shrinks)
```

```ballerina
xlsx:Workbook wb = check xlsx:fromFile("sales.xlsx");
xlsx:Table empTable = check wb.getTable("EmployeeTable");

Employee[] employees = check empTable.getRows();
if check empTable.hasTotalRow() {
    map<xlsx:CellValue> totals = check empTable.getTotalRow();
    // ...
}
check empTable.putRows([...employees, ...newEmployees]);

check wb.save();
check wb.close();
```

```ballerina
import ballerina/time;

type Transaction record {|
    int id;
    time:Civil timestamp;      // date-time cell → time:Civil
    time:Date settledOn;       // date-only cell → time:Date
    decimal amount;
|};

Transaction[] txns = check xlsx:parseSheet("transactions.xlsx");
```

```ballerina
Employee[] employees = check xlsx:parseSheet("data.xlsx", 0, {
    failSafe: {
        enableConsoleLogs: true,
        fileOutputMode: {
            filePath: "./xlsx-errors.log",
            contentType: RAW_AND_METADATA
        }
    }
});
```

<!-- guide: end ballerina/xlsx readme usage -->
