# Modbus RTU Usage, Fixes, and Troubleshooting

## Overview

This project now supports both Modbus TCP and Modbus RTU (serial) for the server and client.
- Server auto-switches to RTU if `[server].mode = "rtu"` or a serial path is provided.
- Client prefers the `[client]` section; if `mode = "rtu"` or a serial path is provided, it uses RTU; otherwise TCP.

This guide documents the RTU setup, configuration keys, code changes, and how to troubleshoot common issues.

---

## Configuration

Top-level settings:
- `csv_file`: CSV data source path
- `update_interval`: default poll interval (e.g. `"5s"`)

Server section `[server]` keys:
- `mode`: `"tcp"` or `"rtu"`
- `listen_address`: host:port for TCP (e.g., `":1502"`)
- `serial_port` or `path`: serial device path (e.g., `/tmp/vport1`)
- `baud_rate`, `data_bits`, `stop_bits`, `parity`: serial parameters (e.g., `9600`, `8`, `1`, `"N"`)
- `slave_id`: RTU slave ID, integer 1..247
- `update_interval`: optional; not always used by server

Client section `[client]` keys (preferred by client if present):
- Same as `[server]` for mode and serial/TCP options
- `update_interval`: client poll interval; falls back to top-level `update_interval` or `5s`

Notes:
- `path` is accepted as an alias for `serial_port`.
- If `[client]` is empty, the client falls back to `[server]` settings.

---

## RTU Setup with Virtual Serial Ports (socat)

When no real serial ports are available, create a virtual pair with socat:

1) Start socat (in a dedicated terminal):

```
socat -d -d pty,raw,echo=0,link=/tmp/vport1 \
              pty,raw,echo=0,link=/tmp/vport2
```

2) Permissions (if needed):

```
sudo chmod 666 /tmp/vport1 /tmp/vport2
```

3) Assign ports:
- Server uses `/tmp/vport1` (the `link` side)
- Client uses `/tmp/vport2` (the `peer` side)

Do not have server and client open the same end simultaneously.

---

## Example Configs

TCP mode:

```toml
[server]
mode = "tcp"
listen_address = ":1502"

[client]
mode = "tcp"
listen_address = ":1502"

update_interval = "5s"
# [[registers]] entries ...
```

RTU mode:

```toml
[server]
mode = "rtu"
path = "/tmp/vport1"
baud_rate = 9600
data_bits = 8
stop_bits = 1
parity = "N"
slave_id = 1

[client]
mode = "rtu"
path = "/tmp/vport2"
baud_rate = 9600
data_bits = 8
stop_bits = 1
parity = "N"
slave_id = 1

update_interval = "5s"
# [[registers]] entries ...
```

---

## How to Run

TCP server:
```
go run ./cmd/server -config config.topway.toml
```

TCP client:
```
go run ./cmd/client --config config.topway.toml
```

RTU server (after socat is running):
```
go run ./cmd/server -config config.topway.rtu.toml
```

RTU client:
```
go run ./cmd/client --config config.topway.rtu.toml
```

---

## Code Changes Summary

- internal/config/config.go
  - Added `[client]` section support.
  - Tolerates `path` as alias for `serial_port`.
  - Parses `slave_id` and `update_interval`.

- cmd/client/main.go
  - Prefers `[client]` over `[server]` settings.
  - RTU handler setup with configurable serial params.
  - Increased timeout to 5s; set `IdleTimeout = 100ms` for RTU.
  - Pre-flight check: ensures serial port path exists.
  - Continuous loop reading controlled by `update_interval`.
  - Auto-reconnect on common serial errors (e.g., "bad file descriptor", "could not select").

- cmd/mocktty/main.go and internal/utils/rtu.go
  - Introduced utilities for serial open and socat command construction.
  - RTU-over-serial stream handling and demo store logic.

- cmd/server/main.go
  - Auto-enable RTU if mode="rtu" or serial path is provided; otherwise TCP server.

---

## Troubleshooting

- Serial timeouts
  - Cause: socat not running; wrong port used; permissions missing; server/client opened the same end.
  - Fix: start socat; ensure `/tmp/vport1` for server and `/tmp/vport2` for client; `chmod 666` both.

- CRC mismatch
  - Cause: wrong serial parameters (baud/parity), noise/misaligned frame, or port contention.
  - Fix: verify both ends use the same 9600 8N1, slave_id matches; ensure only one process per end.

- "no such file or directory"
  - Cause: serial path missing; socat not yet created.
  - Fix: start socat first; check `ls -l /tmp/vport1 /tmp/vport2`.

- "bad file descriptor" / "could not select"
  - Cause: FD closed by OS/driver or race during reconnect.
  - Fix: client now auto-reconnects; if frequent, increase `IdleTimeout` (e.g., 200–300ms) and ensure no other process is grabbing the port.

- Occupied ports
  - Check: `lsof /tmp/vport1 /tmp/vport2`
  - Expectation: server holds `/tmp/vport1`, client holds `/tmp/vport2`.

---

## Tips

- Keep socat running in a dedicated terminal.
- For stability, avoid frequent open/close on RTU; the client keeps a long-lived connection.
- Ensure serial parameters and `slave_id` are identical across server and client.
- For TC P vs RTU testing, maintain separate config files to reduce accidental cross-use.

---

## Future Improvements (Optional)

- Add server-side RTU frame logging behind an env flag (e.g., `LOG_RTU=1`).
- Provide Make targets for `socat-up`, `server-rtu`, `client-rtu`, and health checks.
