// Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
//
// WSO2 LLC. licenses this file to you under the Apache License,
// Version 2.0 (the "License"); you may not use this file except
// in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package thundersvc

// directory.go — the group/user half of the Thunder admin surface, used by the
// build-time roles ensure to make a project's declared roles and test users
// real. Everything here lands in the DEFAULT organization unit, matching where
// the thunder-app operator registers every generated app's OAuth client: a user
// in a different OU is not a user of the app.
//
// Three Thunder facts shape this file, all established against a running
// Thunder 0.34.0 rather than read from documentation:
//
//  1. **Group membership can only be set when the group is CREATED.**
//     `PUT /groups/{id}` accepts a `members` array, answers 200, and silently
//     ignores it; there is no `POST/PUT/PATCH /groups/{id}/members` (404/405)
//     and no SCIM surface. So adding a member to an existing group means
//     delete-and-recreate with the merged list — see AddGroupMembers, and the
//     ownership rule its doc comment names.
//  2. **Passwords are write-only on read-back, but `PUT /users/{id}` echoes the
//     submitted password in plaintext.** Same value the caller sent, so no new
//     disclosure — but that response body must never reach a log or an error
//     string. setUserAttributes is the one place that PUTs a user, and it
//     deliberately does not read the body on success and does not interpolate
//     it on failure.
//  3. **There is no server-side user filter.** `GET /users?filter=…` is a 400,
//     so finding a user by username is a bounded client-side scan.
//
// Requests here go through doJSON rather than the hand-rolled blocks the
// application half of this client uses. That is deliberate scope control: the
// new surface gets the helper, the shipped surface is left alone.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

// Group is one directory group — a Role, in the platform's own vocabulary.
type Group struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	OUID        string `json:"ouId"`
}

// DirectoryUser is one person-type account. It deliberately has no password
// field: Thunder never returns one, which is why the platform seals its own
// copy of every password it generates.
type DirectoryUser struct {
	ID       string
	Username string
	Email    string
}

// directoryPageSize / maxDirectoryPages bound every listing. The cap mirrors
// the application listing's (100 × 100): a directory larger than that is a
// deployment this build-time path was not designed for, and a silent partial
// answer would be worse than an error.
const (
	directoryPageSize = 100
	maxDirectoryPages = 100
)

// morePages decides whether to fetch another page.
//
// It keys on Thunder's `totalResults`, NOT on "this page came back short".
// Nothing obliges a server to honour the requested `limit`: if Thunder capped
// it at, say, 50, a short-page rule would treat page 0 as the last one and
// every listing would silently return the first 50 records — FindGroupByName
// would then report an existing role as absent and the ensure would try to
// create it, 409. A silently partial directory read is the worst failure this
// file can have, so the stop condition is "we have them all" or "the server
// gave us nothing more", never "the page looked small".
// The cursor that goes with it is just as load-bearing: every caller advances
// the offset by what it has actually RECEIVED, never by page × requested-limit.
// Against a server that caps `limit` at 3, `offset=100` on the second request
// lands past the end, comes back empty, and stops the loop three records into a
// seven-record directory — the same silent truncation, moved one line over.
func morePages(got, total, receivedThisPage int) bool {
	if receivedThisPage == 0 {
		return false
	}
	if total > 0 {
		return got < total
	}
	return true
}

// ListGroups returns every group in the default OU.
func (c *client) ListGroups(ctx context.Context) ([]Group, error) {
	token, err := c.getSystemToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("getSystemToken: %w", err)
	}
	var out []Group
	for page := 0; page < maxDirectoryPages; page++ {
		var body struct {
			TotalResults int     `json:"totalResults"`
			Groups       []Group `json:"groups"`
		}
		path := fmt.Sprintf("/groups?limit=%d&offset=%d", directoryPageSize, len(out))
		if err := c.doJSON(ctx, token, http.MethodGet, path, nil, &body); err != nil {
			return nil, err
		}
		out = append(out, body.Groups...)
		if !morePages(len(out), body.TotalResults, len(body.Groups)) {
			return out, nil
		}
	}
	return nil, fmt.Errorf("thunder list groups: more than %d pages", maxDirectoryPages)
}

