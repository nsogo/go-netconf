# go-netconf

[RFC 6241](https://tools.ietf.org/html/rfc6241) / [RFC 6242](https://tools.ietf.org/html/rfc6242) に基づく Go 言語用 NETCONF クライアントライブラリ。

本リポジトリはオリジナルライブラリ（v0.1.1）を以下の機能で拡張している：

- **デバッグログ** — RPC の REQUEST/REPLY をトレース
- **netconf-mock** — 応答遅延・デリミタ制御ができる Docker ベースの NETCONF モックサーバー
- **netconf-client** — NETCONF RPC を1回実行する単独コマンド
- **タイムアウト再現ツール** — タイムアウトやデリミタ欠損をループで再現・観測するスクリプト

---

## ライブラリの使い方

```go
import "github.com/Juniper/go-netconf/netconf"

s, err := netconf.DialSSH("192.168.1.1:830", &ssh.ClientConfig{...})
if err != nil {
    log.Fatal(err)
}
defer s.Close()

reply, err := s.Exec(netconf.RawMethod("<get/>"))
```

### デバッグログの有効化

接続前に `SetLog` を呼ぶことでデバッグログを有効にできる：

```go
import (
    stdlog "log"
    "os"
)

netconf.SetLog(netconf.NewStdLog(
    stdlog.New(os.Stderr, "[NETCONF DEBUG] ", 0),
    netconf.LogDebug,
))

s, err := netconf.DialSSH("192.168.1.1:830", config)
```

または `netconf-client` コマンドの `NETCONF_DEBUG=1` 環境変数でも有効にできる（後述）。

ログ出力の対象イベント：

| イベント | ログ内容 |
|---|---|
| RPC 送信 | `[NETCONF DEBUG] REQUEST: <?xml ...><rpc ...>` |
| RPC 受信 | `[NETCONF DEBUG] REPLY: <rpc-reply ...>` |

---

## netconf-client コマンド

指定した NETCONF RPC を1回実行して終了する単独コマンド。

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
  --rpc '<get-vrrp-information><summary/></get-vrrp-information>' \
  --debug
```

| オプション | 説明 | デフォルト |
|---|---|---|
| `--host` | 接続先ホスト | `localhost` |
| `--port` | 接続先ポート | `830` |
| `--user` | SSH ユーザー名 | *（必須）* |
| `--password` | SSH パスワード | `""` |
| `--timeout` | RPC 応答待ちタイムアウト（`time.After()` + goroutine で実装） | `10s` |
| `--rpc` | 送信する NETCONF RPC XML | `<get-vrrp-information><summary/></get-vrrp-information>` |
| `--debug` | デバッグログを stderr に出力 | `false` |

終了コード：
- `0` — RPC 成功
- `1` — 接続失敗またはタイムアウト

`NETCONF_DEBUG=1` 環境変数は `--debug` フラグと同等。

---

## netconf-mock（Docker）

SSH 接続を受け付け、任意の RPC に応答する Python ベースの NETCONF モックサーバー。
HTTP API で応答遅延・デリミタ送信モードを制御できるため、タイムアウトやデリミタ欠損シナリオの再現に使用する。

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
| `/set_with_delimiter` | POST | `]]>]]>` デリミタ付き応答モードに戻す |
| `/set_split_delimiter` | POST | `]]>]]>` を2回に分けて送信するモードを有効化 |
| `/set_no_delimiter` | POST | `]]>]]>` デリミタなしで応答するモードを有効化 |
| `/vrrp_sessions` | POST | モックが返す VRRP セッションデータを設定 |
| `/reset` | POST | 全状態を初期値にリセット |

```bash
# 応答遅延を15秒に設定
curl -X POST http://localhost:8088/set_use_delays
curl -X POST http://localhost:8088/delays_range \
  -H "Content-Type: application/json" \
  -d '{"delay": 15}'

# VRRP セッションを任意のデータに差し替え
curl -X POST http://localhost:8088/vrrp_sessions \
  -H "Content-Type: application/json" \
  -d '[{"interface":"ae0.0","vrid":1,"state":"MASTER","rip":"192.168.0.1","vip":"192.168.0.254"}]'

# リセット
curl -X POST http://localhost:8088/reset
```

---

## タイムアウト再現ツール

`tools/run_netconf_scenario.sh` は、netconf-mock の起動・モード設定・`netconf-client` のループ実行を一括で行うツールスクリプト。

### 使い方

```bash
./tools/run_netconf_scenario.sh [オプション]
```

| オプション | 説明 | デフォルト |
|---|---|---|
| `--mode` | `normal` / `timeout` / `split-delimiter` / `no-delimiter` | `normal` |
| `--timeout` | RPC 応答待ちタイムアウト秒数 | `10` |
| `--interval` | ループ間隔（秒） | `60` |
| `--count` | ループ回数（`0` = 無限） | `3` |
| `--delay` | mock 応答遅延秒数（timeout モード時） | `15` |
| `--host` | 接続先ホスト | `localhost` |
| `--port` | NETCONF ポート | `830` |
| `--http-port` | mock HTTP 制御ポート | `8088` |
| `--user` | SSH ユーザー名 | `admin` |
| `--password` | SSH パスワード | `admin` |
| `--rpc` | 送信する NETCONF RPC XML | `<get-vrrp-information><summary/></get-vrrp-information>` |
| `--vrrp-sessions` | VRRP セッションデータ JSON ファイルパス（省略時は組み込みの2セッションを使用） | — |
| `--no-build` | `docker compose build` をスキップ | — |

### normal モード — 正常動作をループで確認

```bash
# デフォルト設定（タイムアウト10秒、60秒間隔、3回）
./tools/run_netconf_scenario.sh --mode normal

