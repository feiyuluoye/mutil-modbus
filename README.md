# Modbus Simulator

Modbus Simulator 是一个使用 Go 实现的多模组 Modbus TCP 模拟系统，支持单实例模拟器、并发服务器管理、数据采集与文件导出等功能。项目旨在帮助快速构建 Modbus 联调、测试与演示环境。

## 功能概览

- 最小 Modbus TCP 服务器，支持读 coils、discrete inputs、holding/input registers。
- 基于 CSV 的寄存器周期写入，支持单实例与多实例并发运行。
- 数据采集器可实时拉取点位数据，并按需落盘 JSONL/CSV。
- 支持一次性快照导出，JSON/CSV 两种格式，便于排查和留存。

## 目录结构

- `cmd/server/`：单服务器模拟器，读取 `config.toml`。
- `cmd/servers/`：并发服务器管理器，读取 `config/config.yaml`，可选快照。
- `cmd/collector/`：数据采集器，支持 CLI 启用落盘功能。
- `cmd/export/`：一次性快照导出 CLI。
- `internal/`：核心实现（Modbus 服务、采集器、输出、模型等）。
- `config/`：示例 YAML/TOML 配置。
- `data/`：CSV 数据源及采集输出目录。

## 快速开始

1. 安装 Go 1.20+
2. 克隆仓库并进入目录
3. `go mod tidy`
4. 准备配置与 CSV 数据（可使用仓库自带示例）
5. 参考下方命令运行需要的模块

## 运行示例

### 单服务器模拟器 + 客户端

```bash
go run ./cmd/server --config config.toml
go run ./cmd/client --config config.toml
```

### 并发服务器管理器

```bash
go run ./cmd/servers --config config/config.yaml

# 仅当需要一次性导出快照时，加上 snapshot 参数
go run ./cmd/servers --config config/config.yaml \
  --snapshot-json out.json --snapshot-csv out.csv --snapshot-wait 5s
```

当提供 `--snapshot-json` 或 `--snapshot-csv` 时，程序会等待 `--snapshot-wait` 时长（默认 `3s`）以便 CSV 写入生效，随后导出快照并退出；否则常驻运行。

### 数据采集器

```bash
go run ./cmd/collector --config config/config.yaml

# CLI 覆盖 storage 相关配置
go run ./cmd/collector --config config/config.yaml \
  --storage-enabled --storage-dir data --storage-queue 20000
```

`system.storage.file_type` 决定输出模式：

- `log`：仅日志输出（默认 wrapHandler），不落盘。
- `csv`：写入 `storage-dir/collector.csv`。
- `json` / `jsonl`：写入 `storage-dir/collector.jsonl`。
- `json+csv` / `csv+json` / `both` / `all`：同时输出 JSONL 与 CSV。

CSV 表头：`timestamp, server_id, device_id, connection, slave_id, point_name, address, register, unit, value`

JSONL 字段示例：

```json
{"timestamp":"2025-09-29T14:45:32Z","server_id":"plc_server_1","device_id":"device_001","connection":"0.0.0.0:1502","slave_id":1,"point_name":"temperature","address":1,"register":"holding","unit":"","raw":21,"value":21}
```

### 一次性快照导出 CLI

```bash
go run ./cmd/export --config config/config.yaml \
  --json out.json --csv out.csv --wait 3s
```

`cmd/export` 会自启服务器，等待指定时长后导出当前快照并退出。

## 数据库与最新点位查询（ORM）

项目已迁移为使用 GORM（gorm.io/gorm）管理 SQLite 数据库，模型定义见 `internal/model/modbus.go`，ORM 辅助见 `internal/db/orm.go`。

- 连接与迁移：`db.Open(path)` 会自动创建并迁移表。
- 采集入库：采集器通过 `DB.SavePointValue()` 写入 `point_values`。

### 最新点位查询（去重规则变更）

`DB.LatestPoints(ctx)` 返回“最新点位值”列表，去重规则为复合键：`server_id + device_id + name`。即每个服务器-设备-点位名组合仅保留一条最新记录。

- 返回结构（`PointLatest`）：
  - `server_id`, `device_id`, `name`, `address`, `register_type`, `data_type`, `byte_order`, `unit`, `value`, `timestamp`

### 运行示例（examples/latest）

新增示例 `examples/latest` 用于查询并输出最新点位值（JSON）：

```bash
# 全量最新点位
go run ./examples/latest -db ./data.sqlite -pretty=false

# 仅过滤某个 server
go run ./examples/latest -db ./data.sqlite -server plc_server_1

# 仅过滤某个 device
go run ./examples/latest -db ./data.sqlite -device device_001

# 同时按 server 与 device 过滤
go run ./examples/latest -db ./data.sqlite -server plc_server_1 -device device_001
```

可选参数：

- `-db`: SQLite 文件路径（默认 `./data.sqlite`）
- `-pretty`: 是否美化 JSON（默认 `true`）
- `-timeout`: 查询超时（默认 `5s`）
- `-server`: 按 `server_id` 过滤（可选）
- `-device`: 按 `device_id` 过滤（可选）

## 配置说明

### TOML (`config.toml`, 供 `cmd/server` 使用)

```toml
[server]
listen_address = ":1502"

[[registers]]
name = "temperature"
type = "holding"
address = 1
csv_column = "temperature"
```

### YAML (`config/config.yaml`, 供 `cmd/servers` 与 `cmd/collector` 使用)

- `servers[]`: 定义每个服务器、设备与点位；`points.name` 需与 CSV 列名一致。
- `frequency`: `server_id -> duration`，控制 CSV 写入周期。
- `type` / `devices_file`: 服务器设备来源。
  - `type: device`（默认）：从 `devices` 数组读取点位定义。
  - `type: csvfile`：通过 `devices_file`（相对或绝对路径）加载设备与点位，例如 `data/plc_device_point.csv`。
- `system.storage`: 控制采集器输出行为，示例：

```yaml
system:
  storage:
    enabled: true
    file_type: json+csv   # 支持 log | csv | json | json+csv
    db_path: "data"
    max_workers: 0
    max_queue_size: 10000
```

CLI 参数 `--storage-*` 会覆盖上述配置。

### CSV 数据 (`data/example_data.csv`)

列名需与点位名称一致，例如 `temperature,humidity,pump,alarm`。

## 输出文件说明

- 模拟器数据源：`data/example_data.csv`
- 采集器输出：`data/collector.jsonl`, `data/collector.csv`（取决于 `file_type`）
- 服务器设备清单：`data/plc_device_point.csv`（示例，供 `plc_server_2` 使用）
- 快照导出：按命令指定的 `out.json`, `out.csv`

CSV 行示例：

```csv
timestamp,server_id,device_id,point_name,address,register,unit,value
```

JSONL 行示例：

```json
{"timestamp":"2025-09-29T13:38:00.123456789Z","server_id":"plc_server_1","device_id":"device_001","point_name":"temperature","address":1,"register":"holding","unit":"","raw":21,"value":21}
```

## 开发提示

- `internal/servermgr/manager.go` 的 `Snapshot()` 会按 YAML 点位读取当前寄存器值。
- `internal/collector/storage.go` 使用缓冲队列异步写文件，`max_queue_size` 可调。
- `internal/output/exporter.go` 支持 JSON/CSV 两种快照格式。

欢迎贡献更多协议扩展、输出格式或改进数据处理逻辑！
