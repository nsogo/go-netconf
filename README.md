# go-netconf

[RFC 6241](https://tools.ietf.org/html/rfc6241) / [RFC 6242](https://tools.ietf.org/html/rfc6242) に基づく Go 言語用 NETCONF クライアントライブラリ。

本リポジトリはオリジナルライブラリ（v0.1.1）を以下の機能で拡張している：

- **デバッグログ** — RPC の REQUEST/REPLY やbufferをトレースし、ログファイルを出力
- **netconf-client** — 複数の NETCONF RPC を並列実行する単独コマンド

---

## netconf-client コマンド

指定した NETCONF RPC を並列実行して終了する単独コマンド。

実行結果は stdout/stderr に加え、タイムスタンプ付きのログファイル（`netconf_YYYY_MM_DD_hh_mm_ss.log`）に自動的に書き出される。

### ビルド

```bash
go build -o netconf-client ./cmd/netconf-client/

# mac osでLinux amd64 向けにbuildする場合はこちら
GOOS=linux GOARCH=amd64 go build -o netconf-client ./cmd/netconf-client/
```

### 使い方
以下のコマンドで実行できる。

```bash
./netconf-client \
  --host <ホスト> \
  --port 830 \
  --user <ユーザー名> \
  --password <パスワード> \
  --timeout 60s \
  --debug \
  --rpc '<get-firewall-filter-information><filtername>INET_IN</filtername></get-firewall-filter-information>' \
  --rpc '<get-firewall-filter-information><filtername>INET_OUT</filtername></get-firewall-filter-information>' \
  --rpc '<get-vrrp-information><summary/></get-vrrp-information>'
```

| オプション | 説明 | デフォルト |
|---|---|---|
| `--host` | 接続先ホスト | `localhost` |
| `--port` | 接続先ポート | `830` |
| `--user` | SSH ユーザー名 | *（必須）* |
| `--password` | SSH パスワード | `""` |
| `--timeout` | 全 RPC の完了待ちタイムアウト | `10s` |
| `--rpc` | 送信する NETCONF RPC XML（複数回指定可） | `<get-vrrp-information><summary/></get-vrrp-information>` |
| `--debug` | デバッグログを stderr およびログファイルに出力 | (なし) |

- `--rpc` は複数回指定可能。各 RPC は独立したセッションで並列実行される。
- 実行のたびにカレントディレクトリに `netconf_YYYY_MM_DD_hh_mm_ss.log`（JST）が作成される。

終了コード：
- `0` — 全 RPC 成功
- `1` — 1 つ以上の RPC が失敗またはタイムアウト

### タイムアウト値の目安

実機では、設定データ量や機器の負荷によって応答時間が異なる。

| 環境 | 推奨 `--timeout` |
|------|-----------------|
| netconf-mock（ローカル）| `10s` |
| Junos（設定量少） | `30s` |
| Junos（設定量多・高負荷） | `60s` 以上 |

---

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

## 既知の制限事項

| 制限 | 内容 |
|---|---|
| SSH ホスト鍵検証なし | `InsecureIgnoreHostKey()` を使用。開発・検証用途に限定すること。 |
| パスワード認証のみ | SSH 公開鍵認証は非対応。 |


## リポジトリ構成

```
.
├── netconf/
│   ├── log.go             # SetLog / Logger インターフェース
│   ├── session.go         # NETCONF セッション（Hello 交換・Exec）
│   ├── transport.go       # 基本 I/O（Send・Receive・WaitForFunc）
│   └── transport_ssh.go   # SSH トランスポート（Dial）
└── cmd/
     └── netconf-client/
         └── main.go        # 単独実行コマンド
```
