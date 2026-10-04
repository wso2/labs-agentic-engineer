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

package github

import (
	"context"
	"errors"
)

// Identity reports the GitHub user the gitpat belongs to.
type Identity interface {
	Whoami(ctx context.Context) (login string, id int64, err error)
}

var _ Identity = (*Client)(nil)

// User is the gitpat user as GET /user describes them. Name and Email are
// empty when the user has not made them public.
type User struct {
	Login string
	ID    int64
	Name  string
	Email string
}

// Whoami answers GET /user: the login and numeric id of the gitpat's user.
func (c *Client) Whoami(ctx context.Context) (string, int64, error) {
	u, err := c.User(ctx)
	if err != nil {
		return "", 0, err
	}
	return u.Login, u.ID, nil
}

// User answers GET /user: the gitpat's user, with name and email when
// public. A refusal is an *HTTPStatusError (a rate limit as 429 with
// RetryAfter).
func (c *Client) User(ctx context.Context) (User, error) {
	var user struct {
		Login string `json:"login"`
		ID    int64  `json:"id"`
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if err := c.getJSON(ctx, c.apiBase+"/user", &user); err != nil {
		return User{}, err
	}
	if user.Login == "" || user.ID == 0 {
		return User{}, errors.New("github user: login or id missing")
	}
	return User{Login: user.Login, ID: user.ID, Name: user.Name, Email: user.Email}, nil
}
