# Hop

一个用 [MyGo](https://github.com/egoist/mygo) 写的 macOS 原生代理客户端,内核是 [mihomo](https://github.com/MetaCubeX/mihomo)(Clash.Meta)。

> 🚧 还在开发中,界面和功能都会继续调整。

## 它做什么

Hop 在本机 `127.0.0.1` 上启动一个自己的 mihomo 内核,通过内核的 REST 接口控制它,界面全部用 MyGo 的原生 UI 绘制,没有 webview。

- **订阅**:保存多份 Clash 订阅,独立切换、刷新、改名和删除;显示各自的用量、到期时间、最近成功更新和刷新错误
- **节点**:策略组按「主线路 / 自动选择 / 分流 / 地区」分类,节点按地区排列,显示协议、UDP、延迟,一键测速
- **线路**:顶部实时画出流量经过的组和出口节点
- **模式**:规则、全局、直连
- **连接与日志**:查看、筛选、断开连接;实时内核日志
- **菜单栏**:图标旁显示实时速度,点开是快速切换面板
- **后台常驻**:关掉窗口后 Hop 留在菜单栏继续运行,Dock 图标一起隐藏;可在设置里开启开机启动,登录后只在菜单栏运行。
- **增强模式**(TUN):接管全部流量,不只听代理设置的应用。由一个随 Hop 安装的特权助手(LaunchDaemon)以管理员运行内核实现;安装需要一次管理员密码,可随时在设置里卸载。其他应用的 TUN 占用路由时,Hop 拒绝开启
- **设置**:开机启动、外观(跟随系统、浅色、深色)、测速超时(2、3、5 秒),保存在本地。出站模式和系统代理在节点页的线路卡片底部。
- **内核**(在设置页):显示 mihomo 的版本和来源;一键检查并升级内核,新内核通过检验才启用,失败自动回到原版本,也能恢复为内置版本。内核意外退出会自动重启并在界面提示,反复退出才停下

在节点页点「管理订阅」添加地址。第一份会立即使用,之后添加不会改变当前线路;节点页的订阅选择器可直接切换已保存的配置,不必重新下载。每次只使用一份订阅,不合并不同订阅的节点、策略组或规则。

切换完成后不必等测速结束即可继续操作;测速在后台进行,再次切换订阅、节点或出站模式时会取消旧的测速任务,过期结果不会覆盖当前界面。

「全部刷新」逐份保存结果,一份失败不影响其他订阅;失败的那份保留上次成功内容。删除使用中的订阅必须明确选择接替者,删除最后一份需确认回到未配置状态。重启使用上次保存的当前订阅,旧版单订阅数据会自动迁移。

### 安全边界

订阅里只取 `proxies`、`proxy-groups`、`proxy-providers`、`rules`、`rule-providers`、`sub-rules`。端口、`tun`、`dns`、`listeners`、外部控制器等一律由 Hop 决定,订阅无法打开 TUN 或接管 DNS。

系统代理默认关闭,在节点页线路卡片底部或菜单栏面板里打开。打开时,如果系统代理已被其他应用(比如另一个 Clash 客户端)占用,Hop 会拒绝,不会覆盖;退出 Hop 或内核反复崩溃停止重启时,Hop 只清除指向自己的系统代理。不开系统代理时,也可以把需要走 Hop 的程序的代理设为 `127.0.0.1:17890`(HTTP 或 SOCKS5)。注意:Hop 被强制结束时来不及清除,系统代理会留在 17890,重新打开 Hop 或在系统设置里关掉即可。

增强模式(TUN)默认关闭。打开时 Hop 先检查默认路由:其他应用的 TUN(比如另一个 Clash 客户端的)占着,就拒绝开启并在按钮旁说明。第一次开启会安装 Hop 的特权助手(`/Library/PrivilegedHelperTools` 里的 `hop-helper` 与一个 LaunchDaemon),需要一次管理员密码;助手只听 Hop 自己的 unix socket,并核对连接者的身份。TUN 开着时,内核由助手以管理员运行(设备名 `utun199`,局域网地址段不走 TUN),Hop 自己的内核停下;关掉增强模式或退出 Hop,助手里的内核随之停止,路由自动交还。设置页的「内核」分组里可以查看并卸载助手。

## 开发

需要 Go 1.27.1 以上。内核 mihomo、它需要的 geo 数据(免得首次启动还要联网下载)和内核的许可证文件都随 Hop 一起带,固定在 `mihomo.version`,首次开发前下载一次(会校验 SHA-256):

```sh
scripts/fetch-mihomo.sh darwin-arm64    # Windows 用 windows-amd64
scripts/build-helper.sh darwin-arm64    # 构建特权助手(增强模式需要)

go tool mygo dev     # 开发模式,保存即热重载
go tool mygo build   # 打包
```

想用别的内核开发,把 `HOP_MIHOMO_BINARY` 指向它即可。

检查与测试:

```sh
go vet ./... && go tool mygo vet .
go test ./...        # 设置了 HOP_MIHOMO_BINARY 时,会用真实内核跑订阅导入的集成测试
```

Hop 的数据在 `~/Library/Application Support/hop/`(订阅、出站模式、设置、内核工作目录)。

## 结构

```
config/                     启动配置
internal/
  handler/                  原生 UI:页面、动效组件、菜单栏
  service/                  业务逻辑:运行时配置、视图模型、订阅、模式
  repository/
    mihomo/                 内核进程、升级时的替换与 REST / WebSocket 客户端
    corerelease/            从 GitHub 查找并下载 mihomo 版本(校验 SHA-256)
    corestore/              保存升级下载的内核
    subscription/           下载订阅
    profile/                本地保存订阅与设置
  model/                    共享类型
  integration/              用真实内核的集成测试
```

## 许可证

Hop 本体是 [GPL-3.0](LICENSE)。

随应用分发的还有:mihomo 内核,采用其源码树所带的许可证(v1.19.32 为 GPL-3.0,原文随内核放在 `resources/<平台>/bin/LICENSE`),以及来自 [MetaCubeX/meta-rules-dat](https://github.com/MetaCubeX/meta-rules-dat) 的 geo 数据。
