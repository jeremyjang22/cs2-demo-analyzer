import Segmented from "./Segmented";
import type { ViewState } from "../renderer";

interface Props {
  view: ViewState;
  patch: (next: Partial<ViewState>) => void;
}

/** One layer toggle, as a labelled on/off pair. */
function Layer({
  label, on, onChange,
}: { label: string; on: boolean; onChange: (on: boolean) => void }) {
  return (
    <Segmented
      grow={false}
      options={[{ value: "on", label }, { value: "off", label: "off" }]}
      value={on ? "on" : "off"}
      onChange={(v) => onChange(v === "on")}
    />
  );
}

/**
 * The view options, as one strip under the map.
 *
 * They used to run down the same column as the rosters, which put nine
 * segmented controls between the two teams and the thing they describe. Below
 * the map they sit next to what they change, and the sides are left for the
 * only content that belongs beside a minimap.
 */
export default function ViewControls({ view, patch }: Props) {
  return (
    <div className="controls">
      <Segmented
        grow={false}
        options={[
          { value: "dots", label: "Live" },
          { value: "trail", label: "Trails" },
          { value: "full", label: "Full round" },
        ]}
        value={view.mode}
        onChange={(mode) => patch({ mode })}
      />

      <span className="controls-sep" />

      <Layer label="Cones" on={view.cones} onChange={(cones) => patch({ cones })} />
      <Layer label="Utility" on={view.util} onChange={(util) => patch({ util })} />
      <Layer label="Fire" on={view.fire} onChange={(fire) => patch({ fire })} />
      <Layer label="Bomb" on={view.bomb} onChange={(bomb) => patch({ bomb })} />
      <Layer label="Deaths" on={view.deaths} onChange={(deaths) => patch({ deaths })} />
      <Layer label="Names" on={view.labels} onChange={(labels) => patch({ labels })} />

      <span className="controls-sep" />

      <Segmented
        grow={false}
        options={[{ value: "game", label: "Game" }, { value: "safe", label: "Accessible" }]}
        value={view.palette}
        onChange={(palette) => patch({ palette })}
      />
    </div>
  );
}
