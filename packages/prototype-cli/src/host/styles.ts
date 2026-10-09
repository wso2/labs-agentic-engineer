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
.ph-header{display:flex;align-items:center;min-height:44px;padding:0 16px;background:#fff;border-bottom:1px solid #d5dae1}
.ph-dock button,.ph-dock select,.ph-bubble button,.ph-bubble textarea{font:inherit}
.ph-name{margin:0;font-size:14px;font-weight:600;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.ph-dock{position:relative;align-self:center;max-width:100%;box-sizing:border-box;display:flex;align-items:center;gap:4px;padding:6px;background:#fff;border:1px solid #d5dae1;border-radius:14px;box-shadow:0 8px 24px rgba(15,23,42,.18);white-space:nowrap}
.ph-dock-group{display:flex;align-items:center;gap:4px;min-width:0}
.ph-dock-divider{align-self:stretch;width:1px;margin:6px 4px;background:#d5dae1}
.ph-select{display:inline-flex;align-items:center;gap:6px;height:32px;padding:0 4px 0 10px;border-radius:8px;cursor:pointer}
.ph-select:hover,.ph-select:focus-within{background:#f1f3f6}
.ph-select-label{color:#8a919c}
.ph-select select{max-width:160px;border:0;background:none;color:#1f2328;font-weight:500;padding:4px 2px;cursor:pointer;text-overflow:ellipsis}
.ph-icon-button{display:inline-grid;place-items:center;width:32px;height:32px;border:0;border-radius:8px;background:none;color:#59636e;cursor:pointer}
.ph-icon-button:hover{background:#f1f3f6;color:#1f2328}
.ph-icon-button:disabled{opacity:.45;cursor:default;background:none}
.ph-tools{display:inline-flex;gap:2px;padding:3px;border:1px solid #d5dae1;border-radius:10px;background:#fff}
.ph-tools button{display:inline-flex;align-items:center;gap:6px;border:0;border-radius:7px;padding:4px 10px;background:none;color:#59636e;font-weight:500;cursor:pointer}
.ph-tools button:hover{background:#f1f3f6;color:#1f2328}
.ph-tools button[aria-pressed=true]{background:#eef0f3;color:#1f2328}
.ph-tools .ph-tool-comment[aria-pressed=true]{background:rgba(255,115,0,.16);color:#c2410c}
.ph-icon{width:16px;height:16px;flex-shrink:0;fill:none;stroke:currentColor;stroke-width:2;stroke-linecap:round;stroke-linejoin:round}
.ph-commenting{--proto-window-border:#ff7300;--proto-window-shadow:0 0 0 3px rgba(255,115,0,.16)}
.ph-mode-tag{margin-left:auto;display:inline-flex;align-items:center;gap:6px;font-size:12px;font-weight:600;color:#c2410c;white-space:nowrap}
.ph-mode-tag kbd,.ph-list-empty kbd{font:600 11px/16px inherit;min-width:18px;padding:0 5px;border:1px solid currentColor;border-radius:4px;color:#59636e;text-align:center}
.ph-body{flex:1;min-height:0;display:flex;flex-direction:column;gap:12px;padding:16px;position:relative}
.ph-stage{flex:1;min-height:0;display:flex}
.ph-primary{background:#2563eb;color:#fff;border:1px solid #2563eb;border-radius:6px;padding:4px 12px;cursor:pointer}
.ph-primary:disabled{background:#93b4f5;border-color:#93b4f5;cursor:default}
.ph-dock .ph-primary{height:32px;border-radius:8px;padding:0 13px;font-weight:600}
.ph-danger{color:#b42318}
.ph-bubble{position:fixed;z-index:20;width:320px;box-sizing:border-box;display:flex;flex-direction:column;gap:8px;background:#fff;border:1px solid #d5dae1;border-radius:10px;padding:12px;box-shadow:0 8px 24px rgba(15,23,42,.18)}
.ph-bubble p{margin:0}
.ph-bubble-on{color:#59636e;font-size:12px;overflow-wrap:anywhere}
.ph-bubble-text{white-space:pre-wrap;overflow-wrap:anywhere}
.ph-bubble-actions{display:flex;justify-content:flex-end;gap:8px}
.ph-field{display:flex;flex-direction:column;gap:4px}
.ph-field label{font-size:12px;color:#59636e}
.ph-field textarea{color:#1f2328;resize:vertical;border:1px solid #d5dae1;border-radius:6px;padding:6px 8px}
.ph-field small{align-self:flex-end;color:#59636e}
.ph-dock-above{position:absolute;z-index:10;left:50%;bottom:calc(100% + 8px);transform:translateX(-50%);width:min(560px,calc(100vw - 32px));display:flex;flex-direction:column;gap:8px;white-space:normal}
.ph-dock-above:empty{display:none}
.ph-dock-above>p{margin:0;padding:10px 14px;background:#fff;border:1px solid #d5dae1;border-radius:12px;box-shadow:0 8px 24px rgba(15,23,42,.18)}
.ph-dock-above>p[role=note]{background:#fff8e6;border-color:#f0c36d;color:#7a4b00}
.ph-count{display:inline-flex;align-items:center;height:32px;font-weight:600;background:none;border:0;padding:0 10px;border-radius:8px;cursor:pointer;color:#1f2328}
.ph-caret{margin-left:6px;color:#59636e}
.ph-hint{display:inline-flex;align-items:center;gap:6px;padding:0 8px;color:#c2410c;font-weight:500}
.ph-hint i{width:6px;height:6px;border-radius:50%;background:#ff7300;animation:ph-pulse 1.6s ease-in-out infinite}
@keyframes ph-pulse{50%{opacity:.35}}
@media (prefers-reduced-motion:reduce){.ph-hint i{animation:none}}
.ph-list-panel{display:flex;flex-direction:column;background:#fff;border:1px solid #d5dae1;border-radius:12px;box-shadow:0 8px 24px rgba(15,23,42,.18);overflow:hidden}
.ph-list{margin:0;padding:6px;list-style:none;max-height:40vh;overflow-y:auto;display:flex;flex-direction:column;gap:2px}
.ph-list-action{display:flex;align-items:center;gap:8px;margin:0;padding:10px 12px;border:0;border-top:1px solid #e6e9ee;background:none;font:inherit;color:#1f2328;text-align:left;cursor:pointer}
.ph-list-action:hover{background:#f1f3f6}
.ph-list-action:disabled{opacity:.45;cursor:default;background:none}
.ph-list li{display:flex;align-items:flex-start;gap:4px}
.ph-list-empty{padding:8px;color:#59636e}
.ph-list-entry{flex:1;min-width:0;display:flex;gap:8px;align-items:flex-start;text-align:left;background:none;border:0;border-radius:6px;padding:6px;cursor:pointer;font:inherit;color:inherit}
.ph-list-entry:hover,.ph-count:hover{background:#f1f3f6}
.ph-list-entry small{display:block;color:#59636e;overflow-wrap:anywhere}
.ph-list-text{display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow:hidden;overflow-wrap:anywhere}
.ph-badge{flex-shrink:0;width:22px;height:22px;border-radius:50%;background:#2563eb;color:#fff;font-size:12px;font-weight:700;display:grid;place-items:center}
.ph-list-remove{background:none;border:0;color:#59636e;font-size:16px;line-height:1;padding:6px;border-radius:6px;cursor:pointer}
.ph-list-remove:hover{background:#f1f3f6;color:#b42318}
@media (max-width:1100px){.ph-hint{display:none}}
@media (max-width:1000px){.ph-select-label{display:none}}
.ph-waiting{margin:auto;color:#59636e}
.ph-findings{position:absolute;z-index:30;left:16px;right:16px;bottom:16px;max-height:40%;overflow:auto;background:#fff8f0;border:1px solid #f0b37e;border-radius:10px;padding:12px 16px;box-shadow:0 8px 24px rgba(15,23,42,.18)}
.ph-findings h2{margin:0 0 8px;font-size:14px;color:#9a3412}
.ph-findings ul{margin:0;padding-left:18px}
.ph-where{color:#59636e}
`;
