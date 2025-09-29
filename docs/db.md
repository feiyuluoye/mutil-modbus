# SQLite数据库表设计

基于提供的YAML配置文件，我设计了三个SQLite表来存储工业数据采集系统的信息：服务器表、设备表和点位数据值表。下面是每个表的详细设计，包括字段说明和解释。

## 1. 服务器表 (servers)

存储服务器的基本信息和连接参数：

```sql
CREATE TABLE IF NOT EXISTS servers (
    server_id TEXT PRIMARY KEY,
    server_name TEXT NOT NULL,
    protocol TEXT NOT NULL,
    host TEXT NOT NULL,
    port INTEGER NOT NULL,
    timeout TEXT,
    retry_count INTEGER,
    enabled BOOLEAN NOT NULL DEFAULT 1,
    poll_interval TEXT
);
```

### 字段说明：


| 字段名        | 数据类型 | 约束               | 说明                                      |
| ------------- | -------- | ------------------ | ----------------------------------------- |
| server_id     | TEXT     | PRIMARY KEY        | 服务器唯一标识符，对应YAML中的server_id   |
| server_name   | TEXT     | NOT NULL           | 服务器名称，对应YAML中的server_name       |
| protocol      | TEXT     | NOT NULL           | 通信协议，如"modbus-tcp"                  |
| host          | TEXT     | NOT NULL           | 服务器主机地址，对应YAML中connection.host |
| port          | INTEGER  | NOT NULL           | 服务器端口号，对应YAML中connection.port   |
| timeout       | TEXT     | -                  | 连接超时时间，对应YAML中的timeout         |
| retry_count   | INTEGER  | -                  | 连接重试次数，对应YAML中的retry_count     |
| enabled       | BOOLEAN  | NOT NULL DEFAULT 1 | 服务器是否启用，对应YAML中的enabled       |
| poll_interval | TEXT     | -                  | 服务器默认采集频率，从frequency配置中获取 |

## 2. 设备表 (devices)

存储连接到服务器的设备信息：

```sql
CREATE TABLE IF NOT EXISTS devices (
    device_id TEXT PRIMARY KEY,
    server_id TEXT NOT NULL,
    vendor TEXT,
    slave_id INTEGER,
    poll_interval TEXT,
    FOREIGN KEY (server_id) REFERENCES servers(server_id) ON DELETE CASCADE
);
```

### 字段说明：


| 字段名        | 数据类型 | 约束                  | 说明                                     |
| ------------- | -------- | --------------------- | ---------------------------------------- |
| device_id     | TEXT     | PRIMARY KEY           | 设备唯一标识符，对应YAML中的device_id    |
| server_id     | TEXT     | NOT NULL, FOREIGN KEY | 所属服务器ID，关联到servers表的server_id |
| vendor        | TEXT     | -                     | 设备制造商，对应YAML中的vendor           |
| slave_id      | INTEGER  | -                     | 设备从站ID，对应YAML中的slave_id         |
| poll_interval | TEXT     | -                     | 设备采集频率，对应YAML中的poll_interval  |

### 外键关系：

- `server_id` 字段引用 `servers(server_id)`，并设置级联删除，当服务器被删除时，其关联的设备也会被自动删除。

## 3. 点位数据值表 (point_values)

存储点位定义和实时采集的数据值：

```sql
CREATE TABLE IF NOT EXISTS point_values (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    device_id TEXT NOT NULL,
    name TEXT NOT NULL,
    address INTEGER NOT NULL,
    register_type TEXT NOT NULL,
    data_type TEXT NOT NULL,
    byte_order TEXT NOT NULL,
    scale REAL NOT NULL DEFAULT 1.0,
    offset REAL NOT NULL DEFAULT 0.0,
    unit TEXT,
    value REAL,
    timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (device_id) REFERENCES devices(device_id) ON DELETE CASCADE
);
```

### 字段说明：


