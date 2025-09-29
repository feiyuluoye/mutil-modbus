# Modbus Simulator

This repository provides a lightweight Modbus TCP server simulator written in Go.
Register addresses and CSV data bindings are configured using a TOML file. The
simulator reads data from a CSV file and periodically updates the Modbus
registers with the configured values.

## Features

- Minimal Modbus TCP server that supports reading coils, discrete inputs,
  holding registers, and input registers.
- TOML configuration to define server settings, update interval, and register
  bindings.
- CSV-driven data updates that cycle through rows on a fixed interval.

## Getting Started

1. **Install Go** – version 1.20 or later is recommended.
2. **Clone** this repository and change into the project directory.
3. **Create a configuration file** based on `config.example.toml`.
4. **Prepare a CSV file** containing the columns referenced in the
   configuration.

# Example

```
创建配置文件：
cp config.example.toml config.toml
安装依赖：github.com/goburrow/modbus && go mod tidy
启动单模拟器（config.toml）与客户端联调：
终端1：go run ./cmd/server --config config.toml
终端2：go run ./cmd/client 
启动并发服务端（如果使用 server 管理器）：
go run ./cmd/servers --config config/config.yaml
go run ./cmd/collector --config config/config.yaml
```

It reads each `[[registers]]` entry once and prints the value. Example output:

```
temperature (holding@1) = <uint16>
humidity (input@0) = <uint16>
pump (coil@5) = <true|false>
alarm (discrete@2) = <true|false>
```

Notes:

- Run the client from the repo root so it can find `config.toml`, or adjust the path in code.
- If you change `server.listen_address`, restart the server; the client reads it from `config.toml`.

## Configuration Reference

- `[server]` section:
  - `listen_address`: TCP address for the Modbus server (default `:1502`).
- `[[registers]]` array:
  - `type`: Register type (`holding`, `input`, `coil`, or `discrete`).
  - `address`: Register address to update.
  - `csv_column`: Column name in the CSV file to use for the register value.

The simulator loops through the CSV rows continuously. Coil and discrete input
values treat any non-zero CSV value as `true`.

# mutil-modbus
