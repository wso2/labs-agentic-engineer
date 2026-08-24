<!-- bal library overview v1 -->
# ballerinax/github 0.0.0-fixture

| | |
|---|---|
| Clients | 1 — `Client` — `bal library client ballerinax/github` |
| Classes | none |
| Module functions | none |
| Errors | none declared here; each operation names its error type in its `returns` clause |
| Types | 1,227 declarations (1,101 records, 126 type aliases), not listed here — read one with `type` |
| Guide | 111 lines — `bal library guide ballerinax/github` |

Guide chunks (4): 1. `Step 1: Import the connector`  2. `Step 2: Instantiate a new connector`  3. `Get Private Repositories of Authenticated User`  4. `Create a Private Repository` — `bal library guide ballerinax/github <n>`

## Next

- `bal library client ballerinax/github Client` — the client's whole callable surface
- `bal library overview ballerinax/github -s "<what you need>"` — search every kind at once when you do not know which verb holds it
- `bal library type ballerinax/github <Name> [-r]` — a declaration whole, with the types it names

## Clients — 1

- `Client` — 903 resource · `bal library client ballerinax/github Client`

## Quickstart

*Every Ballerina block in the package's own readme, quoted verbatim and in its order. It is Central's text and can be out of date; the signatures the container verbs generate win wherever the two disagree.*

<!-- guide: begin ballerinax/github readme usage -->

```ballerina
import ballerinax/github;
```

```ballerina
github:ConnectionConfig gitHubConfig = {
    auth: {
        token: authToken
    }
};
github:Client github = check new (gitHubConfig);
```

```ballerina
github:Repository[] userRepos = check github->/user/repos(visibility = "private", 'type = ());
```

```ballerina
github:User_repos_body body = {
    name: "New Test Repo Name",
    'private: true,
    description: "New Test Repo Description"
};
github:Repository createdRepo = check github->/user/repos.post(body);
```

<!-- guide: end ballerinax/github readme usage -->
