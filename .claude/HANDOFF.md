# Handoff — 2026-09-18

Read this first in a new session. It's a pointer, not a re-explanation —
the full phase-by-phase history lives in
[`.claude/plans/volt-implementation-plan.md`](plans/volt-implementation-plan.md)
(progress log at the top of that file, in order).

## Where things stand

- **All 12 phases of the implementation plan are done, merged to `main`,
  and verified.** Not stubs — real code, real tests, `go build`/`vet`/
  `test ./...` and `gofmt -l .` all clean as of the last commit.
- **VoltPanel v0.2.0 is publicly released**: Apache-2.0 licensed,
  downloadable right now from
  https://github.com/alresiainc/alresia-voltpanel/releases/tag/v0.2.0
  (macOS/Linux/Windows binaries, deb/rpm for Linux). Verified by actually
  downloading the released asset over the network and running it.
- Repo (`alresiainc/alresia-voltpanel`) is public. Release automation
  (`.github/workflows/release.yml`) cuts a new release automatically on
  any `git push --tags` of a `v*` tag.

## Open items — need a human, not another agent session to just redo them

1. **GitHub flagged 45 Dependabot vulnerabilities** (10 critical, 10
   high, 23 moderate, 2 low) on the `main` push that shipped all this.
   Never audited. Check
   https://github.com/alresiainc/alresia-voltpanel/security/dependabot
   before pointing real users at this release.
2. **Homebrew tap is disabled** (`goreleaser.yml`'s `brews:` block has
   `skip_upload: true`). To enable: make `alresiainc/homebrew-tap`
   public, create a PAT with write access to it, add it as a repo secret
   (e.g. `HOMEBREW_TAP_GITHUB_TOKEN`) on `alresia-voltpanel`, wire it into
   the `token:` field of the `brews:` block, flip `skip_upload` to
   `false`. The default `GITHUB_TOKEN` can't push cross-repo, which is
   why this was never done tonight.
3. **`.hide` file** in the repo root has what looks like a live GitHub
   PAT in plaintext. Untracked, gitignored, never entered git history —
   but sitting in the working tree since before this session started.
   Never touched or read by any agent this session (not our credential to
   judge or act on). If it's real, rotate/remove it.
4. **`brews:` is deprecated** in favor of `homebrew_casks` per GoReleaser
   — functional today, but worth migrating whenever the tap gets wired up
   for real (item 2).

## Genuinely open engineering gaps (not oversights — noted honestly at the time)

- **No real cross-platform test matrix.** Everything ran on one macOS
  box all session. Linux/Windows are cross-compile-verified
  (`GOOS=... go build ./...`) only, never executed.
- **File-manager sandbox is still rooted at the user's home directory**,
  not per-project. Real gap now that Project exists (Phase 3) — narrowing
  it was never done.
- **WiX MSI path** (`packaging/wix/template.wxs`) isn't wired into the
  GoReleaser pipeline — Windows ships as a `.zip` only.
- Nothing in this codebase has ever written to a real `/etc/hosts`,
  touched a real OS trust store, registered a real persistent login
  service, connected to a real external SSH host, or called the real
  GitHub API — all by design, verified against temp files/local fakes/
  in-process test servers instead. First real use of each of those is
  still ahead of whoever uses this for real.

## Quick orientation for a new session

- `make build` → `dist/voltpanel` (embeds the UI). `make test` → `go test
  ./...`. `bash scripts/integration_test.sh` → smoke test against the
  built binary.
- `docs/architecture.md` and `docs/api.md` are current as of this
  session — read those before the plan doc's prose if you just need "how
  does X work" rather than "why does X exist."
- Product name is **VoltPanel** (binary `voltpanel`) — not "Volt". This
  was deliberated and decided; don't re-litigate it without a reason.
- Config dir is `~/.volt/` (yes, despite the product being named
  VoltPanel — a separate, deliberate decision, also already made).
