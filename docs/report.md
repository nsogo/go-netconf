# REQ-3 / REQ-4 動作検証レポート

**実施日**: 2026-06-16  
**対象コミット**: `2f8591d` (feat: add --rpc flag)  
**検証者**: ttsubo2000

---

## 検証概要

以下の2点を検証する。

1. `time.After()` + goroutine パターンによるタイムアウト機構が正しく機能すること
2. デフォルト RPC が `<get-vrrp-information><summary/></get-vrrp-information>` となり、collector_agent（vrrp_pool）と同一の RPC が送信されること

---

## 環境

| 項目 | 内容 |
|------|------|
| NETCONF クライアント | `cmd/netconf-client`（本リポジトリ） |
| NETCONF モック | `docker/netconf-mock`（paramiko/gevent ベース）|
| モック接続先 | `localhost:830` |
| 認証情報 | `admin` / `admin` |
| デバッグモード | `NETCONF_DEBUG=1` |
| デフォルト RPC | `<get-vrrp-information><summary/></get-vrrp-information>` |

---

## テスト結果サマリー

| # | テスト内容 | timeout | delay | 期待結果 | 実結果 | 経過時間 | 判定 |
|---|-----------|---------|-------|---------|--------|----------|------|
| 1 | タイムアウト確認 | 5s | 15s | exit=1, タイムアウトエラー | `[ERROR] Timeout after 5s waiting for RPC reply` / exit=1 | 5s | ✅ PASS |
| 2 | 正常応答確認 | 10s | 3s | exit=0, RPC 成功 | `[INFO] RPC succeeded` / exit=0 | 3s | ✅ PASS |
| 3 | シナリオスクリプト | 5s | 15s | Failed: 1 / Total: 1 | `Scenario finished. Success: 0 / Failed: 1 / Total: 1` | 5s | ✅ PASS |

**全 3 件 PASS**

---

## テスト詳細

### Test 1: タイムアウト確認

**条件**: mock が 15 秒遅延、netconf-client のタイムアウトは 5 秒

**実行コマンド**:
```bash
curl -X POST http://localhost:8088/set_use_delays
curl -X POST http://localhost:8088/delays_range -H 'Content-Type: application/json' -d '{"delay": 15}'

NETCONF_DEBUG=1 ./netconf-client \
  --host localhost --port 830 --user admin --password admin --timeout 5s
```

**観測されたログ（抜粋）**:
```
[INFO] Connecting to localhost:830 (timeout=5s)
[NETCONF DEBUG] Session: negotiated NETCONF version "v1.0"
[INFO] Connected (session-id=12345)
[NETCONF DEBUG] Send (164 bytes): <rpc message-id="207b01c3-..." ...>
    <get-vrrp-information><summary/></get-vrrp-information></rpc>
[NETCONF DEBUG] Receive: waiting for delimiter "]]>]]>"
  ← goroutine がブロック（mock は 15s 遅延中）
  ← 5 秒後に time.After() 発火 → s.Close()
[NETCONF DEBUG] SSH: closing session
[ERROR] Timeout after 5s waiting for RPC reply
```

**送信 RPC の確認**:
```
Send (164 bytes): <rpc ...><get-vrrp-information><summary/></get-vrrp-information></rpc>
```
→ collector_agent（vrrp_pool）と同一の RPC が送信されていることを確認 ✅

**結果**: exit=1、タイムアウトメッセージ確認 → **PASS**

**ログファイル**: [`logs/test1_timeout_5s_delay_15s.log`](../logs/test1_timeout_5s_delay_15s.log)

---

### Test 2: 正常応答確認

**条件**: mock が 3 秒遅延、netconf-client のタイムアウトは 10 秒

**実行コマンド**:
```bash
curl -X POST http://localhost:8088/delays_range -H 'Content-Type: application/json' -d '{"delay": 3}'

NETCONF_DEBUG=1 ./netconf-client \
  --host localhost --port 830 --user admin --password admin --timeout 10s
```

**観測されたログ（抜粋）**:
```
[INFO] Connected (session-id=12345)
[NETCONF DEBUG] Send (164 bytes): <rpc ...><get-vrrp-information><summary/></get-vrrp-information></rpc>
[NETCONF DEBUG] Receive: waiting for delimiter "]]>]]>"
[NETCONF DEBUG] WaitForFunc: delimiter found, returning 139 bytes
[NETCONF DEBUG] Exec: RPC completed successfully message-id=8744a5b8-...
[INFO] RPC succeeded
[DATA] <rpc-reply ...><ok/></rpc-reply>
```

**結果**: exit=0、3 秒で正常応答確認 → **PASS**

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
[INFO ] Starting netconf scenario (mode=timeout, count=1, interval=60s, timeout=5s,
        rpc=<get-vrrp-information><summary/></get-vrrp-information>)
[INFO ] Enabling delays (mock delay=15s, NETCONF timeout=5s)
[NETCONF DEBUG] Send (164 bytes): <rpc ...><get-vrrp-information><summary/></get-vrrp-information></rpc>
[NETCONF DEBUG] Receive: waiting for delimiter "]]>]]>"
  ← 5 秒後に time.After() 発火
[ERROR] Timeout after 5s waiting for RPC reply
[ERROR] Iteration 1/1 FAILED (exit=1)
[INFO ] Scenario finished. Success: 0 / Failed: 1 / Total: 1
```

**結果**: タイムアウト再現確認 → **PASS**

**ログファイル**: [`logs/test3_scenario_timeout_5s_delay_15s.log`](../logs/test3_scenario_timeout_5s_delay_15s.log)

---

## collector_agent（vrrp_pool）との対応関係

| 項目 | collector_agent | netconf-client（本ツール）|
|------|----------------|--------------------------|
| タイムアウト機構 | `time.After(conn.execTimeout)` | `time.After(*timeout)` ✅ 同一パターン |
| RPC 内容 | `<get-vrrp-information><summary/></get-vrrp-information>` | 同上（デフォルト値）✅ |
| タイムアウト検出メッセージ | `"Timeout happened before netconf reply received"` | `"[ERROR] Timeout after Xs waiting for RPC reply"` |
| ループ実行 | `interval` ごとに繰り返し | `run_netconf_scenario.sh --interval` で制御 ✅ |

---

## ログファイル一覧

| ファイル | テスト内容 |
|---------|-----------|
| [`logs/test1_timeout_5s_delay_15s.log`](../logs/test1_timeout_5s_delay_15s.log) | Test 1: タイムアウト確認（timeout=5s, delay=15s） |
| [`logs/test2_normal_10s_delay_3s.log`](../logs/test2_normal_10s_delay_3s.log) | Test 2: 正常応答確認（timeout=10s, delay=3s） |
| [`logs/test3_scenario_timeout_5s_delay_15s.log`](../logs/test3_scenario_timeout_5s_delay_15s.log) | Test 3: シナリオスクリプト タイムアウト再現 |

---

## 結論

- `time.After()` + goroutine パターンにより、`--timeout` で指定した秒数で確実に RPC 応答待ちタイムアウトが機能することを確認した
- デフォルト RPC `<get-vrrp-information><summary/></get-vrrp-information>` により、collector_agent（vrrp_pool）と同一の NETCONF リクエストを再現できることを確認した
- netconf-mock は RPC 内容によらず `<ok/>` を返すため、タイムアウト再現テストはそのまま使用できる
