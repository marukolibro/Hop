# Hop — agent guide

Hop is a native macOS GUI for the mihomo (Clash.Meta) core, written in Go with
[MyGo](https://github.com/egoist/mygo)'s native UI (package `ui`, no webview).
It runs its own mihomo as a sidecar process on loopback and drives it through
the REST/WebSocket controller. The README covers features and how to run it;
this file covers how to change it.

The user's global rules (strict typing, pure functions, explicit errors,
structured logging, no fallbacks, minimal tests) apply. Code comments are
English only: the UI copy is Chinese, and a comment may quote a UI label but is
never written in Chinese. Below are the rules specific to this project.

## Safety: never disturb the user's running proxy

The user runs Clash Verge (its own mihomo, in service mode, possibly with TUN)
on this machine while developing Hop. Hop must never interfere with it.

- Never set or change DNS, routes or network settings. TUN is the one
  exception, and only through Hop's privileged helper (`cmd/hop-helper`).
  While its core runs with TUN on, the helper points the system DNS of the
  enabled network services at public resolvers whose traffic enters the TUN
  (so lookups get fake IPs), backs up what was there first, and restores it
  when the core stops, when the helper starts and finds a backup, and at
  quit. Tests never let the helper touch the real DNS. TUN is off
  by default, turned on by the user, refused while another TUN holds the
  default routes, and never turned on in a test or experiment unless the user
  is present and agrees. Never run `sudo`, `osascript ... with administrator
  privileges`, `launchctl`, or write under `/Library` yourself: installing the
  helper is the user's action. The system proxy
  is the other exception, and only through `service.SystemProxyService`: it
  changes it only when the user turns it on (off by default),
  refuses when another app already holds it, and clears only a proxy that
  points at Hop itself (at quit and when the core gives up). Never run
  `networksetup` or touch the proxy registry keys anywhere else, and never
  turn the real system proxy on in a test or experiment.
- Never read, write, stop or signal Clash Verge's processes or files
  (`~/.config/mihomo`, `/Library/Application Support/clash-verge-service`,
  `~/Library/Application Support/io.github.clash-verge-rev.*`, mihomo-party).
- Hop runs its own core, fetched by `scripts/fetch-mihomo.sh` — the core
  into `resources/<os>-<arch>/bin` with its LICENSE beside it, the geodata
  the generated configs can need into `resources/geodata` — all pinned in
  `mihomo.version`; never read the
  core from, or point it at, another client's files. Executing another
  client's core binary through `HOP_MIHOMO_BINARY` is fine.
- A leftover core is stopped only through `mihomo.ReapStale`, which acts on
  the pid in Hop's own `mihomo.pid` and only when that process's command line
  carries Hop's home directory. Never kill a process by name or by port.
- Hop listens only on `127.0.0.1:17890` (mixed) and `127.0.0.1:19090`
  (controller), plus `127.0.0.1:17853` (the core's DNS, opened only for a
  subscription that points its node servers at other names, see below); the
  integration tests use `17891`/`19091`/`17854`. Check a port is free before
  adding one.
- Subscriptions are filtered by a whitelist (`service/runtime_config.go`):
  only `proxies`, `proxy-groups`, `proxy-providers`, `rules`, `rule-providers`,
  `sub-rules` pass through. Hop owns every other key. Never widen this list
  without a dry run of the generated config first. The one narrow exception
  is `service/node_dns.go`: `hosts` entries whose name is one of the
  subscription's own node servers, and its encrypted `dns.nameserver`
  resolvers used only for the names those entries point at. Providers use
  them to send nodes through their nearest entry (measured 24-28 ms against
  77-111 ms through public DNS); nothing else from `hosts` or `dns` is taken. A subscription may instead
  run with Hop's own template (`service/hop_rules.go`, rule source
  `hop`): then only its nodes (`proxies`, `proxy-providers`)
  are taken. A body that is not a Clash profile is read as node links with
  the pinned core's own converter (`github.com/metacubex/mihomo/common/convert`,
  kept at the bundled core's version) and always runs with the template;
  links never go to a third-party conversion service. The template's rules
  use only the shipped geodata, which main seeds into the core's home and
  into the config checks' directory.
- The subscription link is a credential. Never log it, print it, put it in
  errors (use `subscription.Redact` / host only), or paste it into chat.
- Hop's data lives in `~/Library/Application Support/hop/` (files mode 0600).

## Architecture

handler → service → repository, interfaces defined where they are used.

