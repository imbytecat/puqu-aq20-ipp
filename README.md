# PUQU AQ20 IPP Bridge

把 PUQU AQ20（以及协议兼容的 PQ/TQ/Q 系列）蓝牙标签机映射为局域网标准打印机。

一个 Go 守护进程负责 BLE、IPP、DNS-SD、作业队列和 SQLite 持久化；内嵌 React 页面只负责本机配置。Windows、macOS、Linux 和可选的 iOS AirPrint 客户端都通过系统打印接口提交作业，不接触蓝牙。

```text
系统打印对话框 ── IPP/PWG Raster/JPEG ──▶ puqu-ipp ── BLE ──▶ PUQU AQ20
                    DNS-SD 发现             │
                                             └── 127.0.0.1:8080 配置页
```

## 功能

- IPP 1.1/2.0：`Print-Job`、`Validate-Job`、`Create-Job`、`Send-Document`、`Cancel-Job`、作业查询和取消。
- 驱动免安装输入：PWG Raster、JPEG；启用 AirPrint 后额外接受 Apple Raster (`image/urf`)。
- DNS-SD `_ipp._tcp,_print` 广播；AirPrint 模式额外广播 `_universal` 子类型和 URF 能力。
- 固定 203 dpi、单色、单面标签；活动标签 profile 决定准确介质尺寸和 PUQU 浓度/速度/纸张类型。
- SQLite 保存打印机、profile、设置和作业历史；服务重启时安全中止状态不确定的在途作业。
- 单活动 BLE 连接、自动重连、串行打印队列和取消传播。
- 单二进制部署；配置页通过 `//go:embed` 内嵌。

## 前置条件

| 平台 | BLE 后端 | 前置条件 |
|---|---|---|
| Linux | BlueZ / D-Bus | `bluetoothd` 正在运行，服务账号能访问系统 D-Bus |
| macOS | CoreBluetooth | 给二进制或承载它的服务授予蓝牙权限 |
| Windows | WinRT | 使用系统蓝牙栈；通常无需额外驱动 |

开发工具由 [mise](https://mise.jdx.dev/) 固定：Go 1.26、Node 24、pnpm 10。

## 快速开始

```bash
mise install
mise run setup
mise run build
./bin/puqu-ipp
```

默认监听：

- IPP：`0.0.0.0:8631`，打印 URI 为 `ipp://HOST:8631/ipp/print`
- 本机配置页：<http://127.0.0.1:8080>
- 数据库：操作系统用户配置目录中的 `puqu-aq20-ipp/puqu.db`

首次启动后：

1. 打开配置页。
2. 扫描蓝牙设备，选择 AQ20。
3. 新建或激活与实体标签完全一致的宽度、高度、间隙和纸张类型 profile。
4. 保持 DNS-SD 广播开启；iPhone/iPad 需要同时开启 AirPrint compatibility。
5. 用“Print test label”确认 BLE 和耗材设置。

自定义数据库路径：

```bash
./bin/puqu-ipp --data /path/to/puqu.db
```

## 添加系统打印机

### Linux / CUPS

DNS-SD 正常时桌面设置会自动显示 `PUQU AQ20 (…)`。也可手动添加：

```bash
lpadmin -p PUQU_AQ20 -E \
  -v ipp://HOST:8631/ipp/print \
  -m everywhere
```

### macOS

在“系统设置 → 打印机与扫描仪 → 添加打印机”中选择发现到的 `PUQU AQ20 (…)`。若未发现，使用 IP 地址页签和 `ipp://HOST:8631/ipp/print`。

### Windows 11

在“设置 → 蓝牙和设备 → 打印机和扫描仪 → 添加设备”中选择发现到的打印机；也可用手动添加并填写 `http://HOST:8631/ipp/print`。Windows 使用系统 IPP Class Driver。

### iPhone / iPad

在配置页启用 AirPrint compatibility。设备和服务器必须位于同一可互通 mDNS 的局域网；随后从系统分享菜单选择“打印”。

## 后台服务

内置服务管理使用各平台原生机制（systemd/launchd/Windows Service Manager）：

```bash
sudo ./bin/puqu-ipp service install
sudo ./bin/puqu-ipp service start
./bin/puqu-ipp service status
sudo ./bin/puqu-ipp service restart
sudo ./bin/puqu-ipp service stop
sudo ./bin/puqu-ipp service uninstall
```

安装时可固定数据路径：

```bash
sudo ./bin/puqu-ipp --data /var/lib/puqu-ipp/puqu.db service install
```

服务账号必须拥有蓝牙权限和数据库目录权限。macOS/Windows 首次蓝牙授权可能要求交互式运行一次二进制。

## CLI

```bash
./bin/puqu-ipp serve       # 默认命令：IPP + DNS-SD + 本机配置页
./bin/puqu-ipp discover    # 扫描并显示 BLE/GATT 信息
./bin/puqu-ipp print       # 使用已保存设备/profile 打印测试标签
./bin/puqu-ipp smoke       # 检查本机蓝牙栈访问
./bin/puqu-ipp service …   # 管理后台服务
```

## 开发

```bash
mise run dev              # Go :8080/:8631 + Vite :5173
mise run test             # 全部 Go 测试，无需硬件
mise run web:typecheck    # TypeScript
mise run vet              # go vet
mise run ci               # 生产构建 + 测试 + vet + 前端类型检查
mise run sqlc             # 查询变更后重新生成 internal/store/sqlitedb
```

测试覆盖 PUQU 字节帧、BLE UUID/标志、打印取消与自动重连、SQLite 持久化、PWG/Apple Raster/JPEG 解码、IPP 端到端提交和管理路由。

## 协议与限制

- PUQU 输出是设备自有光栅协议，不是 TSPL：8 字节打印头后跟 1bpp、MSB-first 位图。
- 1 点 = 1/203 英寸，约等于 8 点/mm。
- 输入页面尺寸必须与活动 profile 匹配；不缩放、不裁切，避免标签内容被静默改变。
- 单次 IPP 文档上限 16 MiB；内存队列容量 32。
- 当前不提供 TLS、认证、彩色、双面、PDF 或任意纸张缩放。仅在可信局域网暴露 `:8631`。
- AirPrint 默认关闭；开启后发布并接受 `image/urf`，但真实 iOS、macOS、Windows 硬件联调仍应在发布环境完成。

官方 IPP Everywhere v1.1 工具已验证：DNS-SD 非 TLS 项 100%，IPP I-1 至 I-13.2 全部通过；依赖实体打印完成的后续用例需要已连接 AQ20。

## 目录

```text
cmd/puqu-ipp/          CLI、守护进程和系统服务管理
internal/admin/        仅本机 React 配置接口
internal/ble/          tinygo 原生 BLE 适配
internal/ipp/          IPP、队列、属性和 DNS-SD
internal/printer/      PUQU 打印流程、取消、自动重连
internal/puqu/         纯 PUQU 线协议
internal/raster/       PWG Raster、Apple Raster、JPEG → 1bpp
internal/store/        SQLite、goose 迁移、sqlc 查询
internal/web/          构建标签控制的嵌入式 SPA
web/                   React 配置页
.github/workflows/     三平台原生 release 构建
```
