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

package specbundle

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/wso2/aep/aep-api/internal/platform/designspec"
)

// Format is the one bundle format this platform reads; the manifest states it.
const Format = "aespecs/1"

// Extension is the bundle's file extension, what the console's picker admits.
const Extension = ".aespecs"

// ManifestPath is the manifest's fixed path at the archive root.
const ManifestPath = "manifest.json"

// The bounds. MaxBundleBytes matches the console's per-file screen and the
// pod's reference limit; MaxFileBytes is the Room's own per-path cap, so a
// seeded file could be saved again from the Room; MaxUnpackedBytes keeps a
// seed commit well under the pod's 16 MiB base64 body.
const (
	MaxBundleBytes   = 5 << 20
	MaxEntries       = 256
	MaxUnpackedBytes = 8 << 20
	MaxFileBytes     = 5 << 20
)

// ErrRejected is every refusal, wrapped with the reason.
var ErrRejected = errors.New("specbundle: bundle rejected")

// Reason is the one sentence behind an ErrRejected, for the caller's message;
// any other error's text unchanged.
func Reason(err error) string {
	return strings.TrimPrefix(err.Error(), ErrRejected.Error()+": ")
}

func rejected(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrRejected, fmt.Sprintf(format, a...))
}

// Manifest is `manifest.json`: what wrote the bundle and from where. Every
// field is validated; the provenance is the uploader's claim, recorded as such.
type Manifest struct {
	Format    string   `json:"format"`
	Producer  Producer `json:"producer"`
	Product   string   `json:"product"`
	Source    Source   `json:"source"`
	CreatedAt string   `json:"createdAt"`
}

// Producer is the tool that wrote the bundle.
type Producer struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Source is the product repository the specs were derived from.
type Source struct {
	// Repository is the remote URL, or LocalCheckout when the skill read a
	// checkout with no remote.
	Repository string `json:"repository"`
	// Commit is the commit the specs were read at, 7 to 40 hex.
	Commit string `json:"commit"`
}

// LocalCheckout is the Repository value of a product read from a checkout
// with no remote.
const LocalCheckout = "local checkout"

// Bundle is an opened archive: its manifest and every spec file, keyed by
// repo-relative path (`specs/...`).
type Bundle struct {
	Manifest Manifest
	Files    map[string][]byte
}

var (
	producerName    = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	producerVersion = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.+-]{0,63}$`)
	sourceRepo      = regexp.MustCompile(`^(https://|git@)[^\s]{1,300}$`)
	sourceCommit    = regexp.MustCompile(`^[0-9a-f]{7,40}$`)
	kebab           = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
)

// dependencyFile reports whether a definition directory may hold file: the
// definition, the design's assumptions, or a contract under one of the names
// the project gate accepts (designspec.IsContractFile), so the two never drift.
func dependencyFile(file string) bool {
	return file == "dependency.json" || file == "assumptions.md" || designspec.IsContractFile(file)
}

const (
	requirementsPrefix = "specs/requirements/"
	referencesPrefix   = "specs/requirements/references/"
	domainModelPath    = "specs/design/domain-model.md"
	dependenciesPrefix = "specs/design/dependencies/"
)

// Admits reports whether the format allows path, a cleaned, slash-separated
// repository path. The seed asks it on a re-seed: an admitted path at the tip
// that the new bundle no longer holds was the earlier seed's, and goes.
func Admits(path string) bool {
	ok, _ := admitted(path)
	return ok && path != ManifestPath
}

// admitted reports whether a cleaned, slash-separated path is one the format
// allows, with the rule's name for a refusal.
func admitted(p string) (bool, string) {
	switch {
	case p == ManifestPath:
		return true, ""
	case strings.HasPrefix(p, referencesPrefix):
		return false, "reference documents are turn inputs, never spec files"
	case strings.HasPrefix(p, requirementsPrefix):
		if !strings.HasSuffix(p, ".md") {
			return false, "a requirements file is markdown"
		}
		return true, ""
	case p == domainModelPath:
		return true, ""
	case strings.HasPrefix(p, dependenciesPrefix):
		rest := strings.TrimPrefix(p, dependenciesPrefix)
		dir, file, ok := strings.Cut(rest, "/")
		if !ok || strings.Contains(file, "/") {
			return false, "a dependency file sits directly in its definition's directory"
		}
		if !kebab.MatchString(dir) {
			return false, "a dependency directory is kebab-case"
		}
		if !dependencyFile(file) {
			return false, "not a dependency definition, assumptions or contract file"
		}
		return true, ""
	default:
		return false, "outside the spec tree the format allows"
	}
}

