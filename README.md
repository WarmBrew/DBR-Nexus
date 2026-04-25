# DBR-Nexus

[English](#english) | [中文](#中文)

---

<a name="中文"></a>

## 中文

### 项目简介

DBR-Nexus 是一个现代化的远程设备管理平台，采用 Server-Agent 架构，支持大规模设备的集中监控与运维管理。通过 WebSocket 实现实时通信，提供端到端加密、远程 Shell、文件管理、隧道代理等核心功能。

### 功能特性

#### 设备管理
- **实时监控**: 设备在线状态、心跳检测、自动离线标记
- **系统信息**: CPU、内存、磁盘、网络接口、系统版本、内核信息
- **设备详情**: 主机名、IP 地址、操作系统、架构、连接时间
- **设备标签**: 支持自定义标签和备注，方便分组管理

#### 远程终端
- **Web 终端**: 基于 xterm.js 的完整终端体验
- **多会话**: 支持同时打开多个终端会话
- **实时交互**: 低延迟的键盘输入和终端输出
- **会话管理**: 终端会话状态跟踪和管理

#### 文件管理
- **文件浏览**: 树形目录结构，支持文件夹展开/折叠
- **文件操作**: 上传、下载、创建、删除、移动、重命名
- **在线编辑**: 基于 Monaco Editor 的代码编辑器，支持语法高亮
- **批量操作**: 支持批量上传和目录下载
- **分块上传**: 大文件分块上传，支持断点续传
- **文件搜索**: 按文件名搜索设备上的文件

#### 隧道代理
- **TCP 端口转发**: 将设备端口映射到服务器，支持临时和永久隧道
- **SOCKS5 代理**: 通过设备访问内网资源
- **IP 白名单**: SOCKS5 代理支持 IP 白名单控制
- **自动超时**: 空闲隧道自动关闭，节省资源
- **隧道管理**: 创建、查看、续期、删除隧道

#### 安全加密
- **端到端加密**: X25519 密钥交换 + AES-256-GCM 加密
- **流量混淆**: 消息填充（128B-16KB）和定时抖动
- **双向认证**: Agent 使用 PSK 认证，Web 用户使用 JWT 认证
- **IP 白名单**: Web 管理界面支持 IP 访问控制
- **权限系统**: 基于角色的权限控制（Shell、文件、隧道、用户管理等）

#### 审计日志
- **操作记录**: 用户登录、设备操作、文件访问、隧道创建
- **安全事件**: 认证失败、权限拒绝、异常行为
- **设备活动**: 设备上线/下线、心跳记录

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

#### 1. 环境要求

- Go 1.25+
- Node.js 18+
- npm 或 yarn

#### 2. 构建项目

```bash
# 构建服务端
go build -o dbr-server ./cmd/server/

# 构建 Agent
go build -o dbr-agent ./cmd/agent/

# 构建前端
cd web && npm install && npm run build
```

#### 3. 生成 TLS 证书（可选但推荐）

```bash
# 使用内置工具生成自签名证书
go run tools/gencert.go -host your-server-domain -out certs/
```

#### 4. 配置服务端

编辑 `configs/server.yaml`：

```yaml
server:
  addr: ":8443"
  tls:
    cert: "certs/server.crt"
    key: "certs/server.key"

auth:
  jwt_secret: "your-secret-key"  # 生产环境必须修改
  jwt_expiry: 24h
  psk: "your-psk-key"           # 生产环境必须修改

database:
  path: "./data/qoder.db"

encryption:
  enabled: true
  min_frame_size: 128
  timing_jitter_percent: 15
```

#### 5. 启动服务

```bash
# 启动服务端
./dbr-server configs/server.yaml

# 启动 Agent（在被管理设备上）
./dbr-agent configs/agent.yaml
```

#### 6. 访问管理界面

打开浏览器访问 `https://your-server:8443`，使用默认账号登录：
- 用户名: `admin`
- 密码: `admin`

**⚠️ 首次登录后请立即修改默认密码！**

### 配置说明

#### 服务端配置 (configs/server.yaml)

| 配置项 | 说明 | 默认值 |
|-------|------|-------|
| `server.addr` | 监听地址 | `:8443` |
| `server.tls.cert` | TLS 证书路径 | 空（不使用 TLS） |
| `server.tls.key` | TLS 私钥路径 | 空（不使用 TLS） |
| `database.path` | SQLite 数据库路径 | `./data/qoder.db` |
| `auth.jwt_secret` | JWT 签名密钥 | 必须修改 |
| `auth.jwt_expiry` | JWT 过期时间 | `24h` |
| `auth.psk` | Agent 认证预共享密钥 | 必须修改 |
| `tunnel.bind_range` | 隧道端口范围 | `127.0.0.1:10000-20000` |
| `tunnel.port_forward_timeout` | 端口转发超时 | `30m` |
| `tunnel.socks5_timeout` | SOCKS5 超时 | `1h` |
| `agent.heartbeat_timeout` | 心跳超时 | `90s` |
| `encryption.enabled` | 是否启用加密 | `true` |
| `encryption.min_frame_size` | 最小帧大小 | `128` |
| `encryption.timing_jitter_percent` | 定时抖动百分比 | `15` |
| `web_access_control.enabled` | Web IP 白名单开关 | `false` |
| `web_access_control.allowed_ips` | 允许的 IP/CIDR | 空 |

#### Agent 配置 (configs/agent.yaml)

| 配置项 | 说明 | 默认值 |
|-------|------|-------|
| `server.url` | 服务端 WebSocket 地址 | `ws://localhost:8443/ws/agent` |
| `server.psk` | 预共享密钥（需与服务端一致） | 必须配置 |
| `server.reconnect.initial_delay` | 初始重连延迟 | `1s` |
| `server.reconnect.max_delay` | 最大重连延迟 | `60s` |
| `server.reconnect.multiplier` | 退避乘数 | `2.0` |
| `heartbeat.interval` | 心跳间隔 | `30s` |
| `device.id` | 设备唯一标识 | 自动生成 |
| `device.labels` | 设备标签 | `[]` |
| `device.tags` | 设备标签 | `[]` |
| `tunnel.max_connections` | 最大隧道连接数 | `100` |
| `encryption.enabled` | 是否启用加密 | `true` |

#### 编译时嵌入配置

Agent 支持在编译时嵌入服务器地址和 PSK：

```bash
go build -ldflags "-X main.defaultServerURL=ws://1.2.3.4:8443/ws/agent -X main.defaultPSK=mypsk" ./cmd/agent/
```

### API 文档

#### 认证接口

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/v1/auth/login` | 用户登录 |
| POST | `/api/v1/auth/refresh` | 刷新 JWT Token |

#### 设备接口

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/devices` | 获取设备列表 |
| GET | `/api/v1/devices/:id` | 获取设备详情 |
| PUT | `/api/v1/devices/:id/notes` | 更新设备备注 |
| DELETE | `/api/v1/devices/:id` | 删除设备 |
| GET | `/api/v1/devices/:id/system` | 获取系统信息 |
| GET | `/api/v1/devices/:id/processes` | 获取进程列表 |
| POST | `/api/v1/devices/:id/processes/:pid/kill` | 终止进程 |

#### 文件接口

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/devices/:id/files` | 浏览文件目录 |
| GET | `/api/v1/devices/:id/files/content` | 读取文件内容 |
| PUT | `/api/v1/devices/:id/files/content` | 写入文件内容 |
| POST | `/api/v1/devices/:id/files/upload` | 上传文件 |
| GET | `/api/v1/devices/:id/files/download` | 下载文件 |
| GET | `/api/v1/devices/:id/files/download/dir` | 下载目录 |
| POST | `/api/v1/devices/:id/files/mkdir` | 创建目录 |
| DELETE | `/api/v1/devices/:id/files` | 删除文件 |
| POST | `/api/v1/devices/:id/files/move` | 移动/重命名文件 |
| POST | `/api/v1/devices/:id/files/chmod` | 修改文件权限 |
| GET | `/api/v1/devices/:id/files/search` | 搜索文件 |

#### 隧道接口

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/tunnels` | 获取隧道列表 |
| POST | `/api/v1/tunnels` | 创建隧道 |
| DELETE | `/api/v1/tunnels/:id` | 删除隧道 |
| PUT | `/api/v1/tunnels/:id/renew` | 续期隧道 |
| POST | `/api/v1/tunnels/socks5` | 创建 SOCKS5 代理 |

#### 用户接口

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/users` | 获取用户列表 |
| POST | `/api/v1/users` | 创建用户 |
| PUT | `/api/v1/users/:id` | 更新用户 |
| DELETE | `/api/v1/users/:id` | 删除用户 |

#### 审计接口

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/audit-logs` | 获取审计日志 |

#### WebSocket 接口

| 路径 | 说明 |
|------|------|
| `/ws/agent` | Agent WebSocket 连接 |
| `/ws/browser` | 浏览器 WebSocket 连接 |

---

### 安全特性

- **端到端加密**: 使用 X25519 进行密钥交换，AES-256-GCM 加密所有通信
- **流量混淆**: 消息填充（128B-16KB）与定时抖动（±15%），防止流量特征分析
- **双向认证**: Agent 使用 PSK 认证，Web 用户使用 JWT 认证
- **IP 白名单**: Web 管理界面支持 IP 访问控制，Agent 连接不受限制
- **权限系统**: 基于角色的权限控制
  - `shell`: 远程终端访问
  - `file_browse`: 文件浏览
  - `file_write`: 文件写入
  - `process_kill`: 进程终止
  - `tunnel_create`: 隧道创建
  - `delete_devices`: 设备删除
  - `manage_users`: 用户管理
  - `view_audit_logs`: 审计日志查看
- **审计追踪**: 完整记录用户操作与设备活动

### 开发指南

#### 前端开发

```bash
cd web
npm install
npm run dev
```

前端将运行在 `http://localhost:5173`，自动代理 API 请求到后端。

#### 后端开发

```bash
go run ./cmd/server/ configs/server.yaml
```

#### 交叉编译

```bash
# Linux amd64
GOOS=linux GOARCH=amd64 go build -o dbr-server-linux-amd64 ./cmd/server/
GOOS=linux GOARCH=amd64 go build -o dbr-agent-linux-amd64 ./cmd/agent/

# Windows amd64
GOOS=windows GOARCH=amd64 go build -o dbr-server-windows-amd64.exe ./cmd/server/
GOOS=windows GOARCH=amd64 go build -o dbr-agent-windows-amd64.exe ./cmd/agent/

# macOS amd64
GOOS=darwin GOARCH=amd64 go build -o dbr-server-darwin-amd64 ./cmd/server/
GOOS=darwin GOARCH=amd64 go build -o dbr-agent-darwin-amd64 ./cmd/agent/
```

### 部署建议

#### 生产环境检查清单

- [ ] 修改默认 JWT 密钥 (`auth.jwt_secret`)
- [ ] 修改默认 PSK (`auth.psk`)
- [ ] 启用 TLS 加密
- [ ] 修改默认管理员密码
- [ ] 配置 Web IP 白名单 (`web_access_control`)
- [ ] 配置日志轮转参数
- [ ] 设置合理的隧道超时时间
- [ ] 使用 systemd 或 supervisor 管理服务

#### systemd 服务示例

```ini
[Unit]
Description=DBR-Nexus Server
After=network.target

[Service]
Type=simple
User=dbr
Group=dbr
WorkingDirectory=/opt/dbr-nexus
ExecStart=/opt/dbr-nexus/dbr-server /opt/dbr-nexus/configs/server.yaml
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=multi-user.target
```

#### Docker 部署（可选）

```dockerfile
FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o dbr-server ./cmd/server/

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /app/dbr-server .
COPY configs/ ./configs/
EXPOSE 8443
CMD ["./dbr-server", "configs/server.yaml"]
```

### 许可证

MIT License

---

<a name="english"></a>

## English

### Overview

DBR-Nexus is a modern remote device management platform using a Server-Agent architecture. It supports centralized monitoring and operations management for large-scale devices. Real-time communication via WebSocket provides end-to-end encryption, remote shell, file management, tunnel proxy, and other core features.

### Features

#### Device Management
- **Real-time Monitoring**: Device online status, heartbeat detection, automatic offline marking
- **System Info**: CPU, Memory, Disk, Network interfaces, OS version, Kernel info
- **Device Details**: Hostname, IP address, OS, Architecture, connection time
- **Device Tags**: Custom tags and notes for grouping

#### Remote Terminal
- **Web Terminal**: Full terminal experience based on xterm.js
- **Multi-Session**: Support multiple terminal sessions simultaneously
- **Real-time Interaction**: Low-latency keyboard input and terminal output
- **Session Management**: Terminal session state tracking and management

#### File Management
- **File Browser**: Tree directory structure with folder expand/collapse
- **File Operations**: Upload, download, create, delete, move, rename
- **Online Editor**: Code editor based on Monaco Editor with syntax highlighting
- **Batch Operations**: Batch upload and directory download
- **Chunked Upload**: Large file chunked upload with resume support
- **File Search**: Search files by name on devices

#### Tunnel Proxy
- **TCP Port Forwarding**: Map device ports to server, support temporary and permanent tunnels
- **SOCKS5 Proxy**: Access internal resources via devices
- **IP Whitelist**: SOCKS5 proxy supports IP whitelist control
- **Auto Timeout**: Idle tunnels automatically close to save resources
- **Tunnel Management**: Create, view, renew, delete tunnels

#### Security Encryption
- **End-to-End Encryption**: X25519 key exchange + AES-256-GCM encryption
- **Traffic Obfuscation**: Message padding (128B-16KB) and timing jitter
- **Dual Authentication**: Agent uses PSK authentication, Web users use JWT
- **IP Whitelist**: Web management interface supports IP access control
- **Permission System**: Role-based permission control (Shell, File, Tunnel, User management)

#### Audit Logs
- **Operation Records**: User login, device operations, file access, tunnel creation
- **Security Events**: Authentication failure, permission denied, abnormal behavior
- **Device Activity**: Device online/offline, heartbeat records

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

#### 1. Requirements

- Go 1.25+
- Node.js 18+
- npm or yarn

#### 2. Build Project

```bash
# Build server
go build -o dbr-server ./cmd/server/

# Build agent
go build -o dbr-agent ./cmd/agent/

# Build frontend
cd web && npm install && npm run build
```

#### 3. Generate TLS Certificate (Optional but Recommended)

```bash
# Generate self-signed certificate using built-in tool
go run tools/gencert.go -host your-server-domain -out certs/
```

#### 4. Configure Server

Edit `configs/server.yaml`:

```yaml
server:
  addr: ":8443"
  tls:
    cert: "certs/server.crt"
    key: "certs/server.key"

auth:
  jwt_secret: "your-secret-key"  # MUST change in production
  jwt_expiry: 24h
  psk: "your-psk-key"           # MUST change in production

database:
  path: "./data/qoder.db"

encryption:
  enabled: true
  min_frame_size: 128
  timing_jitter_percent: 15
```

#### 5. Start Services

```bash
# Start server
./dbr-server configs/server.yaml

# Start agent (on managed devices)
./dbr-agent configs/agent.yaml
```

#### 6. Access Management Interface

Open browser at `https://your-server:8443`, login with default credentials:
- Username: `admin`
- Password: `admin`

**⚠️ Please change the default password immediately after first login!**

### Configuration

#### Server Config (configs/server.yaml)

| Setting | Description | Default |
|---------|-------------|---------|
| `server.addr` | Listening address | `:8443` |
| `server.tls.cert` | TLS certificate path | Empty (no TLS) |
| `server.tls.key` | TLS private key path | Empty (no TLS) |
| `database.path` | SQLite database path | `./data/qoder.db` |
| `auth.jwt_secret` | JWT signing secret | MUST change |
| `auth.jwt_expiry` | JWT expiry duration | `24h` |
| `auth.psk` | Agent pre-shared key | MUST change |
| `tunnel.bind_range` | Tunnel port range | `127.0.0.1:10000-20000` |
| `tunnel.port_forward_timeout` | Port forward timeout | `30m` |
| `tunnel.socks5_timeout` | SOCKS5 timeout | `1h` |
| `agent.heartbeat_timeout` | Heartbeat timeout | `90s` |
| `encryption.enabled` | Enable encryption | `true` |
| `encryption.min_frame_size` | Minimum frame size | `128` |
| `encryption.timing_jitter_percent` | Timing jitter percent | `15` |
| `web_access_control.enabled` | Web IP whitelist toggle | `false` |
| `web_access_control.allowed_ips` | Allowed IPs/CIDRs | Empty |

#### Agent Config (configs/agent.yaml)

| Setting | Description | Default |
|---------|-------------|---------|
| `server.url` | Server WebSocket URL | `ws://localhost:8443/ws/agent` |
| `server.psk` | Pre-shared key (must match server) | MUST configure |
| `server.reconnect.initial_delay` | Initial reconnect delay | `1s` |
| `server.reconnect.max_delay` | Max reconnect delay | `60s` |
| `server.reconnect.multiplier` | Backoff multiplier | `2.0` |
| `heartbeat.interval` | Heartbeat interval | `30s` |
| `device.id` | Device unique ID | Auto-generated |
| `device.labels` | Device labels | `[]` |
| `device.tags` | Device tags | `[]` |
| `tunnel.max_connections` | Max tunnel connections | `100` |
| `encryption.enabled` | Enable encryption | `true` |

#### Build-time Configuration

Agent supports embedding server URL and PSK at build time:

```bash
go build -ldflags "-X main.defaultServerURL=ws://1.2.3.4:8443/ws/agent -X main.defaultPSK=mypsk" ./cmd/agent/
```

### API Documentation

#### Authentication

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/v1/auth/login` | User login |
| POST | `/api/v1/auth/refresh` | Refresh JWT Token |

#### Devices

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/devices` | List devices |
| GET | `/api/v1/devices/:id` | Get device details |
| PUT | `/api/v1/devices/:id/notes` | Update device notes |
| DELETE | `/api/v1/devices/:id` | Delete device |
| GET | `/api/v1/devices/:id/system` | Get system info |
| GET | `/api/v1/devices/:id/processes` | List processes |
| POST | `/api/v1/devices/:id/processes/:pid/kill` | Kill process |

#### Files

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/devices/:id/files` | Browse directory |
| GET | `/api/v1/devices/:id/files/content` | Read file content |
| PUT | `/api/v1/devices/:id/files/content` | Write file content |
| POST | `/api/v1/devices/:id/files/upload` | Upload file |
| GET | `/api/v1/devices/:id/files/download` | Download file |
| GET | `/api/v1/devices/:id/files/download/dir` | Download directory |
| POST | `/api/v1/devices/:id/files/mkdir` | Create directory |
| DELETE | `/api/v1/devices/:id/files` | Delete file |
| POST | `/api/v1/devices/:id/files/move` | Move/rename file |
| POST | `/api/v1/devices/:id/files/chmod` | Change file permissions |
| GET | `/api/v1/devices/:id/files/search` | Search files |

#### Tunnels

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/tunnels` | List tunnels |
| POST | `/api/v1/tunnels` | Create tunnel |
| DELETE | `/api/v1/tunnels/:id` | Delete tunnel |
| PUT | `/api/v1/tunnels/:id/renew` | Renew tunnel |
| POST | `/api/v1/tunnels/socks5` | Create SOCKS5 proxy |

#### Users

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/users` | List users |
| POST | `/api/v1/users` | Create user |
| PUT | `/api/v1/users/:id` | Update user |
| DELETE | `/api/v1/users/:id` | Delete user |

#### Audit

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/audit-logs` | Get audit logs |

#### WebSocket

| Path | Description |
|------|-------------|
| `/ws/agent` | Agent WebSocket connection |
| `/ws/browser` | Browser WebSocket connection |

---

### Security Features

- **End-to-End Encryption**: X25519 key exchange, AES-256-GCM encrypts all communications
- **Traffic Obfuscation**: Message padding (128B-16KB) and timing jitter (±15%), prevents traffic fingerprinting
- **Dual Authentication**: PSK for Agent, JWT for Web users
- **IP Whitelist**: Web interface IP access control, Agent connections不受限制
- **Permission System**: Role-based permission control
  - `shell`: Remote terminal access
  - `file_browse`: File browsing
  - `file_write`: File writing
  - `process_kill`: Process termination
  - `tunnel_create`: Tunnel creation
  - `delete_devices`: Device deletion
  - `manage_users`: User management
  - `view_audit_logs`: Audit log viewing
- **Audit Trail**: Complete user operation and device activity records

### Development

#### Frontend Development

```bash
cd web
npm install
npm run dev
```

Frontend will run at `http://localhost:5173`, automatically proxying API requests to backend.

#### Backend Development

```bash
go run ./cmd/server/ configs/server.yaml
```

#### Cross Compilation

```bash
# Linux amd64
GOOS=linux GOARCH=amd64 go build -o dbr-server-linux-amd64 ./cmd/server/
GOOS=linux GOARCH=amd64 go build -o dbr-agent-linux-amd64 ./cmd/agent/

# Windows amd64
GOOS=windows GOARCH=amd64 go build -o dbr-server-windows-amd64.exe ./cmd/server/
GOOS=windows GOARCH=amd64 go build -o dbr-agent-windows-amd64.exe ./cmd/agent/

# macOS amd64
GOOS=darwin GOARCH=amd64 go build -o dbr-server-darwin-amd64 ./cmd/server/
GOOS=darwin GOARCH=amd64 go build -o dbr-agent-darwin-amd64 ./cmd/agent/
```

### Deployment Guide

#### Production Checklist

- [ ] Change default JWT secret (`auth.jwt_secret`)
- [ ] Change default PSK (`auth.psk`)
- [ ] Enable TLS encryption
- [ ] Change default admin password
- [ ] Configure Web IP whitelist (`web_access_control`)
- [ ] Configure log rotation parameters
- [ ] Set reasonable tunnel timeout
- [ ] Use systemd or supervisor to manage services

#### systemd Service Example

```ini
[Unit]
Description=DBR-Nexus Server
After=network.target

[Service]
Type=simple
User=dbr
Group=dbr
WorkingDirectory=/opt/dbr-nexus
ExecStart=/opt/dbr-nexus/dbr-server /opt/dbr-nexus/configs/server.yaml
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=multi-user.target
```

#### Docker Deployment (Optional)

```dockerfile
FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o dbr-server ./cmd/server/

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /app/dbr-server .
COPY configs/ ./configs/
EXPOSE 8443
CMD ["./dbr-server", "configs/server.yaml"]
```

### License

MIT License