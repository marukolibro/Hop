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
  subscription that points its node servers at other names, see below) and
  `127.0.0.1:17900`-`17907` (the probe listeners, see below). The
  dev build (`mygo dev`) is its own instance (`config.Development`): ports
  `17892`/`19092`/`17855`/`17910`-`17917` and its own data dir `hop-dev`, so it runs beside
  an installed Hop and never reaps its core; it starts with no subscription.
  It also never talks to the privileged helper (`helper.Absent`, `Instance.UsesHelper`):
  the helper is one per machine, so a dev start or quit would stop the core
  the installed Hop's TUN runs under it.
  The integration tests use `17891`/`19091`/`17854`/`17920`-`17927`. Check a port is free before
  adding one.
- LAN access (`service/lan.go`) is the one thing that opens the mixed port to
  the network, and only while the user turned it on (`Settings.AllowLAN`, off
  by default). `LANState` is the live value every config built by
  `SubscriptionService.runtimeConfig` carries, like `TunFlag`: `applyLAN` sets
  `allow-lan`, `bind-address: "*"`, `lan-allowed-ips` to loopback and the
  private ranges only, and, unless the user chose no password
  (`Settings.LANNoPassword`, so older settings stay safe), `authentication`
  with a random password kept in `lan-credential` (0600) and
  `skip-auth-prefixes` for `127.0.0.1` and `::1`, so the system proxy and Hop's
  own requests need no account. Everything else (the controller, the core's
  DNS, the probe listeners) names `127.0.0.1` itself and never follows
  `bind-address`; `TestLANAccess` checks that against a real core. Never widen
  `lanAllowedSources`, and never open any other port to the network. Under TUN
  the core is the helper's and runs as root, and a root process must not parse
  requests from the whole internet, so the helper (`controllerKeys`,
  `checkLANSources`) accepts `allow-lan` only with a `lan-allowed-ips` list
  inside `helper.LANSources` (the one list the app writes too), and refuses a
  listener that is not on the loopback; `TestHelperServesItsCore` checks that
  against a real helper. That rule is a privileged boundary: widening it needs
  the user's say, and it changes `helper.Version` (now 8), which makes an
  installed older helper be replaced on the user's authorization.
- The unlock check (`service/unlock.go`, `repository/unlock`) needs a request
  through one chosen group, which the controller cannot make: its delay test
  is a HEAD that also overwrites the proxy's delay history. So every config
  carries more Hop-owned parts (`service/probe_config.go`): `model.ProbeLanes`
  loopback `http` listeners on consecutive ports from `Instance.ProbePort`,
  each with `proxy` set to its own `model.ProbeGroup(i)`, a `select` group over
  every group of the profile. The check picks a group in a lane and sends its
  requests through that lane's listener, with keep-alives off; the lanes run
  at once. The lane groups are never shown: `Client.Snapshot` drops them from
  the groups and from GLOBAL's options. `RuntimeSettings.ProbePort` 0, or a
  profile with no group, leaves them out. Never route anything but the check
  through them, and never put them in a rule. The check reads a lane's choice
  back after each group and stops when it is not that group: a reload in
  between would put the result under the wrong group.
- Subscriptions are filtered by a whitelist (`service/runtime_config.go`):
  only `proxies`, `proxy-groups`, `proxy-providers`, `rules`, `rule-providers`,
  `sub-rules` pass through. Hop owns every other key. Never widen this list
  without a dry run of the generated config first. The one narrow exception
  is `service/node_dns.go`: `hosts` entries whose name is one of the
  subscription's own node servers, its encrypted `dns.nameserver`
  resolvers used only for the names those entries point at, and its
  encrypted `dns.proxy-server-nameserver` resolvers used only for node
  servers. Providers use them to send nodes through their nearest entry
  (measured 24-28 ms against 77-111 ms through public DNS); nothing else from
  `hosts` or `dns` is taken, and plain or local resolvers never are. A subscription may instead
  run with Hop's own template (`service/hop_rules.go`, rule source
  `hop`): then only its nodes (`proxies`, `proxy-providers`)
  are taken. A body that is not a Clash profile is read as node links with
  the pinned core's own converter (`github.com/metacubex/mihomo/common/convert`,
  kept at the bundled core's version) and always runs with the template;
  links never go to a third-party conversion service. The template's rules
  use only the shipped geodata, which main seeds into the core's home and
  into the config checks' directory. A profile the user adds from a local
  file or pasted text goes through exactly the same reading, whitelist and
  core test; Hop keeps a copy (0600, atomic) and never refreshes it, so the
  original file may move or vanish.
