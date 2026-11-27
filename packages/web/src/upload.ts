/**
 * Getting a demo from this browser into the processing queue.
 *
 * The file never passes through our servers: it is hashed here, declared to
 * the API (which answers with a presigned URL bound to its exact length), PUT
 * straight to R2, and then confirmed. Cloudflare caps a request through the
 * Worker at 100 MB and demos run to 670 MB. docs/uploads.md has the flow.
 */

import { createSHA256 } from "hash-wasm";
import { completeUpload, startUpload, type Upload } from "./api";

/** What an upload is doing right now, for its row. */
export type Stage =
  | { kind: "hashing"; fraction: number }
  | { kind: "uploading"; fraction: number }
  | { kind: "finishing" };

/** The result: the upload as the server now has it, and whether it was already there. */
export interface Done {
  upload: Upload;
  duplicate: boolean;
}

/** .dem or .dem.zst, the two things a demo arrives as. */
export function isDemoFile(name: string): boolean {
  return /\.dem(\.zst)?$/i.test(name);
}

/** Why a file cannot be uploaded, or null. Checked before hashing 600 MB for nothing. */
export function fileProblem(file: { name: string; size: number }, maxBytes: number): string | null {
  if (!isDemoFile(file.name)) return `${file.name} is not a demo: choose a .dem or .dem.zst file`;
  if (file.size === 0) return `${file.name} is empty`;
  if (file.size > maxBytes) return `${file.name} is ${mb(file.size)}; the limit is ${mb(maxBytes)}`;
  return null;
}

export function mb(bytes: number): string {
  return bytes >= 100 << 20 ? `${Math.round(bytes / 2 ** 20)} MB` : `${(bytes / 2 ** 20).toFixed(1)} MB`;
}

/**
 * A server status as its row says it. Offline is no worker running anywhere:
 * the owner's PC is off, and a queued demo waits for it rather than for a
 * slow parse.
 */
export function statusText(u: Upload, online = true): string {
  switch (u.status) {
    case "uploading": return "Not finished - upload the file again";
    case "queued": return online ? "Queued for processing" : "Queued - processing is offline, and starts when it is back";
    case "parsing": return "Processing…";
    case "ready": return "Ready";
    case "failed": return u.error ? `Failed: ${u.error}` : "Failed";
  }
}

/** Whether anything in the list is still changing on the server. */
export function inProgress(list: Upload[]): boolean {
  return list.some((u) => u.status === "queued" || u.status === "parsing");
}

/**
 * Read in slices this size. Hashing a whole demo in one call would need the
 * file in memory at once, and WebCrypto has no streaming digest.
 */
const SLICE = 8 << 20;

export async function sha256(file: Blob, onProgress: (fraction: number) => void): Promise<string> {
  const h = await createSHA256();
  h.init();
  for (let at = 0; at < file.size; at += SLICE) {
    h.update(new Uint8Array(await file.slice(at, at + SLICE).arrayBuffer()));
    onProgress(Math.min(1, (at + SLICE) / file.size));
  }
  return h.digest("hex");
}

/**
 * PUT a file to a presigned URL. XMLHttpRequest rather than fetch, because
 * fetch still reports no upload progress, and a 600 MB transfer without a
 * progress bar looks hung.
 */
export function putFile(url: string, file: Blob, onProgress: (fraction: number) => void): Promise<void> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open("PUT", url);
    xhr.upload.onprogress = (e) => e.lengthComputable && onProgress(e.loaded / e.total);
    xhr.onload = () => (xhr.status >= 200 && xhr.status < 300
      ? resolve()
      : reject(new Error(`storage refused the file (HTTP ${xhr.status})`)));
    xhr.onerror = () => reject(new Error("the upload was interrupted; check your connection and try again"));
    xhr.send(file);
  });
}

/** The whole thing, start to queued. Each step reports through onStage. */
export async function uploadDemo(file: File, onStage: (s: Stage) => void): Promise<Done> {
  onStage({ kind: "hashing", fraction: 0 });
  const hash = await sha256(file, (fraction) => onStage({ kind: "hashing", fraction }));

  const ticket = await startUpload({ filename: file.name, size: file.size, sha256: hash });
  if (ticket.duplicate || !ticket.url) return { upload: ticket.upload, duplicate: true };

  onStage({ kind: "uploading", fraction: 0 });
  await putFile(ticket.url, file, (fraction) => onStage({ kind: "uploading", fraction }));

  onStage({ kind: "finishing" });
  return { upload: await completeUpload(ticket.upload.id), duplicate: false };
}
