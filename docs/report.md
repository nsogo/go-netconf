# 動作検証レポート

**実施日**: 2026-06-18  
**対象ライブラリ**: `github.com/Juniper/go-netconf` v0.1.1  
**検証者**: ttsubo2000

---

## 検証概要

以下の5点を検証する。

1. `time.After()` + goroutine パターンによるタイムアウト機構が正しく機能すること（`cmd/netconf-client` 実装）
2. デフォルト RPC が `<get-vrrp-information><summary/></get-vrrp-information>` となり、VRRP XML が取得できること
3. シナリオスクリプト経由でタイムアウト再現ができること
4. TCPセグメント分割（split-delimiter モード）でも RPC が正常完了すること
5. デリミタ完全欠損（no-delimiter モード）でforループが継続することを確認すること

---

## 環境

| 項目 | 内容 |
|------|------|
| NETCONF ライブラリ | `github.com/Juniper/go-netconf` v0.1.1 |
| NETCONF クライアント | `cmd/netconf-client`（`time.After` タイムアウト実装付き） |
| NETCONF モック | `docker/netconf-mock`（paramiko/gevent ベース）|
| モック接続先 | `localhost:830` |
| 認証情報 | `admin` / `admin` |
| デフォルト RPC | `<get-vrrp-information><summary/></get-vrrp-information>` |
| シナリオスクリプト | `tools/run_netconf_scenario.sh` |

---

## テスト結果サマリー

| # | テスト内容 | timeout | delay | 期待結果 | 実結果 | 経過時間 | 判定 |
|---|-----------|---------|-------|---------|--------|----------|------|
| 1 | タイムアウト確認 | 5s | 15s | exit=1, タイムアウトエラー | `[ERROR] Timeout after 5s waiting for RPC reply` / exit=1 | 5s | ✅ PASS |
| 2 | 正常応答確認（VRRP XML） | 10s | - | exit=0, VRRP XML 応答 | `[INFO] RPC succeeded` + VRRP XML / exit=0 | <1s | ✅ PASS |
| 3 | シナリオスクリプト（タイムアウト再現） | 5s | 15s | Failed: 1 / Total: 1 | `Scenario finished. Success: 0 / Failed: 1 / Total: 1` | 5s | ✅ PASS |
| 4 | デリミタ分割（split-delimiter） | 10s | - | exit=0, 分割でも正常受信 | `[INFO] RPC succeeded` + VRRP XML / exit=0 | <1s | ✅ PASS |
| 5 | デリミタ完全欠損（no-delimiter） | 10s | - | exit=1, forループ継続確認 | `[ERROR] Timeout after 10s waiting for RPC reply` / exit=1 | 10s | ✅ PASS |

**全 5 件 PASS**

---

## テスト詳細

### Test 1: タイムアウト確認

**条件**: mock が 15 秒遅延、`--timeout 5s` 指定

**実行コマンド**:
```bash
tools/run_netconf_scenario.sh --mode timeout --timeout 5 --delay 15 --count 1
```

**観測されたログ（抜粋）**:
```
[INFO ] Enabling delays (mock delay=15s, NETCONF timeout=5s)
[INFO ] Starting netconf scenario (mode=timeout, count=1, interval=60s, timeout=5s)
[INFO ] --- Iteration 1/1 ---
[INFO] Connecting to localhost:830 (timeout=5s)
[INFO] Connected (session-id=12345)
[ERROR] Timeout after 5s waiting for RPC reply
[ERROR] Iteration 1/1 FAILED (exit=1)
[INFO ] Scenario finished. Success: 0 / Failed: 1 / Total: 1
```

**確認ポイント**:
- mock の 15s 遅延に対し、`time.After(5s)` が発火して RPC を強制終了 ✅
- exit=1 で終了 ✅

**結果**: exit=1 → **PASS**

**ログファイル**: [`logs/test1_timeout_5s_delay_15s.log`](../logs/test1_timeout_5s_delay_15s.log)

---

### Test 2: 正常応答確認（VRRP XML）

**条件**: mock に遅延なし、`--timeout 10s` 指定

**実行コマンド**:
```bash
tools/run_netconf_scenario.sh --mode normal --timeout 10 --count 1
```

