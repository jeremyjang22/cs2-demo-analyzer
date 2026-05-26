# Developer Setup

How to get this repo working identically on macOS and Windows laptops.

## Philosophy

Two principles drive every choice below:

1. **On Windows, two paths both work and both are documented.** *Native Windows* (Git for Windows + PowerShell / Git Bash) is fine for editing, git operations, and most single-language workflows — and it's what you already have if you didn't go out of your way to install anything else. *WSL2 (Ubuntu)* is the better long-term choice once language toolchains and shell scripts enter the picture, because it gives you a real Linux environment identical to macOS and Linux CI. Pick whichever matches your current setup; you can switch later without losing work. Sections below tag platform-specific steps as **macOS**, **Windows (native)**, and **Windows (WSL2)**.
2. **Conventions live in the repo, not in your dotfiles.** Anything that has to behave the same on both machines (line endings, indentation, dependency versions) is a committed file. Personal preferences stay personal.

---

## Per-machine setup

Do this once on each laptop.

### 1. Shell

**macOS:** `zsh` is the default. Nothing to do.

**Windows (native):** PowerShell is built in. Most cross-platform shell snippets in this doc also work in **Git Bash**, which ships with Git for Windows (next section). Rule of thumb: copy POSIX-style commands (`curl`, `ls`, `&&`-chains) into Git Bash; use PowerShell for anything Windows-specific (`winget`, `wsl --install`, etc.).

**Windows (WSL2)** — *recommended once we adopt language toolchains*:
```powershell
# In PowerShell (Admin), one-time:
wsl --install -d Ubuntu
```
After it reboots, open Ubuntu from Start Menu and finish the user setup. Once WSL is set up, treat the Ubuntu shell as your "real" Windows shell — `git`, language toolchains, and scripts live there.

> Files live under `\\wsl$\Ubuntu\home\<you>\` from Windows Explorer. Clone repos *inside* the WSL filesystem (e.g. `~/Projects/cs2-demo-analyzer`), not under `/mnt/c/...` — disk I/O across the boundary is 10×+ slower.

> **Switching from native to WSL2 later** is straightforward: commit and push everything from your native clone, then re-clone into `~/Projects/...` inside Ubuntu. The Windows-side clone can be deleted afterwards.

### 2. Git

**macOS:**
```sh
brew install git gh
```
(Install [Homebrew](https://brew.sh) first if you don't have it.)

**Windows (native):** Install [Git for Windows](https://git-scm.com/download/win) — this also installs the **Git Bash** shell. Then install GitHub CLI:
```powershell
winget install --id GitHub.cli
```
(or download from [cli.github.com](https://cli.github.com/)). Restart your terminal so `git` and `gh` are on `PATH`.

**Windows (WSL2 Ubuntu):**
```sh
sudo apt update && sudo apt install -y git
# Install gh per https://github.com/cli/cli/blob/trunk/docs/install_linux.md
```

Then on both:
```sh
git config --global user.name  "Jeremy Jang"
git config --global user.email "your-github-noreply@users.noreply.github.com"
git config --global init.defaultBranch main
git config --global pull.rebase true
```

### 3. GitHub auth

```sh
gh auth login
```
Pick HTTPS, authenticate in browser. If you have multiple GitHub accounts (work + personal), log in to each:
```sh
gh auth login         # account A
gh auth login         # account B
gh auth switch        # pick which one is "active" for git pushes
```

> **Known gotcha:** `gh`'s active account is the one git uses for HTTPS pushes. If you get `403 Permission denied to <other-username>` when pushing, run `gh auth switch --user <correct-account>` and retry.

### 4. Editor

VS Code is the simplest choice — identical on both OSes, settings sync via your GitHub account.

- macOS: `brew install --cask visual-studio-code`
- Windows: download the installer; install the **WSL** extension so VS Code can open repos *inside* the WSL filesystem with native performance.

Recommended extensions (install once, sync to both machines):
- **EditorConfig for VS Code** — picks up `.editorconfig` automatically.
- **GitLens** — git history surfaced in the editor.
- **Remote – WSL** (Windows + WSL2 only) — required to edit WSL files cleanly. Skip if you're using native Windows.
- Language extensions (Python, Go, etc.) will be added as we adopt each language.

### 5. Language toolchains

Install only what we're actually using. Keep this section honest — don't pre-install everything.

**Python (when we adopt it):**
We'll standardize on [`uv`](https://docs.astral.sh/uv/) — fast, single-binary, handles both Python-version management *and* virtualenvs. Works identically on every platform.

macOS / WSL2 / Git Bash:
```sh
curl -LsSf https://astral.sh/uv/install.sh | sh
```

Windows (native, PowerShell):
```powershell
powershell -ExecutionPolicy ByPass -c "irm https://astral.sh/uv/install.ps1 | iex"
```

**Go:**
Install via the OS package manager — Go itself is a single-binary toolchain.

macOS:
```sh
brew install go
```

Windows (native, PowerShell):
```powershell
winget install GoLang.Go
```
Restart your terminal so `go` is on `PATH`.

Windows (WSL2 Ubuntu):
Don't use `apt install golang` — Ubuntu's package often lags by a major
version. Use the official tarball instead:
```sh
# Replace x.y.z with the current stable version from https://go.dev/dl
curl -LO https://go.dev/dl/go1.22.0.linux-amd64.tar.gz
sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf go1.22.0.linux-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
source ~/.bashrc
```

**Rust:**
Install via `rustup` — works the same across all three platforms. It manages
the Rust compiler, `cargo`, `rustfmt`, and `clippy`.

macOS / WSL2:
```sh
curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh
```
Accept the default installation. After it finishes, either restart your
shell or run `source "$HOME/.cargo/env"` to pick up `cargo` on `PATH`.

Windows (native):
Download and run `rustup-init.exe` from [rustup.rs](https://rustup.rs/).
The installer also installs the MSVC build tools prerequisite if missing.

**Node / TypeScript (when Phase 3 arrives):** TBD — likely via `fnm` or `mise`.

---

## In-repo conventions

These files belong *in* the repo (committed to git) so every clone — on either OS — picks them up automatically. Suggested contents below; create them when ready.

### `.gitattributes` — kills line-ending chaos

Without this, Windows checkouts may convert `LF` → `CRLF` and break shell scripts, hash comparisons, etc.

```gitattributes
* text=auto eol=lf