- `internal/model` — shared types only, no methods with logic.
- `internal/repository/mihomo` — core process (`process.go`), REST + WebSocket
  client (`client.go`, `monitor.go`). All controller calls go through
  `Client.do` with a per-request timeout; WebSocket streams go through
  `Client.stream` (read limit raised, the default 32 KiB is too small). A
  core that exits on its own is restarted by the runner's supervisor with
  bounded backoff (`supervise.go`); `Stop` and `Restart` never trigger it,
  and after five crashes in a row it gives up and tells the UI through
  `Runner.OnRestart`. Geodata that ships with the app is seeded into the
  core's home dir before the first start (`geodata.go`).
- `internal/repository/corerelease` and `corestore` — finding, downloading
  (SHA-256 checked against the digest GitHub publishes) and keeping upgraded
  cores under `<AppDir>/core`. The bundled core is never modified; an upgrade
  is a pointer file (`current.json`) to a downloaded one. `config.Load` runs
  `HOP_MIHOMO_BINARY` first, then the upgraded core, then the bundled one.
  `service.CoreService.Upgrade` checks a candidate with `mihomo -v` and `-t`
  on the running config before `mihomo.Runner.Restart`, and puts the old core
  back when the new one does not come up. Core downloads go through Hop's own
  loopback proxy, never the system's or another client's.
- `internal/repository/subscription` — downloads, retries with warnings,
  keeps the `subscription-userinfo` header.
- `internal/repository/profile` — atomic file store for subscription, meta,
  outbound mode and settings (`settings.json`).
- `internal/service` — business logic. View building (`view.go`) is pure:
  node-name parsing, region grouping, group kinds, route resolution. Keep new
  derivations pure and here, not in handlers. Providers name groups and
  nodes their own way, so anything read from a name (`ParseNode`, `ServiceOf`)
  is a keyword rule with a safe default, never an assumption about one
  provider; a tile or row must stay whole for any content.
- `internal/handler` — native UI. `Dashboard` holds UI state; background work
  runs in goroutines started in `Start` and hands results back through
  `Updater.Update` (UI thread only touches state). `MenuBar` is the Updater in
  production so the menu bar panel redraws with the window. Closing the
  window hides it and the Dock icon (activation policy accessory) and Hop
  keeps running; started at login (`mygo.App.WasOpenedAtLogin`) it starts
  that way. The login item itself lives in the system (`mygo.App`
  `OpenAtLogin`), not in Hop's settings; never turn it on for a dev build.
- `config` — startup settings; `main.go` wires everything. User settings
  (`service/settings.go`: appearance, delay test timeout, subscription
  interval, geodata updates) load before the UI and are validated against
  the offered choices; the dashboard's theme is `d.theme(c)`, which applies
  the chosen appearance over the system's.
- `mygo.json` — the update public key and the `updates.privateKey` path.
  `service/updates.go` wraps `mygo.Updater`: the settings 版本 row checks; the
  row and the flyout download and install, then `mygo.App.Relaunch`. mygo fetches
  GitHub with `http.DefaultClient` directly, not Hop's loopback proxy; never
  change `http.DefaultTransport`/`DefaultClient` globally to route it. The
  quit releases (system proxy, helper core) run in `mygo.App.OnQuit`, which
  fires before a Relaunch's new instance starts; `App.Run` returns only after
  finish, so the releases must not move back below it.

WebSocket is `github.com/coder/websocket`, chosen over faster server-oriented
libraries such as gws: Hop only reads three loopback streams at about one
message a second, and `Read(ctx)` gives the clean shutdown on cancel. Do not
swap it for throughput.