**観測されたログ（抜粋）**:
```
[INFO ] Starting netconf scenario (mode=normal, count=1, interval=60s, timeout=10s)
[INFO ] --- Iteration 1/1 ---
[INFO] Connecting to localhost:830 (timeout=10s)
[INFO] Connected (session-id=12345)
[INFO] RPC succeeded
[DATA] <rpc-reply
    xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"
    xmlns:junos="http://xml.juniper.net/junos/14.2R3/junos"
    ...>
<vrrp-information style="brief">
<vrrp-interface>
  <interface>ae0.0</interface>
  <vrrp-state>MASTER</vrrp-state>
  ...
</vrrp-interface>
<vrrp-interface>
  <interface>ae1.0</interface>
  <vrrp-state>BACKUP</vrrp-state>
  ...
</vrrp-interface>
</vrrp-information>
</rpc-reply>
[INFO ] Iteration 1/1 SUCCESS
[INFO ] Scenario finished. Success: 1 / Failed: 0 / Total: 1
```

**確認ポイント**:
- `<vrrp-information>` を含む VRRP XML が返却 ✅
- ae0.0 MASTER / ae1.0 BACKUP の 2 セッション確認 ✅
- exit=0 ✅

**結果**: exit=0 → **PASS**

**ログファイル**: [`logs/test2_normal_10s.log`](../logs/test2_normal_10s.log)

---

### Test 3: シナリオスクリプト（タイムアウト再現）

**条件**: `run_netconf_scenario.sh --mode timeout --timeout 5 --delay 15 --count 1`

**実行コマンド**:
```bash
tools/run_netconf_scenario.sh --mode timeout --timeout 5 --delay 15 --count 1 --no-build
```

**観測されたログ（抜粋）**:
```
[INFO ] Setting default VRRP sessions (ae0.0 MASTER, ae1.0 BACKUP)...
[INFO ] Default VRRP sessions configured (2 sessions)
[INFO ] Enabling delays (mock delay=15s, NETCONF timeout=5s)
[INFO ] Starting netconf scenario (mode=timeout, count=1, interval=60s, timeout=5s)
[INFO ] --- Iteration 1/1 ---
[INFO] Connecting to localhost:830 (timeout=5s)
[INFO] Connected (session-id=12345)
[ERROR] Timeout after 5s waiting for RPC reply
[ERROR] Iteration 1/1 FAILED (exit=1)
[INFO ] Scenario finished. Success: 0 / Failed: 1 / Total: 1
```

**確認ポイント**:
- スクリプト起動時に VRRP セッションが自動セット ✅
- タイムアウト再現確認 ✅

**結果**: タイムアウト再現確認 → **PASS**

**ログファイル**: [`logs/test3_scenario_timeout_5s_delay_15s.log`](../logs/test3_scenario_timeout_5s_delay_15s.log)

---

### Test 4: デリミタ分割（split-delimiter）

**条件**: netconf-mock が `]]>]]>` を `]]>` + 500ms sleep + `]]>` の 2 回送信に分割

**実行コマンド**:
```bash
tools/run_netconf_scenario.sh --mode split-delimiter --timeout 10 --count 1
```

**観測されたログ（抜粋）**:
```
[INFO ] Enabling split-delimiter mode (mock will send ]]>]]> as two separate TCP sends)
[INFO ] Starting netconf scenario (mode=split-delimiter, count=1, interval=60s, timeout=10s)
[INFO ] --- Iteration 1/1 ---
[INFO] Connecting to localhost:830 (timeout=10s)
[INFO] Connected (session-id=12345)
[INFO] RPC succeeded
[DATA] <rpc-reply ...>
<vrrp-information style="brief">
...
</vrrp-information>
</rpc-reply>
[INFO ] Iteration 1/1 SUCCESS
[INFO ] Scenario finished. Success: 1 / Failed: 0 / Total: 1
```

**考察**:

v0.1.1 の `WaitForFunc` は `f(buf[0:pos+n])` で旧データ＋新データを合算してデリミタ検索するため、デリミタが複数の TCP パケットに分割されても必ず検出できる。

```
Iteration 1 (pos=0):
  Read → buf[0:N] に XML + "]]>" を格納
  f(buf[0:N]) → "]]>]]>" なし → not found
  pos = N

Iteration 2 (pos=N):
  Read → buf[N:N+3] に "]]>" を格納
  f(buf[0:N+3]) → buf[N-3:N+3] = "]]>]]>" → FOUND → 正常終了
```

