# PUQU AQ20 IPP Bridge

把 PUQU AQ20（以及协议兼容的 PQ/TQ/Q 系列）蓝牙标签机映射为局域网标准打印机。

一个 Go 守护进程可管理多台物理设备，并为每台已配置打印机发布独立 IPP 队列。Windows、macOS、Linux 和可选的 iOS AirPrint 客户端通过系统打印接口提交作业，不直接接触蓝牙。

```text
系统打印对话框 ── IPP/PWG Raster/JPEG ──▶ puqu-ipp ── BLE ──▶ 多台 PUQU 打印机
                    DNS-SD 发现             │
                                             ├── 每台打印机独立队列
                                             └── 127.0.0.1:8080 管理后台
```

## 功能

- 多打印机：每个逻辑打印机选择驱动、BLE 设备和标签规格，拥有稳定 UUID、队列路径和独立作业队列。
- 通用驱动模型：当前内置 `puqu-aq20`，覆盖 AQ20/PQ/TQ/Q 协议兼容设备；可继续增加驱动实现。
- IPP 1.1/2.0：`Print-Job`、`Validate-Job`、`Create-Job`、`Send-Document`、取消和作业查询。
- 驱动免安装输入：PWG Raster、JPEG；单台打印机启用 AirPrint 后额外接受 Apple Raster (`image/urf`)。
- 每台打印机独立 DNS-SD `_ipp._tcp,_print` 广播和 `/ipp/<queue>` 地址。
- 固定 203 dpi、单色、单面标签；打印机选择的标签规格决定精确介质尺寸及浓度、速度、纸张类型。
- SQLite 保存打印机、物理设备、标签规格和作业历史；服务重启时中止状态不确定的在途作业。
- 多个 BLE 连接可并存；连接扫描由系统适配器串行协调，不同打印机可并行处理作业。
- React 19 + TanStack Router/Query + Tailwind CSS v4 管理后台；生产构建内嵌进单个 Go 二进制。

## 配置边界

监听地址属于进程启动配置，不写入 SQLite：

| 配置 | CLI | 环境变量 | 默认值 |
|---|---|---|---|
| IPP 监听 | `--ipp-listen` | `PUQU_IPP_LISTEN` | `:8631` |
| 管理后台监听 | `--admin-listen` | `PUQU_ADMIN_LISTEN` | `127.0.0.1:8080` |
| SQLite 路径 | `--data` | — | 操作系统用户配置目录 |

优先级为 CLI > 环境变量 > 默认值。管理后台监听必须是 localhost 或回环 IP；修改启动配置后重启服务。

SQLite 只保存业务状态：打印机、设备、标签规格、每台打印机的 AirPrint/DNS-SD 开关及作业记录。

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

打开 <http://127.0.0.1:8080>：

1. 在“设备”页扫描并保存附近打印机。
2. 在“标签规格”页创建与实体标签一致的尺寸和设备参数。
3. 在“打印机”页添加逻辑打印机，选择驱动、设备和标签规格。
4. 按需开启该打印机的 DNS-SD 和 AirPrint。
5. 打开打印机详情，重新连接并打印测试标签。

每个逻辑打印机的地址为：

```text
ipp://HOST:8631/ipp/<queue-name>
```

## 添加系统打印机

DNS-SD 正常时，系统会发现每个已启用广播的逻辑打印机。也可手动添加：

```bash
lpadmin -p WAREHOUSE_LABELS -E \
  -v ipp://HOST:8631/ipp/warehouse-labels \
  -m everywhere
```

- macOS：系统设置 → 打印机与扫描仪 → 添加打印机。
- Windows 11：设置 → 蓝牙和设备 → 打印机和扫描仪；使用系统 IPP Class Driver。
- iPhone/iPad：在目标打印机设置中开启 AirPrint；客户端和服务器须位于可互通 mDNS 的局域网。

## 后台服务

内置服务管理使用 systemd、launchd 或 Windows Service Manager：

```bash
sudo ./bin/puqu-ipp \
  --data /var/lib/puqu-ipp/puqu.db \
  --ipp-listen :8631 \
  --admin-listen 127.0.0.1:8080 \
  service install
sudo ./bin/puqu-ipp service start
./bin/puqu-ipp service status
```

安装命令会把当前数据路径和监听参数写入系统服务定义。服务账号必须拥有蓝牙权限和数据库目录权限。

## CLI

```bash
./bin/puqu-ipp serve       # 默认命令：IPP + DNS-SD + 本机管理后台
./bin/puqu-ipp discover    # 扫描并显示 BLE/GATT 信息
./bin/puqu-ipp print-test  # 直接连接设备并打印条纹测试标签
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
mise run sqlc             # 重新生成 internal/store/sqlitedb
```

测试覆盖 PUQU 字节帧、BLE UUID/标志、打印取消与自动重连、SQLite 迁移、多打印机隔离、PWG/Apple Raster/JPEG 解码、IPP 端到端提交和管理路由。

## 协议与限制

- PUQU 输出是设备自有光栅协议，不是 TSPL：8 字节打印头后跟 1bpp、MSB-first 位图。
- 1 点约等于 1/203 英寸，即 8 点/mm。
- 输入页面尺寸必须与目标打印机选择的标签规格匹配；不缩放、不裁切。
- 单次 IPP 文档上限 16 MiB；每台打印机内存队列容量 32。
- 当前不提供 TLS、认证、彩色、双面、PDF 或任意纸张缩放。仅在可信局域网暴露 IPP 端口。
- 真实 macOS、Windows、iOS 和多台实体打印机仍需在目标硬件环境联调。

## 目录

```text
cmd/puqu-ipp/          CLI、启动配置和系统服务管理
internal/admin/        本机管理 JSON 接口
internal/ble/          tinygo 原生 BLE 适配
internal/config/       CLI/环境变量启动配置验证
internal/fleet/        多打印机运行时和驱动注册
internal/ipp/          多队列 IPP 网关、属性和 DNS-SD
internal/printer/      PUQU 打印流程、取消、自动重连
internal/puqu/         纯 PUQU 线协议
internal/raster/       PWG/Apple Raster、JPEG → 1bpp
internal/store/        SQLite、goose 迁移、sqlc 查询
internal/web/          构建标签控制的嵌入式 SPA
web/                   React + TanStack + Tailwind 管理后台
```
