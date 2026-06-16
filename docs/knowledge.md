# go-netconf タイムアウト判定ロジック解説

---

## 全体像

go-netconf のタイムアウトは **2 層構造** になっている。

| 層 | 担当範囲 | 実装 | 実効性 |
|---|---|---|---|
| SSH 接続確立 | TCP ダイヤル〜認証〜セッション確立 | `deadlineConn` + `net.DialTimeout` | ✅ 有効 |
| RPC 応答待ち | `<get/>` 送信〜`<rpc-reply>` 受信 | `time.After()` + goroutine | ✅ 有効 |

---

## ① SSH 接続確立フェーズのタイムアウト（`deadlineConn`）

```go
// netconf/transport_ssh.go
func DialSSHTimeout(target string, config *ssh.ClientConfig, timeout time.Duration) (*Session, error) {
    bareConn, err := net.DialTimeout("tcp", target, timeout)   // TCP 接続確立タイムアウト
    conn := &deadlineConn{Conn: bareConn, timeout: timeout}    // Read/Write deadline ラッパー
    ...
}

func (c *deadlineConn) Read(b []byte) (n int, err error) {
    c.SetReadDeadline(time.Now().Add(c.timeout))  // Read のたびに「今から N 秒」と期限をセット
    return c.Conn.Read(b)
}
```

`deadlineConn` は TCP レイヤーに `SetReadDeadline` を設定する。
SSH ハンドシェイク・認証フェーズは TCP を直接読むため、ここでタイムアウトが効く。

---

## ② なぜ `deadlineConn` は RPC 応答待ちに効かないか

NETCONF セッション確立後、RPC 応答の読み取りは `sshSession.StdoutPipe()` 経由になる。

```
WaitForFunc()
  └── t.Read()  ← TransportBasicIO.Read
        └── sshSession.StdoutPipe() ← io.PipeReader（内部バッファ）から読む
                                         ↑
                                         TCP の SetReadDeadline は伝播しない
```

`io.PipeReader` は SSH クライアントライブラリの内部バッファであり、
TCP コネクションの deadline 設定が届かない。

**実測結果（旧実装）：`--timeout 5s --delay 15s` で実行しても 15 秒後に成功応答が返った。**

---

## ③ RPC 応答待ちタイムアウトの正しい実装（`time.After()` パターン）

```go
// cmd/netconf-client/main.go
timeoutCh := time.After(*timeout)
done := make(chan struct{})
var reply *netconf.RPCReply
var rpcErr error
go func() {
    reply, rpcErr = s.Exec(netconf.RawMethod("<get/>"))  // ← SSH StdoutPipe をブロック読み
    close(done)
}()

select {
case <-timeoutCh:
    s.Close()
    fmt.Fprintf(os.Stderr, "[ERROR] Timeout after %s waiting for RPC reply\n", *timeout)
    os.Exit(1)
case <-done:
    // 正常終了 or rpcErr チェック
}
```

`s.Exec()` をゴルーチンに閉じ込め、`time.After()` で外側から確実にタイムアウトを検出する。
SSH の内部バッファ構造に依存しないため、どの環境でも正確に機能する。

これは `collector_agent`（`netconf_connection.go`）が採用しているパターンと同じ。

---

## ④ タイムアウト発動フロー（新実装）

```
時刻  0s : netconf-client が <get/> RPC を送信
           goroutine: s.Exec() → WaitForFunc() → StdoutPipe() でブロック
           main:      time.After(5s) がカウント開始

時刻  5s : time.After() チャネルが発火
           main: s.Close() を呼ぶ → SSH セッションが閉じる
                 → StdoutPipe が EOF を返す
                 → goroutine: WaitForFunc EOF → s.Exec() が返る（close(done)）
           main: [ERROR] Timeout after 5s waiting for RPC reply → exit 1
```

---

## ⑤ デバッグログでの観測ポイント

`NETCONF_DEBUG=1` を有効にした際のログ出力：

### 正常系（delay < timeout）

```
[NETCONF DEBUG] SSH Dial: connecting to localhost:830
[NETCONF DEBUG] SSH: creating new session
[NETCONF DEBUG] Session: receiving server Hello
[NETCONF DEBUG] Receive: waiting for delimiter "]]>]]>"
[NETCONF DEBUG] WaitForFunc: delimiter found, returning N bytes
[NETCONF DEBUG] Session: negotiated NETCONF version "v1.0"
[NETCONF DEBUG] Exec: sending RPC message-id=xxxx
[NETCONF DEBUG] Receive: waiting for delimiter "]]>]]>"
[NETCONF DEBUG] WaitForFunc: delimiter found, returning N bytes
[INFO] RPC succeeded
```

### タイムアウト系（delay > timeout）

```
[NETCONF DEBUG] Exec: sending RPC message-id=xxxx
[NETCONF DEBUG] Receive: waiting for delimiter "]]>]]>"
  ← ここで goroutine がブロック中（mock は遅延応答中）
  ← timeout 秒後に time.After() が発火 → s.Close()
[NETCONF DEBUG] WaitForFunc EOF: returning 0 bytes   ← goroutine が EOF を検出
[ERROR] Timeout after 5s waiting for RPC reply       ← main goroutine が出力
```

`"Receive: waiting for delimiter"` から `"Timeout after Xs"` までの時間が
実際のタイムアウト待機時間（= `--timeout` で指定した値）になる。

---

## ⑥ `--timeout` 値と mock 遅延の関係

| `--timeout` | `--delay`（mock 遅延） | 結果 |
|---|---|---|
| `5s` | `15s` | タイムアウト（5s < 15s）|
| `10s` | `15s` | タイムアウト（10s < 15s）|
| `30s` | `15s` | 成功（30s > 15s）|

