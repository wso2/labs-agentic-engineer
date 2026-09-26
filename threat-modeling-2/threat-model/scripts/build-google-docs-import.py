#!/usr/bin/env python3
# Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
#
# WSO2 LLC. licenses this file to you under the Apache License,
# Version 2.0 (the "License"); you may not use this file except
# in compliance with the License.
# You may obtain a copy of the License at
#
# http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing,
# software distributed under the License is distributed on an
# "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
# KIND, either express or implied.  See the License for the
# specific language governing permissions and limitations
# under the License.

"""Assemble the threat model chapters into one file for Google Docs import.

Order: the top-level `NN-*.md` chapters in name order, with the `05`
chapter replaced by `05-interactions/s*/*.md` in name order. Each file is
already written at its final heading level, so headings are kept as written.

The build:
- strips the leading license comment of each file;
- rewrites image paths so they resolve from this folder;
- makes sure the WSO2 header image opens the cover;
- adds a table of contents (H1 and H2) after the cover title block;
- rewrites links between chapters to in-page heading anchors. A link to an
  `<a id>` anchor points to the heading that anchor sits under, and the
  `<a id>` tags are removed (Google Docs does not import raw HTML).

Images are not embedded: after import, upload each PNG listed on stdout.
The output depends only on the chapter files, so a re-run gives the same bytes.
"""

from __future__ import annotations

import posixpath
import re
import sys
from dataclasses import dataclass, field
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / "[Threat Model] - Agentic Engineer.md"
HEADER_IMAGE = "diagrams/wso2-header.png"
INTERACTIONS_DIR = "05-interactions"
TOC_TITLE = "Table of Contents"
TOC_DEPTH = 2

LICENSE_RE = re.compile(r"\A\s*<!--.*?-->\s*", re.DOTALL)
HEADING_RE = re.compile(r"^(#{1,6}) +(.+?) *#*$")
ANCHOR_TAG_RE = re.compile(r'<a id="([^"]+)"></a>')
IMAGE_RE = re.compile(r"!\[([^\]]*)\]\(([^)\s]+)\)")
LINK_RE = re.compile(r"(?<!!)\[([^\]]*)\]\(([^)\s]+)\)")
EXTERNAL_RE = re.compile(r"^(?:[a-z][a-z0-9+.-]*:)", re.IGNORECASE)


class BuildError(Exception):
    pass


@dataclass
class Heading:
    level: int
    text: str
    local_slug: str  # slug inside its own file, as GitHub renders it there
    slug: str = ""  # slug in the single file


@dataclass
class Chapter:
    rel: str  # POSIX path relative to ROOT
    text: str
    headings: list[Heading] = field(default_factory=list)
    anchor_owner: dict[str, str] = field(default_factory=dict)  # <a id> -> heading local slug


def chapter_files() -> list[str]:
    files: list[str] = []
    for path in sorted(ROOT.glob("[0-9][0-9]-*.md")):
        if path.name.startswith(INTERACTIONS_DIR[:3]):
            raise BuildError(f"{path.name}: interaction chapters belong in {INTERACTIONS_DIR}/")
        files.append(path.name)
    sessions = sorted((ROOT / INTERACTIONS_DIR).glob("s*/*.md"))
    if not sessions:
        raise BuildError(f"no chapters under {INTERACTIONS_DIR}/s*/")
    rels = [p.relative_to(ROOT).as_posix() for p in sessions]
    at = next((i for i, f in enumerate(files) if f > INTERACTIONS_DIR), len(files))
    return files[:at] + rels + files[at:]


def slugify(text: str) -> str:
    """GitHub's heading anchor rule."""
    text = re.sub(r"[`*_]|\[|\]\([^)]*\)", "", text).strip().lower()
    text = re.sub(r"[^\w\- ]", "", text)
    return text.replace(" ", "-")


def iter_lines_outside_fences(text: str):
    in_fence = False
    for line in text.split("\n"):
        if line.lstrip().startswith("```"):
            in_fence = not in_fence
            yield line, True
            continue
        yield line, in_fence


def unique(slug: str, seen: dict[str, int]) -> str:
    if slug not in seen:
        seen[slug] = 0
        return slug
    seen[slug] += 1
    return f"{slug}-{seen[slug]}"


def scan(chapter: Chapter) -> None:
    local_seen: dict[str, int] = {}
    current = ""
    for line, fenced in iter_lines_outside_fences(chapter.text):
        if fenced:
            continue
        m = HEADING_RE.match(line)
        if m:
            local = unique(slugify(m.group(2)), local_seen)
            chapter.headings.append(Heading(len(m.group(1)), m.group(2), local))
            current = local
        for anchor in ANCHOR_TAG_RE.findall(line):
            if not current:
                raise BuildError(f"{chapter.rel}: <a id=\"{anchor}\"> comes before any heading")
            chapter.anchor_owner[anchor] = current


