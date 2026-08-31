# FasterEdgeDoctor

FasterEdgeDoctor 是 FasterEdge 主仓库、MCU/FPGA 移植仓库和远端节点的 Go 诊断工具。仅依赖 Go 标准库。

## 能力

- **本地检查**：发现全部 `MCU-*` / `FPGA-*` 版本，检查 README、LICENSE、入口源码及 PlatformIO、Keil、MounRiver、Vivado、MicroBlaze、Vitis HLS、SymbiFlow 等工程标记，并运行 `FasterEdge/go test ./...`。
- **远程检查**：通过固定的只读 `POST /v1/check` 健康接口获取相同结构的报告。
- **远程 OneKey 检查**：使用与 FasterEdge `OneKeyAbility` 相容的 `subject + issuedAt + expiresAt + HMAC-SHA256` 凭据认证后检查。
- **服务端**：把本地检查以只读 HTTP API 暴露给 Doctor 客户端；不会暴露任意 Component/Command，更不会执行远程 shell。
- **机器输出**：`-json` 输出稳定 JSON，检查失败进程退出码为 1，参数错误为 2。

## 构建

```bash
cd FasterEdgeDoctor
go test ./...
go build -o fasteredge-doctor ./cmd/fasteredge-doctor
```

## 本地检查

```bash
./fasteredge-doctor -mode local -root ..
./fasteredge-doctor -mode local -root .. -json
```

结构错误会被标记为 `fail`。外部工具不可用属于 `warn/skip`；默认不会自动下载 PlatformIO 包或运行 Vivado 等专有工具。

## 启动远程检查服务

无认证（只应在受信网络或 loopback 使用）：

```bash
./fasteredge-doctor -mode serve -root .. -listen 127.0.0.1:7080
```

OneKey 认证：

```bash
printf '%s' '0123456789abcdef' > onekey.secret
chmod 600 onekey.secret
./fasteredge-doctor -mode serve -root .. -listen :7080 \
  -require-onekey -secret-file onekey.secret
```

生产环境必须使用反向代理或网关提供 HTTPS。当前 FasterEdge OneKey 是有效期内可复用的身份令牌，不绑定 HTTP 请求体，因此应使用短 TTL、TLS，并及时吊销/轮换。

## 远程检查

```bash
./fasteredge-doctor -mode remote -endpoint http://127.0.0.1:7080
./fasteredge-doctor -mode onekey -endpoint https://edge.example.com \
  -token token.json
```

`token.json`：

```json
{
  "subject": "doctor-client",
  "issued_at": "2026-08-31T08:00:00Z",
  "expires_at": "2026-08-31T08:05:00Z",
  "signature": "base64url-hmac-sha256"
}
```

签名载荷与 FasterEdge 一致：

```text
subject|issuedAt.UnixNano|expiresAt.UnixNano
```

## HTTP 协议

请求：

```http
POST /v1/check
Content-Type: application/json
Authorization: OneKey <base64url(JSON credential)>

{"scope":"health"}
```

- 请求体限制 8 KiB，拒绝未知字段。
- 响应体客户端最多读取 1 MiB。
- `200` 表示健康，`503` 表示检查完成但发现问题，`401` 表示认证失败。
- 此 API 只运行诊断，不接受 component、command 或 shell 参数。

## 当前仓库诊断提示

Doctor 会按 README 声明检查 FPGA 工程。当前 Artix-7 版本若缺少 `vivado/scripts/create_project.tcl` 或 `vivado/xdc/*.xdc`，会按实际情况报告失败，而不是隐藏文档与工程不一致。
