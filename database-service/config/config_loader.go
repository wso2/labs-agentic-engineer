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

package config

import (
	"os"
	"strconv"
)

// Load reads environment variables and returns a Config.
func Load() (*Config, error) {
	serverPort := 3500 // default port
	if p := os.Getenv("SERVER_PORT"); p != "" {
		if parsed, err := strconv.Atoi(p); err == nil {
			serverPort = parsed
		}
	}

	mysqlPort := 3306 // default MySQL port
	if p := os.Getenv("MYSQL_PORT"); p != "" {
		if parsed, err := strconv.Atoi(p); err == nil {
			mysqlPort = parsed
		}
	}

	return &Config{
		ServerHost:   getEnv("SERVER_HOST", "0.0.0.0"),
		ServerPort:   serverPort,
		LogLevel:     getEnv("LOG_LEVEL", "info"),
		MySQLRootURL: getEnv("MYSQL_ROOT_URL", "root:root@tcp(mysql:3306)/"),
		MySQLHost:    getEnv("MYSQL_HOST", "mysql"),
		MySQLPort:    mysqlPort,
	}, nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