`run_netconf_scenario.sh` でこの閾値をループ実行しながら観測できる。

---

## ⑦ タイムアウト判定が行われるコードの場所

| ファイル | 場所 | 内容 |
|---|---|---|
| `cmd/netconf-client/main.go` | `time.After(*timeout)` + `select` | RPC 応答待ちタイムアウト（主要） |
| `netconf/transport_ssh.go` | `deadlineConn.Read()` | SSH 接続確立フェーズのみ有効 |
| `netconf/transport_ssh.go` | `net.DialTimeout()` | TCP 接続確立タイムアウト |
| `netconf/transport.go` | `WaitForFunc()` の `t.Read()` 直後 | EOF / エラーの検出（goroutine 内） |

---

## ⑧ タイムアウト値の変更方法

### netconf-client コマンドから変更する場合

```bash
./netconf-client --timeout 30s --debug
```

### run_netconf_scenario.sh から変更する場合

```bash
./tools/run_netconf_scenario.sh --mode timeout --timeout 30 --delay 15 --count 3
```

`--timeout` の値は以下の両方に適用される：
- SSH 接続確立フェーズ（`DialSSHTimeout` 経由）
- RPC 応答待ちフェーズ（`time.After()` 経由）

---

## ⑨ collector_agent の config.yml との関係

`collector_agent` の `netconf_connection.go` も同じ `time.After(conn.execTimeout)` パターンを採用している。
`config.yml` の `timeout_seconds` は `conn.execTimeout` として `time.After()` に渡されるため、設定変更は正しく有効になる。

`DialSSHTimeout` に渡されるタイムアウトがハードコード（10 秒）であっても、
それは SSH 接続確立フェーズにのみ影響し、RPC 応答待ちタイムアウトは `time.After()` が管理するため問題ない。

---

## ⑩ SSH Keep-Alive の動作

### 概要

`DialSSHTimeout` はセッション確立直後に Keep-Alive goroutine を起動する。

```go
// netconf/transport_ssh.go
go func() {
    ticker := time.NewTicker(timeout / 2)   // timeout の半分の間隔
    defer ticker.Stop()
    for range ticker.C {
        _, _, err := t.sshClient.Conn.SendRequest("KEEP_ALIVE", true, nil)
        if err != nil {
            return   // 接続断を検出したら静かに終了
        }
    }
}()
```

### Keep-Alive の種別と対象レイヤー

```
[ TCP コネクション ]
  └── [ SSH コネクション ]  ← ここに "KEEP_ALIVE" グローバルリクエストを送る
        └── [ SSH チャネル（NETCONF サブシステム）]
              └── StdoutPipe (io.PipeReader)  ← WaitForFunc はここを読む
```

- **TCP keepalive とは別物**：OS レベルの TCP keepalive ではなく、SSH プロトコルのグローバルリクエスト
- **チャネルレベルとも別物**：NETCONF チャネルが応答できるかどうかは別問題
- **"KEEP_ALIVE" は非標準**：OpenSSH は `keepalive@openssh.com` を使う。Junos 等は RFC 4254 に従い `SSH_MSG_REQUEST_FAILURE`（`ok=false`、`err=nil`）を返すが、コードは `ok` を無視して `err` のみチェックするため正常動作する

### Keep-Alive 間隔と --timeout の関係

| `--timeout` | Keep-Alive 間隔 | collector_agent との比較 |
|------------|----------------|------------------------|
| `5s` | 2.5 秒ごと | collector_agent（5s）より頻繁 |
| `10s` | 5 秒ごと | collector_agent と同じ |
| `30s` | 15 秒ごと | collector_agent より疎 |

collector_agent は `DialSSHTimeout(target, config, time.Second*10)` でハードコードのため、常に 5 秒間隔固定。

### コード遷移：TCP 断が起きた場合

```
【Keep-Alive goroutine】         【RPC goroutine】          【main goroutine】

ticker 発火
  ↓
SendRequest("KEEP_ALIVE", ...)
  ↓ TCP 断 → SSH mux が error 検出
err != nil → return              ←  Keep-Alive goroutine 終了
                                          ↓（SSH コネクション断）
                                 StdoutPipe.Read()
                                 ↓ SSH client 内部のパイプ書き込み側が死んで EOF
                                 WaitForFunc EOF
                                          ↓
                                 s.Exec() → rpcErr = io.EOF
                                 close(done)
                                                         ↓
                                                <-done 発火
                                                         ↓
                                                rpcErr != nil
                                                s.Close()
                                                [ERROR] RPC failed: EOF
                                                os.Exit(1)
```

### コード遷移：time.After() が先に発火した場合

```
【Keep-Alive goroutine】  【RPC goroutine】            【main goroutine】

ticker ごとに              StdoutPipe.Read()            time.After(*timeout)
SendRequest → ok           でブロック中                 ↓ 発火
↓ 継続中                   （mock 遅延応答中）           s.Close()
                                                         ↓ SSH セッション強制クローズ
SendRequest                StdoutPipe → EOF             [ERROR] Timeout after Xs...
→ error（session closed）  → close(done)                os.Exit(1)
→ return                   ※ main はすでに os.Exit 済み
```

### 役割の分担まとめ

| goroutine | 役割 | 失敗時の通知 |
|-----------|------|------------|
| Keep-Alive goroutine | SSH コネクション死活確認 | なし（静かに終了するだけ） |
| RPC goroutine | NETCONF 応答受信 | `close(done)` → main の `<-done` |
| `time.After()` | タイムアウト強制終了 | main goroutine が直接検出 |

Keep-Alive が失敗してもアプリには直接通知されず、SSH コネクション断の副作用として
`StdoutPipe` が EOF になるという間接的な経路で RPC goroutine が検出する。
