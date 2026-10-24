import * as P from "@radix-ui/react-popover";
import { CalendarDays } from "lucide-react";
import { useEffect, useState } from "react";
import { DayPicker } from "react-day-picker";
import { formatDate, fromISO, parseUserDate, toISO, weekStartsOn } from "@/lib/dates";
import { usePrefs } from "@/lib/queries";
import { cn } from "@/lib/utils";
import { fieldBase } from "./ui/input";

/**
 * A date field that displays and accepts dates in the user's chosen format,
 * instead of <input type="date"> which always follows the OS locale.
 * The value is always an ISO date (YYYY-MM-DD) or null.
 */
export function DateInput({
  value,
  onChange,
  id,
  invalid,
  placeholder,
  autoFocus,
}: {
  value: string | null;
  onChange: (iso: string | null) => void;
  id?: string;
  invalid?: boolean;
  placeholder?: string;
  autoFocus?: boolean;
}) {
  const { dateFormat } = usePrefs();
  const [text, setText] = useState(() => formatDate(value, dateFormat));
  const [bad, setBad] = useState(false);
  const [open, setOpen] = useState(false);

  useEffect(() => {
    setText(formatDate(value, dateFormat));
    setBad(false);
  }, [value, dateFormat]);

  const commit = () => {
    if (!text.trim()) {
      setBad(false);
      if (value !== null) onChange(null);
      return;
    }
    const iso = parseUserDate(text, dateFormat);
    if (iso) {
      setBad(false);
      setText(formatDate(iso, dateFormat));
      if (iso !== value) onChange(iso);
    } else {
      setBad(true);
    }
  };

  const selected = value ? fromISO(value) : undefined;
  const sample = formatDate("2025-12-31", dateFormat);

  return (
    <P.Root open={open} onOpenChange={setOpen}>
      <div className="relative">
        <input
          id={id}
          autoFocus={autoFocus}
          value={text}
          inputMode="text"
          autoComplete="off"
          placeholder={placeholder ?? sample}
          aria-invalid={invalid || bad || undefined}
          onChange={(e) => setText(e.target.value)}
          onBlur={commit}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault();
              commit();
            }
            if (e.key === "ArrowDown" && e.altKey) setOpen(true);
          }}
          className={cn(fieldBase, "h-9 pr-9 tabular-nums")}
        />
        <P.Trigger asChild>
          <button
            type="button"
            aria-label="Open calendar"
            className="absolute top-1/2 right-1 flex size-7 -translate-y-1/2 cursor-pointer items-center justify-center rounded-md text-fg-subtle hover:bg-surface-3 hover:text-fg"
          >
            <CalendarDays className="size-4" />
          </button>
        </P.Trigger>
      </div>
      {bad && <p className="mt-1 text-xs text-bad">Use the format {sample}</p>}
      <P.Portal>
        <P.Content
          align="end"
          sideOffset={6}
          className="animate-slide-up z-50 rounded-xl border border-border bg-surface p-3 shadow-pop"
        >
          <DayPicker
            mode="single"
            selected={selected}
            defaultMonth={selected}
            weekStartsOn={weekStartsOn(dateFormat)}
            captionLayout="dropdown"
            startMonth={new Date(1990, 0)}
            endMonth={new Date(new Date().getFullYear() + 30, 11)}
            onSelect={(d) => {
              if (d) {
                onChange(toISO(d));
                setOpen(false);
              }
            }}
          />
          <div className="mt-1 flex justify-between border-t border-border pt-2">
            <button
              type="button"
              className="cursor-pointer rounded-md px-2 py-1 text-xs font-medium text-primary hover:bg-primary-soft"
              onClick={() => {
                onChange(toISO(new Date()));
                setOpen(false);
              }}
            >
              Today
            </button>
            {value && (
              <button
                type="button"
                className="cursor-pointer rounded-md px-2 py-1 text-xs text-fg-muted hover:bg-surface-3"
                onClick={() => {
                  onChange(null);
                  setOpen(false);
                }}
              >
                Clear
              </button>
            )}
          </div>
        </P.Content>
      </P.Portal>
    </P.Root>
  );
}
