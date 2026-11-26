import { activeFilters, filterChoices, NO_FILTERS, type Filters } from "../demoTable";
import type { DemoSummary } from "../payload";
import { KIND_LABEL, mapLabel } from "../pros";
import DateRange from "./DateRange";

interface Props {
  /** The page's demos before filtering: the dropdowns offer only what is in them. */
  demos: DemoSummary[];
  value: Filters;
  onChange: (f: Filters) => void;
  /** A win or loss needs a side to be on; off when the table has no subject. */
  result?: boolean;
}

/** Map, type, result and date filters over one demo table. */
export default function DemoFilters({ demos, value, onChange, result = true }: Props) {
  const choices = filterChoices(demos);
  const set = (patch: Partial<Filters>) => onChange({ ...value, ...patch });

  return (
    <div className="filters">
      {choices.maps.length > 1 && (
        <select className={value.map ? "on" : undefined} aria-label="Map" value={value.map}
          onChange={(e) => set({ map: e.target.value })}>
          <option value="">All maps</option>
          {choices.maps.map((m) => <option key={m} value={m}>{mapLabel(m)}</option>)}
        </select>
      )}
      {choices.kinds.length > 1 && (
        <select className={value.kind ? "on" : undefined} aria-label="Type" value={value.kind}
          onChange={(e) => set({ kind: e.target.value })}>
          <option value="">Any type</option>
          {choices.kinds.map((k) => <option key={k} value={k}>{KIND_LABEL[k]}</option>)}
        </select>
      )}
      {result && (
        <select className={value.result ? "on" : undefined} aria-label="Result" value={value.result}
          onChange={(e) => set({ result: e.target.value as Filters["result"] })}>
          <option value="">Any result</option>
          <option value="W">Wins</option>
          <option value="L">Losses</option>
        </select>
      )}
      <DateRange from={value.from} to={value.to} dated={choices.dated} onChange={(r) => set(r)} />
      {activeFilters(value) > 0 && (
        <button type="button" className="clear" onClick={() => onChange(NO_FILTERS)}>Clear filters</button>
      )}
    </div>
  );
}
