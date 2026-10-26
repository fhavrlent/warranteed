import { X } from "lucide-react";
import { useMemo, useRef, useState } from "react";
import { Trans, useTranslation } from "react-i18next";
import type { Tag } from "@/lib/api";
import { useTags } from "@/lib/queries";
import { cn } from "@/lib/utils";
import { tagClasses } from "@/lib/warranty";
import { fieldBase } from "./ui/input";

export const TagChip = ({
  tag,
  className,
  onRemove,
}: {
  tag: Pick<Tag, "name" | "color">;
  className?: string;
  onRemove?: () => void;
}) => {
  const { t } = useTranslation();
  return (
    <span
      className={cn(
        "inline-flex h-5.5 items-center gap-1 rounded-md px-1.5 text-xs font-medium whitespace-nowrap",
        tagClasses[tag.color] ?? tagClasses.slate,
        className,
      )}
    >
      {tag.name}
      {onRemove && (
        <button
          type="button"
          onClick={onRemove}
          className="-mr-0.5 cursor-pointer rounded opacity-60 hover:opacity-100"
          aria-label={t("common.remove", { name: tag.name })}
        >
          <X className="size-3" />
        </button>
      )}
    </span>
  );
};

/** Free-form tag entry with suggestions from existing tags. */
export const TagInput = ({
  value,
  onChange,
  id,
}: {
  value: string[];
  onChange: (v: string[]) => void;
  id?: string;
}) => {
  const { t } = useTranslation();
  const { data: allTags = [] } = useTags();
  const [text, setText] = useState("");
  const [focus, setFocus] = useState(false);
  const [active, setActive] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);

  const colorOf = (name: string) => allTags.find((t) => t.name.toLowerCase() === name.toLowerCase())?.color ?? "slate";
  const lower = value.map((v) => v.toLowerCase());
  const suggestions = useMemo(() => {
    const q = text.trim().toLowerCase();
    const chosen = new Set(value.map((v) => v.toLowerCase()));
    return allTags
      .filter((t) => !chosen.has(t.name.toLowerCase()) && (!q || t.name.toLowerCase().includes(q)))
      .slice(0, 8);
  }, [allTags, text, value]);

  const add = (name: string) => {
    const n = name.trim().replace(/,$/, "");
    if (n && !lower.includes(n.toLowerCase())) onChange([...value, n]);
    setText("");
    setActive(0);
  };

  const showCreate = text.trim() && !allTags.some((t) => t.name.toLowerCase() === text.trim().toLowerCase());
  const open = focus && (suggestions.length > 0 || showCreate);

  return (
    <div className="relative">
      <div
        className={cn(fieldBase, "flex min-h-9 flex-wrap items-center gap-1 px-1.5 py-1.5")}
        onClick={() => inputRef.current?.focus()}
      >
        {value.map((name) => (
          <TagChip
            key={name}
            tag={{ name, color: colorOf(name) }}
            onRemove={() => onChange(value.filter((x) => x !== name))}
          />
        ))}
        <input
          id={id}
          ref={inputRef}
          value={text}
          placeholder={value.length ? "" : t("tags.placeholder")}
          onFocus={() => setFocus(true)}
          onBlur={() => setTimeout(() => setFocus(false), 120)}
          onChange={(e) => {
            if (e.target.value.endsWith(",")) add(e.target.value);
            else setText(e.target.value);
            setActive(0);
          }}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault();
              if (suggestions[active] && !(showCreate && active === suggestions.length)) add(suggestions[active].name);
              else if (text.trim()) add(text);
            } else if (e.key === "Backspace" && !text && value.length) {
              onChange(value.slice(0, -1));
            } else if (e.key === "ArrowDown") {
              e.preventDefault();
              setActive((a) => Math.min(a + 1, suggestions.length - (showCreate ? 0 : 1)));
            } else if (e.key === "ArrowUp") {
              e.preventDefault();
              setActive((a) => Math.max(a - 1, 0));
            }
          }}
          className="h-6 min-w-24 flex-1 bg-transparent px-1.5 text-sm outline-none placeholder:text-fg-subtle"
        />
      </div>
      {open && (
        <div className="animate-slide-up absolute z-30 mt-1 w-full rounded-xl border border-border bg-surface p-1 shadow-pop">
          {suggestions.map((t, i) => (
            <button
              key={t.id}
              type="button"
              onMouseDown={(e) => e.preventDefault()}
              onClick={() => add(t.name)}
              onMouseEnter={() => setActive(i)}
              className={cn(
                "flex w-full cursor-pointer items-center justify-between rounded-lg px-2 py-1.5 text-left text-sm",
                active === i && "bg-surface-3",
              )}
            >
              <TagChip tag={t} />
              <span className="text-xs text-fg-subtle tabular-nums">{t.count}</span>
            </button>
          ))}
          {showCreate && (
            <button
              type="button"
              onMouseDown={(e) => e.preventDefault()}
              onClick={() => add(text)}
              onMouseEnter={() => setActive(suggestions.length)}
              className={cn(
                "flex w-full cursor-pointer items-center gap-2 rounded-lg px-2 py-1.5 text-left text-sm text-fg-muted",
                active === suggestions.length && "bg-surface-3",
              )}
            >
              <Trans
                i18nKey="tags.create"
                values={{ name: text.trim() }}
                components={{ b: <span className="font-medium text-fg" /> }}
              />
            </button>
          )}
        </div>
      )}
    </div>
  );
};