// FindGroupByName returns the group with this exact name, if one exists.
// Thunder enforces name uniqueness per OU (409 GRP-1004 on a duplicate), so at
// most one can match. The comparison is case-insensitive because the platform
// treats two role names differing only in case as one role.
func (c *client) FindGroupByName(ctx context.Context, name string) (*Group, bool, error) {
	groups, err := c.ListGroups(ctx)
	if err != nil {
		return nil, false, err
	}
	for i := range groups {
		if strings.EqualFold(groups[i].Name, name) {
			return &groups[i], true, nil
		}
	}
	return nil, false, nil
}

// GroupMembers returns the user ids in a group. Only `user`-type members are
// returned; a nested group member would not be an account that can sign in.
func (c *client) GroupMembers(ctx context.Context, groupID string) ([]string, error) {
	token, err := c.getSystemToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("getSystemToken: %w", err)
	}
	return c.groupMembersWith(ctx, token, groupID)
}

// groupMembersWith is GroupMembers with a token the caller already holds, so
// AddGroupMembers can read membership inside its lock without re-minting.
func (c *client) groupMembersWith(ctx context.Context, token, groupID string) ([]string, error) {
	var out []string
	seen := 0
	for page := 0; page < maxDirectoryPages; page++ {
		var body struct {
			TotalResults int `json:"totalResults"`
			Members      []struct {
				ID   string `json:"id"`
				Type string `json:"type"`
			} `json:"members"`
		}
		path := fmt.Sprintf("/groups/%s/members?limit=%d&offset=%d",
			url.PathEscape(groupID), directoryPageSize, seen)
		if err := c.doJSON(ctx, token, http.MethodGet, path, nil, &body); err != nil {
			return nil, err
		}
		for _, m := range body.Members {
			if strings.EqualFold(m.Type, "user") {
				out = append(out, m.ID)
			}
		}
		// seen counts RAW members, not the user-typed subset: paging is over
		// what the server returned, and a page of nested groups still advances
		// the window.
		seen += len(body.Members)
		if !morePages(seen, body.TotalResults, len(body.Members)) {
			return out, nil
		}
	}
	return nil, fmt.Errorf("thunder list group members: more than %d pages", maxDirectoryPages)
}

// CreateGroup creates a group in the default OU with exactly these members.
// This is the ONLY call that can set membership (see the file header), so a
// caller that needs a member on an EXISTING group goes through AddGroupMembers
// instead — which composes a read and this call under one lock.
func (c *client) CreateGroup(ctx context.Context, name, description string, memberIDs []string) (Group, error) {
	token, err := c.getSystemToken(ctx)
	if err != nil {
		return Group{}, fmt.Errorf("getSystemToken: %w", err)
	}
	ou, err := c.getDefaultOUID(ctx, token)
	if err != nil {
		return Group{}, err
	}
	c.lockGroup(name)
	defer c.unlockGroup(name)
	return c.createGroupLocked(ctx, token, ou, name, description, memberIDs)
}

// createGroupLocked is the body of CreateGroup, callable by a holder of the
// per-name lock (AddGroupMembers holds it across its read+delete+create).
func (c *client) createGroupLocked(ctx context.Context, token, ou, name, description string, memberIDs []string) (Group, error) {
	members := make([]map[string]string, 0, len(memberIDs))
	for _, id := range memberIDs {
		members = append(members, map[string]string{"id": id, "type": "user"})
	}
	req := map[string]any{
		"name": name, "description": description, "ouId": ou, "members": members,
	}
	var created Group
	if err := c.doJSON(ctx, token, http.MethodPost, "/groups", req, &created); err != nil {
		return Group{}, err
	}
	return created, nil
}

