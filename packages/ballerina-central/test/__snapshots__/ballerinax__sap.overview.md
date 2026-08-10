<!-- bal-library overview v1 -->
# ballerinax/sap 0.0.0-fixture

| | |
|---|---|
| Source | central |
| Clients | `Client` |
| Module functions | none |
| Errors | 1, listed below |
| Types | 3 declarations (1 unions, 2 other), not listed here — read one with `type` |

## Next

- `bal-library ops ballerinax/sap <path>` — navigate a client's operations
- `bal-library type ballerinax/sap <Name> [--deps]` — read a declaration whole
- `bal-library api ballerinax/sap` — every declaration, when nothing above answered

## Client `Client`

The `sap` client provides the capability for initiating contact with a remote HTTP service provided by any SAP products. The API it

### Constructor

```ballerina
function init(string url, http:ClientConfiguration config) returns ClientError?; // Special Agent Note: ClientConfiguration FROM ballerina/http package
```

### Remote functions — 7, call with `->`

```ballerina
# The `Client.post()` function can be used to send HTTP POST requests to SAP HTTP endpoints.
remote function post(string path, http:RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), typedesc<TargetType> targetType = <>) returns targetType|ClientError; // Special Agent Note: RequestMessage FROM ballerina/http package

# The `Client.put()` function can be used to send HTTP PUT requests to SAP HTTP endpoints.
remote function put(string path, http:RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), typedesc<TargetType> targetType = <>) returns targetType|ClientError; // Special Agent Note: RequestMessage FROM ballerina/http package

# The `Client.patch()` function can be used to send HTTP PATCH requests to SAP HTTP endpoints.
remote function patch(string path, http:RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), typedesc<TargetType> targetType = <>) returns targetType|ClientError; // Special Agent Note: RequestMessage FROM ballerina/http package

# The `Client.delete()` function can be used to send HTTP DELETE requests to SAP HTTP endpoints.
remote function delete(string path, http:RequestMessage message = (), map<string|string[]> headers = (), string? mediaType = (), typedesc<TargetType> targetType = <>) returns targetType|ClientError; // Special Agent Note: RequestMessage FROM ballerina/http package

# The `Client.head()` function can be used to send HTTP HEAD requests to SAP HTTP endpoints.
remote function head(string path, map<string|string[]> headers = ()) returns http:Response|ClientError; // Special Agent Note: Response FROM ballerina/http package

# The `Client.get()` function can be used to send HTTP GET requests to SAP HTTP endpoints.
remote function get(string path, map<string|string[]> headers = (), typedesc<TargetType> targetType = <>) returns targetType|ClientError;

# The `Client.options()` function can be used to send HTTP OPTIONS requests to SAP HTTP endpoints.
remote function options(string path, map<string|string[]> headers = (), typedesc<TargetType> targetType = <>) returns targetType|ClientError;
```

### Resource functions — 7, call with `->` and a path

```ballerina
# The client resource function to send HTTP POST requests to SAP HTTP endpoints.
resource function post [http:PathParamType... path](http:RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), typedesc<TargetType> targetType = <>, http:QueryParams params) returns targetType|ClientError; // Special Agent Note: RequestMessage, QueryParams FROM ballerina/http package

# The client resource function to send HTTP PUT requests to SAP HTTP endpoints.
resource function put [http:PathParamType... path](http:RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), typedesc<TargetType> targetType = <>, http:QueryParams params) returns targetType|ClientError; // Special Agent Note: RequestMessage, QueryParams FROM ballerina/http package

# The client resource function to send HTTP PATCH requests to SAP HTTP endpoints.
resource function patch [http:PathParamType... path](http:RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), typedesc<TargetType> targetType = <>, http:QueryParams params) returns targetType|ClientError; // Special Agent Note: RequestMessage, QueryParams FROM ballerina/http package

# The client resource function to send HTTP DELETE requests to SAP HTTP endpoints.
resource function delete [http:PathParamType... path](http:RequestMessage message = (), map<string|string[]> headers = (), string? mediaType = (), typedesc<TargetType> targetType = <>, http:QueryParams params) returns targetType|ClientError; // Special Agent Note: RequestMessage, QueryParams FROM ballerina/http package

# The client resource function to send HTTP HEAD requests to SAP HTTP endpoints.
resource function head [http:PathParamType... path](map<string|string[]> headers = (), http:QueryParams params) returns http:Response|ClientError; // Special Agent Note: QueryParams, Response FROM ballerina/http package

# The client resource function to send HTTP GET requests to SAP HTTP endpoints.
resource function get [http:PathParamType... path](map<string|string[]> headers = (), typedesc<TargetType> targetType = <>, http:QueryParams params) returns targetType|ClientError; // Special Agent Note: QueryParams FROM ballerina/http package

# The client resource function to send HTTP OPTIONS requests to SAP HTTP endpoints.
resource function options [http:PathParamType... path](map<string|string[]> headers = (), typedesc<TargetType> targetType = <>, http:QueryParams params) returns targetType|ClientError; // Special Agent Note: QueryParams FROM ballerina/http package
```

## Errors — 1

The subtype chain is what `is` tests against, so `e is <Name>` works off these lines directly.

