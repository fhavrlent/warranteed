import * as P from "@radix-ui/react-popover";
import { CalendarDays } from "lucide-react";
import { useState } from "react";
import { DayPicker } from "react-day-picker";
import { useTranslation } from "react-i18next";
import { dateLocale } from "@/i18n";
import { formatDate, fromISO, toISO, weekStartsOn } from "@/lib/dates";
import { parseUserDate } from "@/lib/parseDate";
import { usePrefs } from "@/lib/queries";
import { cn } from "@/lib/utils";
import { fieldBase } from "./ui/input";

// Calendar dropdown range.
const FIRST_MONTH = new Date(1990, 0);
const LAST_MONTH = new Date(new Date().getFullYear() + 30, 11);

/**
 * A date field that displays and accepts dates in the user's chosen format,
 * instead of <input type="date"> which always follows the OS locale.
 * The value is always an ISO date (YYYY-MM-DD) or null.
 */
export const DateInput = ({
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
}) => {
  const { t } = useTranslation();
  const { dateFormat } = usePrefs();
  // While the user is typing, `draft` holds their text; otherwise the field
  // shows the formatted value, so it follows value and format changes.
  const [draft, setDraft] = useState<string | null>(null);
  // The draft that failed to parse; editing it clears the error.
  const [rejected, setRejected] = useState<string | null>(null);
  const [open, setOpen] = useState(false);

  const text = draft ?? formatDate(value, dateFormat);
  const bad = draft !== null && draft === rejected;

  const commit = () => {
    if (draft === null) return;
    if (!draft.trim()) {
      setDraft(null);
      if (value !== null) onChange(null);
      return;
    }
    const iso = parseUserDate(draft, dateFormat);
    if (iso) {
      setDraft(null);
      if (iso !== value) onChange(iso);
    } else {
      setRejected(draft);
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
          aria-invalid={invalid || bad ? true : undefined}
          onChange={(e) => setDraft(e.target.value)}
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
            aria-label={t("date.openCalendar")}
            className="absolute top-1/2 right-1 flex size-7 -translate-y-1/2 cursor-pointer items-center justify-center rounded-md text-fg-subtle hover:bg-surface-3 hover:text-fg"
          >
            <CalendarDays className="size-4" />
          </button>
        </P.Trigger>
      </div>
      {bad && <p className="mt-1 text-xs text-bad">{t("date.useFormat", { sample })}</p>}
      <P.Portal>
        <P.Content
          align="end"
          sideOffset={6}
          className="animate-slide-up z-50 rounded-xl border border-border bg-surface p-3 shadow-pop"
        >
          <DayPicker
            mode="single"
            locale={dateLocale()}
            selected={selected}
            defaultMonth={selected}
            weekStartsOn={weekStartsOn(dateFormat)}
            captionLayout="dropdown"
            startMonth={FIRST_MONTH}
            endMonth={LAST_MONTH}
            onSelect={(d) => {
              if (d) {
                setDraft(null);
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
                setDraft(null);
                onChange(toISO(new Date()));
                setOpen(false);
              }}
            >
              {t("date.today")}
            </button>
            {value && (
              <button
                type="button"
                className="cursor-pointer rounded-md px-2 py-1 text-xs text-fg-muted hover:bg-surface-3"
                onClick={() => {
                  setDraft(null);
                  onChange(null);
                  setOpen(false);
                }}
              >
                {t("date.clear")}
              </button>
            )}
          </div>
        </P.Content>
      </P.Portal>
    </P.Root>
  );
};
