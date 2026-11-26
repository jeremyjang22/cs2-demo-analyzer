import { useEffect, useRef, useState } from "react";
import { formatDate, monthGrid, pick, shiftMonth, validDate, WEEKDAYS } from "../calendar";

interface Props {
  from: string;
  to: string;
  onChange: (range: { from: string; to: string }) => void;
  /** Days a demo was played, dotted on the calendars. */
  dated: Set<string>;
}

/**
 * A date-range filter: a dropdown holding two calendars, start and end, each
 * with a field above it so a date can be picked or typed.
 *
 * The committed range lives with the caller; this holds only what is mid-edit
 * (the months on show and half-typed text), so clearing the filters elsewhere
 * resets it without a second source of truth.
 */
export default function DateRange({ from, to, onChange, dated }: Props) {
  const [open, setOpen] = useState(false);
  const [view, setView] = useState({ from: "", to: "" });
  const [draft, setDraft] = useState({ from, to });
  const root = useRef<HTMLDivElement>(null);

  // Typed text follows the committed range whenever it changes from outside.
  useEffect(() => setDraft({ from, to }), [from, to]);

  useEffect(() => {
    if (!open) return;
    const away = (e: MouseEvent) => {
      // composedPath, not contains(): picking a day re-renders the grid, and
      // a detached target would otherwise read as a click outside.
      if (root.current && !e.composedPath().includes(root.current)) setOpen(false);
    };
    const esc = (e: KeyboardEvent) => e.key === "Escape" && setOpen(false);
    document.addEventListener("mousedown", away);
    document.addEventListener("keydown", esc);
    return () => {
      document.removeEventListener("mousedown", away);
      document.removeEventListener("keydown", esc);
    };
  }, [open]);

  const disabled = dated.size === 0;

  function toggle() {
    if (open) { setOpen(false); return; }
    // Start on the chosen dates, else on the first and last day anything was played.
    const days = [...dated].sort();
    const fallback = new Date().toISOString().slice(0, 7);
    setView({
      from: (from || days[0] || fallback).slice(0, 7),
      to: (to || days[days.length - 1] || fallback).slice(0, 7),
    });
    setOpen(true);
  }

  function typed(which: "from" | "to", text: string) {
    setDraft((d) => ({ ...d, [which]: text }));
    const t = text.trim();
    if (t === "") onChange({ from, to, [which]: "" });
    else if (validDate(t)) {
      onChange(which === "from" ? { from: t, to: to && t > to ? "" : to } : { from, to: t });
      setView((v) => ({ ...v, [which]: t.slice(0, 7) }));
    }
  }

  const label = from && to ? `${formatDate(from)} – ${formatDate(to)}`
    : from ? `From ${formatDate(from)}` : to ? `Until ${formatDate(to)}` : "Any date";

  return (
    <div className="daterange" ref={root}>
      <button
        type="button"
        className={`dr-btn${from || to ? " on" : ""}`}
        aria-haspopup="dialog"
        aria-expanded={open}
        disabled={disabled}
        title={disabled ? "None of these demos has a date yet" : undefined}
        onClick={toggle}
      >
        {label}
      </button>
      {open && (
        <div className="dr-pop" role="dialog" aria-label="Choose a date range">
          <div className="dr-cals">
            {(["from", "to"] as const).map((which) => (
              <div className="dr-cal" key={which}>
                <label>
                  {which === "from" ? "Start" : "End"}
                  <input
                    type="text"
                    className={draft[which] && !validDate(draft[which].trim()) ? "bad" : undefined}
                    placeholder="YYYY-MM-DD"
                    autoComplete="off"
                    spellCheck={false}
                    value={draft[which]}
                    onChange={(e) => typed(which, e.target.value)}
                  />
                </label>
                <Calendar
                  month={view[which]}
                  onMonth={(m) => setView((v) => ({ ...v, [which]: m }))}
                  from={from}
                  to={to}
                  dated={dated}
                  onPick={(day) => onChange(pick({ from, to }, which, day))}
                />
              </div>
            ))}
          </div>
          <div className="dr-foot">
            <span><span className="dr-dot" /> a demo was played that day</span>
            <span className="dr-actions">
              <button type="button" className="clear" onClick={() => onChange({ from: "", to: "" })}>Clear dates</button>
              <button type="button" className="dr-done" onClick={() => setOpen(false)}>Done</button>
            </span>
          </div>
        </div>
      )}
    </div>
  );
}

interface CalendarProps {
  month: string;
  onMonth: (month: string) => void;
  from: string;
  to: string;
  dated: Set<string>;
  onPick: (day: string) => void;
}

function Calendar({ month, onMonth, from, to, dated, onPick }: CalendarProps) {
  const grid = monthGrid(month);
  const today = new Date().toISOString().slice(0, 10);
  return (
    <div>
      <div className="dr-head">
        <button type="button" className="dr-nav" aria-label="Previous month" onClick={() => onMonth(shiftMonth(month, -1))}>‹</button>
        <span>{grid.label}</span>
        <button type="button" className="dr-nav" aria-label="Next month" onClick={() => onMonth(shiftMonth(month, 1))}>›</button>
      </div>
      <div className="dr-days">
        {WEEKDAYS.map((d) => <span key={d} className="dow">{d}</span>)}
        {Array.from({ length: grid.lead }, (_, i) => <span key={`lead${i}`} />)}
        {grid.days.map((day) => {
          const selected = day === from || day === to;
          const cls = ["day",
            selected && "sel",
            from && to && day > from && day < to && "in",
            dated.has(day) && "has",
            day === today && "today"].filter(Boolean).join(" ");
          return (
            <button
              key={day}
              type="button"
              className={cls}
              aria-label={formatDate(day)}
              aria-pressed={selected}
              onClick={() => onPick(day)}
            >
              {Number(day.slice(8))}
            </button>
          );
        })}
      </div>
    </div>
  );
}
