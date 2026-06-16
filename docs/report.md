# 動作検証レポート

**実施日**: 2026-06-16
**対象コミット**: `c73109b` (docs: add Keep-Alive debug log and interval alignment notes)
**検証者**: ttsubo2000

---

## 検証概要

以下の3点を検証する。

1. `time.After()` + goroutine パターンによるタイムアウト機構が正しく機能すること
2. デフォルト RPC が `<get-vrrp-information><summary/></get-vrrp-information>` となり、collector_agent（vrrp_pool）と同一の RPC が送信されること
3. SSH Keep-Alive 間隔が collector_agent と同じ 5 秒固定になっており、デバッグログで確認できること

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
| SSH 接続タイムアウト | 10s 固定（collector_agent 準拠） |
| Keep-Alive 間隔 | 5s 固定（collector_agent 準拠） |

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
[NETCONF DEBUG] Send (164 bytes): <rpc ...><get-vrrp-information><summary/></get-vrrp-information></rpc>
[NETCONF DEBUG] Receive: waiting for delimiter "]]>]]>"
  ← goroutine がブロック中（mock は 15s 遅延中）
[NETCONF DEBUG] SSH: Keep-Alive sent          ← 5秒後に Keep-Alive 送信（collector_agent準拠）
  ← time.After(5s) 発火 → s.Close()
[NETCONF DEBUG] SSH: closing session
[ERROR] Timeout after 5s waiting for RPC reply
```

**確認ポイント**:
- `<get-vrrp-information><summary/></get-vrrp-information>` の RPC 送信 ✅
- `SSH: Keep-Alive sent` が RPC 待機中に出力（5s 間隔）✅
- 5 秒でタイムアウト ✅

**結果**: exit=1 → **PASS**

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
[NETCONF DEBUG] Exec: RPC completed successfully message-id=...
[INFO] RPC succeeded
[DATA] <rpc-reply ...><ok/></rpc-reply>
```

**確認ポイント**:
- 3 秒で正常応答（Keep-Alive 発火前に完了）✅
- exit=0 ✅

**結果**: exit=0 → **PASS**

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
[NETCONF DEBUG] Send (164 bytes): <rpc ...><get-vrrp-information><summary/></get-vrrp-information></rpc>
[NETCONF DEBUG] Receive: waiting for delimiter "]]>]]>"
[NETCONF DEBUG] SSH: Keep-Alive sent          ← 5s後に Keep-Alive 送信
[NETCONF DEBUG] SSH: closing session          ← time.After(5s) 発火
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
| SSH 接続タイムアウト | `time.Second*10`（ハードコード） | `10s`（固定）✅ |
| Keep-Alive 間隔 | 5s（10s÷2）固定 | 5s（10s÷2）固定 ✅ |
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
- SSH Keep-Alive が 5 秒間隔（collector_agent と同一）で動作し、タイムアウト待機中に `SSH: Keep-Alive sent` ログが出力されることを確認した
- netconf-mock は RPC 内容によらず `<ok/>` を返すため、タイムアウト再現テストはそのまま使用できる