- The subscription link is a credential, and so is a local profile's content
  (it carries node passwords). Never log either, print it, put it in errors
  (use `subscription.Redact` / host or file name only), or paste it into chat.
- Hop's data lives in `~/Library/Application Support/hop/` (files mode 0600); the dev build's in `hop-dev/`.

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
- `internal/repository/exitip` — one HTTPS GET of
  `https://1.1.1.1/cdn-cgi/trace` through Hop's own loopback proxy, retried
  once with a warning and bounded to 8 s and 4 KiB; the reply's `ip=` is
  validated with `net/netip` and its `loc=` kept as an ISO 3166 alpha-2 code.
  The IP is the user's public address: keep it in memory only; never write it
  to disk, settings, logs, errors or fixtures.
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
  that way. The route bar asks for the exit IP when the resolved exit, the
  mode or the core epoch changes; one lookup runs at a time and a reply for
  an older exit is dropped. The login item itself lives in the system (`mygo.App`
  `OpenAtLogin`), not in Hop's settings; never turn it on for a dev build.
- `config` — startup settings; `main.go` wires everything. User settings
  (`service/settings.go`: appearance, delay test timeout, subscription
  interval, geodata updates, color theme and window clarity) load before the UI and are
  validated against the offered choices; the main window's theme is
  `d.windowTheme(c)`, which applies the chosen appearance, color theme and
  clarity; the flyout uses the same palette through `d.theme(c)` with opaque
  cards.
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

- Layout: the main window follows the user's finished glass design. The base is the window's own colour; the header
  and the sidebar are tonal veils, the content column carries a lighter one,
  separated by full-width and full-height hairlines, and the content reaches the
  window's bottom edge (there is no status bar). Under the header the body is
  the sidebar | one vertical hairline | content. The sidebar runs the window's
  height: the subscription in use on a card at its top (picker, the `Hop 智能规则`
  tag only while the profile runs Hop's rules, usage, expiry, refresh and
  manage), the groups, and the core's state at its foot. The content column
  is the hero card, the group's header, the region filters and one grid of
  node tiles. Node tiles and the lists, charts and tables on the other pages
  are translucent cards, as the design shows. Dialogs, picker popups, tooltips,
  toasts and the menu-bar flyout stay opaque cards.
- The hero is one card: the panel glass with the theme's own accent mixed in
  (`heroFace`, so it follows every palette and appearance; never a fixed
  colour), in the window's ink: the ring gauge with the exit's flag (outer arc
  download, inner upload, both easing to the traffic and then resting), the
  exit's name with its latency pill, the `经 …` line, its exit IP and its
  facts as chips; on the right the speeds with the outbound mode switch, over
  the sparkline with its `最近 2 分钟` caption and idle state. The right part
  wraps under the exit in a narrow window and drops the plot below
  `heroPlotMinPane`. It has no current-line caption. The flyout has the same
  card (`heroFace`) around its own smaller gauge and plot, then the three modes as pills and
  the two traffic switches as pills on the next line.
- Node tiles: the chosen tile is filled with the accent (`onAccentTheme` is
  its ink) and carries a check; there is no choice ring. The region filters
  (`regionChips`: the groups and DIRECT as 线路 first, then 全部, then one per
  region) choose what the grid shows. Until the user chooses, a group that
  has groups and DIRECT shows them (`activeFilter`), so the first view is
  short; 全部 lists every option in titled sections, the groups first, never
  one flat list. The filters sit in one row: the regions that do not fit are
  under 更多 (`splitRegionChips`); a filter left over from another group is
  that group's default.
