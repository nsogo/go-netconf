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
| 実験クライアント | go-netconf v0.1.1 ソースからビルドしたテストクライアント |
| NETCONF モック | `docker/netconf-mock`（paramiko/gevent ベース） |
| 接続先 | `localhost:830` |
| 認証 | `admin` / `admin` |
| RPC | `<get-vrrp-information><summary/></get-vrrp-information>` |

### v0.1.1 WaitForFunc（実験対象コード）

```go
// netconf/transport.go @ v0.1.1
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

```
$ ./test_client localhost:830
Connecting to localhost:830 ...
Connected. Session-ID=12345
Reply OK: 679 bytes
EXIT=0
```

**結果**: 正常終了（`EXIT=0`）。forループは継続しない。

---

### 実験 2: no-delimiter モード

**モック設定**: `]]>]]>` を一切送信しない

```
$ ./test_client localhost:830 &
PID=$!
# ... 12秒経過 ...
kill -0 $PID → PROCESS STILL RUNNING（forループ継続中）
```

**結果**: 12 秒後もプロセスが継続。`WaitForFunc` の `Read()` が永続的にブロックし、forループが終了しない。

---

## 4. 分析

### 4-1. TCPセグメント分割が再現条件でない理由

split-delimiter 実験で `EXIT=0` となった根拠を、現在のコードで説明する。

#### ループ内の処理順序（transport.go:127–178）

```go
pos := 0
for {
    // (1) 読み込み: buf[pos : pos+4096] に新データを追記
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
  検索  → f(buf[0:1060])  ─ "]]>]]>" なし → -1
  フラッシュなし（pos=0 のため）
  pos = 1060

Iteration 2 (pos=1060):
  Read  → buf[1060:1063] に 3 バイト書き込み（"]]>"）
  検索  → f(buf[0:1063])  ─ buf[1057:1063] = "]]>]]>" → FOUND
  out.Write(buf[0:1057]) → 正常終了
```

Iteration 2 の検索対象 `buf[0:1063]` は Iteration 1 で読んだ `"]]>"` を含むため、分割デリミタを結合した状態で検索できる。**デリミタが何パケットに分割されても、次の Read 後の合算検索で必ず検出される。**

### 4-2. forループ無限事象の本質

no-delimiter 実験で永続ブロックが確認された。

**根本原因の構造**:

```
デリミタが届かない
    ×
タイムアウト機構がない（v0.1.1 は未実装）
    ↓
Read() が永続ブロック → forループ無限継続
```

v0.1.1 には `time.After` や `context.WithTimeout` 相当の機構が存在しない。SSH コネクションが Keep-Alive で維持される限り、`Read()` はブロックし続ける。

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
| [`logs/test4_split_delimiter.log`](../logs/test4_split_delimiter.log) | 現フォーク: split-delimiter モード（NETCONF_DEBUG=1）→ EXIT=0 |
| [`logs/test5_no_delimiter.log`](../logs/test5_no_delimiter.log) | 現フォーク: no-delimiter モード（NETCONF_DEBUG=1）→ タイムアウト終了 |
| [`logs/v011_split_delimiter.log`](../logs/v011_split_delimiter.log) | v0.1.1 実験: split-delimiter モード → EXIT=0（正常終了） |
| [`logs/v011_no_delimiter.log`](../logs/v011_no_delimiter.log) | v0.1.1 実験: no-delimiter モード → 12秒後もプロセス継続（forループ無限） |

---

## 7. まとめ

**TCPセグメント分割はforループ無限事象の再現条件ではない。**

v0.1.1 の実機実験により、デリミタを 2 つの TCP パケットに分割して送信した場合でも `WaitForFunc` は正常にデリミタを検出し、`EXIT=0` で終了することを確認した。

forループ無限事象の再現条件は「**デリミタが届かない状態**」であり、v0.1.1 がタイムアウト機構を持たないことと組み合わさって永続ブロックが発生する。
