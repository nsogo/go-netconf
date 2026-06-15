# go-netconf タイムアウト判定ロジック解説

---

## 全体像

go-netconf のタイムアウトは「固定の N 秒間を待つ」仕組みではなく、
**「Read を呼ぶたびに、そこから N 秒以内にデータが届かなければアウト」** という TCP レベルの期限設定で実現されている。

---

## ① タイムアウトの「仕掛け」は `DialSSHTimeout` で設置される

```go
// netconf/transport_ssh.go
func DialSSHTimeout(target string, config *ssh.ClientConfig, timeout time.Duration) (*Session, error) {
    bareConn, err := net.DialTimeout("tcp", target, timeout)  // TCP 接続確立
    conn := &deadlineConn{Conn: bareConn, timeout: timeout}   // タイムアウトラッパーで包む
    ...
}
```

`deadlineConn` は通常の TCP 接続にタイムアウト機能を後付けするラッパー。

```go
// netconf/transport_ssh.go
type deadlineConn struct {
    net.Conn
    timeout time.Duration  // ← DialSSHTimeout に渡した値（例: 10s）が保存される
}

func (c *deadlineConn) Read(b []byte) (n int, err error) {
    c.SetReadDeadline(time.Now().Add(c.timeout))  // ← Read のたびに「今から N 秒」と期限をセット
    return c.Conn.Read(b)
}
```

---

## ② 通常フローでは `WaitForFunc` が Read をループする

```go
// netconf/transport.go
func (t *TransportBasicIO) WaitForFunc(f func([]byte) (int, error)) ([]byte, error) {
    buf := make([]byte, 8192)
    pos := 0
    for {
        // ↓ ここで deadlineConn.Read() を呼ぶ → 毎回 deadline がリセットされる
        n, err := t.Read(buf[pos : pos+(len(buf)/2)])
        if err != nil {
            if err != io.EOF {
                return nil, err  // ← i/o timeout はここで返る
            }
            ...
        }
        ...
    }
}
```

mock が素早くデータを返し続ける場合、Read のたびに `deadline = 今 + N秒` がリセットされるため、
大きなレスポンスでも問題なく受け取れる。

---

## ③ mock が遅延した場合のタイムアウト発動フロー

```
時刻  0s : netconf-client が <get/> RPC を送信
時刻  0s : mock が受信 → 15秒スリープ開始
時刻  0s : WaitForFunc ループ開始
           t.Read() を呼ぶ → deadline = 今 + 10s に設定
           TCP がブロック（mock はまだ何も返さない）
           ↓
時刻 10s : deadline 超過！
           t.Read() が err = "read tcp: i/o timeout" を返す
           err != io.EOF なので return nil, err  ← ★ タイムアウト確定
           ↓
           Receive() が err を受け取り return nil, err
           ↓
           Exec() が err を受け取り return nil, err
           ↓
           netconf-client: [ERROR] RPC failed: read tcp: i/o timeout → exit 1
```

---

## ④ デバッグログでの観測ポイント

`NETCONF_DEBUG=1` を有効にした際のログ出力：

```
[NETCONF DEBUG] Exec: sending RPC message-id=xxxx          ← 送信完了
[NETCONF DEBUG] Receive: waiting for delimiter "]]>]]>"    ← 待機開始
                                                             ↑ N 秒間 Read がブロック
[NETCONF DEBUG] WaitForFunc read error: read tcp: i/o timeout  ← ★ タイムアウト検出
[NETCONF DEBUG] Receive error: read tcp: i/o timeout
[NETCONF DEBUG] Exec: Receive error: read tcp: i/o timeout
```

`"Receive: waiting for delimiter"` と `"WaitForFunc read error"` の時刻差が、
実際のタイムアウト待機時間（= `--timeout` で指定した値）になる。

---

## ⑤ `--timeout` 値と mock 遅延の関係

| `--timeout` | `--delay`（mock 遅延） | 結果 |
|---|---|---|
| `5s` | `15s` | タイムアウト（5s < 15s） |
| `10s` | `15s` | タイムアウト（10s < 15s） |
| `30s` | `15s` | 成功（30s > 15s） |

`--timeout` の値が `--delay` を超えた瞬間に「成功」に転じる。
`run_netconf_scenario.sh` でこの閾値をループ実行しながら観測できる。

---

## ⑥ タイムアウト判定が行われるコードの場所

| ファイル | 行 | 内容 |
|---|---|---|
| `netconf/transport_ssh.go` | `deadlineConn.Read()` | TCP Read ごとに deadline をセット |
| `netconf/transport.go` | `WaitForFunc()` の `t.Read()` 直後 | `i/o timeout` エラーを最初に受け取る場所 |
| `netconf/transport.go` | `Receive()` | `WaitForFunc` のエラーを上位に伝播 |
| `netconf/session.go` | `Exec()` の `Transport.Receive()` 直後 | RPC レベルでエラーを返す |

---

## ⑦ タイムアウト値の変更方法

### netconf-client コマンドから変更する場合

```bash
./netconf-client --timeout 30s --debug
```

### run_netconf_scenario.sh から変更する場合

```bash
./tools/run_netconf_scenario.sh --mode timeout --timeout 30 --delay 15 --count 3
```

`--timeout` の値は `netconf.DialSSHTimeout()` に直接渡され、`deadlineConn` の Read deadline として機能する。
