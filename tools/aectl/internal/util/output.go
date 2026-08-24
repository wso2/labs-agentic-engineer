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

// Package util provides CLI output helpers.
// Prefer the ui package for styled, user-facing output.
package util

import (
	"github.com/wso2/aep/aectl/internal/ui"
)

func Info(msg string)  { ui.Print(msg) }
func Error(msg string) { ui.Fail(msg) }
func Fatal(msg string) { ui.Fatal(msg) }
