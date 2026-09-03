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

package genaiturns

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path"
	"strings"

	"github.com/wso2/aep/aep-api/internal/clients/agentsvc"
	"github.com/wso2/aep/aep-api/internal/platform/apierr"
)

// Chat attachments (console #428 / ADR-0019) are the files a user attaches to
// ONE message in the agent chat. Unlike reference documents they are never
// stored: nothing here touches the disk. The bytes ride this request into the
// detached turn and are durable only as parts of the conversation's history,
// which is also what makes re-sending one free — the agents service dedupes by
// file name.
//
// That is why the transport is multipart on the EXISTING create-turn endpoint
// rather than a separate upload: with no store, there is nothing for an upload's
// returned ids to point at.

// The multipart field names the console sends.
const (
	attachmentsField = "files"
	instructionField = "instruction"
	collabField      = "collab"
	targetField      = "target"
	anchorField      = "anchor"
	intentField      = "intent"
)

// The caps, and every one of them restates ONE number.
//
// The agents service enforces a 20 MiB base64-ENCODED attachment budget per
// turn (MAX_REFERENCE_ATTACHMENT_ENCODED_BYTES, #384), past which it warns and
// SKIPS the rest. base64 is 4 bytes out per 3 in, so 20 MiB encoded is exactly
// 15 MiB raw. The console screens the same three numbers before sending; this is
// the authority.
//
// MaxChatAttachmentTotalBytes is the load-bearing one. A per-file cap alone
// cannot hold the line: ten 5 MiB files each pass it and together are 50 MiB,
// three times the budget, so the agent would silently drop the tail. Refusing
// the batch here turns a silent truncation into a 400 the user can act on.
const (
	MaxChatAttachmentBytes      = 5 << 20
	MaxChatAttachmentCount      = 10
	MaxChatAttachmentTotalBytes = 15 << 20
)

// chatAttachmentMediaTypes maps an accepted extension to the media type the
// MODEL reads it as — not the file's conventional type.
//
// Every text format arrives as `text/plain` on purpose: the Anthropic provider
// maps exactly two document media types, `application/pdf` and `text/plain`,
// plus the four image types. A `.csv` sent as `text/csv` would not be read as a
// document at all, so the honest mapping is the one the model honours.
//
// Office formats are absent deliberately: the models do not read them natively,
// so accepting one would carry bytes no turn can use.
var chatAttachmentMediaTypes = map[string]string{
	// Read natively as file parts.
	".pdf":  "application/pdf",
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	// Read as text.
	".md": "text/plain", ".txt": "text/plain", ".csv": "text/plain",
	".tsv": "text/plain", ".json": "text/plain", ".yaml": "text/plain",
	".yml": "text/plain", ".xml": "text/plain", ".html": "text/plain",
	".rst": "text/plain",
}

// multipartTurn is the create-turn fields recovered from a multipart body.
type multipartTurn struct {
	Instruction string
	Target      string
	Collab      bool
	// Anchor/Intent are the aim (#666), recovered as raw strings and validated
	// with the JSON path's rules in aim.go — one place decides what a valid aim
	// is, so the two request forms cannot drift into accepting different things.
	Anchor      string
	Intent      string
	Attachments []agentsvc.TurnAttachment
}