**TCPセグメント分割はforループ無限の再現条件ではない。**（詳細: [`docs/investigation_loop_termination.md`](investigation_loop_termination.md)）

**結果**: exit=0 → **PASS**

**ログファイル**: [`logs/test4_split_delimiter.log`](../logs/test4_split_delimiter.log)

---

### Test 5: デリミタ完全欠損（no-delimiter）

**条件**: netconf-mock が `]]>]]>` を一切送信しない

**実行コマンド**:
```bash
tools/run_netconf_scenario.sh --mode no-delimiter --timeout 10 --count 1
```

**観測されたログ（抜粋）**:
```
[INFO ] Enabling no-delimiter mode (mock will send replies WITHOUT ]]>]]>)
[INFO ] Starting netconf scenario (mode=no-delimiter, count=1, interval=60s, timeout=10s)
[INFO ] --- Iteration 1/1 ---
[INFO] Connecting to localhost:830 (timeout=10s)
[INFO] Connected (session-id=12345)
[ERROR] Timeout after 10s waiting for RPC reply
[ERROR] Iteration 1/1 FAILED (exit=1)
[INFO ] Scenario finished. Success: 0 / Failed: 1 / Total: 1
```

**考察**:

デリミタが届かない場合、v0.1.1 の `WaitForFunc` は `Read()` でブロックし続ける。v0.1.1 ライブラリ自体にはタイムアウト機構がないため、`cmd/netconf-client` 側の `time.After(timeout)` goroutine がセッションを強制クローズすることでループを終了させる。

```
デリミタが届かない
    × v0.1.1 ライブラリにタイムアウト機構なし
    ↓
Read() 永続ブロック → forループ継続
    ↓（cmd/netconf-client の time.After が発火）
s.Close() → [ERROR] Timeout after 10s waiting for RPC reply
```

これが **forループ無限事象の真の再現条件**である。（詳細: [`docs/investigation_loop_termination.md`](investigation_loop_termination.md)）

**確認ポイント**:
- デリミタ欠損時にforループが継続 ✅
- `time.After(10s)` により強制終了 ✅

**結果**: exit=1（タイムアウト）→ **PASS**

**ログファイル**: [`logs/test5_no_delimiter.log`](../logs/test5_no_delimiter.log)

---

## ログファイル一覧

| ファイル | テスト内容 |
|---------|-----------|
| [`logs/test1_timeout_5s_delay_15s.log`](../logs/test1_timeout_5s_delay_15s.log) | Test 1: タイムアウト確認（timeout=5s, delay=15s） |
| [`logs/test2_normal_10s.log`](../logs/test2_normal_10s.log) | Test 2: 正常応答確認（timeout=10s）VRRP XML 取得 |
| [`logs/test3_scenario_timeout_5s_delay_15s.log`](../logs/test3_scenario_timeout_5s_delay_15s.log) | Test 3: シナリオスクリプト タイムアウト再現 |
| [`logs/test4_split_delimiter.log`](../logs/test4_split_delimiter.log) | Test 4: デリミタ分割（split-delimiter）→ 正常受信 |
| [`logs/test5_no_delimiter.log`](../logs/test5_no_delimiter.log) | Test 5: デリミタ完全欠損（no-delimiter）→ forループ継続確認 |

---

## 結論

- `time.After()` + goroutine パターン（`cmd/netconf-client` 実装）により、`--timeout` で指定した秒数で確実に RPC 応答待ちタイムアウトが機能することを確認した
- デフォルト RPC `<get-vrrp-information><summary/></get-vrrp-information>` により、VRRP XML（`<vrrp-information style="brief">` 形式）が取得できることを確認した
- **TCPセグメント分割はforループ無限の再現条件ではない**: v0.1.1 の `WaitForFunc` は `f(buf[0:pos+n])` による合算バッファ検索で分割デリミタを正しく検出する（Test 4 で確認）
- **forループ無限の真の再現条件はデリミタ完全欠損**: v0.1.1 ライブラリはタイムアウト機構を持たないため、デリミタが届かない場合は `Read()` が永続ブロックする。`cmd/netconf-client` の `time.After` によりタイムアウトを検知できる（Test 5 で確認）
- 詳細分析は [`docs/investigation_loop_termination.md`](investigation_loop_termination.md) を参照
