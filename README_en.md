<div align="center">
  <img src="https://avatars.githubusercontent.com/u/245985800?s=200&v=4" alt="FasterEdge logo" width="100" />
  <h2>FasterEdgeDoctor</h2>
  <h3>Local and Remote Diagnostics for FasterEdge Repositories and Nodes</h3>
</div>

### 1. Introduction

FasterEdgeDoctor is a Go diagnostic tool for the FasterEdge core repository, MCU/FPGA port repositories and remote nodes. It depends only on the Go standard library.

### 2. Capabilities

- **Local checks**: discovers all `MCU-*` / `FPGA-*` variants; checks README, LICENSE, entry source files and project markers for PlatformIO, Keil, MounRiver, Vivado, MicroBlaze, Vitis HLS, SymbiFlow and other recognized toolchains; and runs `go test ./...` in `FasterEdge`.
- **Remote checks**: obtains the same report structure through the fixed, read-only `POST /v1/check` health endpoint.
- **Remote OneKey checks**: authenticates checks with `subject + issuedAt + expiresAt + HMAC-SHA256` credentials compatible with FasterEdge `OneKeyAbility`.
- **Server mode**: exposes local diagnostics through a read-only HTTP API for Doctor clients. It does not expose arbitrary Component/Command access and does not execute remote shells.
- **Machine-readable output**: `-json` emits stable JSON. A failed check exits with status 1, while invalid arguments exit with status 2.

### 3. Build

```bash
cd FasterEdgeDoctor
go test ./...
go build -o fasteredge-doctor ./cmd/fasteredge-doctor
```

### 4. Local Checks

```bash
./fasteredge-doctor -mode local -root ..
./fasteredge-doctor -mode local -root .. -json
```

Structural errors are marked `fail`. Unavailable external tools are reported as `warn/skip`; by default, Doctor neither downloads PlatformIO packages automatically nor runs proprietary tools such as Vivado.

### 5. Start the Remote Check Service

Without authentication; use this only on a trusted network or loopback interface:

```bash
./fasteredge-doctor -mode serve -root .. -listen 127.0.0.1:7080
```

With OneKey authentication:

```bash
printf '%s' '0123456789abcdef' > onekey.secret
chmod 600 onekey.secret
./fasteredge-doctor -mode serve -root .. -listen :7080 \
  -require-onekey -secret-file onekey.secret
```

Production deployments must provide HTTPS through a reverse proxy or gateway. The current FasterEdge OneKey is a reusable identity token during its validity period and is not bound to an HTTP request body. Use a short TTL and TLS, and revoke or rotate tokens promptly.

### 6. Remote Checks

```bash
./fasteredge-doctor -mode remote -endpoint http://127.0.0.1:7080
./fasteredge-doctor -mode onekey -endpoint https://edge.example.com \
  -token token.json
```

Example `token.json`:

```json
{
  "subject": "doctor-client",
  "issued_at": "2026-08-31T08:00:00Z",
  "expires_at": "2026-08-31T08:05:00Z",
  "signature": "base64url-hmac-sha256"
}
```

The signed payload matches FasterEdge:

```text
subject|issuedAt.UnixNano|expiresAt.UnixNano
```

### 7. HTTP Protocol

Request:

```http
POST /v1/check
Content-Type: application/json
Authorization: OneKey <base64url(JSON credential)>

{"scope":"health"}
```

- **Request limit**: the request body is limited to 8 KiB, and unknown fields are rejected.
- **Response limit**: the client reads at most 1 MiB from the response body.
- **Status codes**: `200` means healthy, `503` means the check completed but found problems, and `401` means authentication failed.
- **Narrow interface**: the API runs diagnostics only; it accepts no component, command or shell parameters.

### 8. Current Repository Diagnostic Note

Doctor validates FPGA projects according to their README declarations. If the current Artix-7 variant lacks `vivado/scripts/create_project.tcl` or `vivado/xdc/*.xdc`, Doctor reports the discrepancy as a failure instead of hiding the mismatch between documentation and project contents.