def fix_images(chapter: Chapter) -> None:
    base = posixpath.dirname(chapter.rel)

    def repl(m: re.Match[str]) -> str:
        target = m.group(2)
        if EXTERNAL_RE.match(target):
            return m.group(0)
        resolved = posixpath.normpath(posixpath.join(base, target))
        if not (ROOT / resolved).is_file():
            raise BuildError(f"{chapter.rel}: image not found: {target}")
        return f"![{m.group(1)}]({resolved})"

    chapter.text = IMAGE_RE.sub(repl, chapter.text)


def ensure_header_image(cover: Chapter) -> None:
    first = cover.text.lstrip().split("\n", 1)[0]
    if not (IMAGE_RE.fullmatch(first) and IMAGE_RE.fullmatch(first).group(2) == HEADER_IMAGE):
        cover.text = f"![WSO2]({HEADER_IMAGE})\n\n{cover.text}"


def rewrite_links(chapter: Chapter, by_rel: dict[str, Chapter]) -> None:
    base = posixpath.dirname(chapter.rel)

    def target_slug(dest: Chapter, fragment: str, raw: str) -> str:
        if not fragment:
            return dest.headings[0].slug
        for h in dest.headings:
            if h.local_slug == fragment:
                return h.slug
        owner = dest.anchor_owner.get(fragment)
        if owner is not None:
            return next(h.slug for h in dest.headings if h.local_slug == owner)
        raise BuildError(f"{chapter.rel}: link target not found: {raw}")

    def repl(m: re.Match[str]) -> str:
        label, raw = m.group(1), m.group(2)
        if EXTERNAL_RE.match(raw):
            return m.group(0)
        path, _, fragment = raw.partition("#")
        if path:
            if not path.endswith(".md"):
                raise BuildError(f"{chapter.rel}: link to a non-chapter file: {raw}")
            rel = posixpath.normpath(posixpath.join(base, path))
            dest = by_rel.get(rel)
            if dest is None:
                raise BuildError(f"{chapter.rel}: link to a file outside the build: {raw}")
        else:
            dest = chapter
        return f"[{label}](#{target_slug(dest, fragment, raw)})"

    chapter.text = ANCHOR_TAG_RE.sub("", LINK_RE.sub(repl, chapter.text))


def split_cover(cover: Chapter) -> tuple[str, str]:
    """Cover title block, then the rest (from the cover's second H1)."""
    lines = cover.text.split("\n")
    h1 = [i for i, line in enumerate(lines) if HEADING_RE.match(line) and line.startswith("# ")]
    if len(h1) < 2:
        return cover.text, ""
    return "\n".join(lines[: h1[1]]), "\n".join(lines[h1[1]:])


def build() -> tuple[str, list[tuple[str, str]]]:
    chapters = []
    for rel in chapter_files():
        text = LICENSE_RE.sub("", (ROOT / rel).read_text(encoding="utf-8"), count=1)
        chapters.append(Chapter(rel, text.strip() + "\n"))
    by_rel = {c.rel: c for c in chapters}

    cover = chapters[0]
    ensure_header_image(cover)
    for c in chapters:
        fix_images(c)
        scan(c)
        if not c.headings:
            raise BuildError(f"{c.rel}: no heading")

    # Slugs in single-file order: cover title, the TOC, then everything else.
    seen: dict[str, int] = {}
    cover.headings[0].slug = unique(cover.headings[0].local_slug, seen)
    unique(slugify(TOC_TITLE), seen)
    for c in chapters:
        for h in c.headings:
            if not h.slug:
                h.slug = unique(slugify(h.text), seen)

    for c in chapters:
        rewrite_links(c, by_rel)

    toc_entries = [h for c in chapters for h in c.headings if h.level <= TOC_DEPTH][1:]
    toc = "\n".join(
        [f"# {TOC_TITLE}", ""]
        + [f"{'  ' * (h.level - 1)}- [{h.text}](#{h.slug})" for h in toc_entries]
    )

    title_block, revision = split_cover(cover)
    parts = [title_block, toc, revision] + [c.text for c in chapters[1:]]
    body = "\n\n".join(p.strip() for p in parts if p.strip()) + "\n"

    uploads: list[tuple[str, str]] = []
    heading = ""
    for line, fenced in iter_lines_outside_fences(body):
        if fenced:
            continue
        m = HEADING_RE.match(line)
        if m:
            heading = m.group(2)
        for img in IMAGE_RE.finditer(line):
            uploads.append((img.group(2), heading or "(cover)"))
    return body, uploads


def main() -> int:
    try:
        body, uploads = build()
    except BuildError as err:
        print(f"build failed: {err}", file=sys.stderr)
        return 1
    OUT.write_bytes(body.encode("utf-8"))
    print(f"wrote {OUT.name} ({len(body.encode('utf-8'))} bytes)")
    print("images to upload after import (file, under heading):")
    for path, heading in uploads:
        print(f"  {path}  ->  {heading}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
