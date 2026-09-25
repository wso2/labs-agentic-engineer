# The roles view names the project's sign-in

The Security/roles view carries `signIn { issuer, clientId }`: how a client
that is not one of the project's own components signs in to it. The platform's
test app (`apps/tryit`) is that client — the console opens it with these two
values, the project's resource server and the published test users' scopes,
and the app runs Authorization Code + PKCE against the project's own identity
provider. Both values are public; a password never travels this path.

## Read off the binding, not the directory

Client id and issuer are the sign-in resource's binding outputs, reached
through `SignInCoordinates` (satisfied by provisioning, which owns the
binding; wired late because provisioning is built after the panel). They are
NOT read from the environment's directory: that needs the T2 admin credential,
and when it is unavailable the panel already degrades to
`directoryAvailable: false`. Publishing the sign-in from the binding keeps the
console offering a way in exactly when the admin half of the panel cannot say
who exists. The directory's issuer is only a fallback for a binding that
predates the `issuer` output.

## Absent, never empty

No sign-in resource, or a binding not yet resolved, means the block is absent.
An empty client id would have a reader build an authorize URL that can only
fail on the identity provider's page.

## What this replaced

The console's Test tab relayed the caller's platform token to the component
(`invoke-component`), and later signed a test user in from inside aep-api
(`actAs`). Both were removed with the test app: a person signs in on the
identity provider's page as the project's test user, and the platform holds no
token on their behalf.
