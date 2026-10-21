# Hop

一个用 [MyGo](https://github.com/egoist/mygo) 写的 macOS 原生代理客户端,内核是 [mihomo](https://github.com/MetaCubeX/mihomo)(Clash.Meta)。

> 🚧 还在开发中,界面和功能都会继续调整。

## 它做什么

Hop 在本机 `127.0.0.1` 上启动一个自己的 mihomo 内核,通过内核的 REST 接口控制它,界面全部用 MyGo 的原生 UI 绘制,没有 webview。

- **订阅**:保存多份订阅,独立切换、刷新、改名和删除;显示各自的用量、到期时间、最近成功更新和刷新错误。可按 6、12、24 小时自动刷新全部订阅,失败后等下一个间隔再试。既支持 Clash 订阅,也支持 V2Ray 式节点列表(Base64 或明文的 ss/ssr/vmess/vless/trojan/hysteria/hysteria2/tuic/anytls 链接),链接在本机由内核同款转换器解析,不经过第三方转换服务
- **分流规则**:每份订阅可选「机场规则」或「Hop 智能规则」。Hop 智能规则只取订阅的节点,自动分出节点选择、自动选择、各地区组和 AI、流媒体、Telegram、谷歌、苹果、微软分组;被墙和境外网站走代理,国内网站直连,名单里没有的按 IP 所在地判断。规则全部来自 Hop 的 geo 数据;节点列表没有自带规则,固定使用 Hop 智能规则。可以添加自己的规则(域名后缀、完整域名、域名关键词、IP 段、进程名),排在订阅规则之前;局域网直连始终最先
- **规则数据**:GeoSite 与 GeoIP 数据每周自动更新,也可以在设置里手动更新;新文件先校验 SHA-256 并用内核的 `-t` 检查,通过后才替换并重启内核
- **节点**:策略组按「主线路 / 自动选择 / 分流 / 地区」分类,节点按地区排列,显示协议、UDP、延迟,一键测速
- **线路**:顶部实时画出流量经过的组和出口节点
- **模式**:规则、全局、直连
- **连接与日志**:查看、筛选、断开连接;实时内核日志
- **菜单栏**:图标旁显示实时速度,点开是快速切换面板
- **后台常驻**:关掉窗口后 Hop 留在菜单栏继续运行,Dock 图标一起隐藏;可在设置里开启开机启动,登录后只在菜单栏运行。
- **增强模式**(TUN):接管全部流量,不只听代理设置的应用。由一个随 Hop 安装的增强组件(LaunchDaemon)以管理员运行内核实现;安装需要一次管理员密码,可随时在设置里卸载。其他应用的 TUN 占用路由时,Hop 拒绝开启
- **设置**:开机启动、外观(跟随系统、浅色、深色)、测速超时(2、3、5 秒)、订阅自动更新间隔(关闭、6、12、24 小时)、规则数据更新、自定义规则和应用版本,保存在本地。出站模式和系统代理在节点页的线路卡片底部。
- **内核**(在设置页):显示 mihomo 的版本和来源;一键检查并升级内核,新内核通过检验才启用,失败自动回到原版本,也能恢复为内置版本。内核意外退出会自动重启并在界面提示,反复退出才停下
- **应用更新**:启动后自动检查,之后每天一次;设置页也能手动检查。版本和说明来自 GitHub 最新发布,下载后由 mygo 用内置公钥验证更新归档的 Ed25519 签名与大小,通过才替换当前应用并重启;开发版和未打包的运行不自动更新

在节点页点「管理订阅」添加地址。第一份会立即使用,之后添加不会改变当前线路;节点页的订阅选择器可直接切换已保存的配置,不必重新下载。每次只使用一份订阅,不合并不同订阅的节点、策略组或规则。

切换完成后不必等测速结束即可继续操作;测速在后台进行,再次切换订阅、节点或出站模式时会取消旧的测速任务,过期结果不会覆盖当前界面。

「全部刷新」逐份保存结果,一份失败不影响其他订阅;失败的那份保留上次成功内容。删除使用中的订阅必须明确选择接替者,删除最后一份需确认回到未配置状态。重启使用上次保存的当前订阅,旧版单订阅数据会自动迁移。

### 安全边界

订阅里只取 `proxies`、`proxy-groups`、`proxy-providers`、`rules`、`rule-providers`、`sub-rules`。端口、`tun`、`dns`、`listeners`、外部控制器等一律由 Hop 决定,订阅无法打开 TUN 或接管 DNS。

系统代理默认关闭,在节点页线路卡片底部或菜单栏面板里打开。打开时,如果系统代理已被其他应用(比如另一个 Clash 客户端)占用,Hop 会拒绝,不会覆盖;退出 Hop 或内核反复崩溃停止重启时,Hop 只清除指向自己的系统代理。不开系统代理时,也可以把需要走 Hop 的程序的代理设为 `127.0.0.1:17890`(HTTP 或 SOCKS5)。注意:Hop 被强制结束时来不及清除,系统代理会留在 17890,重新打开 Hop 或在系统设置里关掉即可。

增强模式(TUN)默认关闭。打开时 Hop 先检查默认路由:其他应用的 TUN(比如另一个 Clash 客户端的)占着,就拒绝开启并在按钮旁说明。第一次开启会安装 Hop 的增强组件(`/Library/PrivilegedHelperTools` 里的 `hop-helper` 与一个 LaunchDaemon),需要一次管理员密码;组件只听 Hop 自己的 unix socket,并核对连接者的身份。TUN 开着时,内核由组件以管理员运行(设备名 `utun199`,局域网地址段不走 TUN),Hop 自己的内核停下;关掉增强模式或退出 Hop,组件里的内核随之停止,路由自动交还。设置页的「内核」分组里可以查看并卸载增强组件。

## 安装

Apple 芯片(M1 及以后)的 Mac 用下面的命令安装最新的发布版,装到 `/Applications`(当前用户可写时)或 `~/Applications`:

```sh
curl -fsSL https://raw.githubusercontent.com/marukolibro/Hop/main/install.sh | sh
```

脚本校验归档大小、安装包的版本和代码签名,以当前用户身份安装(不需要管理员密码),只清除装好的 `Hop.app` 上的隔离属性。装好后由 Hop 自己检查并安装后续更新,更新归档的签名由应用内置的公钥校验;再次运行脚本也能更新到最新版,已经是最新或更新时不会降级。Intel Mac 尚无安装包。

## 发布

发布版本的版本号写在 `mygo.json`,更新说明写在 `CHANGELOG.md` 的 `## <版本>` 一节。发布脚本用更新私钥签名归档和更新清单:

```sh
scripts/release.sh --dry-run   # 构建 arm64 版并列出产物,不上传
scripts/release.sh --publish   # 构建并上传为草稿发布
gh release edit v<版本> --draft=false   # 检查草稿后发布,应用从此能看到更新
```

`--publish` 只创建草稿,不会自动公开;先在 GitHub 上检查发布说明和产物,再执行它打印的发布命令。

## 开发

需要 Go 1.27.1 以上。内核 mihomo、它需要的 geo 数据(免得首次启动还要联网下载)和内核的许可证文件都随 Hop 一起带,固定在 `mihomo.version`,首次开发前下载一次(会校验 SHA-256):

```sh
scripts/fetch-mihomo.sh darwin-arm64    # Windows 用 windows-amd64
scripts/build-helper.sh darwin-arm64    # 构建增强组件(增强模式需要)

go tool mygo dev     # 开发模式,保存即热重载
go tool mygo build   # 打包(发布签名需要更新私钥)
```

想用别的内核开发,把 `HOP_MIHOMO_BINARY` 指向它即可。

检查与测试:

```sh
go vet ./... && go tool mygo vet .
go test ./...        # 设置了 HOP_MIHOMO_BINARY 时,会用真实内核跑订阅导入的集成测试
```

Hop 的数据在 `~/Library/Application Support/hop/`(订阅、出站模式、设置、自定义规则、geo 数据、内核工作目录)。

## 结构

```
config/                     启动配置
scripts/                    获取内核、构建增强组件、发布
install.sh                  安装最新发布版
internal/
  handler/                  原生 UI:页面、动效组件、菜单栏
  service/                  业务逻辑:运行时配置、视图模型、订阅、模式
  repository/
    mihomo/                 内核进程、升级时的替换与 REST / WebSocket 客户端
    corerelease/           从 GitHub 查找并下载 mihomo 版本(校验 SHA-256)
    corestore/              保存升级下载的内核
    geodata/                从 meta-rules-dat 下载 geo 数据(校验 SHA-256)
    subscription/           下载订阅
    profile/                本地保存订阅与设置
  model/                    共享类型
  integration/              用真实内核的集成测试
```

## 许可证

Hop 本体是 [GPL-3.0](LICENSE)。

随应用分发的还有:mihomo 内核,采用其源码树所带的许可证(v1.19.32 为 GPL-3.0,原文随内核放在 `resources/<平台>/bin/LICENSE`),以及来自 [MetaCubeX/meta-rules-dat](https://github.com/MetaCubeX/meta-rules-dat) 的 geo 数据。