Verify any mihomo API shape against the running core before relying on it
(`curl` with the secret from Hop's own `config.yaml`); do not code from memory.

## Design system

All sizes come from `internal/handler/style.go`, and the control metrics
(`switchHeight`, `opticalLift`) from `motion.go`; do not introduce raw numbers
for font sizes, weights, gaps or radii in views.

- Type: 11 caption, 12 secondary, 13 body/controls, 14 tile and tab titles,
  18 headline. Weights 500/600/700 only. Numbers use `FontFeatures("tnum")`.
- Radii: chips 4, controls and tiles 8, panels 12; capsules (buttons, switches,
  status) are `switchHeight` (28) tall with fully rounded ends.
- Colours: theme tokens only (`theme.go`). Accent = route/download, Warning =
  upload/multiplier, Success = connected/fast. Brand gradient (`brand.go`)
  only on the mark and wordmark.
- Buttons: always `button(...)` / `iconButton(...)` from `buttons.go` with
  `primaryButton`, `softButton` or `dangerButton`; never `ui.Button`,
  `ui.PrimaryButton` or `ui.Segmented`. Switches are `slidingSwitch`; on/off
  and exclusive choices that steer traffic (outbound mode, system proxy) are
  `togglePill` (`pill.go`), at the foot of the route card and in the flyout.
- Icons: 24-unit SVG strokes in `icons.go`, tinted by text colour.
- CJK text in small controls sits ~1px low: apply `Translate(0, -opticalLift)`.
- UI copy is Chinese, sentence-short, in the user's terms. No ALL-CAPS labels,
  no `·` joined meta strings, no decorative eyebrows.
- The mark's geometry lives once in `trayicon.go`; the menu bar icon and
  `hopMark` both draw from it.

## Motion

Motion shows change: the switch slides, numbers roll, packets flow with
traffic. Continuous drawings (packets, pulses, spinners) must animate in
`Draw` with `p.Now()` + `p.AnimationFrame()` so they only repaint; never use
`Loop`/per-frame state that rebuilds the whole view. Springs (`ui.Snappy`) for
moves, `AnimateColor` for fills. One orchestrated entrance (the flyout), no
scattered fade-ins.

## MyGo pitfalls

- Keys must be unique among siblings, or development builds panic. Dedupe
  data in the service (see the option dedupe in `buildGroup`).
- Panes that scroll inside a flex row need `.Basis(0).MinHeight(0)` on the
  row, the pane and its `Scroll`, or content spills over the header.
- Never store `ui.Element` in state; never touch state off the UI thread.
- `Transition` enter animations do not play on a window's first frame; use
  `Keyframes` (see `entrance`).
- Read MyGo's source for APIs (`go list -m -f '{{.Dir}}' github.com/egoist/mygo`),
  and `.agents/skills/mygo-maintenance/SKILL.md`.

## Verify every change

1. `gofmt -l .` prints nothing, `go vet ./...` and `go tool mygo vet .` pass.
2. `HOP_MIHOMO_BINARY="/Applications/Clash Verge.app/Contents/MacOS/verge-mihomo" go test ./...`
   — the integration test starts a real core on 17891/19091.
3. For UI changes, render and look at the result before claiming it works:
   a temporary `internal/handler/preview_test.go` using `ui.NewTester`
   (`SetScale(2)`, `SetDark`, `SetTitleBar`, `Advance` past animations,
   `Image()` → PNG) — light and dark, with realistic data (the running core's
   `View` is best). Delete the preview file afterwards; it is not a test.
4. If `go tool mygo dev` is running, confirm it rebuilt (`✓ Rebuilt`) and its
   log has no `panic`. Editing files restarts the core: wait before querying it.
5. Report what was verified and what was not (real-window interaction,
   animation feel) honestly.

## Tests

Do not add unit tests by default. The one integration test
(`internal/integration`) covers import, the whitelist, reload, usage header,
token redaction and restart from the store; extend it when those paths change.
`TestWindowsTunEndToEnd` installs the helper service and takes the routes
over, so it runs only with `HOP_WINDOWS_TUN_E2E=1`, which only the Windows
CI job (a disposable runner) sets; never set it on a machine someone uses.

## Distribution

Hop is for the user and colleagues for now, shipped ad hoc signed without an
Apple Developer ID, as one universal app for Apple silicon and Intel (mygo
combines the two pinned cores and helpers). Colleagues install with `curl -fsSL
https://raw.githubusercontent.com/marukolibro/Hop/main/install.sh | sh`
(curl sets no quarantine attribute, so Gatekeeper lets the app open). The
script refuses while Hop Dev is running, quits a running release Hop, verifies
the archive size and the extracted app's version and ad-hoc code signature,
and installs it
user-owned to `/Applications` when the user can write there, else
`~/Applications`; it removes quarantine only from the installed `Hop.app`
and never uses sudo. MyGo's manifest carries no SHA-256 and POSIX sh does
not verify Ed25519, so the bootstrap download relies on HTTPS; the app's own
updater verifies the signature of every later update. The private key must
stay outside the repository and out of output; `mygo.json` only points at
its path.

Releases are built by `.github/workflows/release.yml` when a `v<version>`
tag is pushed: it checks the tag against `mygo.json` and `CHANGELOG.md`,
builds and signs with the `MYGO_UPDATER_PRIVATE_KEY` repository secret, and
uploads to a draft release that a person publishes. `ci.yml` runs the checks of
"Verify every change" on every push and pull request. Locally, `scripts/release.sh` does the
same: `--dry-run` (build and list) or
`--publish` (build with `mygo build -upload`, which creates a draft release;
the script then prints the `gh release edit v<version> --draft=false` command
for the user to run after review). It refuses a dirty tree, a changelog
without the version, an existing tag, and a build without the signing key.
Only `darwin/universal` is built. Never use shared or third-party signing
certificates. A Developer ID with notarization (MyGo's
`macos.signingIdentity` and `notarize`) waits for a public release.

## Git

Small commits with messages that say why. End commit messages with the
co-author line the session's attribution instructions give. Do not push or
rewrite history without being asked. Changes made by other agents will be
reviewed later, so keep each commit focused and self-explanatory.
