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

/** Forms: form card, filter bar, field and validation summary. */

import type { ThemeFieldProps, ThemeFiltersProps, ThemeFormProps, ThemeValidationSummaryProps } from "@wso2/prototype-kit";

export const FORMS_CSS = `
.pt-form form{display:flex;flex-direction:column;gap:14px}
.pt-form-actions{display:flex;gap:8px;justify-content:flex-end}
.pt-field{display:flex;flex-direction:column;gap:4px}
.pt-field-compact{min-width:180px}
.pt-field label{font-size:13px;font-weight:500}
.pt-field input,.pt-field select,.pt-field textarea{font:inherit;padding:7px 10px;border:1px solid var(--pt-border);border-radius:6px;background:var(--pt-surface);color:var(--pt-text)}
.pt-field textarea{min-height:72px;resize:vertical}
.pt-field-invalid input,.pt-field-invalid select,.pt-field-invalid textarea{border-color:var(--pt-error)}
.pt-field-error{color:var(--pt-error);font-size:12px}
.pt-switch{display:flex;align-items:center;gap:8px}
.pt-filters{display:flex;gap:12px;flex-wrap:wrap;align-items:flex-end}
.pt-summary{border-left:4px solid var(--pt-error);background:color-mix(in srgb,var(--pt-error) 8%,var(--pt-surface));padding:10px 14px;border-radius:6px}
.pt-summary ul{margin:4px 0 0;padding-left:18px}
`;

export function Field({ inputId, label, type, value, options, error, required, placeholder, readOnly, compact, onChange }: ThemeFieldProps) {
  const errorId = `${inputId}-error`;
  const common = {
    id: inputId,
    required,
    "aria-invalid": error !== undefined,
    ...(error !== undefined ? { "aria-describedby": errorId } : {}),
  };
  let control;
  if (type === "switch") {
    control = (
      <span className="pt-switch">
        <input {...common} type="checkbox" role="switch" checked={value === "on"} disabled={readOnly} onChange={(e) => onChange(e.target.checked ? "on" : "off")} />
        <label htmlFor={inputId}>{label}</label>
      </span>
    );
  } else if (type === "select") {
    control = (
      <select {...common} value={value} disabled={readOnly} onChange={(e) => onChange(e.target.value)}>
        {value === "" && <option value="">{placeholder ?? "Choose…"}</option>}
        {options.map((o) => (
          <option key={o} value={o}>
            {o}
          </option>
        ))}
      </select>
    );
  } else if (type === "textarea") {
    control = <textarea {...common} value={value} readOnly={readOnly} placeholder={placeholder} onChange={(e) => onChange(e.target.value)} />;
  } else {
    control = <input {...common} type={type} value={value} readOnly={readOnly} placeholder={placeholder} onChange={(e) => onChange(e.target.value)} />;
  }
  return (
    <div className={`pt-field${compact ? " pt-field-compact" : ""}${error !== undefined ? " pt-field-invalid" : ""}`}>
      {type !== "switch" && <label htmlFor={inputId}>{label}</label>}
      {control}
      {error !== undefined && (
        <span id={errorId} className="pt-field-error">
          {error}
        </span>
      )}
    </div>
  );
}

export function Form({ title, summary, children, actions, onSubmit }: ThemeFormProps) {
  return (
    <section className="pt-card pt-form">
      {title && <h3 className="pt-card-title">{title}</h3>}
      <form
        noValidate
        onSubmit={(e) => e.preventDefault()}
        onKeyDown={(e) => {
          if (e.key === "Enter" && e.target instanceof HTMLInputElement && e.target.type !== "checkbox") {
            e.preventDefault();
            onSubmit();
          }
        }}
      >
        {summary}
        {children}
        {actions !== undefined && <div className="pt-form-actions">{actions}</div>}
      </form>
    </section>
  );
}

export function Filters({ children }: ThemeFiltersProps) {
  return <div className="pt-filters">{children}</div>;
}

export function ValidationSummary({ title, issues }: ThemeValidationSummaryProps) {
  return (
    <div role="alert" className="pt-summary">
      <strong>{title}</strong>
      <ul>
        {issues.map((issue, i) => (
          <li key={`${issue}-${i}`}>{issue}</li>
        ))}
      </ul>
    </div>
  );
}