// Open reads an archive held whole and answers its manifest and files, or
// ErrRejected naming the first entry and rule that failed.
func Open(data []byte) (*Bundle, error) {
	if len(data) > MaxBundleBytes {
		return nil, rejected("the bundle is over %d MiB", MaxBundleBytes>>20)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, rejected("not a zip archive")
	}
	if len(zr.File) > MaxEntries {
		return nil, rejected("more than %d entries", MaxEntries)
	}
	files := map[string][]byte{}
	left := MaxUnpackedBytes
	for _, f := range zr.File {
		name := f.Name
		if strings.HasSuffix(name, "/") {
			continue // a directory entry carries nothing; its files are their own entries
		}
		if f.Mode()&os.ModeSymlink != 0 {
			return nil, rejected("%q is a symbolic link", name)
		}
		if !f.Mode().IsRegular() {
			return nil, rejected("%q is not a regular file", name)
		}
		clean := path.Clean(name)
		if clean != name || path.IsAbs(name) || strings.HasPrefix(name, "..") || strings.Contains(name, "/../") {
			return nil, rejected("%q is not a plain relative path", name)
		}
		for _, seg := range strings.Split(clean, "/") {
			if strings.HasPrefix(seg, ".") {
				return nil, rejected("%q has a dot-led segment", name)
			}
		}
		if ok, rule := admitted(clean); !ok {
			return nil, rejected("%q: %s", name, rule)
		}
		if _, dup := files[clean]; dup {
			return nil, rejected("%q appears twice", name)
		}
		content, err := readEntry(f, min(MaxFileBytes, left))
		if err != nil {
			return nil, err
		}
		left -= len(content)
		if !utf8.Valid(content) || bytes.IndexByte(content, 0) >= 0 {
			return nil, rejected("%q is not UTF-8 text", name)
		}
		if rule := credentialIn(content); rule != "" {
			return nil, rejected("%q carries what looks like a credential: %s", name, rule)
		}
		files[clean] = content
	}
	raw, ok := files[ManifestPath]
	if !ok {
		return nil, rejected("no %s at the archive root", ManifestPath)
	}
	delete(files, ManifestPath)
	m, err := parseManifest(raw)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, rejected("no spec files beside the manifest")
	}
	return &Bundle{Manifest: m, Files: files}, nil
}

// readEntry reads one entry up to budget bytes; one byte past it is the
// refusal, so a declared size is never trusted.
func readEntry(f *zip.File, budget int) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, rejected("%q cannot be read", f.Name)
	}
	defer rc.Close()
	b, err := io.ReadAll(io.LimitReader(rc, int64(budget)+1))
	if err != nil {
		return nil, rejected("%q cannot be read", f.Name)
	}
	if len(b) > budget {
		if budget < MaxFileBytes {
			return nil, rejected("the files unpack to more than %d MiB", MaxUnpackedBytes>>20)
		}
		return nil, rejected("%q is over %d MiB", f.Name, MaxFileBytes>>20)
	}
	return b, nil
}

