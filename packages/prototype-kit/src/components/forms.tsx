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

/**
 * Forms: form, filter bar, field and validation summary. Fields take typing
 * locally; a `<Form>` validates `required` and `pattern` when submitted (a
 * `<Button submit>`, or Enter in a single-line field), shows the problems in
 * a ValidationSummary and beside each field, and passes the values to
 * `onSubmit`. Nothing is sent anywhere: what a form saves is the screen's own
 * mock data.
 */

import { createContext, useCallback, useContext, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { useKit } from "../runtime/context.js";
import { SelectableBox, requireId } from "../runtime/selectable.js";
import { useThemed } from "../theme/context.js";
import { FormSubmitContext } from "./form-submit.js";

export type FieldType = "text" | "number" | "select" | "date" | "textarea" | "switch";

interface FieldRules {
  label: string;
  type: FieldType;
  required: boolean;
  pattern: RegExp | undefined;
  patternMessage: string | undefined;
}

interface FieldEntry {
  rules: FieldRules;
  value: string;
}

interface FieldIssue {
  name: string;
  message: string;
}

interface FormApi {
  /** The value the form holds for a field that does not control its own (`value`). */
  valueOf(name: string): string | undefined;
  /** A field's value changed by typing. */
  change(name: string, value: string): void;
  register(name: string, entry: { current: FieldEntry }): () => void;
  /** The field's problem from the last submit, kept current while the reviewer fixes it. */
  issueOf(name: string): string | undefined;
}

const FormContext = createContext<FormApi | null>(null);

/** Fields a filter bar lays out compactly, in a row. */
const CompactFields = createContext(false);

function fieldIssue(rules: FieldRules, value: string): string | undefined {
  // A switch always holds "on" or "off": required means it must be on.
  if (rules.required && (rules.type === "switch" ? value !== "on" : value.trim() === "")) return `${rules.label} is required`;
  if (rules.pattern && value !== "" && !rules.pattern.test(value)) return rules.patternMessage ?? `${rules.label} is not in the expected format`;
  return undefined;
}

function compilePattern(id: string, pattern: string | undefined): RegExp | undefined {
  if (pattern === undefined) return undefined;
  try {
    return new RegExp(`^(?:${pattern})$`, "u");
  } catch {
    throw new Error(`<Field id=${JSON.stringify(id)}> pattern ${JSON.stringify(pattern)} is not a valid regular expression`);
  }
}

export interface FieldProps {
  id: string;
  label: string;
  /** The key of its value in the enclosing Form's `onSubmit` values (default: its id). */
  name?: string | undefined;
  type?: FieldType | undefined;
  /** Controls the field: it shows this; with `onChange` it is editable, without it read-only. */
  value?: string | undefined;
  /** The starting value of a field that holds its own (no `value`). A switch is `on` or `off`. */
  defaultValue?: string | undefined;
  /** A select's choices. */
  options?: string[] | undefined;
  /** A problem to show regardless of validation (e.g. in a validation-error display state). */
  error?: string | undefined;
  required?: boolean | undefined;
  /** A regular expression the whole value must match when it is not empty. */
  pattern?: string | undefined;
  /** What a value that does not match `pattern` shows. */
  patternMessage?: string | undefined;
  placeholder?: string | undefined;
  onChange?: ((value: string) => void) | undefined;
}

export interface ThemeFieldProps {
  /** The input's DOM id, for its label. */
  inputId: string;
  label: string;
  type: FieldType;
  value: string;
  options: string[];
  error?: string | undefined;
  required: boolean;
  placeholder?: string | undefined;
  readOnly: boolean;
  /** Laid out compactly in a filter bar. */
  compact: boolean;
  onChange: (value: string) => void;
}

/** One input. It holds what is typed into it; inside a Form, the Form validates and submits it. */
export function Field({ id, label, name, type = "text", value, defaultValue, options = [], error, required = false, pattern, patternMessage, placeholder, onChange }: FieldProps) {
  const { view } = useKit();
  const Themed = useThemed("Field");
  const form = useContext(FormContext);
  const compact = useContext(CompactFields);
  requireId("Field", id);
  const key = name ?? id;
  const [local, setLocal] = useState(defaultValue ?? "");
  const compiled = useMemo(() => compilePattern(id, pattern), [id, pattern]);
  const shown = value ?? (form ? (form.valueOf(key) ?? defaultValue ?? "") : local);
  const readOnly = view.mode === "annotate" || (value !== undefined && onChange === undefined);

  const entry = useRef<FieldEntry>({ rules: { label, type, required, pattern: compiled, patternMessage }, value: shown });
  useLayoutEffect(() => {
    entry.current = { rules: { label, type, required, pattern: compiled, patternMessage }, value: shown };
  });
  const register = form?.register;
  useLayoutEffect(() => register?.(key, entry), [register, key]);

  const change = (next: string) => {
    if (readOnly) return;
    form?.change(key, next);
    if (value === undefined && !form) setLocal(next);
    onChange?.(next);
  };
  const options_ = type === "select" && shown !== "" && !options.includes(shown) ? [shown, ...options] : options;
  return (
    <SelectableBox id={id} label={label} inline={compact}>
      <Themed
        inputId={`proto-field-${id}`}
        label={label}
        type={type}
        value={shown}
        options={options_}
        error={error ?? form?.issueOf(key)}
        required={required}
        placeholder={placeholder}
        readOnly={readOnly}
        compact={compact}
        onChange={change}
      />
    </SelectableBox>
  );
}

export interface FormProps {
  id: string;
  title?: string | undefined;
  /** Its Fields. */
  children?: ReactNode;
  /** Its Buttons, under the fields; a `<Button submit>` submits. */
  actions?: ReactNode;
  /** Called with every field's value, keyed by name, when a submit passes validation. */
  onSubmit?: ((values: Record<string, string>) => void) | undefined;
}

export interface ThemeFormProps {
  title?: string | undefined;
  /** The ValidationSummary after a failed submit, above the fields. */
  summary?: ReactNode;
  children?: ReactNode;
  actions?: ReactNode;
  /**
   * Submit by keyboard: call it on Enter in a single-line field. (The
   * sandboxed frame never fires a native submit event; a `<Button submit>`
   * submits through the kit.)
   */
  onSubmit: () => void;
}

/** A form card: its Fields, then its action Buttons. Validates on submit. */
export function Form({ id, title, children, actions, onSubmit }: FormProps) {
  const { view } = useKit();
  const Themed = useThemed("Form");
  const Summary = useThemed("ValidationSummary");
  requireId("Form", id);
  const [values, setValues] = useState<Record<string, string>>({});
  const [issues, setIssues] = useState<FieldIssue[] | null>(null);
  const fields = useRef(new Map<string, { current: FieldEntry }>());

  const validate = (changed?: { name: string; value: string }): FieldIssue[] =>
    [...fields.current].flatMap(([name, entry]) => {
      const message = fieldIssue(entry.current.rules, changed?.name === name ? changed.value : entry.current.value);
      return message === undefined ? [] : [{ name, message }];
    });

  // Stable, so a field registers once, in the order the fields mount.
  const register = useCallback((name: string, entry: { current: FieldEntry }) => {
    fields.current.set(name, entry);
    return () => {
      if (fields.current.get(name) === entry) fields.current.delete(name);
    };
  }, []);

  const api: FormApi = {
    valueOf: (name) => values[name],
    change: (name, value) => {
      setValues((previous) => ({ ...previous, [name]: value }));
      if (issues !== null) setIssues(validate({ name, value }));
    },
    register,
    issueOf: (name) => issues?.find((i) => i.name === name)?.message,
  };

  const submit = () => {
    if (view.mode === "annotate") return;
    const found = validate();
    setIssues(found);
    if (found.length === 0) onSubmit?.(Object.fromEntries([...fields.current].map(([name, entry]) => [name, entry.current.value])));
  };

  const problems = issues ?? [];
  const summaryId = `${id}.validation`;
  const title_ = problems.length === 1 ? "Fix 1 problem" : `Fix ${problems.length} problems`;
  return (
    <SelectableBox id={id} label={title ?? "Form"} container>
      <FormContext.Provider value={api}>
        <FormSubmitContext.Provider value={submit}>
          <Themed
            title={title}
            summary={
              problems.length > 0 ? (
                <SelectableBox id={summaryId} label={title_}>
                  <Summary title={title_} issues={problems.map((p) => p.message)} />
                </SelectableBox>
              ) : undefined
            }
            actions={actions}
            onSubmit={submit}
          >
            {children}
          </Themed>
        </FormSubmitContext.Provider>
      </FormContext.Provider>
    </SelectableBox>
  );
}

export interface FiltersProps {
  id: string;
  /** Its Fields, laid out compactly in a row. */
  children?: ReactNode;
}

export interface ThemeFiltersProps {
  children?: ReactNode;
}

/** A filter bar above a list. */
export function Filters({ id, children }: FiltersProps) {
  const Themed = useThemed("Filters");
  return (
    <SelectableBox id={requireId("Filters", id)} label="Filters" container>
      <CompactFields.Provider value>
        <Themed>{children}</Themed>
      </CompactFields.Provider>
    </SelectableBox>
  );
}

export interface ValidationSummaryProps {
  id: string;
  issues: string[];
}

export interface ThemeValidationSummaryProps {
  title: string;
  issues: string[];
}

/** The problems a failed submit shows, for a display state that shows them without a submit. */
export function ValidationSummary({ id, issues }: ValidationSummaryProps) {
  const Themed = useThemed("ValidationSummary");
  const title = issues.length === 1 ? "Fix 1 problem" : `Fix ${issues.length} problems`;
  return (
    <SelectableBox id={requireId("ValidationSummary", id)} label={title}>
      <Themed title={title} issues={issues} />
    </SelectableBox>
  );
}
