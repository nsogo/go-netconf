# REQ-3 動作検証レポート

**実施日**: 2026-06-16  
**対象 PR**: [PR #4](https://github.com/ttsubo2000/go-netconf/pull/4)  
**対象 Issue**: [Issue #3](https://github.com/ttsubo2000/go-netconf/issues/3)  
**検証者**: Claude Code

---

## 検証概要

`netconf-client` のタイムアウト機構を `deadlineConn`（TCP Read deadline）から
`time.After()` + goroutine パターンに置き換えたことを検証する。

**背景**: `deadlineConn` は SSH セッションの `StdoutPipe()`（`io.PipeReader`）に
deadline が伝播しないため、RPC 応答待ちのタイムアウトが機能しなかった。

---

## 環境

| 項目 | 内容 |
|------|------|
| NETCONF クライアント | `cmd/netconf-client`（本リポジトリ） |
| NETCONF モック | `docker/netconf-mock`（paramiko/gevent ベース）|
| モック接続先 | `localhost:830` |
| 認証情報 | `admin` / `admin` |
| デバッグモード | `NETCONF_DEBUG=1` |

---

## テスト結果サマリー

| # | テスト内容 | timeout | delay | 期待結果 | 実結果 | 経過時間 | 判定 |
|---|-----------|---------|-------|---------|--------|----------|------|
| 1 | タイムアウト確認 | 5s | 15s | exit=1, タイムアウトエラー | `[ERROR] Timeout after 5s waiting for RPC reply` / exit=1 | ~5.0s | ✅ PASS |
| 2 | 正常応答確認 | 10s | 3s | exit=0, RPC 成功 | `[INFO] RPC succeeded` / exit=0 | ~3.0s | ✅ PASS |
| 3 | シナリオスクリプト | 5s | 15s | Failed: 1 / Total: 1 | `Scenario finished. Success: 0 / Failed: 1 / Total: 1` | ~5.0s | ✅ PASS |

**全 3 件 PASS**

---

## テスト詳細

### Test 1: タイムアウト確認

**条件**: mock が 15 秒遅延、netconf-client のタイムアウトは 5 秒

**実行コマンド**:
```bash
# mock に 15 秒遅延を設定
curl -X POST http://localhost:8088/set_use_delays
curl -X POST http://localhost:8088/delays_range -H 'Content-Type: application/json' -d '{"delay": 15}'

# netconf-client 実行
NETCONF_DEBUG=1 ./netconf-client --host localhost --port 830 \
  --user admin --password admin --timeout 5s
```

**観測されたログ（抜粋）**:
```
[INFO] Connecting to localhost:830 (timeout=5s)
[NETCONF DEBUG] SSH: creating new session
[NETCONF DEBUG] Session: receiving server Hello
[NETCONF DEBUG] Session: negotiated NETCONF version "v1.0"
[INFO] Connected (session-id=12345)
[NETCONF DEBUG] Exec: sending RPC message-id=af1dda35-...
[NETCONF DEBUG] Receive: waiting for delimiter "]]>]]>"
  ← ここで goroutine がブロック（mock は 15s 遅延中）
  ← 5 秒後に time.After() が発火 → s.Close()
[NETCONF DEBUG] SSH: closing session
[NETCONF DEBUG] SSH: session closed
[ERROR] Timeout after 5s waiting for RPC reply
```

**結果**: exit=1、タイムアウトメッセージ確認 → **PASS**

**ログファイル**: [`logs/test1_timeout_5s_delay_15s.log`](../logs/test1_timeout_5s_delay_15s.log)

---

### Test 2: 正常応答確認

**条件**: mock が 3 秒遅延、netconf-client のタイムアウトは 10 秒

**実行コマンド**:
```bash
curl -X POST http://localhost:8088/delays_range -H 'Content-Type: application/json' -d '{"delay": 3}'

NETCONF_DEBUG=1 ./netconf-client --host localhost --port 830 \
  --user admin --password admin --timeout 10s
```

**観測されたログ（抜粋）**:
```
[INFO] Connecting to localhost:830 (timeout=10s)
[NETCONF DEBUG] Session: negotiated NETCONF version "v1.0"
[INFO] Connected (session-id=12345)
[NETCONF DEBUG] Exec: sending RPC message-id=68877cb7-...
[NETCONF DEBUG] Receive: waiting for delimiter "]]>]]>"
[NETCONF DEBUG] WaitForFunc: read 146 bytes (total buffered: 146)
[NETCONF DEBUG] WaitForFunc: delimiter found, returning 139 bytes
[NETCONF DEBUG] Exec: RPC completed successfully message-id=68877cb7-...
[INFO] RPC succeeded
[DATA] <rpc-reply ...><ok/></rpc-reply>
```

**結果**: exit=0、RPC 成功確認 → **PASS**

**ログファイル**: [`logs/test2_normal_10s_delay_3s.log`](../logs/test2_normal_10s_delay_3s.log)

---

### Test 3: シナリオスクリプト（タイムアウト再現）

**条件**: `run_netconf_scenario.sh --mode timeout --timeout 5 --delay 15 --count 1`

**実行コマンド**:
```bash
tools/run_netconf_scenario.sh --mode timeout --timeout 5 --delay 15 --count 1
```

**観測されたログ（抜粋）**:
```
[INFO ] Enabling delays (mock delay=15s, NETCONF timeout=5s)
[INFO ] Delay configured: mock will wait 15s before responding
[INFO ] Starting netconf scenario (mode=timeout, count=1, interval=60s, timeout=5s)
[INFO ] --- Iteration 1/1 ---
[NETCONF DEBUG] Receive: waiting for delimiter "]]>]]>"
  ← 5 秒後に time.After() 発火
[ERROR] Timeout after 5s waiting for RPC reply
[ERROR] Iteration 1/1 FAILED (exit=1)
[INFO ] Scenario finished. Success: 0 / Failed: 1 / Total: 1
```

**結果**: タイムアウト再現確認 → **PASS**

**ログファイル**: [`logs/test3_scenario_timeout_5s_delay_15s.log`](../logs/test3_scenario_timeout_5s_delay_15s.log)

---

## タイムアウト前後の動作比較

| | 旧実装（`deadlineConn`） | 新実装（`time.After()`） |
|--|--|--|
| `--timeout 5s --delay 15s` | ❌ 15 秒後に成功応答が返る | ✅ 5 秒でタイムアウト |
| タイムアウトメッセージ | なし（成功扱い） | `[ERROR] Timeout after 5s waiting for RPC reply` |
| exit code | 0（誤り） | 1（正しい） |
| 根本原因 | `deadlineConn` が SSH `StdoutPipe` に伝播しない | - |

---

## ログファイル一覧

| ファイル | テスト内容 |
|---------|-----------|
| [`logs/test1_timeout_5s_delay_15s.log`](../logs/test1_timeout_5s_delay_15s.log) | Test 1: タイムアウト確認（timeout=5s, delay=15s） |
| [`logs/test2_normal_10s_delay_3s.log`](../logs/test2_normal_10s_delay_3s.log) | Test 2: 正常応答確認（timeout=10s, delay=3s） |
| [`logs/test3_scenario_timeout_5s_delay_15s.log`](../logs/test3_scenario_timeout_5s_delay_15s.log) | Test 3: シナリオスクリプト タイムアウト再現 |

---

## 結論

`time.After()` + goroutine パターンへの置き換えにより、
`--timeout` で指定した秒数で確実に RPC 応答待ちタイムアウトが機能することを確認した。

旧実装（`deadlineConn`）では SSH セッションの内部バッファ（`io.PipeReader`）に
TCP deadline が伝播しないため事実上タイムアウトしなかったが、
新実装ではこの制約を goroutine + `select` で回避している。
