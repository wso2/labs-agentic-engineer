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

/** The host page's own styles (the prototype's are the theme's, inside the frame). */

export const HOST_CSS = `
html,body,#root{height:100%;margin:0}
body{font:13px/1.4 system-ui,-apple-system,"Segoe UI",Roboto,sans-serif;background:#e9ecf1;color:#1f2328}
.ph-app{height:100%;display:flex;flex-direction:column;position:relative}
.ph-toolbar{display:flex;flex-wrap:wrap;gap:12px;align-items:center;padding:8px 16px;background:#fff;border-bottom:1px solid #d5dae1}
.ph-toolbar label{display:flex;gap:6px;align-items:center}
.ph-toolbar select,.ph-toolbar button,.ph-feedback button,.ph-feedback textarea{font:inherit}
.ph-name{margin-right:8px}
.ph-modes{display:inline-flex;margin-left:auto}
.ph-modes button[aria-pressed=true]{background:#2563eb;color:#fff;border-color:#2563eb}
.ph-body{flex:1;min-height:0;display:flex;gap:16px;padding:16px}
.ph-window{flex:1;min-width:0;display:flex;flex-direction:column;border:1px solid #c9d0d9;border-radius:10px;overflow:hidden;background:#fff;box-shadow:0 8px 24px rgba(15,23,42,.12)}
.ph-window-bar{display:flex;gap:12px;align-items:center;padding:6px 12px;background:#f1f3f6;border-bottom:1px solid #d5dae1}
.ph-window-dots{display:flex;gap:6px}
.ph-window-dots i{width:10px;height:10px;border-radius:50%;background:#d0d5dc}
.ph-window-title{font-weight:600}
.ph-window-address{flex:1;min-width:0;padding:2px 10px;border-radius:6px;background:#fff;color:#59636e;font-family:ui-monospace,monospace;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.ph-window-body{flex:1;min-height:0;display:flex;flex-direction:column}
.ph-feedback{width:300px;display:flex;flex-direction:column;gap:10px;background:#fff;border:1px solid #d5dae1;border-radius:10px;padding:12px;overflow:auto}
.ph-feedback h2{margin:0;font-size:15px}
.ph-feedback label{display:flex;flex-direction:column;gap:4px}
.ph-queue{margin:0;padding-left:18px;display:flex;flex-direction:column;gap:6px}
.ph-queue li span{display:block}
.ph-queue small{color:#59636e;display:block}
.ph-waiting{margin:auto;color:#59636e}
.ph-findings{position:absolute;left:16px;right:16px;bottom:16px;max-height:40%;overflow:auto;background:#fff8f0;border:1px solid #f0b37e;border-radius:10px;padding:12px 16px;box-shadow:0 8px 24px rgba(15,23,42,.18)}
.ph-findings h2{margin:0 0 8px;font-size:14px;color:#9a3412}
.ph-findings ul{margin:0;padding-left:18px}
.ph-where{color:#59636e}
`;