# タイムアウトを30秒に変えて挙動を比較
./tools/run_netconf_scenario.sh --mode normal --timeout 30 --count 3
```

### timeout モード — タイムアウト発生をループで再現

mock が指定秒数だけ応答を遅延させ、クライアントのタイムアウトが発火することを観測する。

```bash
# timeout=10秒、mock遅延=15秒 → 毎回タイムアウト発生
./tools/run_netconf_scenario.sh --mode timeout --timeout 10 --delay 15 --count 3

# timeout=30秒、mock遅延=15秒 → 遅延がタイムアウト以内なので成功
./tools/run_netconf_scenario.sh --mode timeout --timeout 30 --delay 15 --count 3
```

### split-delimiter モード — デリミタ分割送信の再現

mock が `]]>]]>` を2回に分けて TCP 送信する。`WaitForFunc` が分割されたデリミタを正しく組み立てられることを検証する。

```bash
./tools/run_netconf_scenario.sh --mode split-delimiter --timeout 10 --count 1
```

### no-delimiter モード — デリミタ欠損の再現

mock が `]]>]]>` を付けずに応答する。`WaitForFunc` がデリミタを見つけられずループし続け、タイムアウトで終了することを観測する。

```bash
./tools/run_netconf_scenario.sh --mode no-delimiter --timeout 10 --count 1
```

### 出力例（timeout モード）

```
[2026-06-16 10:00:00] [INFO ] Starting netconf scenario (mode=timeout, count=3, interval=60s, timeout=10s)
[2026-06-16 10:00:00] [INFO ] Starting netconf-mock...
[2026-06-16 10:00:03] [INFO ] netconf-mock is ready
[2026-06-16 10:00:03] [INFO ] Enabling delays (mock delay=15s, NETCONF timeout=10s)
[2026-06-16 10:00:03] [INFO ] --- Iteration 1/3 ---
[INFO] Connecting to localhost:830 (timeout=10s)
[INFO] Connected (session-id=12345)
[NETCONF DEBUG] REQUEST: <?xml version='1.0' encoding='UTF-8'?><rpc ...>
  ← ここで goroutine がブロック（mock は遅延応答中）
  ← timeout 秒後に time.After() が発火 → s.Close()
[ERROR] Timeout after 10s waiting for RPC reply
[2026-06-16 10:00:13] [ERROR] Iteration 1/3 FAILED (exit=1)
...
[2026-06-16 10:02:13] [INFO ] Scenario finished. Success: 0 / Failed: 3 / Total: 3
```

### タイムアウト閾値の比較実験

`--timeout` の値は RPC 応答待ちタイムアウトとして機能する（`time.After()` + goroutine で実装）。
異なる閾値での挙動の違いを確認できる：

```bash
# 閾値 < mock遅延 → 毎回タイムアウト
./tools/run_netconf_scenario.sh --mode timeout --timeout 5  --delay 15 --count 3

# 閾値 > mock遅延 → 毎回成功
./tools/run_netconf_scenario.sh --mode timeout --timeout 30 --delay 15 --count 3
```

---

## Junos 実機への接続

`netconf-client` は netconf-mock だけでなく、実際の Junos ルーターに対しても使用できる。
ただし以下の点に注意すること。

### Junos 側の事前設定

NETCONF over SSH を有効にするには、Junos 側で以下の設定が必要：

```
# NETCONF SSH サービスを有効化
set system services netconf ssh

# NETCONF 接続を許可するユーザーの作成（例）
set system login user netconf-user class super-user authentication plain-text-password
```

設定確認：

```bash
show system services
# 出力に "netconf { ssh; }" が含まれていれば有効
```

### 接続コマンド例

```bash
# Junos ルーターへの接続（ポート 830）
./netconf-client \
  --host 192.168.1.1 \
  --port 830 \
  --user admin \
  --password <パスワード> \
  --timeout 30s \
  --debug
```

> **ポートについて**: Junos の NETCONF デフォルトポートは `830`。
> 環境によっては `22`（通常 SSH ポート）でも NETCONF subsystem として接続できる場合がある。

### 既知の制限事項

| 制限 | 内容 | 対処 |
|------|------|------|
| **SSH ホスト鍵検証なし** | `InsecureIgnoreHostKey()` を使用しており、中間者攻撃に対して無防備 | 本ツールは開発・検証用途に限定する |
| **パスワード認証のみ** | `--password` フラグのみ対応。SSH 公開鍵認証は未対応 | ライブラリの `SSHConfigPubKeyFile` / `SSHConfigPubKeyAgent` を直接使用する場合は別途実装が必要 |

### タイムアウト値の目安

実機では、設定データ量や機器の負荷によって応答時間が異なる。

| 環境 | 推奨 `--timeout` |
|------|-----------------|
| netconf-mock（ローカル）| `10s` |
| Junos（設定量少） | `30s` |
| Junos（設定量多・高負荷） | `60s` 以上 |

---

## リポジトリ構成

```
.
├── netconf/
│   ├── log.go             # SetLog / Logger インターフェース
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
│   └── run_netconf_scenario.sh  # シナリオ実行ツール
├── docker-compose.yml
└── examples/
    ├── ssh1/
    └── ssh2/
```