// AddGroupMembers adds memberIDs to a group, preserving everyone already in it.
//
// The read of the current membership and the write of the union happen under
// ONE hold of the group's lock. Serialising only the write would not be enough:
// two goroutines could each read {u1}, then write {u1,u2} and {u1,u3} in turn,
// and the second would drop u2 — the lost update this whole mechanism exists to
// prevent. The lock has to span the read-modify-write, so the composition is
// here rather than in the caller.
//
// Nothing to add is nothing to do. That matters beyond efficiency: the only
// membership write Thunder offers is a delete-and-recreate, so a no-op that
// "wrote anyway" would change the group's id on every build for no reason.
//
// Returns the group as it now stands — a DIFFERENT id when members were added.
func (c *client) AddGroupMembers(ctx context.Context, group Group, memberIDs []string) (Group, error) {
	token, err := c.getSystemToken(ctx)
	if err != nil {
		return Group{}, fmt.Errorf("getSystemToken: %w", err)
	}
	ou := group.OUID
	if ou == "" {
		if ou, err = c.getDefaultOUID(ctx, token); err != nil {
			return Group{}, err
		}
	}
	c.lockGroup(group.Name)
	defer c.unlockGroup(group.Name)

	existing, err := c.groupMembersWith(ctx, token, group.ID)
	if err != nil {
		return Group{}, fmt.Errorf("read members of group %q: %w", group.Name, err)
	}
	have := make(map[string]bool, len(existing))
	for _, id := range existing {
		have[id] = true
	}
	union := append([]string(nil), existing...)
	for _, id := range memberIDs {
		if !have[id] {
			have[id] = true
			union = append(union, id)
		}
	}
	if len(union) == len(existing) {
		return group, nil
	}
	if err := c.doJSON(ctx, token, http.MethodDelete, "/groups/"+url.PathEscape(group.ID), nil, nil); err != nil {
		return Group{}, fmt.Errorf("add members to group %q: %w", group.Name, err)
	}
	recreated, err := c.createGroupLocked(ctx, token, ou, group.Name, group.Description, union)
	if err != nil {
		return Group{}, fmt.Errorf("add members to group %q: recreate after delete: %w", group.Name, err)
	}
	return recreated, nil
}

// userRecord is Thunder's wire shape for a person. `attributes` is an open map
// on the wire, but Thunder rejects unknown keys (400 USR-1019), so the platform
// cannot stamp an ownership marker on the account itself — its own `test_users`
// row is the marker instead.
//
// Attributes is `map[string]any`, NOT `map[string]string`, because Thunder's
// attribute values are NOT all strings: the default `admin` account ships with
// `email_verified: true` and `phone_number_verified: true`. A string-typed map
// fails to decode the whole LISTING the moment one such account exists, which
// is every deployment — so `FindUserByUsername` would error on every build.
// Found by running the ensure against a live Thunder; a fixture of
// string-only attributes never sees it.
//
// It is also what makes SetUserPassword's read-merge-write correct: `PUT` is a
// full replace, so a boolean attribute has to survive the round trip as a
// boolean rather than being coerced or dropped.
type userRecord struct {
	ID         string         `json:"id,omitempty"`
	OUID       string         `json:"ouId,omitempty"`
	Type       string         `json:"type,omitempty"`
	Attributes map[string]any `json:"attributes,omitempty"`
}

func (u userRecord) toDirectoryUser() DirectoryUser {
	return DirectoryUser{
		ID:       u.ID,
		Username: stringAttr(u.Attributes, "username"),
		Email:    stringAttr(u.Attributes, "email"),
	}
}

// stringAttr reads one attribute as a string, answering "" for an absent key or
// a non-string value. The platform only ever reads `username` and `email`, both
// of which Thunder types as strings; anything else is somebody's data to carry
// through untouched, not to interpret.
func stringAttr(attrs map[string]any, key string) string {
	s, _ := attrs[key].(string)
	return s
}

