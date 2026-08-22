interface Option<T extends string> {
  value: T;
  label: string;
}

interface Props<T extends string> {
  options: Option<T>[];
  value: T;
  onChange: (value: T) => void;
  /** Fill the row evenly. Off for short pairs like the speed picker. */
  grow?: boolean;
}

/** A row of mutually exclusive buttons, pressed state carried by aria-pressed. */
export default function Segmented<T extends string>({ options, value, onChange, grow = true }: Props<T>) {
  return (
    <div className="seg">
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          aria-pressed={o.value === value}
          style={grow ? { flex: 1 } : undefined}
          onClick={() => onChange(o.value)}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}
