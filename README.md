# PUQU IPP Bridge

把 PUQU AQ20（以及协议兼容的 PQ/TQ/Q 系列）USB 标签机映射为局域网标准打印机。服务端仅支持 Linux。

一个 Go 守护进程可管理多台物理设备，并为每台已配置打印机提供独立 IPP 队列。客户端通过系统打印接口提交作业，不直接接触 USB。

```text
系统打印对话框 ── IPP/PWG Raster/JPEG ──▶ puqu-ipp ── USB ──▶ 多台 PUQU 打印机
                                             ├── 每台打印机独立队列
                                             └── 127.0.0.1:8080 管理后台
```

## 功能

- 多打印机：每个逻辑打印机选择驱动、USB 设备和标签规格，拥有稳定 UUID、队列路径和独立作业队列。
- 通用驱动模型：当前内置 `puqu-aq20`，覆盖 AQ20/PQ/TQ/Q 协议兼容设备；可继续增加驱动实现。
- IPP 1.1/2.0：`Print-Job`、`Validate-Job`、`Create-Job`、`Send-Document`、取消和作业查询。
- 驱动免安装输入：PWG Raster、JPEG；每台打印机使用稳定的 `/ipp/<queue>` 地址。
- 固定 203 dpi、单色、单面标签；标签规格决定精确介质尺寸，官方半色调与亮度处理默认自动，专业用户可按规格覆盖。
- SQLite 保存打印机、物理设备、标签规格和作业历史；服务重启时中止状态不确定的在途作业。
- 多个 USB 连接可并存；每台打印机按 USB 序列号稳定绑定，不同打印机可并行处理作业。
- React 19 + TanStack Router/Query + Tailwind CSS v4 管理后台；生产构建内嵌进单个 Go 二进制。

## 启动配置

Koanf 统一合并 TOML、环境变量和 CLI。默认配置文件为操作系统用户配置目录下的 `puqu-ipp/config.toml`，也可用 `--config` 或 `PUQU_CONFIG` 指定：

```toml
data_path = ""
ipp_listen = ":8631"
admin_listen = "127.0.0.1:8080"
log_level = "info"
```

| TOML | CLI | 环境变量 | 默认值 |
|---|---|---|---|
| `data_path` | `--data` | `PUQU_DATA_PATH` | 操作系统用户配置目录 |
| `ipp_listen` | `--ipp-listen` | `PUQU_IPP_LISTEN` | `:8631` |
| `admin_listen` | `--admin-listen` | `PUQU_ADMIN_LISTEN` | `127.0.0.1:8080` |
| `log_level` | `--log-level` | `PUQU_LOG_LEVEL` | `info` |

优先级为 CLI > 环境变量 > TOML > 默认值。未知 TOML 键、无效日志级别和非法监听地址会阻止启动。`admin_listen` 会按配置原样监听；局域网使用可设为 `0.0.0.0:8080` 或指定网卡地址。配置文件中的相对 `data_path` 相对于配置文件目录解析；环境变量和 CLI 中的相对路径相对于当前工作目录解析。修改后重启服务，不热加载。

SQLite 只保存业务状态：打印机、设备、标签规格和作业记录。

## 前置条件

Linux 通过 usbfs 直接访问打印机。服务账号必须能读写 USB `8888:0026`；程序会在接口 2 被内核 `usblp` 占用时仅解除该驱动并原子 claim，不会抢占其他用户态进程。

Docker 需挂载 `/dev/bus/usb:/dev/bus/usb`，并提供执行 `USBDEVFS_DISCONNECT_CLAIM` 所需权限（最简单为 `privileged: true`）。生产主机也可 `blacklist usblp`，避免热插拔或重启后内核重新绑定。

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
4. 打开打印机详情，重新连接并打印测试标签。

每个逻辑打印机的地址为：

```text
ipp://HOST:8631/ipp/<queue-name>
```

## 添加系统打印机

本项目不运行 mDNS/DNS-SD，也不实现 AirPrint；客户端直接使用稳定 IPP 地址。需要自动发现或额外格式转换时，可在上层接入 CUPS。

Linux/CUPS：

```bash
lpadmin -p WAREHOUSE_LABELS -E \
  -v ipp://HOST:8631/ipp/warehouse-labels \
  -m everywhere
```

- macOS：系统设置 → 打印机与扫描仪 → 添加打印机 → IP，协议选择 IPP，队列填写 `ipp/<queue-name>`。
- Windows 11：设置 → 蓝牙和设备 → 打印机和扫描仪 → 手动添加，使用完整 IPP URL 和系统 IPP Class Driver。

## 后台服务

内置服务管理使用 systemd、launchd 或 Windows Service Manager。系统服务只保存配置文件路径；环境变量或其他 CLI 覆盖不会被固化，安装时检测到这类覆盖会直接报错：

```bash
sudo ./bin/puqu-ipp --config /etc/puqu-ipp/config.toml service install
sudo ./bin/puqu-ipp service start
./bin/puqu-ipp service status
```

配置文件必须能被服务账号读取；数据库目录必须可写；Linux 服务账号还需能读写 PUQU USB 设备。

## CLI

```bash
./bin/puqu-ipp serve       # 默认命令：IPP + 本机管理后台
./bin/puqu-ipp discover    # 列出 USB 打印机及稳定序列号
./bin/puqu-ipp print-test  # 通过 USB 打印条纹测试标签
./bin/puqu-ipp smoke       # 检查 USB 接口访问
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

测试覆盖 PUQU 字节帧、USB 发现、打印取消与自动重连、配置优先级、SQLite 迁移、多打印机隔离、PWG Raster/JPEG 解码、IPP 端到端提交和管理路由。

## 协议与限制

- PUQU USB 输出不是 TSPL：每页为 `2A 76 30 02 xL xH yL yH`，随后是 1bpp、MSB-first 位图。
- 官方 USB 输出中没有浓度、速度或纸张类型字段；Windows 驱动保存这些旧设置但不写入设备数据流，因此管理界面不暴露无效控制。
- 多级灰度输入默认自动使用官方 Floyd-Steinberg；专业设置可改为直接阈值、4×4 聚簇抖动或扩展误差扩散，亮度范围为 -10～10。
- 1 点约等于 1/203 英寸，即 8 点/mm。
- 输入页面尺寸必须与目标打印机选择的标签规格匹配；不缩放、不裁切。
- 单次 IPP 文档上限 16 MiB；每台打印机内存队列容量 32。
- 当前不提供 TLS、认证、彩色、双面、PDF 或任意纸张缩放。仅在可信局域网暴露 IPP 端口。
- 服务端仅支持 Linux；多台实体打印机仍需在目标硬件环境联调。

## 目录

```text
cmd/puqu-ipp/          CLI、启动配置和系统服务管理
internal/admin/        本机管理 JSON 接口
internal/usb/          Linux usbfs 直接传输
internal/config/       Koanf TOML/环境变量/CLI 启动配置
internal/fleet/        多打印机运行时和驱动注册
internal/ipp/          多队列 IPP 网关和属性
internal/printer/      PUQU 打印流程、取消、自动重连
internal/puqu/         纯 PUQU 线协议
internal/raster/       PWG Raster、JPEG → 1bpp
internal/store/        SQLite、goose 迁移、sqlc 查询
internal/web/          构建标签控制的嵌入式 SPA
web/                   React + TanStack + Tailwind 管理后台
```
