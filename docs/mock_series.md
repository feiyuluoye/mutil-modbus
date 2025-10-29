非常好的问题。 "模拟串口" 是实现一个**无需任何物理硬件**（如USB-to-485适配器）的纯软件模拟器的关键。

当您没有物理串口或不想依赖硬件时，有两种主流的 "模拟串口" 方案。

1. **方案一 (最真实)：使用虚拟串口软件**
   * **原理：** 在操作系统层面创建一对“背靠背”连接的虚拟串口（例如 `COM10` 和 `COM11`）。您的Go模拟器连接到 `COM10`，而Modbus Master测试工具连接到 `COM11`。系统会将 `COM10` 发出的所有数据直接转发到 `COM11`。
   * **优点：** 对您的Go代码**零修改**（代码依然认为它在和 `COM10` 通信），这是对真实串口环境最精确的模拟。
   * **缺点：** 需要安装和配置第三方软件。
2. **方案二 (最便捷)：使用 Modbus RTU-over-TCP**
   * **原理：** 这是一种“混合”模式。协议*内容*是 Modbus RTU（包括原始的功能码和CRC校验），但*传输方式*不是串口，而是TCP网络。
   * **优点：****无需任何串口驱动或虚拟软件**。只需一个Go程序和一个支持"RTU-over-TCP"的Master工具（如 `Modbus Poll`），非常适合纯软件开发和测试。
   * **缺点：** Master工具必须支持这种特定模式。

---

### 方案一：添加细节 - 使用 `socat` (Linux/macOS)

这是最真实的 "模拟串口" 方案。Windows上对应的工具是 `com0com`。

1. 安装 socat

(在Linux上)

**Bash**

```
sudo apt-get update
sudo apt-get install socat
```

(在macOS上)

**Bash**

```
brew install socat
```

2. 创建虚拟串口对

打开一个新的终端窗口并运行以下命令。这个窗口必须保持打开，它就是你的“虚拟串口线”：

**Bash**

```
socat -d -d pty,raw,echo=0,link=/tmp/vport1 pty,raw,echo=0,link=/tmp/vport2
```

* `socat` 会创建两个虚拟串口设备：`/tmp/vport1` 和 `/tmp/vport2`。
* 它会把这两个端口连接起来。
* `-d -d` 会打印详细的日志，方便你看到数据流动。

3. 修改Go模拟器代码 (main.go)

您在 main.go 中的代码几乎不需要改变，只需要修改 DefaultConfig 中的 Port 指向 socat 创建的一个端口：

**Go**

```
// 在 config.go 或 main.go 中
func DefaultConfig() *Config {
    return &Config{
        // 将端口指向 vport1
        Port:     "/tmp/vport1", 
        BaudRate: 9600,
        DataBits: 8,
        Parity:   "N",
        StopBits: 1,
        SlaveID:  1,
    }
}
```

**4. 运行和测试**

1. **运行 `socat`：** 确保 `socat` 命令在第一个终端中正在运行。
2. **运行Go模拟器：** 在第二个终端中，运行您的模拟器：
   **Bash**

   ```
   go run .
   ```

   它会成功连接到 `/tmp/vport1` 并开始监听。
3. **连接Master工具：**

   * 打开 `Modbus Poll` 或 `qModMaster`。
   * 在连接设置中，**不要**选择TCP。
   * 选择 "Serial Port" (串口)。
   * 将串口号设置为 `socat` 创建的**另一个**端口：`/tmp/vport2`。
   * 设置完全一致的波特率、数据位等 (9600, 8, N, 1)。
   * 开始轮询 (Poll)。

现在，Master工具会通过 `/tmp/vport2` 发送数据，`socat` 将其转发到 `/tmp/vport1`，您的Go模拟器会收到数据并响应。

---

### 方案二：添加细节 - 使用 RTU-over-TCP (推荐)

这是最便捷的纯软件方案。它完全绕过了串口，`goburrow/modbus` 库原生支持此模式。

1. 修改Go模拟器代码 (main.go)

这个方案需要对 main.go 进行修改。我们不再需要 serial.Config，而是直接监听一个TCP端口。

这是修改后的 `main.go`：

**Go**

```
package main

import (
    "github.com/goburrow/modbus"
    "github.com/goburrow/modbus/server"
    // "github.com/goburrow/serial" // 不再需要
    "log"
    "os"
    "os/signal"
    "syscall"
    // "time" // 不再需要
)

func main() {
    // 1. 加载配置 (现在只需要 SlaveID)
    cfg := DefaultConfig()
    // TCP 监听地址
    listenAddr := "127.0.0.1:5020" 

    log.Printf("启动 Modbus RTU-over-TCP 模拟器...")
    log.Printf("  监听地址: %s, SlaveID: %d", listenAddr, cfg.SlaveID)

    // 2. 创建数据存储
    store := createStore(cfg.SlaveID)

    // 3. (可选) 启动动态数据模拟
    startDynamicSimulation(store, cfg.SlaveID)

    // 4. 创建 Modbus 处理器
    handler := modbus.NewLoggingSlaveHandler(modbus.NewSlaveHandler(store))

    // 5. 创建 Modbus 服务器
    srv := server.New(handler)

    // 6. 【关键修改】 启动服务器并监听 RTU-over-TCP
    // 我们不再使用 srv.ListenRTU(serialConfig)
    // 而是使用:
    log.Println("正在启动TCP监听...")
    err := srv.ListenRTUOverTCP(listenAddr)
    if err != nil {
        log.Fatalf("启动 RTU-over-TCP 监听失败: %v", err)
    }

    // 7. 等待程序退出信号 (Ctrl+C)
    waitForSignal()

    // 8. 关闭服务器
    log.Println("正在关闭模拟器...")
    srv.Close()
    log.Println("已关闭。")
}

// waitForSignal 保持不变...
func waitForSignal() {
    sigs := make(chan os.Signal, 1)
    signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
    <-sigs
}

// config.go 和 simulator.go 中的 createStore/startDynamicSimulation 保持不变
// (尽管 Config 结构中除了 SlaveID 之外的字段都用不上了)
```

**2. 运行和测试**

1. **运行Go模拟器：**
   **Bash**

   ```
   go run .
   ```

   它会立即开始监听 `127.0.0.1:5020`。
2. **连接Master工具 (以 `Modbus Poll` 为例)：**

   * 打开 `Modbus Poll`。
   * 点击菜单 `Connection` -> `Connect...`。
   * 在弹出的窗口中，**关键选择**：
     * 选择 `Modbus RTU Over TCP/IP` (这是一个与 "Modbus TCP/IP" 不同的选项)。
   * **设置IP和端口：**
     * IP Address: `127.0.0.1` (或 `localhost`)
     * Port: `5020`
   * 点击 `OK`。
3. **配置轮询 (Poll Definition)：**

   * 设置 `Slave ID: 1`。
   * Function `03 Holding Registers`, Address `100`, Quantity `3`。

您会看到 `Modbus Poll` 成功读取到了数据，并且您的Go模拟器终端会打印出它收到和发送的**原始RTU帧**日志。
