# DBR-Nexus

[English](#english) | [中文](#中文)

---

<a name="中文"></a>

## 中文

### 项目简介

DBR-Nexus 是一个现代化的远程设备管理平台，采用 Server-Agent 架构，支持大规模设备的集中监控与运维管理。通过 WebSocket 实现实时通信，提供端到端加密、远程 Shell、文件管理、隧道代理等核心功能。

### 功能特性

| 功能模块 | 描述 |
|---------|------|
| **设备管理** | 设备在线状态监控、系统信息采集（CPU/内存/磁盘/网络）、设备分组与标签 |
| **远程终端** | 基于 xterm.js 的 Web 终端，支持实时交互、多会话管理 |
| **文件管理** | 远程文件浏览、上传/下载、在线编辑（Monaco Editor）、批量操作 |
| **隧道代理** | TCP 端口转发、SOCKS5 代理，支持通过设备访问内网资源 |
| **安全加密** | X25519 密钥交换 + AES-256-GCM 端到端加密，流量混淆与填充 |
| **审计日志** | 用户操作记录、设备活动追踪、安全事件审计 |

### 技术栈

**后端**
- Go 1.25
- Gin (HTTP框架)
- Gorilla WebSocket
- SQLite (数据存储)
- gopsutil (系统信息采集)
- X25519 + AES-256-GCM (加密)

**前端**
- React 18 + TypeScript
- Ant Design 5
- Vite 8
- xterm.js (终端模拟)
- Monaco Editor (代码编辑)
- Zustand (状态管理)

### 项目结构

```
DBR-Nexus/
├── cmd/
│   ├── server/          # 服务端入口
│   └── agent/           # Agent 入口
├── internal/
│   ├── agent/           # Agent 核心逻辑
│   │   ├── executor/    # Shell/进程管理
│   │   ├── filemanager/ # 文件操作
│   │   └── tunnel/      # 隧道转发
│   ├── server/          # 服务端核心逻辑
│   │   ├── auth/        # JWT/PSK 认证
│   │   └── database/    # 数据库操作
│   ├── crypto/          # 加密模块
│   ├── protocol/        # 通信协议定义
│   └── logging/         # 日志模块
├── web/                 # 前端项目
│   ├── src/
│   │   ├── pages/       # 页面组件
│   │   ├── components/  # 业务组件
│   │   └── api/         # API 客户端
│   └── package.json
├── configs/             # 配置文件
└── tools/               # 工具脚本
```

### 快速开始

#### 1. 构建项目

```bash
# 构建服务端
go build -o dbr-server ./cmd/server/

# 构建 Agent
go build -o dbr-agent ./cmd/agent/

# 构建前端
cd web && npm install && npm run build
```

#### 2. 配置服务端

编辑 `configs/server.yaml`：

```yaml
server:
  addr: ":8443"
  tls:
    cert: "certs/server.crt"
    key: "certs/server.key"

auth:
  jwt_secret: "your-secret-key"
  psk: "your-psk-key"

encryption:
  enabled: true
```

#### 3. 启动服务

```bash
# 启动服务端
./dbr-server configs/server.yaml

# 启动 Agent（在被管理设备上）
./dbr-agent configs/agent.yaml
```

#### 4. 访问管理界面

打开浏览器访问 `https://your-server:8443`，使用默认账号登录：
- 用户名: `admin`
- 密码: `admin`

### 配置说明

#### 服务端配置 (configs/server.yaml)

| 配置项 | 说明 |
|-------|------|
| `server.addr` | 监听地址 |
| `server.tls` | TLS 证书配置 |
| `auth.jwt_secret` | JWT 签名密钥 |
| `auth.psk` | Agent 认证预共享密钥 |
| `tunnel.bind_range` | 隧道端口范围 |
| `encryption.enabled` | 是否启用加密 |

#### Agent 配置 (configs/agent.yaml)

| 配置项 | 说明 |
|-------|------|
| `server.url` | 服务端 WebSocket 地址 |
| `server.psk` | 预共享密钥（需与服务端一致） |
| `heartbeat.interval` | 心跳间隔 |
| `device.id` | 设备唯一标识（可选，自动生成） |

### 安全特性

- **端到端加密**: 使用 X25519 进行密钥交换，AES-256-GCM 加密所有通信
- **流量混淆**: 消息填充与定时抖动，防止流量特征分析
- **双向认证**: PSK 认证 Agent，JWT 认证 Web 用户
- **IP 白名单**: 支持 Web 管理界面的 IP 访问控制
- **审计追踪**: 完整记录用户操作与设备活动

### 开发指南

```bash
# 前端开发模式
cd web && npm run dev

# 后端开发
go run ./cmd/server/ configs/server.yaml
```

### 许可证

MIT License

---

<a name="english"></a>

## English

### Overview

DBR-Nexus is a modern remote device management platform using a Server-Agent architecture. It supports centralized monitoring and operations management for large-scale devices. Real-time communication via WebSocket provides end-to-end encryption, remote shell, file management, tunnel proxy, and other core features.

### Features

| Module | Description |
|--------|-------------|
| **Device Management** | Device online status monitoring, system info collection (CPU/Memory/Disk/Network), device grouping and tagging |
| **Remote Terminal** | Web terminal based on xterm.js, real-time interaction, multi-session management |
| **File Management** | Remote file browsing, upload/download, online editing (Monaco Editor), batch operations |
| **Tunnel Proxy** | TCP port forwarding, SOCKS5 proxy, access internal resources via devices |
| **Security Encryption** | X25519 key exchange + AES-256-GCM end-to-end encryption, traffic obfuscation and padding |
| **Audit Logs** | User operation records, device activity tracking, security event auditing |

### Tech Stack

**Backend**
- Go 1.25
- Gin (HTTP framework)
- Gorilla WebSocket
- SQLite (data storage)
- gopsutil (system info collection)
- X25519 + AES-256-GCM (encryption)

**Frontend**
- React 18 + TypeScript
- Ant Design 5
- Vite 8
- xterm.js (terminal emulator)
- Monaco Editor (code editor)
- Zustand (state management)

### Project Structure

```
DBR-Nexus/
├── cmd/
│   ├── server/          # Server entry point
│   └── agent/           # Agent entry point
├── internal/
│   ├── agent/           # Agent core logic
│   │   ├── executor/    # Shell/process management
│   │   ├── filemanager/ # File operations
│   │   └── tunnel/      # Tunnel forwarding
│   ├── server/          # Server core logic
│   │   ├── auth/        # JWT/PSK authentication
│   │   └── database/    # Database operations
│   ├── crypto/          # Encryption module
│   ├── protocol/        # Communication protocol
│   └── logging/         # Logging module
├── web/                 # Frontend project
│   ├── src/
│   │   ├── pages/       # Page components
│   │   ├── components/  # Business components
│   │   └── api/         # API client
│   └── package.json
├── configs/             # Configuration files
└── tools/               # Utility scripts
```

### Quick Start

#### 1. Build Project

```bash
# Build server
go build -o dbr-server ./cmd/server/

# Build agent
go build -o dbr-agent ./cmd/agent/

# Build frontend
cd web && npm install && npm run build
```

#### 2. Configure Server

Edit `configs/server.yaml`:

```yaml
server:
  addr: ":8443"
  tls:
    cert: "certs/server.crt"
    key: "certs/server.key"

auth:
  jwt_secret: "your-secret-key"
  psk: "your-psk-key"

encryption:
  enabled: true
```

#### 3. Start Services

```bash
# Start server
./dbr-server configs/server.yaml

# Start agent (on managed devices)
./dbr-agent configs/agent.yaml
```

#### 4. Access Management Interface

Open browser at `https://your-server:8443`, login with default credentials:
- Username: `admin`
- Password: `admin`

### Configuration

#### Server Config (configs/server.yaml)

| Setting | Description |
|---------|-------------|
| `server.addr` | Listening address |
| `server.tls` | TLS certificate config |
| `auth.jwt_secret` | JWT signing secret |
| `auth.psk` | Agent pre-shared key |
| `tunnel.bind_range` | Tunnel port range |
| `encryption.enabled` | Enable encryption |

#### Agent Config (configs/agent.yaml)

| Setting | Description |
|---------|-------------|
| `server.url` | Server WebSocket URL |
| `server.psk` | Pre-shared key (must match server) |
| `heartbeat.interval` | Heartbeat interval |
| `device.id` | Device unique ID (optional, auto-generated) |

### Security Features

- **End-to-End Encryption**: X25519 key exchange, AES-256-GCM encrypts all communications
- **Traffic Obfuscation**: Message padding and timing jitter, prevents traffic fingerprinting
- **Dual Authentication**: PSK for Agent, JWT for Web users
- **IP Whitelist**: Web interface IP access control support
- **Audit Trail**: Complete user operation and device activity records

### Development

```bash
# Frontend dev mode
cd web && npm run dev

# Backend dev
go run ./cmd/server/ configs/server.yaml
```

### License

MIT License