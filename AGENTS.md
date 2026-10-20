# Hop — agent guide

Hop is a native macOS GUI for the mihomo (Clash.Meta) core, written in Go with
[MyGo](https://github.com/egoist/mygo)'s native UI (package `ui`, no webview).
It runs its own mihomo as a sidecar process on loopback and drives it through
the REST/WebSocket controller. The README covers features and how to run it;
this file covers how to change it.

The user's global rules (strict typing, pure functions, explicit errors,
structured logging, no fallbacks, minimal tests) apply. Below are the rules
specific to this project.

## Safety: never disturb the user's running proxy

The user runs Clash Verge (its own mihomo, in service mode, possibly with TUN)
on this machine while developing Hop. Hop must never interfere with it.

- Never set or change the system proxy, TUN, DNS, routes or network settings.
  System proxy support does not exist yet; adding it needs the user's explicit
  go-ahead, off by default.
- Never read, write, stop or signal Clash Verge's processes or files
  (`~/.config/mihomo`, `/Library/Application Support/clash-verge-service`,
  `~/Library/Application Support/io.github.clash-verge-rev.*`, mihomo-party).
  Executing its bundled core binary (`HOP_MIHOMO_BINARY`) is fine.
- Hop listens only on `127.0.0.1:17890` (mixed) and `127.0.0.1:19090`
  (controller); the integration test uses `17891`/`19091`. Check a port is free
  before adding one.
- Subscriptions are filtered by a whitelist (`service/runtime_config.go`):
  only `proxies`, `proxy-groups`, `proxy-providers`, `rules`, `rule-providers`,
  `sub-rules` pass through. Hop owns every other key. Never widen this list
  without a dry run of the generated config first.
- The subscription link is a credential. Never log it, print it, put it in
  errors (use `subscription.Redact` / host only), or paste it into chat.
- Hop's data lives in `~/Library/Application Support/hop/` (files mode 0600).

## Architecture

handler → service → repository, interfaces defined where they are used.

- `internal/model` — shared types only, no methods with logic.
- `internal/repository/mihomo` — core process (`process.go`), REST + WebSocket
  client (`client.go`, `monitor.go`). All controller calls go through
  `Client.do` with a per-request timeout; WebSocket streams go through
  `Client.stream` (read limit raised, the default 32 KiB is too small).
- `internal/repository/subscription` — downloads, retries with warnings,
  keeps the `subscription-userinfo` header.
- `internal/repository/profile` — atomic file store for subscription, meta,
  outbound mode.
- `internal/service` — business logic. View building (`view.go`) is pure:
  node-name parsing, region grouping, group kinds, route resolution. Keep new
  derivations pure and here, not in handlers.
- `internal/handler` — native UI. `Dashboard` holds UI state; background work
  runs in goroutines started in `Start` and hands results back through
  `Updater.Update` (UI thread only touches state). `MenuBar` is the Updater in
  production so the menu bar panel redraws with the window.
- `config` — startup settings; `main.go` wires everything.

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
  `ui.PrimaryButton` or `ui.Segmented`. Switches are `slidingSwitch`.
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

## Git

Small commits with messages that say why. End commit messages with the
co-author line the session's attribution instructions give. Do not push or
rewrite history without being asked. Changes made by other agents will be
reviewed later, so keep each commit focused and self-explanatory.
