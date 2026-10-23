<p align="center">
  <img src="resources/icon.png" width="128" alt="Hop">
</p>

<h1 align="center">Hop</h1>

<p align="center">
  A native macOS and Windows proxy client for <a href="https://github.com/MetaCubeX/mihomo">mihomo</a> (Clash.Meta). Built in Go with <a href="https://github.com/egoist/mygo">MyGo</a>: the interface is drawn natively, with no webview.
</p>

<p align="center">
  English · <a href="README.zh-CN.md">简体中文</a>
</p>

<p align="center">
  <a href="https://github.com/marukolibro/Hop/releases/latest"><img src="https://img.shields.io/github/v/release/marukolibro/Hop" alt="Latest release"></a>
  <a href="https://github.com/marukolibro/Hop/actions/workflows/ci.yml"><img src="https://github.com/marukolibro/Hop/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/marukolibro/Hop" alt="License: GPL-3.0"></a>
  <a href="go.mod"><img src="https://img.shields.io/github/go-mod/go-version/marukolibro/Hop?logo=go&logoColor=white" alt="Go version"></a>
  <img src="https://img.shields.io/badge/platform-macOS%20%7C%20Windows-lightgrey" alt="Platforms: macOS and Windows">
  <a href="https://github.com/MetaCubeX/mihomo"><img src="https://img.shields.io/badge/core-mihomo-6f42c1" alt="Core: mihomo"></a>
  <a href="https://github.com/egoist/mygo"><img src="https://img.shields.io/badge/UI-MyGo-ff69b4" alt="UI: MyGo"></a>
</p>

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/screenshots/nodes-dark.png">
    <img src="docs/screenshots/nodes-light.png" width="860" alt="The nodes page: the route's exit, live traffic, and the nodes by region">
  </picture>
</p>

