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

package requests

import "fmt"

// HttpError represents a non-successful HTTP response.
type HttpError struct {
	StatusCode int
	Body       string
	err        error
}

func (e *HttpError) Error() string {
	if e.err != nil {
		return fmt.Sprintf("http error %d: %s: %v", e.StatusCode, e.Body, e.err)
	}
	return fmt.Sprintf("http error %d: %s", e.StatusCode, e.Body)
}

func (e *HttpError) Unwrap() error {
	return e.err
}