// FindUserByUsername scans the directory for an exact username match. Thunder
// has no server-side filter (`?filter=` is a 400), so this is a bounded
// client-side scan — acceptable because the roles ensure runs once per build,
// not per request.
func (c *client) FindUserByUsername(ctx context.Context, username string) (*DirectoryUser, bool, error) {
	token, err := c.getSystemToken(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("getSystemToken: %w", err)
	}
	seen := 0
	for page := 0; page < maxDirectoryPages; page++ {
		var body struct {
			TotalResults int          `json:"totalResults"`
			Users        []userRecord `json:"users"`
		}
		path := fmt.Sprintf("/users?limit=%d&offset=%d", directoryPageSize, seen)
		if err := c.doJSON(ctx, token, http.MethodGet, path, nil, &body); err != nil {
			return nil, false, err
		}
		for _, u := range body.Users {
			if stringAttr(u.Attributes, "username") == username {
				found := u.toDirectoryUser()
				return &found, true, nil
			}
		}
		seen += len(body.Users)
		if !morePages(seen, body.TotalResults, len(body.Users)) {
			return nil, false, nil
		}
	}
	return nil, false, fmt.Errorf("thunder find user: more than %d pages", maxDirectoryPages)
}

// CreateUser creates a person-type account in the default OU. The password is
// write-only from here on: Thunder never returns it on a read, which is why the
// caller must seal its own copy before this function's result is discarded.
func (c *client) CreateUser(ctx context.Context, username, email, password string) (DirectoryUser, error) {
	token, err := c.getSystemToken(ctx)
	if err != nil {
		return DirectoryUser{}, fmt.Errorf("getSystemToken: %w", err)
	}
	ou, err := c.getDefaultOUID(ctx, token)
	if err != nil {
		return DirectoryUser{}, err
	}
	req := userRecord{
		OUID: ou, Type: "Person",
		Attributes: map[string]any{"username": username, "email": email, "password": password},
	}
	var created userRecord
	if err := c.doJSONNoEcho(ctx, token, http.MethodPost, "/users", req, &created); err != nil {
		return DirectoryUser{}, err
	}
	return created.toDirectoryUser(), nil
}

// SetUserPassword rotates an account's password.
//
// `PUT /users/{id}` is a FULL REPLACE of the attributes map, so this reads the
// user first and re-sends every attribute with the password merged in; sending
// only the password would erase the account's email and username. The response
// echoes the new password in plaintext and is therefore discarded unread.
func (c *client) SetUserPassword(ctx context.Context, userID, password string) error {
	token, err := c.getSystemToken(ctx)
	if err != nil {
		return fmt.Errorf("getSystemToken: %w", err)
	}
	var existing userRecord
	// The read uses the no-echo path as well. Thunder does not return a password
	// on a GET today, but the whole attributes map would otherwise land in an
	// error string — and this is the one endpoint whose attributes map is known
	// to be able to carry a password.
	if err := c.doJSONNoEcho(ctx, token, http.MethodGet, "/users/"+url.PathEscape(userID), nil, &existing); err != nil {
		return err
	}
	// Every existing attribute is carried through VERBATIM, whatever its type:
	// PUT is a full replace, so dropping or coercing one loses it.
	attrs := make(map[string]any, len(existing.Attributes)+1)
	for k, v := range existing.Attributes {
		attrs[k] = v
	}
	attrs["password"] = password
	req := userRecord{OUID: existing.OUID, Type: existing.Type, Attributes: attrs}
	return c.doJSONNoEcho(ctx, token, http.MethodPut, "/users/"+url.PathEscape(userID), req, nil)
}

// DeleteUser removes an account. Idempotent: an account that is already gone
// is success, so a retried console delete does not surface an error.
func (c *client) DeleteUser(ctx context.Context, userID string) error {
	token, err := c.getSystemToken(ctx)
	if err != nil {
		return fmt.Errorf("getSystemToken: %w", err)
	}
	return c.deleteIfPresent(ctx, token, "/users/"+url.PathEscape(userID))
}

// deleteIfPresent makes DELETE idempotent by treating "already gone" as done.
// doRequest errors on any non-2xx, so without this a retried delete — the
// console clicking twice, a re-run after a partial failure — would surface a
// 404 as a failure the user cannot act on.
func (c *client) deleteIfPresent(ctx context.Context, token, path string) error {
	err := c.doRequest(ctx, token, http.MethodDelete, path, nil, nil, true)
	var status *statusError
	if errors.As(err, &status) && status.code == http.StatusNotFound {
		return nil
	}
	return err
}