// parseManifest decodes and validates the manifest; unknown fields are
// refused so a bundle written for a later format is never read as this one.
func parseManifest(raw []byte) (Manifest, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var m Manifest
	if err := dec.Decode(&m); err != nil {
		return Manifest{}, rejected("%s: %v", ManifestPath, err)
	}
	switch {
	case m.Format != Format:
		return Manifest{}, rejected("%s: format %q is not %q", ManifestPath, m.Format, Format)
	case !producerName.MatchString(m.Producer.Name):
		return Manifest{}, rejected("%s: producer.name %q is not a lower-case kebab name", ManifestPath, m.Producer.Name)
	case !producerVersion.MatchString(m.Producer.Version):
		return Manifest{}, rejected("%s: producer.version %q is not a version", ManifestPath, m.Producer.Version)
	case strings.TrimSpace(m.Product) == "" || len(m.Product) > 200 || strings.ContainsAny(m.Product, "\r\n"):
		return Manifest{}, rejected("%s: product is one line of up to 200 characters", ManifestPath)
	case m.Source.Repository != LocalCheckout && !sourceRepo.MatchString(m.Source.Repository):
		return Manifest{}, rejected("%s: source.repository is a remote URL or %q", ManifestPath, LocalCheckout)
	case !sourceCommit.MatchString(m.Source.Commit):
		return Manifest{}, rejected("%s: source.commit is 7 to 40 hex characters", ManifestPath)
	}
	if _, err := time.Parse(time.RFC3339, m.CreatedAt); err != nil {
		return Manifest{}, rejected("%s: createdAt is not RFC 3339", ManifestPath)
	}
	return m, nil
}

// The credential rules. The skill that writes a bundle copies configuration
// key NAMES and never values, so any value-shaped string is a leak to refuse,
// not a false positive to tolerate. Prose rarely trips them: an assignment
// must carry one unbroken value of at least 16 characters (a passphrase
// without a digit is still a credential), and a bare token must be long,
// mixed and dense.
var (
	privateKeyBlock = regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)
	assignedSecret  = regexp.MustCompile(`(?i)\b(password|passwd|secret|api[_-]?key|access[_-]?key|private[_-]?key|token|client[_-]?secret)\b\s*[:=]\s*["']?([A-Za-z0-9_\-+/=.]{16,})`)
	credentialInURL = regexp.MustCompile(`://[^/\s:@]+:[^/\s@]+@`)
	bareToken       = regexp.MustCompile(`[A-Za-z0-9+` + tokenSeparators + `]{32,}`)
)

// tokenSeparators are the characters a token may carry that also join the
// words of a slug and the segments of a path. The hyphen is last so the
// bareToken class reads it literally.
const tokenSeparators = "/_-"

// credentialIn answers the rule a text trips, or "" when it trips none.
func credentialIn(text []byte) string {
	if privateKeyBlock.Match(text) {
		return "a private key block"
	}
	if credentialInURL.Match(text) {
		return "a URL with credentials in it"
	}
	if m := assignedSecret.FindSubmatch(text); m != nil {
		return fmt.Sprintf("a value assigned to %q", string(m[1]))
	}
	for _, tok := range bareToken.FindAll(text, -1) {
		if looksLikeToken(tok) {
			return "a high-entropy token"
		}
	}
	return ""
}

// looksLikeToken: three character classes and over 4 bits of entropy per
// byte, which a word, a hex sha or a plain path (two classes) never reaches.
// A name made of words can, taken whole: a feature link such as
// features/F12-visas-and-travel-documents carries one upper-case letter and
// two digits in its ID. So a run of three or more separated segments is
// judged segment by segment and reads as a name when no segment is dense on
// its own. A key with word prefixes (sk_live_<body>) still trips: its body is.
func looksLikeToken(tok []byte) bool {
	segments := bytes.FieldsFunc(tok, func(r rune) bool { return strings.ContainsRune(tokenSeparators, r) })
	if len(segments) < 3 {
		return denseMixed(tok)
	}
	for _, segment := range segments {
		if denseMixed(segment) {
			return true
		}
	}
	return false
}

// denseMixed: upper case, lower case and digits all present, and over 4 bits
// of entropy per byte.
func denseMixed(s []byte) bool {
	var upper, lower, digit bool
	counts := map[byte]int{}
	for _, c := range s {
		counts[c]++
		switch {
		case c >= 'A' && c <= 'Z':
			upper = true
		case c >= 'a' && c <= 'z':
			lower = true
		case c >= '0' && c <= '9':
			digit = true
		}
	}
	if !(upper && lower && digit) {
		return false
	}
	var entropy float64
	n := float64(len(s))
	for _, k := range counts {
		p := float64(k) / n
		entropy -= p * math.Log2(p)
	}
	return entropy > 4.0
}
