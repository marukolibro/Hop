<p align="center">
  <img src="resources/icon.png" width="128" alt="Hop">
</p>

<h1 align="center">Hop</h1>

<p align="center">
  用 Go 写的 macOS 原生 <a href="https://github.com/MetaCubeX/mihomo">mihomo</a>(Clash.Meta)客户端。没有 webview,也没有 Electron。
</p>

<p align="center">
  <a href="README.md">English</a> · 简体中文
</p>

<p align="center">
  <a href="https://github.com/marukolibro/Hop/actions/workflows/ci.yml"><img src="https://github.com/marukolibro/Hop/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/marukolibro/Hop" alt="License: GPL-3.0"></a>
</p>

> [!NOTE]
> Hop 还在早期开发中,尚未发布正式版本,界面和功能都会继续调整。

<p align="center">
  <img src="docs/screenshots/nodes-light.png" width="720" alt="节点页">
</p>

## 为什么做 Hop

Hop 在 `127.0.0.1` 上运行自己的 mihomo 内核,通过内核的 REST 接口控制它。界面全部由
[MyGo](https://github.com/egoist/mygo) 原生绘制,启动快、占用小,用起来就是一个 Mac 应用。
它可以和另一个 Clash 客户端同时运行,不碰对方的设置、端口和文件。

## 功能

- **订阅**:保存多份订阅,随时切换,按间隔自动更新,显示用量和到期时间。支持 Clash 订阅,也支持节点链接列表(ss、vmess、vless、trojan、hysteria2、tuic、anytls 等),链接在本机用内核同款转换器解析,不经过第三方转换服务。
- **智能分流**:每份订阅可选「机场规则」或「Hop 智能规则」。Hop 智能规则自动分出 AI、流媒体、Telegram、谷歌、苹果、微软分组和各地区组,国内直连、境外走代理,规则数据随应用自带并每周更新。还能在最前面加自己的规则。
- **节点一目了然**:策略组按用途分类,节点按地区排列,显示协议、UDP 和延迟;线路图实时画出流量经过的路径。
- **系统代理与增强模式**:可以打开系统代理,也可以打开增强模式(TUN)接管所有应用的流量,由一次安装的增强组件以管理员身份运行内核。其他应用已经占用系统代理或路由时,Hop 会拒绝开启,不会覆盖。
- **菜单栏**:图标旁显示实时网速,点开面板就能切换线路、模式和代理,不用打开主窗口。
- **连接、日志与统计**:查看和断开连接,实时内核日志,按天统计流量并区分代理和直连。
- **安静地待在后台**:关掉窗口后留在菜单栏,可开机启动,支持浅色和深色。
- **自动更新**:从 GitHub 发布获取签名更新;内核也能在设置里一键升级,新内核启动失败会自动回退。

<p align="center">
  <img src="docs/screenshots/menubar-dark.png" width="240" alt="菜单栏面板">
  &nbsp;
  <img src="docs/screenshots/stats-light.png" width="460" alt="统计页">
</p>

## 安装

Apple 芯片和 Intel 的 Mac 都能用(同一个通用安装包)。发布第一个版本后,运行:

```sh
curl -fsSL https://raw.githubusercontent.com/marukolibro/Hop/main/install.sh | sh
```

脚本把 `Hop.app` 装到 `/Applications`(没有写权限时装到 `~/Applications`),不需要 `sudo`。之后 Hop 会自己保持最新。目前的安装包是本机临时签名(ad hoc),用这个脚本安装,而不是下载 `.dmg`,macOS 才能正常打开它。

## 隐私与安全

- 订阅链接等同于密码:Hop 不会把它写进日志,也只发给订阅自己的服务器。
- 订阅里只取节点、策略组和规则;端口、DNS、TUN 和控制接口始终由 Hop 决定。
- 系统代理和增强模式默认关闭,只在你打开时才改动,退出 Hop 时还原。
- 所有数据只保存在本机的 `~/Library/Application Support/hop/`。

## 开发

需要 Go 1.27.2 或更高版本。

```sh
scripts/fetch-mihomo.sh darwin-arm64   # 下载随应用自带的内核和规则数据(固定版本并校验哈希)
scripts/build-helper.sh darwin-arm64   # 构建增强组件

go tool mygo dev                       # 开发模式,保存即热重载
go vet ./... && go tool mygo vet .
HOP_MIHOMO_BINARY="$PWD/resources/darwin-arm64/bin/mihomo" go test ./...
```

`HOP_MIHOMO_BINARY` 可以让开发时改用别的内核;设置后,集成测试会在 17891 和 19091 端口启动真实内核。架构和改代码要遵守的规则见 [AGENTS.md](AGENTS.md)。

### 发布

改 `mygo.json` 里的 `version`,在 `CHANGELOG.md` 里加 `## <版本>` 一节,提交后推送标签:

```sh
git tag v0.1.0 && git push origin v0.1.0
```

[发布流程](.github/workflows/release.yml)会编译、签名,并上传成草稿发布。检查草稿后点发布,已安装的 Hop 就会更新过去。在 Mac 上用 `scripts/release.sh` 也能完成同样的事。

## 许可证

Hop 以 [GPL-3.0](LICENSE) 发布。随应用分发的 mihomo 内核同为 GPL-3.0(许可证随内核一起附带),规则数据来自 [MetaCubeX/meta-rules-dat](https://github.com/MetaCubeX/meta-rules-dat)。
