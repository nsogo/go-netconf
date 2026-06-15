# go-netconf

[RFC 6241](https://tools.ietf.org/html/rfc6241) / [RFC 6242](https://tools.ietf.org/html/rfc6242) に基づく Go 言語用 NETCONF クライアントライブラリ。

本リポジトリはオリジナルライブラリを以下の機能で拡張している：

- **デバッグログ** — トランスポート層の Send/Receive/Timeout 挙動をトレース
- **netconf-mock** — 応答遅延を制御できる Docker ベースの NETCONF モックサーバー
- **netconf-client** — NETCONF RPC を1回実行する単独コマンド
- **タイムアウト再現ツール** — タイムアウト発生をループで再現・観測するスクリプト

---

## ライブラリの使い方

```go
import "github.com/Juniper/go-netconf/netconf"

s, err := netconf.DialSSH("192.168.1.1", netconf.SSHConfigPassword("admin", "admin"))
if err != nil {
    log.Fatal(err)
}
defer s.Close()

reply, err := s.Exec(netconf.RawMethod("<get/>"))
```

### デバッグログの有効化

接続前に `SetDebugLogger` を呼ぶことでデバッグログを有効にできる：

```go
import "os"

netconf.SetDebugLogger(os.Stderr)

s, err := netconf.DialSSHTimeout("192.168.1.1:830", config, 10*time.Second)
```

または `netconf-client` コマンドの `NETCONF_DEBUG=1` 環境変数でも有効にできる（後述）。

ログ出力の対象イベント：

| イベント | ログ内容 |
|---|---|
| SSH 接続 | `SSH Dial: connecting to host:port` |
| SSH セッション確立 | `SSH: creating new session` / `subsystem "netconf" established` |
| Hello 交換 | `Session: receiving server Hello` / `session-id=N` |
| RPC 送信 | `Send (N bytes): <rpc ...>` |
| RPC 受信 | `Receive: waiting for delimiter` / `Receive (N bytes): ...` |
| 読み取りループ | `WaitForFunc: read N bytes (total buffered: N)` |
| タイムアウト | `WaitForFunc read error: read tcp: i/o timeout` |
| セッションクローズ | `SSH: closing session` |

---

## netconf-client コマンド

NETCONF `<get/>` RPC を1回実行して終了する単独コマンド。

### ビルド

```bash
go build -o netconf-client ./cmd/netconf-client/
```

### 使い方

```bash
./netconf-client \
  --host localhost \
  --port 830 \
  --user admin \
  --password admin \
  --timeout 10s \
  --debug
```

| オプション | 説明 | デフォルト |
|---|---|---|
| `--host` | 接続先ホスト | `localhost` |
| `--port` | 接続先ポート | `830` |
| `--user` | SSH ユーザー名 | *（必須）* |
| `--password` | SSH パスワード | `""` |
| `--timeout` | `DialSSHTimeout` に渡すタイムアウト値 | `10s` |
| `--debug` | デバッグログを stderr に出力 | `false` |

終了コード：
- `0` — RPC 成功
- `1` — 接続失敗またはタイムアウト

`NETCONF_DEBUG=1` 環境変数は `--debug` フラグと同等。

---

## netconf-mock（Docker）

SSH 接続を受け付け、任意の RPC に `<ok/>` を返す Python ベースの NETCONF モックサーバー。
HTTP API で応答遅延を制御できるため、タイムアウトシナリオの再現に使用する。

### 起動

```bash
docker compose up -d netconf-mock
```

デフォルト認証情報：`admin` / `admin`、ポート：`830`

### HTTP 制御 API（ポート 8088）

| エンドポイント | メソッド | 説明 |
|---|---|---|
| `/` | GET | 利用可能なエンドポイント一覧 |
| `/set_use_delays` | POST | 応答遅延を有効化 |
| `/set_no_delays` | POST | 応答遅延を無効化 |
| `/delays_range` | POST | 遅延秒数を設定 |
| `/reset` | POST | 全状態を初期値にリセット |

```bash
# 応答遅延を15秒に設定
curl -X POST http://localhost:8088/set_use_delays
curl -X POST http://localhost:8088/delays_range \
  -H "Content-Type: application/json" \
  -d '{"delay": 15}'

# リセット
curl -X POST http://localhost:8088/reset
```

---

## タイムアウト再現ツール

`tools/run_netconf_scenario.sh` は、netconf-mock の起動・遅延設定・`netconf-client` のループ実行を一括で行うツールスクリプト。

### 使い方

```bash
./tools/run_netconf_scenario.sh [オプション]
```

| オプション | 説明 | デフォルト |
|---|---|---|
| `--mode` | `normal`（通常）/ `timeout`（タイムアウト再現） | `normal` |
| `--timeout` | `DialSSHTimeout` に渡すタイムアウト秒数 | `10` |
| `--interval` | ループ間隔（秒） | `60` |
| `--count` | ループ回数（`0` = 無限） | `3` |
| `--delay` | mock 応答遅延秒数（timeout モード時） | `15` |
| `--host` | 接続先ホスト | `localhost` |
| `--port` | NETCONF ポート | `830` |
| `--http-port` | mock HTTP 制御ポート | `8088` |
| `--user` | SSH ユーザー名 | `admin` |
| `--password` | SSH パスワード | `admin` |
| `--no-build` | `docker compose build` をスキップ | — |

### normal モード — 正常動作をループで確認

```bash
# デフォルト設定（タイムアウト10秒、60秒間隔、3回）
./tools/run_netconf_scenario.sh --mode normal

# タイムアウトを30秒に変えて挙動を比較
./tools/run_netconf_scenario.sh --mode normal --timeout 30 --count 3
```

### timeout モード — タイムアウト発生をループで再現

```bash
# timeout=10秒、mock遅延=15秒 → 毎回タイムアウト発生
./tools/run_netconf_scenario.sh --mode timeout --timeout 10 --delay 15 --count 3

# timeout=30秒、mock遅延=15秒 → 遅延がタイムアウト以内なので成功
./tools/run_netconf_scenario.sh --mode timeout --timeout 30 --delay 15 --count 3
```

### 出力例（timeout モード）

```
[2026-06-16 10:00:00] [INFO ] Starting netconf scenario (mode=timeout, count=3, interval=60s, timeout=10s)
[2026-06-16 10:00:00] [INFO ] Starting netconf-mock...
[2026-06-16 10:00:03] [INFO ] netconf-mock is ready
[2026-06-16 10:00:03] [INFO ] Enabling delays (mock delay=15s, NETCONF timeout=10s)
[2026-06-16 10:00:03] [INFO ] --- Iteration 1/3 ---
[NETCONF DEBUG] SSH Dial: connecting to localhost:830
[NETCONF DEBUG] SSH Dial: connected to localhost:830
[NETCONF DEBUG] SSH: subsystem "netconf" established
[NETCONF DEBUG] Session: server Hello received, session-id=12345
[NETCONF DEBUG] Exec: sending RPC message-id=xxxx
[NETCONF DEBUG] Receive: waiting for delimiter "]]>]]>"
[NETCONF DEBUG] WaitForFunc read error: read tcp: i/o timeout
[NETCONF DEBUG] Exec: Receive error: read tcp: i/o timeout
[ERROR] RPC failed: read tcp: i/o timeout
[2026-06-16 10:00:13] [ERROR] Iteration 1/3 FAILED (exit=1)
...
[2026-06-16 10:02:13] [INFO ] Scenario finished. Success: 0 / Failed: 3 / Total: 3
```

### タイムアウト閾値の比較実験

`--timeout` の値は `netconf.DialSSHTimeout()` に直接渡され、TCP レベルの `SetReadDeadline` として機能する。
異なる閾値での挙動の違いを確認できる：

```bash
# 閾値 < mock遅延 → 毎回タイムアウト
./tools/run_netconf_scenario.sh --mode timeout --timeout 5  --delay 15 --count 3

# 閾値 > mock遅延 → 毎回成功
./tools/run_netconf_scenario.sh --mode timeout --timeout 30 --delay 15 --count 3
```

---

## リポジトリ構成

```
.
├── netconf/
│   ├── debug.go           # SetDebugLogger / debugf
│   ├── session.go         # NETCONF セッション（Hello 交換・Exec）
│   ├── transport.go       # 基本 I/O（Send・Receive・WaitForFunc）
│   └── transport_ssh.go   # SSH トランスポート（Dial・deadlineConn）
├── cmd/
│   └── netconf-client/
│       └── main.go        # 単独実行コマンド
├── docker/
│   └── netconf-mock/
│       ├── netconf_server.py   # Python NETCONF モックサーバー
│       ├── Dockerfile
│       └── requirements.txt
├── tools/
│   └── run_netconf_scenario.sh  # タイムアウト再現ツールスクリプト
├── docker-compose.yml
└── examples/
    ├── ssh1/
    └── ssh2/
```
