import { useCallback, useEffect, useRef, useState, type DragEvent } from "react";
import { fetchUploads, type Upload, type UploadsState } from "../api";
import { viewerHref } from "../route";
import { fileProblem, inProgress, mb, statusText, uploadDemo, type Stage } from "../upload";

/** How often the rows ask whether processing has moved on. */
const POLL_MS = 5000;
/** Ready uploads stay listed this long; after that the demo table has them. */
const READY_SHOWN_MS = 24 * 3600 * 1000;

/**
 * The Upload button and a row per recent upload, on Your demos.
 *
 * Shown only to accounts that can upload or have uploaded. While anything is
 * queued or processing it polls, and when an upload turns ready it tells the
 * page, which fetches the demo list again so the game appears in the table.
 */
export default function Uploads({ onPublished }: { onPublished: () => void }) {
  const [state, setState] = useState<UploadsState | null>(null);
  const [active, setActive] = useState<{ file: string; stage: Stage } | null>(null);
  const [note, setNote] = useState<{ text: string; error: boolean } | null>(null);
  const [dragging, setDragging] = useState(false);
  const input = useRef<HTMLInputElement>(null);
  const readySeen = useRef<Set<string> | null>(null);

  const refresh = useCallback(async () => {
    const next = await fetchUploads().catch(() => null);
    if (!next) return;
    const ready = new Set(next.uploads.filter((u) => u.status === "ready").map((u) => u.id));
    const before = readySeen.current;
    readySeen.current = ready;
    if (before && [...ready].some((id) => !before.has(id))) onPublished();
    setState(next);
  }, [onPublished]);

  useEffect(() => { void refresh(); }, [refresh]);
  useEffect(() => {
    if (!state || !inProgress(state.uploads)) return;
    const t = setInterval(() => void refresh(), POLL_MS);
    return () => clearInterval(t);
  }, [state, refresh]);

  if (!state || (!state.canUpload && state.uploads.length === 0)) return null;

  async function send(file: File) {
    if (!state) return;
    const problem = fileProblem(file, state.maxBytes);
    if (problem) {
      setNote({ text: problem, error: true });
      return;
    }
    setNote(null);
    try {
      const done = await uploadDemo(file, (stage) => setActive({ file: file.name, stage }));
      if (done.duplicate) {
        setNote({ text: `${file.name} is already here, as ${done.upload.name}.`, error: false });
      }
    } catch (e) {
      setNote({ text: `${file.name}: ${(e as Error).message}`, error: true });
    } finally {
      setActive(null);
      await refresh();
    }
  }

  function drop(e: DragEvent) {
    e.preventDefault();
    setDragging(false);
    const file = e.dataTransfer.files[0];
    if (file && state?.canUpload && !active) void send(file);
  }

  const now = Date.now();
  const rows = state.uploads.filter((u) =>
    u.status !== "ready" || now - Date.parse(u.createdAt) < READY_SHOWN_MS);

  return (
    <section
      className={`uploads${dragging ? " dragging" : ""}`}
      onDragOver={(e) => { if (state.canUpload) { e.preventDefault(); setDragging(true); } }}
      onDragLeave={() => setDragging(false)}
      onDrop={drop}
    >
      <div className="list-head">
        <h2 className="home-h2">Uploads</h2>
        {state.canUpload && (
          <button
            type="button"
            className="upload-btn"
            disabled={active !== null}
            onClick={() => input.current?.click()}
          >
            Upload a demo
          </button>
        )}
        {state.canUpload && (
          <span className="count">.dem or .dem.zst, up to {mb(state.maxBytes)} - or drop it here</span>
        )}
        <input
          ref={input}
          type="file"
          accept=".dem,.zst"
          hidden
          onChange={(e) => {
            const file = e.target.files?.[0];
            e.target.value = "";
            if (file) void send(file);
          }}
        />
      </div>

      {note && <p className={`upload-note${note.error ? " error" : ""}`}>{note.text}</p>}

      {(active || rows.length > 0) && (
        <ul className="upload-list">
          {active && (
            <li>
              <span className="upload-file">{active.file}</span>
              <span className="upload-status">{stageText(active.stage)}</span>
              <Progress stage={active.stage} />
            </li>
          )}
          {rows.map((u) => <Row key={u.id} upload={u} online={state.processing?.online ?? true} />)}
        </ul>
      )}
    </section>
  );
}

function Row({ upload: u, online }: { upload: Upload; online: boolean }) {
  return (
    <li className={`status-${u.status}`}>
      <span className="upload-file" title={u.filename}>{u.name}</span>
      <span className="upload-status">{statusText(u, online)}</span>
      {u.status === "ready"
        ? <a className="upload-open" href={viewerHref(u.name)}>Open</a>
        : <span className="muted">{mb(u.size)}</span>}
    </li>
  );
}

function stageText(s: Stage): string {
  switch (s.kind) {
    case "hashing": return `Reading the file… ${Math.floor(s.fraction * 100)}%`;
    case "uploading": return `Uploading… ${Math.floor(s.fraction * 100)}%`;
    case "finishing": return "Checking it arrived…";
  }
}

function Progress({ stage }: { stage: Stage }) {
  const fraction = stage.kind === "finishing" ? 1 : stage.fraction;
  return (
    <span className="upload-bar" role="progressbar" aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.floor(fraction * 100)}>
      <span style={{ width: `${fraction * 100}%` }} />
    </span>
  );
}