- Type: 11 caption, 12 secondary, 13 body/controls and page tabs, 14 tile
  and section titles, 18 page headings, 22 the hero's exit, 30 the stats
  total. Weights 500/600/700 only. Numbers use `FontFeatures("tnum")`.
- Radii: chips 4, tiles 10, controls 8, cards and dialogs 12; capsules
  (buttons, switches, status) are `switchHeight` (28) tall with fully rounded
  ends.
- Glass materials live in `theme_glass.go`; the 海蓝 and 暮紫 palettes and
  static backdrop live in `theme_palette.go`. The backdrop is blurred before
  the base veil `(100-clarity)/100` and the regional veils are painted; cards
  blur what lies under them. Never blur the content or draw the backdrop over
  it. 海蓝 is the default theme, with 68% clarity. 设置 → 通用 → 主题 changes
  the palette independently of appearance and persists it. 窗口通透度 changes
  the base veil live on macOS; existing saved clarity values are preserved.
- Colours: theme tokens only, one hue family. Accent (sky blue) = route,
  download, rings, bars, selection; coloured text takes the deeper
  `accentInk`. Upload (ring inner arc, plot upload line, ↑) is a blue-slate
  `uploadColor`. Success marks only the live proxy/TUN pill and the core
  dot; Warning and Danger mark only latency tiers, genuine warnings and
  errors. Node chips and bars are neutral. Brand gradient (`brand.go`) only
  on the mark and wordmark, starting from the accent. Header switches use the
  design's 38% button face, not a separate opaque backing.
- Buttons: always `button(...)` / `iconButton(...)` from `buttons.go` with
  `primaryButton`, `mainSoftButton` or `mainDangerButton` on main pages;
  `softButton`/`dangerButton` remain for the flyout and dialogs. Never
  `ui.Button`, `ui.PrimaryButton` or `ui.Segmented`. The header's page switch
  is `pageTabs` (a translucent track with a solid light pill that slides to
  the chosen page); settings and mode
  choices are `slidingSwitch`, on/off settings `ui.Switch`. In the window, the
  system proxy and TUN are the header's `statusToggle` buttons (a green dot
  while on; every page shows them) and the outbound mode is a `slidingSwitch`
  at the foot of the hero; the flyout keeps all three as `togglePill`
  (`pill.go`). Text actions are `textButton`.
- Icons: 24-unit SVG strokes in `icons.go`, tinted by text colour.
- CJK text in small controls sits ~1px low: apply `Translate(0, -opticalLift)`.
- UI copy is Chinese, sentence-short, in the user's terms. No ALL-CAPS labels,
  no `·` joined meta strings, no decorative eyebrows.
- The mark's geometry lives once in `trayicon.go`; the menu bar icon and
  `hopMark` both draw from it.

## Motion

Motion shows change: switches and tabs slide, numbers roll, and the spinner
turns while work runs. The hero's gauge arcs ease to the traffic and then
rest. Continuous drawings
(spinners, the flyout's live pulse) must animate in `Draw` with `p.Now()` +
`p.AnimationFrame()` so they only repaint; never use `Loop`/per-frame state
that rebuilds the whole view. Springs (`ui.Snappy`) for moves, `AnimateColor`
for fills. One orchestrated entrance (the flyout), no scattered fade-ins.

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
The release workflow builds `darwin/universal` and `windows/amd64` (an NSIS installer, not code signed); `scripts/release.sh` builds only the former. Never use shared or third-party signing
certificates. A Developer ID with notarization (MyGo's
`macos.signingIdentity` and `notarize`) waits for a public release.

## Git

Small commits with messages that say why. End commit messages with the
co-author line the session's attribution instructions give. Do not push or
rewrite history without being asked. Changes made by other agents will be
reviewed later, so keep each commit focused and self-explanatory.