```ballerina
# Defines the possible client error types.
type ClientError error;
```

## Guide

*The package's own readme, verbatim, with its headings demoted two levels.*

#### Overview

[SAP](https://www.sap.com/index.html) is a global leader in enterprise resource planning (ERP) software. Beyond ERP, SAP offers a diverse range of solutions including human capital management (HCM), customer relationship management (CRM), enterprise performance management (EPM), product lifecycle management (PLM), supplier relationship management (SRM), supply chain management (SCM), and business technology platform (BTP).

The SAP connector provides an HTTP client for interfacing with APIs across SAP's product suite. This client comes with built-in SAP system-compliant CSRF token authentication.

##### Key Features

- Built-in CSRF token authentication support
- Seamless integration with various SAP product APIs
- Efficient handling of HTTP-based SAP service communications
- Support for complex SAP business object interactions

#### Setup guide

In this guide, we'll be utilizing the `S/4HANA` Sales Order API to showcase the capabilities of the SAP Client.

##### Step 1: Login

1. Sign in to your S/4HANA dashboard.

##### Step 2: Create a Communication System

1. Under the `Communication Management` section, click on the `Display Communications Scenario` title.

   ![Communication Systems](https://raw.githubusercontent.com/ballerina-platform/module-ballerinax-sap/main/docs/setup/2-1-communications-system.png)

2. In the top right corner of the screen, click `New`.

   ![Click New](https://raw.githubusercontent.com/ballerina-platform/module-ballerinax-sap/main/docs/setup/2-2-create-new.png)

3. Give a system id.

   ![System Id](https://raw.githubusercontent.com/ballerina-platform/module-ballerinax-sap/main/docs/setup/2-3-system-id.png)

4. Give the hostname as your S/4HANA hostname.

   ![Give Hostname](https://raw.githubusercontent.com/ballerina-platform/module-ballerinax-sap/main/docs/setup/2-4-give-hostname.png)

5. Add `Users` for `Inbound Communication`.

   ![Add User](https://raw.githubusercontent.com/ballerina-platform/module-ballerinax-sap/main/docs/setup/2-5-add-user.png)

6. Select the `Authentication Method` and `User`.

   ![Select User](https://raw.githubusercontent.com/ballerina-platform/module-ballerinax-sap/main/docs/setup/2-6-select-user.png)

7. Click Save.

##### Step 3: Create a Communication Arrangement

1. Under the `Communication Management` section, click on the `Display Communications Scenario` title.

   ![Display Scenarios](https://raw.githubusercontent.com/ballerina-platform/module-ballerinax-sap/main/docs/setup/3-1-display-scenarios.png)

2. In the search bar, type `Sales Order Integration` and select the corresponding scenario from the results.

   ![Search Sales Order](https://raw.githubusercontent.com/ballerina-platform/module-ballerinax-sap/main/docs/setup/3-2-search-sales-order.png)

3. In the top right corner of the screen, click on `Create Communication Arrangement`.

   ![Click Create Arrangement](https://raw.githubusercontent.com/ballerina-platform/module-ballerinax-sap/main/docs/setup/3-3-click-create-arrangement.png)

4. Enter a unique name for the arrangement.

   ![Give Arrangement Name](https://raw.githubusercontent.com/ballerina-platform/module-ballerinax-sap/main/docs/setup/3-4-give-arrangement-name.png)

5. Choose an existing `Communication System` from the dropdown menu and save your arrangement.

   ![Select Existing Communication Arrangement](https://raw.githubusercontent.com/ballerina-platform/module-ballerinax-sap/main/docs/setup/3-5-select-communication-system.png)

6. The hostname (`<unique id>-api.s4hana.cloud.sap`) will be displayed in the top right corner of the screen.

   ![View Hostname](https://raw.githubusercontent.com/ballerina-platform/module-ballerinax-sap/main/docs/setup/3-6-view-hostname.png)

#### Quickstart

To use the `sap` connector in your Ballerina application, modify the `.bal` file as follows:

##### Step 1: Import the module

Import the `sap` module.

```ballerina
import ballerinax/sap;
```

##### Step 2: Instantiate a new connector

```ballerina
configurable string hostname = ?;
configurable string username = ?;
configurable string password = ?;

sap:Client sapClient = check new (string `https://${hostname}/sap/opu/odata/sap/API_SALES_ORDER_SRV`, {
    auth: {
        username,
        password
    }
});
```

##### Step 3: Invoke the connector operation

Now, utilize the available connector operations.

```ballerina
json salesOrderList = check sapClient->/A_SalesOrder();
```

##### Step 4: Run the Ballerina application

```bash
bal run
```

#### Examples

The `sap` connector provides practical examples illustrating usage in various scenarios. Explore
these [examples](https://github.com/ballerina-platform/module-ballerinax-sap/tree/master/examples), covering use cases
like accessing S/4HANA Sales Order (A2X) API.

1. [Send a reminder on approval of pending orders](https://github.com/ballerina-platform/module-ballerinax-sap/tree/main/examples/pending-order-reminder) -
   This example illustrates the use of the `sap:Client` in Ballerina to interact with S/4HANA APIs. Specifically, it
   demonstrates how to send a reminder email for sales orders that are pending approval.