# Binary files: never touch
*.dem      binary
*.png      binary
*.jpg      binary
*.parquet  binary
*.sqlite   binary
*.db       binary

# Platform-specific files
*.bat      text eol=crlf
*.ps1      text eol=crlf
*.sh       text eol=lf
```

### `.editorconfig` — consistent indentation across editors

```editorconfig
root = true

[*]
charset                  = utf-8
end_of_line              = lf
insert_final_newline     = true
trim_trailing_whitespace = true
indent_style             = space
indent_size              = 4

[*.{js,jsx,ts,tsx,json,yml,yaml,md}]
indent_size = 2

[Makefile]
indent_style = tab
```

### `.gitignore`

Start small and grow it as actual artifacts appear. A reasonable starter:
```
# Editor
.vscode/*
!.vscode/extensions.json
!.vscode/settings.json
.idea/

# OS
.DS_Store
Thumbs.db

# Python (when we adopt it)
__pycache__/
*.py[cod]
.venv/
.python-version

# Data — never commit demo files or local databases
data/
*.dem
*.sqlite
*.db
*.parquet

# Env / secrets
.env
.env.*
```

---

## Cloning the repo on a new machine

```sh
gh auth login                        # if first time on this machine
git clone https://github.com/jeremyjang22/cs2-demo-analyzer.git
cd cs2-demo-analyzer
```

**Where to clone:**
- **macOS / Windows (native):** anywhere convenient (e.g. `~/Projects/` on macOS, `C:\Users\<you>\Projects\` on Windows).
- **Windows (WSL2):** *inside* the WSL filesystem (`~/Projects/...`), **not** under `/mnt/c/...`.

Once a language toolchain is in use, this section will grow a "bootstrap" step (`uv sync`, `go mod download`, etc.).

---

## "Does it work?" smoke test

Until there's actual code, the smoke test is just:
```sh
git pull           # no errors
git status         # clean
echo "test from $(uname -s)" >> /tmp/x && rm /tmp/x   # shell works
```

Once we have a project bootstrap, this section will become a real "verify your environment" checklist (run the tests, run the parser on a tiny demo, etc.).

---

## Troubleshooting

- **`Permission denied (publickey)` or `403` on push** — see "GitHub auth" gotcha above.
- **Line-ending diffs you didn't make** — `.gitattributes` is missing or you didn't re-checkout after adding it. Run `git add --renormalize .` once.
- **Slow git/file operations on Windows (WSL2)** — your repo is probably under `/mnt/c/...` instead of inside the WSL2 filesystem. Move it to `~/Projects/...`.
- **`git` or `gh` "not recognized" on Windows (native)** — restart your terminal after install so `PATH` picks them up. If still missing, re-run the installer and confirm the "Add to PATH" option was checked.
- **VS Code on Windows opens files in CRLF** — install the `EditorConfig` extension and confirm `.editorconfig` is committed.
