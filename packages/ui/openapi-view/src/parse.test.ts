/**
 * Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
 *
 * WSO2 LLC. licenses this file to you under the Apache License,
 * Version 2.0 (the "License"); you may not use this file except
 * in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

import { describe, it, expect } from 'vitest';
import { parseOpenApi, type Operation, type ParsedOpenApi } from './parse.js';

/**
 * A protected component's spec, in the shape the build gate fixes: the one
 * `oauth2` scheme, the document default `security: [ { oauth2: [] } ]`, and one
 * operation per protection state.
 */
function spec(paths: Record<string, unknown>, overrides: Record<string, unknown> = {}): string {
  return JSON.stringify({
    openapi: '3.0.3',
    info: { title: 'Expense API', version: '1.0.0' },
    components: {
      securitySchemes: {
        oauth2: {
          type: 'oauth2',
          flows: { authorizationCode: { scopes: { 'claims:read': 'See own claims' } } },
        },
      },
    },
    security: [{ oauth2: [] }],
    paths,
    ...overrides,
  });
}

function op(text: string, path: string): Operation {
  const parsed = parseOpenApi(text) as ParsedOpenApi;
  expect('kind' in parsed).toBe(false);
  const found = parsed.sections.flatMap((s) => s.endpoints).find((e) => e.path === path);
  expect(found, `no operation for ${path}`).toBeDefined();
  return found!;
}

describe('parseOpenApi — protection', () => {
  it('an operation with no security block inherits the document default: signed in', () => {
    const text = spec({ '/me': { get: { summary: 'Who am I' } } });
    expect(op(text, '/me').protection).toEqual({ kind: 'signedIn' });
  });

  it('security: [] is public', () => {
    const text = spec({ '/health': { get: { summary: 'Health', security: [] } } });
    expect(op(text, '/health').protection).toEqual({ kind: 'public' });
  });

  it('one requirement with one scope is that scope', () => {
    const text = spec({
      '/claims': { get: { summary: 'List claims', security: [{ oauth2: ['claims:read'] }] } },
    });
    expect(op(text, '/claims').protection).toEqual({ kind: 'scope', scope: 'claims:read' });
  });

  it('the document default spelled out on the operation is still signed in', () => {
    const text = spec({ '/me': { get: { security: [{ oauth2: [] }] } } });
    expect(op(text, '/me').protection).toEqual({ kind: 'signedIn' });
  });

  it('a document with no security block at all protects nothing', () => {
    // The shape the gate requires of a component with no sign-in dependency:
    // no scheme, no document default, no operation security. Every row public.
    const text = JSON.stringify({
      openapi: '3.0.3',
      info: { title: 'Public API', version: '1.0.0' },
      paths: { '/slots': { get: { summary: 'Free slots' } } },
    });
    expect(op(text, '/slots').protection).toEqual({ kind: 'public' });
  });

  it('the declared scheme name wins over the platform default', () => {
    const text = JSON.stringify({
      openapi: '3.0.3',
      info: { title: 'Hand-written', version: '1.0.0' },
      components: { securitySchemes: { idp: { type: 'oauth2', flows: {} } } },
      security: [{ idp: [] }],
      paths: { '/reports': { get: { security: [{ idp: ['reports:read'] }] } } },
    });
    expect(op(text, '/reports').protection).toEqual({ kind: 'scope', scope: 'reports:read' });
  });
});

describe('parseOpenApi — protection degrades rather than throwing', () => {
  it('several requirement objects: the first one is read', () => {
    const text = spec({
      '/claims': {
        get: { security: [{ oauth2: ['claims:read'] }, { oauth2: ['claims:read-all'] }] },
      },
    });
    expect(op(text, '/claims').protection).toEqual({ kind: 'scope', scope: 'claims:read' });
  });

  it('several scopes in one requirement: the first one is read', () => {
    const text = spec({
      '/claims': { get: { security: [{ oauth2: ['claims:read', 'claims:read-all'] }] } },
    });
    expect(op(text, '/claims').protection).toEqual({ kind: 'scope', scope: 'claims:read' });
  });

  it('a requirement naming only an unknown scheme reads as signed in, not as its handle', () => {
    const text = spec({ '/claims': { get: { security: [{ apiKey: ['claims:read'] }] } } });
    expect(op(text, '/claims').protection).toEqual({ kind: 'signedIn' });
  });

  it('an extra scheme alongside oauth2 does not hide the oauth2 handle', () => {
    const text = spec({
      '/claims': { get: { security: [{ apiKey: [], oauth2: ['claims:read'] }] } },
    });
    expect(op(text, '/claims').protection).toEqual({ kind: 'scope', scope: 'claims:read' });
  });

  it('a malformed security block falls back to the document default', () => {
    const text = spec({
      '/a': { get: { security: 'claims:read' } },
      '/b': { get: { security: [null] } },
      '/c': { get: { security: [{ oauth2: 'claims:read' }] } },
      '/d': { get: { security: [{ oauth2: [42] }] } },
    });
    expect(op(text, '/a').protection).toEqual({ kind: 'signedIn' });
    expect(op(text, '/b').protection).toEqual({ kind: 'signedIn' });
    expect(op(text, '/c').protection).toEqual({ kind: 'signedIn' });
    expect(op(text, '/d').protection).toEqual({ kind: 'signedIn' });
  });

  it('an empty requirement object is OpenAPI\'s "security optional": public', () => {
    const text = spec({ '/slots': { get: { security: [{}] } } });
    expect(op(text, '/slots').protection).toEqual({ kind: 'public' });
  });

  it('a document-level security block that is junk leaves the default public', () => {
    const text = spec({ '/me': { get: {} } }, { security: 'oauth2' });
    expect(op(text, '/me').protection).toEqual({ kind: 'public' });
  });
});
