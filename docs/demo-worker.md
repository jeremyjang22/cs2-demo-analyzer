# demo-worker

Turns demos uploaded through the site into replays. It runs on the owner's PC,
started at login; later the same binary runs as a cloud service
(docs/uploads.md, phase E). It only makes outbound connections - Postgres and
the R2 bucket - so nothing has to reach the PC.

## Install, update, remove

```powershell
.\scripts\worker\install-worker.ps1               # build, register at login, start
.\scripts\worker\install-worker.ps1               # again after a pull: the new version
.\scripts\worker\install-worker.ps1 -Uninstall
Stop-ScheduledTask cs2-demo-worker                # stop until next login
Start-ScheduledTask cs2-demo-worker
```

It installs to `%LOCALAPPDATA%\cs2-demo-worker\` and reads its settings
(`DATABASE_URL`, `R2_*`) from `packages\api\.env`, the pro catalogue and the
radar files from the checkout it was installed from. Without the scheduled
task, from a terminal:

```sh
cd packages
go run ./demo-worker            # until Ctrl-C, which hands a running job back
go run ./demo-worker -once      # whatever is queued, then exit
```

## What one job does

| Step | | Fails for good when |
|---|---|---|
| `download` | `incoming/<id>` to a scratch folder, hashed on the way | it is gone (storage clears `incoming/` after 7 days), or its size or SHA-256 is not what the browser declared |
| `check` | the first bytes: CS2 (`PBDEMS2`), or zstd around it | anything else, CS:GO demos included |
| `process` | `demo-worker process` in a **child process**, below normal priority: the same build `demo-export` does | the parser cannot read it (exit 3), or it takes over 15 minutes |
| `keep` | the original to `raw/<sha256>.dem.zst`, compressed unless it came compressed | - |
| `publish` | `data/<name>/` and the row, then `index.json` (catalog.Publisher.Put) | - |
| `tidy` | deletes `incoming/<id>` | - |

Anything not in the last column - the network, the database, the child
crashing - is retried, up to three attempts. The uploader sees the reason on
their row; a failure that is ours reads "processing failed: ...", one that is
the file's reads as what is wrong with it.

A claim is a **lease**, renewed every 30 seconds. A job whose lease lapses for
two minutes - the PC slept, crashed, lost the network - is claimed again by
the next worker to look, the same PC after a restart included. A demo that
takes the worker down every time is failed after three such lapses. Stopping
the worker with Ctrl-C (or SIGTERM in the cloud) hands its job back without
spending an attempt; ending the task in Task Manager does not, and the lease
lapses instead.

## Where to look

| Question | Where |
|---|---|
| Is a worker running? | `SELECT name, version, seen_at, job_id FROM workers` - reports in every 30 s. The upload rows say "processing is offline" when nothing has for two minutes. |
| What happened to an upload? | `SELECT state, attempts, last_error, steps, locked_by, started_at, finished_at FROM jobs WHERE demo_id = (SELECT id FROM demos WHERE name = '<name>')` |
| Where did the time go? | `jobs.steps`: milliseconds per step, e.g. `{"download": 8123, "process": 10950, ...}` |
| The full story of one attempt | `logs/jobs/<job id>-<attempt>.txt` in the bucket: every step, everything the parser printed, the error. `wrangler r2 object get cs2-demo-payloads/logs/jobs/12-1.txt --remote --pipe` |
| The worker itself, on the PC | `%LOCALAPPDATA%\cs2-demo-worker\logs\demo-worker-<date>.log`, JSON lines, two weeks kept |
| The worker itself, in the cloud | the service's log in Northflank (stdout, the same JSON lines) |

None of `incoming/`, `raw/` or `logs/` is ever served: the Worker serves
`data/` alone.

## Retrying a failed upload

A failed job stays failed. To run it again after a fix:

```sql
UPDATE jobs SET state = 'queued', attempts = 0, last_error = NULL
WHERE demo_id = (SELECT id FROM demos WHERE name = '<name>');
UPDATE demos SET status = 'queued' WHERE name = '<name>';
```

It needs the upload still in `incoming/`, so within a week of uploading;
after that, upload it again.