Hop is a proxy client for the [mihomo](https://github.com/MetaCubeX/mihomo)
core, one more option next to the clients that already exist. It does not
provide servers: you bring your own subscription or node links. The interface is
drawn natively in Go with [MyGo](https://github.com/egoist/mygo) instead of
running in a webview, and the same code runs on macOS and Windows.

> [!NOTE]
> Hop's interface is in Chinese for now. Everything in this README is the same
> in both languages; the labels quoted below are the ones you will see.

## Contents

[Features](#features) · [Limits](#limits) · [Install](#install) ·
[Getting started](#getting-started) · [How it works](#how-it-works) ·
[Privacy and safety](#privacy-and-safety) · [Troubleshooting](#troubleshooting) ·
[Development](#development) · [License](#license)

## Features

### Subscriptions and rules

- **Subscriptions.** Keep several, switch between them, and refresh them on a
  schedule (off, 6, 12 or 24 hours). Each shows its usage and expiry. Hop
  accepts Clash profiles and plain node-link lists (ss, vmess, vless, trojan,
  hysteria2, tuic, anytls, ...), converted locally with the core's own
  converter, never by a third-party service.
- **Local profiles.** No subscription link? Paste node links or a Clash config,
  or pick a file. Hop keeps a copy, so the profile needs no refresh and the
  original file can move or vanish. Handy for your own VPS.
- **Provider rules or Hop's smart rules.** Per subscription, keep the
  provider's rules or switch to Hop's: AI, streaming, Telegram, Google, Apple
  and Microsoft groups, region groups, and China-direct routing from bundled
  geodata that updates weekly.
- **Your own rules.** Put rules ahead of everything else: a domain suffix, a
  full domain, a domain keyword, an IP range or an application, sent direct,
  through the proxy, or rejected. From the **Connections** page, the **+**
  button on any connection opens the same dialog already filled in with the
  rule that would catch it (its site, the host, the application or the
  address), so adding a rule is two clicks.
- **The provider's nearest entry.** When a subscription sends its nodes through
  a closer entry point (its own `hosts` or encrypted DNS), Hop uses it, as other
  clients do, and takes nothing else from the subscription's DNS settings.

### Nodes and the route

- **Nodes at a glance.** Groups by purpose, nodes by region, each with its
  protocol, UDP support, line labels and latency. Test a whole group at once
  (many nodes in parallel), then switch the grid from the subscription's order
  to **latency order**, fastest first.
- **The route.** A ring gauge with the exit's flag (outer arc download, inner
  arc upload), the exit node, the groups traffic passes through, a two-minute
  trace and the live speed.
- **The exit's address.** Click to copy the public IP and country traffic
  leaves by. Hop looks it up once through its own proxy when the exit changes
  and keeps it in memory only.
- **Unlock check.** **解锁检测** tries ChatGPT, Netflix and YouTube Premium
  through every group at once, by the node each group uses now, and marks the
  ones that work on the group's tile, with the region the service saw. It goes
  through listeners of its own on the loopback, so your rules and your current
  route stay as they are. The results are kept until Hop quits.
- **Three modes.** Rule, Global and Direct, with a note on what each does.

### System integration

- **System proxy.** One switch in the header. Hop refuses when another app
  already holds the system proxy, clears only a proxy that points at itself, and
  puts it back when it quits.
- **Enhanced mode (TUN).** Carries every app's traffic through a small
  privileged helper you install once (a LaunchDaemon on macOS, a service on
  Windows), so the core runs with the rights a TUN needs and Hop itself never
  does. On macOS the helper also points the system DNS at resolvers whose
  traffic enters the TUN and restores the previous DNS when the core stops.
  Hop refuses to turn TUN on while another TUN holds the default routes.
- **Menu bar and tray.** On macOS the menu bar shows the live speed beside the
  icon (optional); on Windows the same panel opens from the tray. It switches
  routes, nodes, modes and the proxy without opening the window.
- **Starts the way you want.** Close the window and Hop keeps running in the
  menu bar or tray; start it at login; copy a ready-made command that points a
  terminal at Hop.

### Insight

- **Connections.** Every connection the core carries, with its application,
  the rule that matched and the node it leaves by. Filter by host, application,
  rule or route; close one or all; add a rule.
- **Logs.** The core's log, live, with a level filter.
- **Statistics.** Today, this month and the last 30 days, split into proxied and
  direct traffic, and this month per subscription.

### Look and feel

- **Glass.** A frosted window with a calm sea-blue (or dusk-purple) backdrop on
  macOS and Windows 11 (Mica, Acrylic for the panel); a solid window with the
  same layout on Windows 10. Light, dark or follow the system, independently of
  the colour theme. On macOS a slider sets how translucent the window is.
- **Calm motion.** Motion shows what changes: a switch slides, a number rolls,
  a spinner turns while work runs.

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/screenshots/stats-dark.png">
    <img src="docs/screenshots/stats-light.png" width="620" alt="The statistics page">
  </picture>
</p>

<p align="center">
  <img src="docs/screenshots/connections-light.png" width="420" alt="The connections page">
  <img src="docs/screenshots/dusk-dark.png" width="420" alt="The dusk theme in dark appearance">
</p>

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/screenshots/menubar-dark.png">
    <img src="docs/screenshots/menubar-light.png" width="240" alt="The menu bar panel">
  </picture>
  &nbsp;
  <img src="docs/screenshots/add-rule-light.png" width="560" alt="Adding a rule from a connection">
</p>

### Updates

- **Hop updates itself** from GitHub releases. Every update is signed, and the
  app checks the signature before it installs one; when it can, it downloads
  only what changed since your version.
- **The core upgrades from Settings**, with its download checked against the
  digest GitHub publishes, and rolls back by itself if the new core does not
  come up. The bundled core can always be restored.
- **Geodata** (the rules' databases) refreshes weekly, or on demand.

## Limits

Hop is young and deliberately small. The interface is in Chinese only. There is
no Linux build. It has no profile scripts or merge files, so how far you can
reshape a provider's configuration is limited by design. The macOS app is ad
hoc signed and the Windows installer is not code signed, so the first run needs
a confirmation (see [Install](#install)).

## Install

| Platform | Runs on | How |
| --- | --- | --- |
| macOS | Apple silicon and Intel (one universal app) | the install script below |
| Windows | x64, Windows 10 and 11 (glass needs Windows 11 22H2 or later) | `Hop Setup <version> amd64.exe` from the [latest release](https://github.com/marukolibro/Hop/releases/latest) |

Linux is not supported.

### macOS

```sh
curl -fsSL https://raw.githubusercontent.com/marukolibro/Hop/main/install.sh | sh
```

The script installs `Hop.app` into `/Applications` (or `~/Applications`)
without `sudo`, checks the download's size, version and code signature, and
opens it. Running it again installs a newer version. Builds are ad hoc signed
for now, so installing through the script rather than a downloaded `.dmg` is
what lets macOS open the app.

### Windows

Download `Hop Setup <version> amd64.exe` from the
[latest release](https://github.com/marukolibro/Hop/releases/latest) and run it.
The installer is not code signed yet, so Windows SmartScreen asks you to
confirm the first run: choose **More info → Run anyway**.

From then on Hop keeps itself up to date on both platforms.

## Getting started

1. **Add a subscription.** Open the nodes page, choose the subscription bar and
   add one: paste your provider's link, or choose **本地配置** (local config) and
   paste node links or a Clash config.
2. **Pick the rules.** Keep the provider's rules, or use Hop's smart rules.
3. **Choose a route.** Pick a group in the sidebar, test it, and click a node
   (or switch the grid to latency order).
4. **Turn something on.** The system proxy switch in the header points your
   apps at Hop. Enhanced mode asks to install the helper the first time.
5. **Adjust.** Add your own rules from **Settings** or from any connection,
   choose the theme and appearance, and decide whether Hop starts at login.

Apps that ignore the system proxy can be pointed at the mixed port
`127.0.0.1:17890` (HTTP and SOCKS5), or use **Settings → 终端代理** to copy a
ready-made terminal command.

## How it works

```
 Hop (MyGo native UI) ── REST / WebSocket ──▶ mihomo core (loopback)
        │                                          ▲
        └── privileged helper (TUN only) ──────────┘ runs the core with TUN
```

- Hop starts its own mihomo as a sidecar process, supervised and restarted on a
  crash with backoff, and drives it through the controller.
- It generates the core's configuration itself. From a subscription it takes
  only proxies, groups and rules (a whitelist); ports, DNS, TUN and the
  controller are always Hop's own.
- With TUN on, the helper runs the core instead, and Hop sends its
  configuration to that core.

| What | Where |
| --- | --- |
| Mixed (HTTP and SOCKS5) proxy | `127.0.0.1:17890` |
| Core controller | `127.0.0.1:19090` |
| Core DNS, only for subscriptions that route node names through it | `127.0.0.1:17853` |
| The unlock check's own listeners, each sent through the group under test | `127.0.0.1:17900`–`17907` |
| Data, mode 0600 | `~/Library/Application Support/hop/` on macOS, `%AppData%\hop` on Windows |

The ports sit away from the ones other Clash clients use.

## Privacy and safety

- Your subscription link is a credential: Hop never logs it or sends it
  anywhere but its own host. A local profile's content, which carries node
  passwords, is treated the same way.
- Links are converted on your machine; no conversion service ever sees them.
- Hop reads nothing from, and writes nothing to, another Clash client's files
  or processes. It does not change DNS or routes, apart from TUN through its
  own helper.
- The system proxy and TUN are off by default, change only when you turn them
  on, and are put back when Hop quits.
- The exit's address is looked up with one request to Cloudflare
  (`1.1.1.1/cdn-cgi/trace`) through Hop's own proxy, kept in memory, and never
  written to disk or a log.
- All data stays on your machine, in the directory above.

## Troubleshooting

- **macOS says the app is damaged or from an unidentified developer.** Install
  with the script instead of opening a downloaded `.dmg`.
- **Windows SmartScreen blocks the installer.** It is not code signed yet:
  **More info → Run anyway**.
- **Enhanced mode will not turn on.** Another VPN or Clash client holds the
  default routes; turn its TUN off first. Hop never takes routes over.
- **Another client uses the same ports.** Hop's own are `17890`, `19090`,
  `17853` and `17900`–`17907`, apart from other clients', so both can run at once.
- **Latency looks high.** Hop uses the provider's nearest entry when the
  subscription names one; re-test after switching subscription or node.

## Development

Requires Go 1.27.2 or later.

```sh
scripts/fetch-mihomo.sh darwin-arm64   # bundled core and geodata, pinned and hash-checked
scripts/build-helper.sh darwin-arm64   # the TUN helper

go tool mygo dev                       # run with live reload
go vet ./... && go tool mygo vet .
HOP_MIHOMO_BINARY="$PWD/resources/darwin-arm64/bin/mihomo" go test ./...
```

On Windows use `windows-amd64` for both scripts (from Git Bash) and the core's
`mihomo.exe`.

- **A dev build runs beside an installed Hop.** `go tool mygo dev` is its own
  instance: ports `17892`, `19092` and `17855`, its own data directory
  `hop-dev`, no subscription to begin with, and no access to the privileged
  helper, which is one per machine and belongs to the installed Hop.
- `HOP_MIHOMO_BINARY` points Hop at another core while developing; with it set,
  the integration tests run a real core on ports 17891 and 19091.
- Build for another platform with `go tool mygo build -platform windows/amd64`.

```
main.go               wires everything
config/               instances (release and dev): ports, data directory, core choice
cmd/hop-helper/       the privileged TUN helper
internal/handler/     the native UI
internal/service/     business logic: views, rules, subscriptions, TUN, updates
internal/repository/  the core process and client, profiles, the helper's client,
                      system proxy, routes, subscriptions, geodata
internal/model/       shared types
scripts/              fetch the core, build the helper, release, diagnose TUN
```

[AGENTS.md](AGENTS.md) describes the architecture and the rules changes follow.

### Releasing

Bump `version` in `mygo.json`, add a `## <version>` section to
`CHANGELOG.md`, commit, and push a tag:

```sh
git tag v0.3.0 && git push origin v0.3.0
```

The [release workflow](.github/workflows/release.yml) builds and signs the
macOS app and the Windows installer and uploads them, with the update manifests,
to a draft release. Review the draft, then publish it; installed copies update
to it. `scripts/release.sh` builds the macOS release from a Mac.

## License

Hop is licensed under [GPL-3.0](LICENSE). It bundles the mihomo core
(GPL-3.0, license shipped beside the binary) and geodata from
[MetaCubeX/meta-rules-dat](https://github.com/MetaCubeX/meta-rules-dat).
