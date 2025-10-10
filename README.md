# P3Y - Simple Reverse Proxy

一个轻量级、高性能的 Go 反向代理服务器，支持 TLS、BasicAuth 和 Prometheus 指标监控。

## 特性

- ✅ **反向代理**：高效转发 HTTP/HTTPS 请求到后端服务器
- ✅ **认证**：内置 BasicAuth 支持
- ✅ **TLS**：支持 TLS/HTTPS 加密传输
- ✅ **监控**：集成 Prometheus 指标（请求数、延迟、认证失败等）
- ✅ **优雅关闭**：支持 SIGINT/SIGTERM 信号优雅关闭
- ✅ **结构化日志**：使用 Uber Zap 提供高性能结构化日志

## 快速开始

### 安装

```bash
go install github.com/dbds-team/my_p3y@latest
```

或从源码编译：

```bash
git clone https://github.com/dbds-team/my_p3y.git
cd my_p3y
go build -o p3y .
```

### 基本使用

```bash
# 启动代理，转发到 http://localhost:8080
./p3y --backend http://localhost:8080

# 使用 BasicAuth 认证
./p3y --backend http://api.example.com --username admin --password secret

# 启用 TLS
./p3y --backend https://api.example.com --tls --crt ./server.crt --key ./server.key
```

## 配置

### 命令行参数

| 参数 | 环境变量 | 默认值 | 说明 |
|------|---------|--------|------|
| `--backend` | `BACKEND` | `http://example.com:80` | 后端服务器地址 |
| `--ip` | `IP` | `0.0.0.0` | 监听 IP 地址 |
| `--port` | `PORT` | `8080` | 监听端口 |
| `--metrics_port` | `METRICS_PORT` | `2112` | Prometheus 指标端口 |
| `--username` | `USERNAME` | - | BasicAuth 用户名 |
| `--password` | `PASSWORD` | - | BasicAuth 密码 |
| `--tls` | `TLS` | `false` | 启用 TLS |
| `--crt` | `CRT` | `./example.crt` | TLS 证书路径 |
| `--key` | `KEY` | `./example.key` | TLS 私钥路径 |
| `--skip-verify` | `SKIP_VERIFY` | `false` | 跳过后端 TLS 验证 |
| `--logout` | `LOGOUT` | `stdout` | 日志输出路径 |
| `--tlsCfg` | `TLSCFG` | - | TLS 配置文件路径 |

### 环境变量示例

```bash
export BACKEND=https://api.example.com
export USERNAME=admin
export PASSWORD=secret123
export PORT=8443
export TLS=true

./p3y
```

## Prometheus 指标

访问 `http://localhost:2112/metrics` 获取以下指标：

- `p3y_total_requests` - 总请求数
- `p3y_total_authentication_failures` - 认证失败次数
- `p3y_response_time` - 响应延迟（Summary）

## 架构设计

本项目严格遵循以下编程原则：

- **KISS**：保持简单，使用标准库 `r.BasicAuth()` 而非手动实现
- **SOLID**：
  - **SRP**：每个组件单一职责（Config、Metrics、Logger 等）
  - **ISP**：定义清晰的 `ProxyHandler` 接口
  - **DIP**：依赖注入认证中间件
- **DRY**：消除重复代码，统一配置管理

### 目录结构

```
.
├── p3y.go              # 主程序
├── go.mod              # Go 模块定义
├── go.sum              # 依赖校验和
├── README.md           # 说明文档
├── LICENSE             # 许可证
└── .github/
    └── workflows/
        └── release.yml # 自动构建和发布
```

## 开发

### 运行测试

```bash
go test ./...
```

### 本地开发

```bash
# 安装依赖
go mod download

# 运行
go run p3y.go --backend http://localhost:3000

# 构建
go build -o p3y .
```

## 许可证

本项目采用 Apache 2.0 许可证 - 详见 [LICENSE](LICENSE) 文件

## 贡献

欢迎贡献！请遵循以下步骤：

1. Fork 本仓库
2. 创建特性分支 (`git checkout -b feature/AmazingFeature`)
3. 提交更改 (`git commit -m 'Add some AmazingFeature'`)
4. 推送到分支 (`git push origin feature/AmazingFeature`)
5. 开启 Pull Request

## 作者

DBDS Team

## 致谢

- [httputil](https://pkg.go.dev/net/http/httputil) - Go 标准库反向代理
- [Prometheus](https://prometheus.io/) - 监控和指标
- [Zap](https://github.com/uber-go/zap) - 高性能日志库
