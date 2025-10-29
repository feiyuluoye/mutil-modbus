非常好的问题。 "模拟串口" 是实现一个**无需任何物理硬件**（如USB-to-485适配器）的纯软件模拟器的关键。

这是一个非常棒的Go项目实践。用Go来模拟Modbus RTU（我假设您指的RTU是Modbus RTU，这是工业领域最常见的RTU串口协议）设备（即Slave/Server），可以创建一个功能强大且轻量级的测试工具。

以下是一个详细的步骤和开发方案，我们将使用`goburrow/modbus`这个功能强大且流行的Go库来极大地简化开发。

### 方案概述

我们将创建一个Go应用程序，它会打开一个真实的（或虚拟的）串口（如 `COM3` 或 `/dev/ttyUSB0`），并作为Modbus Slave在该端口上侦听。当一个Modbus Master（如测试软件）向它发送请求时，它会根据内部维护的数据（寄存器）进行响应。

**核心功能：**

1. **可配置：** 可配置串口号、波特率、数据位、停止位、校验位和Slave ID。
2. **数据存储：** 内部维护一个“内存映射”，模拟设备的线圈、离散输入、保持寄存器和输入寄存器。
3. **动态模拟：** （进阶）能自动模拟某些数据的变化（例如，一个“温度”寄存器每秒自动+1）。
4. **日志记录：** 清晰地打印所有收到的请求和发送的响应，便于调试。

### 核心依赖库

我们将主要使用两个库：

1. **`github.com/goburrow/modbus`**：处理所有Modbus协议的帧解析、功能码（0x01, 0x03, 0x06, 0x10等）处理和CRC校验。
2. **`github.com/goburrow/serial`**：`goburrow/modbus`库依赖此库来进行跨平台的串口通信。

### 开发步骤详解

#### 第1步：项目初始化与环境配置

1. 创建项目目录：
   **Bash**

   ```
   mkdir go-modbus-rtu-simulator
   cd go-modbus-rtu-simulator
   ```
2. 初始化Go Module：
   **Bash**

   ```
   go mod init simulator.com/modbus
   ```
3. 获取依赖：
   **Bash**

   ```
   go get github.com/goburrow/modbus
   go get github.com/goburrow/serial
   ```

#### 第2步：定义配置结构

我们需要一个结构体来管理模拟器的配置。

`config.go`

**Go**

```
package main

// Config 定义了模拟器的所有配置
type Config struct {
    Port     string // 串口号, e.g., "COM3" (Windows) or "/dev/ttyUSB0" (Linux)
    BaudRate int    // 波特率, e.g., 9600
    DataBits int    // 数据位, e.g., 8
    Parity   string // 校验位, "N" (None), "E" (Even), "O" (Odd)
    StopBits int    // 停止位, e.g., 1
    SlaveID  byte   // 设备的Slave ID
}

// DefaultConfig 返回一个默认配置
func DefaultConfig() *Config {
    return &Config{
        Port:     "/dev/ttyUSB0", // 在Windows上改为 "COM3"
        BaudRate: 9600,
        DataBits: 8,
        Parity:   "N",
        StopBits: 1,
        SlaveID:  1,
    }
}
```

#### 第3步：创建数据存储（模拟设备内存）

`goburrow/modbus`库提供了一个非常方便的内存数据存储 `modbus.MapStore`。

`simulator.go`

**Go**

```
package main

import (
    "github.com/goburrow/modbus"
    "log"
)

// createStore 初始化模拟器的数据存储
func createStore(slaveID byte) modbus.Store {
    store := modbus.NewMapStore()

    // --- 预置一些数据 ---
  
    // 预置线圈 (Coils) - 功能码 0x01, 0x05, 0x0F
    // (SlaveID, 地址, 值)
    store.SetCoils(slaveID, 0, []bool{true, false, true, true})

    // 预置离散输入 (Discrete Inputs) - 功能码 0x02
    store.SetDiscreteInputs(slaveID, 0, []bool{false, false, true, false})

    // 预置保持寄存器 (Holding Registers) - 功能码 0x03, 0x06, 0x10
    // 地址 100 存入 0x0001
    // 地址 101 存入 0x0002
    // 地址 102 存入 0xABCD
    store.SetHoldingRegisters(slaveID, 100, []uint16{1, 2, 0xABCD})

    // 预置输入寄存器 (Input Registers) - 功能码 0x04
    // 地址 200 存入 0xCAFE
    store.SetInputRegisters(slaveID, 200, []uint16{0xCAFE})

    log.Printf("数据存储已初始化, SlaveID: %d", slaveID)
    log.Printf("  - Holding Registers @ 100: %v", []uint16{1, 2, 0xABCD})
    log.Printf("  - Coils @ 0: %v", []bool{true, false, true, true})

    return store
}
```

#### 第4步：(进阶) 实现动态数据模拟

这是"模拟器"区别于"静态Slave"的关键。我们启动一个goroutine在后台自动更新 `Store` 中的数据。

`simulator.go` (续)

**Go**

