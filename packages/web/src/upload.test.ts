import { describe, expect, it } from "vitest";
import type { Upload } from "./api";
import { fileProblem, inProgress, isDemoFile, mb, sha256, statusText } from "./upload";

const up = (status: Upload["status"], error?: string): Upload => ({
  id: "1", name: "x", status, filename: "x.dem", size: 1, createdAt: "", ...(error ? { error } : {}),
});

describe("upload", () => {
  it("takes .dem and .dem.zst, in any case", () => {
    expect(isDemoFile("nukepug.dem")).toBe(true);
    expect(isDemoFile("1-abc.DEM.ZST")).toBe(true);
    expect(isDemoFile("x.dem.gz")).toBe(false);
    expect(isDemoFile("x.zip")).toBe(false);
  });

  it("refuses before hashing what the server would refuse after", () => {
    const max = 1 << 30;
    expect(fileProblem({ name: "a.dem", size: 5 }, max)).toBeNull();
    expect(fileProblem({ name: "a.txt", size: 5 }, max)).toMatch(/not a demo/);
    expect(fileProblem({ name: "a.dem", size: 0 }, max)).toMatch(/empty/);
    expect(fileProblem({ name: "a.dem", size: max + 1 }, max)).toMatch(/limit is 1024 MB/);
  });

  it("writes sizes people read", () => {
    expect(mb(5 * 2 ** 20)).toBe("5.0 MB");
    expect(mb(670 * 2 ** 20)).toBe("670 MB");
  });

  it("says what each status means", () => {
    expect(statusText(up("queued"))).toBe("Queued for processing");
    expect(statusText(up("failed", "not a CS2 demo"))).toBe("Failed: not a CS2 demo");
    expect(statusText(up("uploading"))).toMatch(/again/);
    expect(statusText(up("queued"), false)).toMatch(/offline/);
    expect(statusText(up("parsing"), false)).toBe("Processing…");
  });

  it("keeps polling only while the server is still working", () => {
    expect(inProgress([up("ready"), up("failed")])).toBe(false);
    expect(inProgress([up("ready"), up("queued")])).toBe(true);
    expect(inProgress([up("parsing")])).toBe(true);
    expect(inProgress([up("uploading")])).toBe(false);
  });

  // The hash is what the server dedups by and the worker verifies against,
  // so it must be the standard one, computed across slice boundaries alike.
  it("hashes in slices to the standard SHA-256", async () => {
    const seen: number[] = [];
    const abc = await sha256(new Blob(["abc"]), (f) => seen.push(f));
    expect(abc).toBe("ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad");
    expect(seen.at(-1)).toBe(1);

    const big = new Uint8Array((8 << 20) + 3).fill(97); // crosses one slice boundary
    const whole = await crypto.subtle.digest("SHA-256", big);
    const hex = [...new Uint8Array(whole)].map((b) => b.toString(16).padStart(2, "0")).join("");
    expect(await sha256(new Blob([big]), () => {})).toBe(hex);
  });
});
