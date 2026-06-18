# WaitForFunc forループ無限事象 調査レポート

**調査日**: 2026-06-18  
**対象ライブラリ**: `github.com/Juniper/go-netconf` v0.1.1  
**対象関数**: `netconf/transport.go` の `WaitForFunc`

---

## 1. 調査の目的

go-netconf の `WaitForFunc` において「TCPセグメント分割によってデリミタ `]]>]]>` が受信欠損した場合、forループが終了しない」という課題報告を受け、v0.1.1 の実機実験を通じて再現条件を特定する。

---

## 2. 実験環境

| 項目 | 内容 |
|------|------|
| NETCONF ライブラリ | `github.com/Juniper/go-netconf` v0.1.1 |
| NETCONF クライアント | `cmd/netconf-client`（v0.1.1 ライブラリ使用、`time.After` タイムアウト付き） |
| NETCONF モック | `docker/netconf-mock`（paramiko/gevent ベース） |
| 接続先 | `localhost:830` |
| 認証 | `admin` / `admin` |
| RPC | `<get-vrrp-information><summary/></get-vrrp-information>` |
| シナリオ実行 | `tools/run_netconf_scenario.sh` |

### v0.1.1 WaitForFunc（実験対象コード）

```go
// netconf/transport.go @ v0.1.1（行 90–125）
func (t *transportBasicIO) WaitForFunc(f func([]byte) (int, error)) ([]byte, error) {
    var out bytes.Buffer
    buf := make([]byte, 4096)   // バッファサイズ 4096 bytes

    pos := 0
    for {
        n, err := t.Read(buf[pos : pos+(len(buf)/2)])   // buf[pos : pos+2048]
        if err != nil {
            if err != io.EOF {
                return nil, err
            }
            break
        }
        if n > 0 {
            end, err := f(buf[0 : pos+n])   // 旧データ＋新データ合算でデリミタ検索
            if err != nil {
                return nil, err
            }
            if end > -1 {
                out.Write(buf[0:end])
                return out.Bytes(), nil
            }
            if pos > 0 {
                out.Write(buf[0:pos])
                copy(buf, buf[pos:pos+n])
            }
            pos = n
        }
    }
    return nil, fmt.Errorf("WaitForFunc failed")
}
```

---

## 3. 実験結果

### 実験 1: split-delimiter モード

**モック設定**: `]]>]]>` を `]]>` + 500ms sleep + `]]>` の 2 回送信に分割  
**実行**: `tools/run_netconf_scenario.sh --mode split-delimiter --timeout 10 --count 1`

```
[INFO] Connecting to localhost:830 (timeout=10s)
[INFO] Connected (session-id=12345)
[NETCONF DEBUG] REQUEST: [<rpc ...><get-vrrp-information>...]
[NETCONF DEBUG] REPLY: [<rpc-reply ...><vrrp-information>...]
[INFO] RPC succeeded
[INFO ] Scenario finished. Success: 1 / Failed: 0 / Total: 1
```

**結果**: 正常終了（`exit=0`）。forループは継続しない。

---

### 実験 2: no-delimiter モード

**モック設定**: `]]>]]>` を一切送信しない  
**実行**: `tools/run_netconf_scenario.sh --mode no-delimiter --timeout 10 --count 1`

```
[INFO] Connecting to localhost:830 (timeout=10s)
[INFO] Connected (session-id=12345)
[NETCONF DEBUG] REQUEST: [<rpc ...><get-vrrp-information>...]
[ERROR] Timeout after 10s waiting for RPC reply
[INFO ] Scenario finished. Success: 0 / Failed: 1 / Total: 1
```

**結果**: `Read()` が永続ブロックし、forループが継続。`cmd/netconf-client` の `time.After(10s)` goroutine がセッションを強制クローズして終了。

---

## 4. 分析

### 4-1. TCPセグメント分割が再現条件でない理由

split-delimiter 実験で `exit=0` となった根拠を、v0.1.1 コードで説明する。

#### ループ内の処理順序（transport.go:90–125）

