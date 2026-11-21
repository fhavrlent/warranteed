import { createContext, use, useEffect, useLayoutEffect, useRef, useState, type AriaAttributes } from "react";
import { cn } from "./utils";

export interface FieldInfo {
  id: string;
  labelId: string;
  /** The hint or error text, when there is one. */
  descId?: string;
  invalid: boolean;
  claim: () => () => void;
}
export const FieldContext = createContext<FieldInfo | null>(null);

interface ControlProps {
  id?: string;
  "aria-label"?: string;
  "aria-labelledby"?: string;
  "aria-describedby"?: string;
  "aria-invalid"?: AriaAttributes["aria-invalid"];
}

/**
 * Ties a control to the Field around it: its label, hint or error, and whether it's invalid.
 * A control that names itself (its own aria-label, or an id that isn't the field's) is left alone.
 */
export const useFieldControl = (props: ControlProps): ControlProps => {
  const field = use(FieldContext);
  const mine =
    field !== null &&
    (props.id === field.id || (props.id === undefined && !props["aria-label"] && !props["aria-labelledby"]));
  const claim = field?.claim;
  useLayoutEffect(() => (mine && claim ? claim() : undefined), [mine, claim]);
  if (!mine) return {};
  return {
    id: field.id,
    "aria-describedby": cn(props["aria-describedby"], field.descId) || undefined,
    "aria-invalid": props["aria-invalid"] ?? (field.invalid || undefined),
  };
};

/** For a group of controls (radio buttons and such): labelled and described by the Field. */
export const useFieldGroup = () => {
  const field = use(FieldContext);
  return field ? { "aria-labelledby": field.labelId, "aria-describedby": field.descId } : {};
};

/**
 * A form's errors. `report` shows them and moves focus to the first invalid field, so keyboard
 * and screen reader users land on what needs fixing; it returns whether the form is valid.
 */
export const useFormErrors = () => {
  const [errors, setErrors] = useState<Record<string, string>>({});
  const focusInRef = useRef<HTMLFormElement | null>(null);
  useEffect(() => {
    const form = focusInRef.current;
    focusInRef.current = null;
    form?.querySelector<HTMLElement>('[aria-invalid="true"]')?.focus();
  }, [errors]);
  const report = (form: HTMLFormElement, e: Record<string, string>) => {
    const valid = Object.keys(e).length === 0;
    focusInRef.current = valid ? null : form;
    setErrors(e);
    return valid;
  };
  return { errors, report };
};