// readMultipartTurn drains a multipart create-turn body into memory.
//
// Bounded twice per file, and the second bound is the reason the first is not
// enough: io.LimitReader ends a capped read with io.EOF, which is
// indistinguishable from a small file simply ending — so one byte PAST the cap
// is read deliberately, and a file that fills it is refused rather than
// truncated. Silently handing the model half a PDF is worse than refusing the
// send.
func readMultipartTurn(body *multipart.Reader) (multipartTurn, error) {
	var out multipartTurn
	if body == nil {
		return out, apierr.BadRequest("missing multipart body")
	}
	seen := map[string]bool{}
	var total int
	for {
		part, err := body.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return out, apierr.BadRequest("can't decode multipart body: " + err.Error())
		}
		switch part.FormName() {
		case instructionField:
			v, err := readFormValue(part)
			if err != nil {
				return out, err
			}
			out.Instruction = v
			continue
		case targetField:
			v, err := readFormValue(part)
			if err != nil {
				return out, err
			}
			out.Target = v
			continue
		case anchorField:
			v, err := readFormValue(part)
			if err != nil {
				return out, err
			}
			out.Anchor = v
			continue
		case intentField:
			v, err := readFormValue(part)
			if err != nil {
				return out, err
			}
			out.Intent = v
			continue
		case collabField:
			v, err := readFormValue(part)
			if err != nil {
				return out, err
			}
			// A form field has no boolean type; the console sends "true".
			out.Collab = strings.EqualFold(strings.TrimSpace(v), "true")
			continue
		case attachmentsField:
		default:
			// Unknown parts are IGNORED, and this is a deliberate divergence
			// from the JSON arm, which is strict (`additionalProperties: false`
			// on TurnInputBody). Under strict parsing a rolling deploy in which
			// the console ships a new field before the server knows it would 400
			// every send; ignoring degrades gracefully instead. Relaxing the
			// multipart schema to match is a contract decision, so it is left to
			// be made explicitly rather than inferred from this handler.
			continue
		}

		// Refuse early rather than buffering an eleventh file only to reject
		// the batch afterwards.
		if len(out.Attachments) >= MaxChatAttachmentCount {
			return out, apierr.BadRequest(fmt.Sprintf(
				"at most %d attachments per message", MaxChatAttachmentCount))
		}
		name := path.Base(strings.ReplaceAll(strings.TrimSpace(part.FileName()), `\`, "/"))
		if name == "" || name == "." || name == ".." {
			return out, apierr.BadRequest("an attachment has no usable file name")
		}
		mediaType, ok := chatAttachmentMediaTypes[strings.ToLower(path.Ext(name))]
		if !ok {
			return out, apierr.BadRequest(fmt.Sprintf("%q: unsupported attachment type", name))
		}
		// Two files under one name would collapse into one downstream: the
		// agents service dedupes attachments BY NAME against the conversation's
		// history, so the second would be dropped without a word.
		if seen[name] {
			return out, apierr.BadRequest(fmt.Sprintf("%q is attached twice — rename one", name))
		}
		seen[name] = true

		content, err := io.ReadAll(io.LimitReader(part, MaxChatAttachmentBytes+1))
		if err != nil {
			return out, apierr.BadRequest("read attachment: " + err.Error())
		}
		if len(content) > MaxChatAttachmentBytes {
			return out, apierr.BadRequest(fmt.Sprintf(
				"%q exceeds the %d MiB per-file limit", name, MaxChatAttachmentBytes>>20))
		}
		total += len(content)
		if total > MaxChatAttachmentTotalBytes {
			return out, apierr.BadRequest(fmt.Sprintf(
				"attachments exceed the %d MiB total for one message",
				MaxChatAttachmentTotalBytes>>20))
		}
		out.Attachments = append(out.Attachments, agentsvc.TurnAttachment{
			Name:      name,
			MediaType: mediaType,
			Data:      base64.StdEncoding.EncodeToString(content),
		})
	}
	return out, nil
}

// readFormValue reads one non-file field, capped at the instruction limit so a
// hostile body cannot buffer without bound on a field that should be short.
func readFormValue(part *multipart.Part) (string, error) {
	v, err := io.ReadAll(io.LimitReader(part, createTurnMaxInstructionBytes+1))
	if err != nil {
		return "", apierr.BadRequest("read form field: " + err.Error())
	}
	if len(v) > createTurnMaxInstructionBytes {
		return "", apierr.New(http.StatusRequestEntityTooLarge, "request_too_large",
			"instruction exceeds the size limit", nil)
	}
	return string(v), nil
}
