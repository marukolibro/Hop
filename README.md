# Hop

一个用 [MyGo](https://github.com/egoist/mygo) 写的 macOS 原生代理客户端,内核是 [mihomo](https://github.com/MetaCubeX/mihomo)(Clash.Meta)。

> 🚧 还在开发中,界面和功能都会继续调整。

## 它做什么

Hop 在本机 `127.0.0.1` 上启动一个自己的 mihomo 内核,通过内核的 REST 接口控制它,界面全部用 MyGo 的原生 UI 绘制,没有 webview。

- **订阅**:粘贴 Clash 订阅链接导入;显示已用流量、总量和到期时间(读取订阅的 `subscription-userinfo`)
- **节点**:策略组按「主线路 / 自动选择 / 分流 / 地区」分类,节点按地区排列,显示协议、UDP、延迟,一键测速
- **线路**:顶部实时画出流量经过的组和出口节点
- **模式**:规则、全局、直连
- **连接与日志**:查看、筛选、断开连接;实时内核日志
- **菜单栏**:图标旁显示实时速度,点开是快速切换面板

### 安全边界

订阅里只取 `proxies`、`proxy-groups`、`proxy-providers`、`rules`、`rule-providers`、`sub-rules`。端口、`tun`、`dns`、`listeners`、外部控制器等一律由 Hop 决定,订阅无法打开 TUN 或接管 DNS。

Hop 目前**不会**设置系统代理。需要走 Hop 的程序,把代理设为 `127.0.0.1:17890`(HTTP 或 SOCKS5)。它可以和其他 Clash 客户端同时运行,互不影响。

## 开发

需要 Go 1.27.1 以上,以及一个 mihomo 可执行文件。

```sh
# 指向 mihomo 内核(这里借用 Clash Verge 自带的)
export HOP_MIHOMO_BINARY="/Applications/Clash Verge.app/Contents/MacOS/verge-mihomo"

go tool mygo dev     # 开发模式,保存即热重载
go tool mygo build   # 打包
```

检查与测试:

```sh
go vet ./... && go tool mygo vet .
go test ./...        # 设置了 HOP_MIHOMO_BINARY 时,会用真实内核跑订阅导入的集成测试
```

Hop 的数据在 `~/Library/Application Support/hop/`(订阅、出站模式、内核工作目录)。

## 结构

```
config/                     启动配置
internal/
  handler/                  原生 UI:页面、动效组件、菜单栏
  service/                  业务逻辑:运行时配置、视图模型、订阅、模式
  repository/
    mihomo/                 内核进程与 REST / WebSocket 客户端
    subscription/           下载订阅
    profile/                本地保存订阅与设置
  model/                    共享类型
  integration/              用真实内核的集成测试
```

## 许可证

[GPL-3.0](LICENSE)