| 字段名        | 数据类型 | 约束                      | 说明                                                 |
| ------------- | -------- | ------------------------- | ---------------------------------------------------- |
| id            | INTEGER  | PRIMARY KEY AUTOINCREMENT | 记录唯一ID，自增主键                                 |
| device_id     | TEXT     | NOT NULL, FOREIGN KEY     | 所属设备ID，关联到devices表的device_id               |
| name          | TEXT     | NOT NULL                  | 点位名称，对应YAML中的name                           |
| address       | INTEGER  | NOT NULL                  | 点位地址，对应YAML中的address                        |
| register_type | TEXT     | NOT NULL                  | 寄存器类型，如"holding"、"input"、"coil"、"discrete" |
| data_type     | TEXT     | NOT NULL                  | 数据类型，如"uint16"                                 |
| byte_order    | TEXT     | NOT NULL                  | 字节顺序，如"ABCD"                                   |
| scale         | REAL     | NOT NULL DEFAULT 1.0      | 缩放因子，用于原始值转换，对应YAML中的scale          |
| offset        | REAL     | NOT NULL DEFAULT 0.0      | 偏移量，用于原始值转换，对应YAML中的offset           |
| unit          | TEXT     | -                         | 单位，对应YAML中的unit                               |
| value         | REAL     | -                         | 点位实际值，经过scale和offset转换后的值              |
| timestamp     | DATETIME | DEFAULT CURRENT_TIMESTAMP | 数据采集时间戳                                       |

### 外键关系：

- `device_id` 字段引用 `devices(device_id)`，并设置级联删除，当设备被删除时，其关联的点位数据也会被自动删除。

## 索引设计

为提高查询性能，创建以下索引：

```sql
CREATE INDEX IF NOT EXISTS idx_point_values_device_id ON point_values(device_id);
CREATE INDEX IF NOT EXISTS idx_point_values_timestamp ON point_values(timestamp);
CREATE INDEX IF NOT EXISTS idx_devices_server_id ON devices(server_id);
```

### 索引说明：


| 索引名                     | 表名         | 字段      | 用途                       |
| -------------------------- | ------------ | --------- | -------------------------- |
| idx_point_values_device_id | point_values | device_id | 加速按设备ID查询点位数据   |
| idx_point_values_timestamp | point_values | timestamp | 加速按时间范围查询点位数据 |
| idx_devices_server_id      | devices      | server_id | 加速按服务器ID查询设备     |

## 表关系图

```
servers (服务器表)
├── server_id (PK)
├── server_name
├── protocol
├── host
├── port
├── timeout
├── retry_count
├── enabled
└── poll_interval

devices (设备表)
├── device_id (PK)
├── server_id (FK → servers.server_id)
├── vendor
├── slave_id
└── poll_interval

point_values (点位数据值表)
├── id (PK)
├── device_id (FK → devices.device_id)
├── name
├── address
├── register_type
├── data_type
├── byte_order
├── scale
├── offset
├── unit
├── value
└── timestamp
```

## 设计考虑

1. **数据完整性**：

   - 使用主键确保每条记录的唯一性
   - 使用外键约束确保表之间的关系完整性
   - 设置级联删除，当父记录被删除时，子记录也会被自动删除
2. **查询效率**：

   - 为常用查询字段创建索引，提高查询性能
   - 特别是对点位数据值表的时间戳字段创建索引，因为按时间范围查询是最常见的操作
3. **数据存储**：

   - 使用SQLite的轻量级存储，适合嵌入式和中小规模应用
   - 使用REAL类型存储经过转换的实际值，便于后续分析和处理
   - 使用CURRENT_TIMESTAMP自动记录数据采集时间
4. **扩展性**：

   - 表结构设计灵活，可以方便地添加新的服务器、设备和点位
   - 支持多种寄存器类型和数据类型，适应不同工业设备的需求

这个设计完全满足了基于YAML配置文件的Modbus数据采集系统的存储需求，具有良好的数据完整性、查询效率和扩展性。