```
import (
    "time"
    // ... other imports
)

// startDynamicSimulation 启动一个goroutine来模拟动态数据
func startDynamicSimulation(store modbus.Store, slaveID byte) {
    // 让我们模拟地址为 100 的保持寄存器是一个 "计数器"
    // 让我们模拟地址为 200 的输入寄存器是一个 "随机温度"

    log.Println("动态数据模拟已启动...")

    go func() {
        ticker := time.NewTicker(2 * time.Second)
        defer ticker.Stop()

        for {
            <-ticker.C

            // 1. 更新计数器 (Holding Register 100)
            // 读取当前值
            currentVal, err := store.GetHoldingRegisters(slaveID, 100, 1)
            if err != nil || len(currentVal) == 0 {
                log.Println("读取模拟数据失败:", err)
                continue
            }
          
            // 值 +1
            newValue := currentVal[0] + 1
            store.SetHoldingRegisters(slaveID, 100, []uint16{newValue})
            log.Printf("[模拟器] Holding Register 100 (计数器) -> %d", newValue)

            // 2. 更新随机温度 (Input Register 200)
            // (这里只是示例，实际Get/Set需要匹配您在createStore中定义的地址)
            // newTemp := uint16(rand.Intn(100) + 200) // 20.0-30.0度
            // store.SetInputRegisters(slaveID, 200, []uint16{newTemp})
            // log.Printf("[模拟器] Input Register 200 (温度) -> %d", newTemp)
        }
    }()
}
```

#### 第5步：主程序 - 启动RTU服务器

这是将所有部分组合起来的 `main.go`。

`main.go`

**Go**

```
package main

import (
    "github.com/goburrow/modbus"
    "github.com/goburrow/modbus/server"
    "github.com/goburrow/serial"
    "log"
    "os"
    "os/signal"
    "syscall"
)

func main() {
    // 1. 加载配置
    // (为简化，我们直接使用默认配置。实际项目中您可以使用 flag 或 Viper 加载)
    cfg := DefaultConfig()
    // 在Windows上，请务必修改这里
    // cfg.Port = "COM3" 

    log.Printf("启动 Modbus RTU 模拟器...")
    log.Printf("  端口: %s, 波特率: %d, SlaveID: %d", cfg.Port, cfg.BaudRate, cfg.SlaveID)

    // 2. 创建数据存储
    store := createStore(cfg.SlaveID)

    // 3. (可选) 启动动态数据模拟
    startDynamicSimulation(store, cfg.SlaveID)

    // 4. 创建 Modbus 处理器
    // NewSlaveHandler 会处理所有的功能码，并从 store 中读写数据
    // 我们还添加了一个 NewLoggingSlaveHandler 来打印所有通信
    handler := modbus.NewLoggingSlaveHandler(modbus.NewSlaveHandler(store))

    // 5. 创建 Modbus 服务器
    srv := server.New(handler)

    // 6. 配置串口
    serialConfig := &serial.Config{
        Address:  cfg.Port,
        BaudRate: cfg.BaudRate,
        DataBits: cfg.DataBits,
        StopBits: cfg.StopBits,
        Parity:   cfg.Parity,
        Timeout:  10 * time.Second, // 读取超时
    }

    // 7. 启动服务器并监听RTU
    log.Println("正在打开串口并开始监听...")
    err := srv.ListenRTU(serialConfig)
    if err != nil {
        log.Fatalf("打开串口失败: %v", err)
    }

    // 等待程序退出信号 (Ctrl+C)
    waitForSignal()

    // 8. 关闭服务器
    log.Println("正在关闭模拟器...")
    srv.Close()
    log.Println("已关闭。")
}

// waitForSignal 阻塞主 goroutine 直到收到退出信号
func waitForSignal() {
    sigs := make(chan os.Signal, 1)
    signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
    <-sigs
}
```

*(注意：您需要将 `simulator.go` 中的 `createStore` 和 `startDynamicSimulation` 函数与 `main.go` 放在同一个 `main` 包下)*

### 第6步：测试模拟器

现在是最关键的一步：验证模拟器是否工作。

#### A. 物理测试 (推荐)

1. **硬件：** 您需要一个 **USB-to-RS485** 适配器。
2. **接线：** 将适配器的 `A+` 和 `B-` 端子短接（自发自收）。或者，如果您有另一个Modbus设备（如PLC）或另一个USB-to-RS485适配器，将它们交叉连接 (A+ 对 A+, B- 对 B-)。
3. **配置：**

   * 在Windows上，查看设备管理器，找到适配器的COM口（例如 `COM3`）。
   * 在Linux上，通常是 `/dev/ttyUSB0`。
   * 修改 `main.go` 中的 `cfg.Port` 为您的实际端口。
4. **运行模拟器：**
   **Bash**

   ```
   go run .
   ```

   您应该会看到 "正在打开串口并开始监听..."
5. **运行Master工具：**

   * 打开一个Modbus Master测试工具（如 `Modbus Poll` (Windows) 或 `qModMaster` (跨平台)）。
   * **连接设置：** 设置与模拟器**完全相同**的串口参数（COM3, 9600, 8, N, 1）。
   * **轮询设置 (Poll Definition)：**
     * Slave ID: 1
     * Function: 03 Holding Registers
     * Address: 100
     * Quantity: 3
   * 点击 "Poll" 或 "Connect"。
6. **查看结果：**

   * **Master工具：** 您应该能立即看到读取到的数据：`[1, 2, 43981]` (43981 就是 0xABCD)。
   * **Go模拟器终端：** 您会看到 `modbus` 库打印的日志，显示收到了 0x03 请求并发送了响应。您还会看到 "[模拟器] Holding Register 100 (计数器) -> X" 的值在不断增加，并且Master工具上显示的值也在同步更新。

#### B. 虚拟测试 (无硬件时)

如果您没有硬件，可以使用虚拟串口软件。

* **Windows:** 使用 `com0com` (Null-modem emulator) 创建一对虚拟串口，例如 `COM10` 和 `COM11`。
* **Linux:** 使用 `socat` 创建一对虚拟pty。
  **Bash**

  ```
  socat -d -d pty,raw,echo=0,link=/tmp/vport1 pty,raw,echo=0,link=/tmp/vport2
  ```

然后将Go模拟器连接到 `COM10` (或 `/tmp/vport1`)，将Modbus Master工具连接到 `COM11` (或 `/tmp/vport2`)。测试步骤同上。

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