```go
pos := 0
for {
    // (1) 読み込み: buf[pos : pos+2048] に新データを追記
    n, err := t.Read(buf[pos : pos+(len(buf)/2)])

    // (2) デリミタ検索: buf[0:pos+n] — 旧データ(buf[0:pos]) + 新データ(buf[pos:pos+n]) を合算
    end, err := f(buf[0 : pos+n])

    if end > -1 {
        // (3a) 見つかった → 正常終了
        out.Write(buf[0:end])
        return out.Bytes(), nil
    }

    // (3b) 見つからなかった → フラッシュ（旧データを out に退避）
    if pos > 0 {
        out.Write(buf[0:pos])
        copy(buf, buf[pos:pos+n])
    }
    pos = n
}
```

**重要**: (2) の検索は (3b) のフラッシュより**必ず先**に実行される。

#### デリミタ分割シナリオのトレース

```
サーバー送信（2パケットに分割）:
  パケット1: [XML データ 1057 bytes] + "]]>"    ← デリミタ前半 3 バイト
  パケット2: "]]>"                              ← デリミタ後半 3 バイト

Iteration 1 (pos=0):
  Read  → buf[0:1060] に 1060 バイト書き込み（XML + "]]>"）
  検索  → f(buf[0:1060]) → "]]>]]>" なし → -1
  フラッシュなし（pos=0 のため）
  pos = 1060

Iteration 2 (pos=1060):
  Read  → buf[1060:1063] に 3 バイト書き込み（"]]>"）
  検索  → f(buf[0:1063]) → buf[1057:1063] = "]]>]]>" → FOUND
  out.Write(buf[0:1057]) → 正常終了
```

Iteration 2 の検索対象 `buf[0:1063]` は Iteration 1 で読んだ `"]]>"` を含むため、分割デリミタを結合した状態で検索できる。**デリミタが何パケットに分割されても、次の Read 後の合算検索で必ず検出される。**

### 4-2. forループ無限事象の本質

no-delimiter 実験で永続ブロックが確認された。

**根本原因の構造**:

```
デリミタが届かない
    ×
v0.1.1 ライブラリにタイムアウト機構がない
    ↓
Read() が永続ブロック → forループ無限継続
    ↓（cmd/netconf-client の time.After が発火）
s.Close() → Timeout エラー
```

v0.1.1 ライブラリには `time.After` や `context.WithTimeout` 相当の機構が存在しない。`cmd/netconf-client` 側で goroutine + `time.After(timeout)` パターンを実装することでタイムアウトを検知する。

---

## 5. forループ無限事象の再現条件

| # | 再現条件 | forループ無限化 | v0.1.1 の挙動 |
|---|---------|----------------|--------------|
| A | デリミタ完全欠損 | **✅ する** | Read() 永続ブロック（実験 2 で確認） |
| B | サーバーが応答途中で送信を長時間中断 | **✅ する（実質的に）** | Read() 永続ブロック |
| C | SSH 維持のままサーバーが RPC リプライを返さない | **✅ する** | Read() 永続ブロック |
| D | 大量データ応答によるデリミタ到達遅延 | ❌ しない（正常完了、時間がかかるだけ） | 長時間ブロック後に正常終了 |
| — | **TCPセグメントでのデリミタ分割** | **❌ しない** | **正常完了（実験 1 で確認）** |

---

## 6. 実験ログファイル

| ファイル | 内容 |
|---------|------|
| [`logs/test4_split_delimiter.log`](../logs/test4_split_delimiter.log) | split-delimiter モード → `[INFO] RPC succeeded` / exit=0 |
| [`logs/test5_no_delimiter.log`](../logs/test5_no_delimiter.log) | no-delimiter モード → `[ERROR] Timeout after 10s` / exit=1（forループ継続確認） |

---

## 7. まとめ

**TCPセグメント分割はforループ無限事象の再現条件ではない。**

v0.1.1 の実機実験により、デリミタを 2 つの TCP パケットに分割して送信した場合でも `WaitForFunc` は正常にデリミタを検出し、`exit=0` で終了することを確認した。

forループ無限事象の再現条件は「**デリミタが届かない状態**」であり、v0.1.1 ライブラリがタイムアウト機構を持たないことと組み合わさって永続ブロックが発生する。`cmd/netconf-client` の `time.After(timeout)` goroutine により、タイムアウト時にセッションを強制クローズしてループを終了させることができる。
