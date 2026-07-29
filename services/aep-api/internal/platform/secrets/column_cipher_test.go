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

package secrets

import (
	"bytes"
	"testing"
)

func TestColumnCipher_RoundTrip(t *testing.T) {
	c, err := NewColumnCipher(bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatalf("NewColumnCipher: %v", err)
	}
	plain := []byte("publisher-client-secret-value")
	sealed, err := c.Seal(plain)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if sealed == string(plain) {
		t.Fatal("Seal returned plaintext")
	}
	if !c.IsSealed(sealed) {
		t.Fatal("IsSealed(sealed) = false")
	}
	got, err := c.Open(sealed)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("Open = %q; want %q", got, plain)
	}
}

func TestColumnCipher_OpenTolerant_Plaintext(t *testing.T) {
	c, err := NewColumnCipher(make([]byte, 32))
	if err != nil {
		t.Fatalf("NewColumnCipher: %v", err)
	}
	got, err := c.OpenTolerant("still-plaintext")
	if err != nil {
		t.Fatalf("OpenTolerant: %v", err)
	}
	if string(got) != "still-plaintext" {
		t.Fatalf("OpenTolerant = %q", got)
	}
	if c.IsSealed("still-plaintext") {
		t.Fatal("plaintext must not report IsSealed")
	}
}

func TestColumnCipher_OpenTolerant_Ciphertext(t *testing.T) {
	c, err := NewColumnCipher(make([]byte, 32))
	if err != nil {
		t.Fatalf("NewColumnCipher: %v", err)
	}
	sealed, err := c.Seal([]byte("secret"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	got, err := c.OpenTolerant(sealed)
	if err != nil {
		t.Fatalf("OpenTolerant: %v", err)
	}
	if string(got) != "secret" {
		t.Fatalf("OpenTolerant = %q", got)
	}
}

func TestNewColumnCipher_BadKey(t *testing.T) {
	if _, err := NewColumnCipher([]byte("short")); err == nil {
		t.Fatal("want error for short key")
	}
}