// -- per-group serialisation ------------------------------------------------

// lockGroup serialises writes to one group NAME within this process.
//
// The build fans out, and group membership has no atomic add: two goroutines
// reading the same group's members and then writing would lose one of them.
// That exact shape has bitten this codebase before (the per-org git-secret
// race), so the whole READ-MODIFY-WRITE is serialised at its narrowest useful
// key — AddGroupMembers holds this lock across its members read, its delete and
// its create. Serialising only the write would not be enough: both goroutines
// would still have read the same stale membership first.
//
// This does NOT serialise across BFF replicas. The residual is a lost test-user
// membership on a role two projects build simultaneously; the next build of
// either re-adds it, because the ensure is idempotent and re-reads membership.
func (c *client) lockGroup(name string) {
	c.groupLocks.mu.Lock()
	if c.groupLocks.byName == nil {
		c.groupLocks.byName = map[string]*sync.Mutex{}
	}
	key := strings.ToLower(name)
	m, ok := c.groupLocks.byName[key]
	if !ok {
		m = &sync.Mutex{}
		c.groupLocks.byName[key] = m
	}
	c.groupLocks.mu.Unlock()
	m.Lock()
}

func (c *client) unlockGroup(name string) {
	c.groupLocks.mu.Lock()
	m := c.groupLocks.byName[strings.ToLower(name)]
	c.groupLocks.mu.Unlock()
	if m != nil {
		m.Unlock()
	}
}

// -- request helper ----------------------------------------------------------

// doJSON issues one authenticated JSON request. A 2xx with out != nil decodes
// the body; 204 and out == nil discard it. A non-2xx becomes an error carrying
// the response body, which is how the rest of this client reports Thunder
// failures — use doJSONNoEcho for any request whose body contains a secret.
func (c *client) doJSON(ctx context.Context, token, method, path string, in, out any) error {
	return c.doRequest(ctx, token, method, path, in, out, true)
}

// doJSONNoEcho is doJSON for a request whose body or response carries a
// password. It never puts a response body into an error, because Thunder's
// user endpoints echo the submitted password back in plaintext.
func (c *client) doJSONNoEcho(ctx context.Context, token, method, path string, in, out any) error {
	return c.doRequest(ctx, token, method, path, in, out, false)
}

func (c *client) doRequest(ctx context.Context, token, method, path string, in, out any, echoBody bool) error {
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("thunder %s %s: encode request: %w", method, redactPath(path), err)
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("thunder %s %s: %w", method, redactPath(path), err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		if !echoBody {
			return &statusError{code: resp.StatusCode, msg: fmt.Sprintf(
				"thunder %s %s returned %d", method, redactPath(path), resp.StatusCode)}
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &statusError{code: resp.StatusCode, msg: fmt.Sprintf(
			"thunder %s %s returned %d: %s", method, redactPath(path), resp.StatusCode, string(raw))}
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("thunder %s %s: decode response: %w", method, redactPath(path), err)
	}
	return nil
}

// statusError carries the HTTP status alongside the message so a caller can
// branch on it — deleteIfPresent needs "was it a 404" without string-matching.
type statusError struct {
	code int
	msg  string
}

func (e *statusError) Error() string { return e.msg }

// redactPath keeps a directory id out of an error string that may reach a log.
// The id is not a secret, but it is a directory identifier and errors from this
// path travel into build output the project's members read.
func redactPath(path string) string {
	base, query, _ := strings.Cut(path, "?")
	segments := strings.Split(base, "/")
	for i, s := range segments {
		if len(s) >= 32 && strings.Count(s, "-") >= 4 {
			segments[i] = "{id}"
		}
	}
	out := strings.Join(segments, "/")
	if query != "" {
		// Keep the paging window; it is diagnostic and carries nothing sensitive.
		return out + "?" + query
	}
	return out
}
