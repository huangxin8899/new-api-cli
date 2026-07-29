# channel +health — 渠道健康概览

> 前置：先读 [`../../new-api-shared/SKILL.md`](../../new-api-shared/SKILL.md)。

一次拉取全部渠道（自动翻页，`status=-1` 取所有状态），在**本地**聚合出需要关注的部分。只读，不向上游发任何请求，因此可以随意调用。

排查类任务的第一条命令。

## 命令

```bash
new-api-cli channel +health
new-api-cli channel +health --format table
new-api-cli channel +health --slow-ms 3000
new-api-cli channel +health --min-balance 10
```

## 参数

| 参数 | 默认 | 说明 |
|---|---|---|
| `--slow-ms <n>` | 5000 | 响应时间超过该毫秒数视为慢 |
| `--min-balance <f>` | 1 | 余额低于该值视为不足 |

## 返回结构

`data` 是一个对象（不是数组），键名稳定：

```json
{
  "total": 12,
  "healthy": 9,
  "disabled":    [ { "id": 3, "name": "azure-east", "status": 3, "status_text": "自动禁用", "reason": "自动禁用" } ],
  "slow":        [ { "id": 5, "name": "proxy-cn", "response_time_ms": 8200, "reason": "响应 8200ms 超过阈值 5000ms" } ],
  "low_balance": [ { "id": 8, "name": "openai-sub", "balance": 0.4, "reason": "余额 0.40 低于阈值 1.00" } ]
}
```

注意字段名是 **`low_balance`**（不是 `low`）。每条记录还带 `group`、`tag`、`used_quota` 等辅助字段。

一行结论写到 **stderr**（如「12 个渠道中 9 个健康；1 个未启用，1 个响应慢，1 个余额不足」），stdout 只有 JSON。

## 判定规则

| 分类 | 规则 |
|---|---|
| `disabled` | `status != 1`，含手动禁用（2）与自动禁用（3） |
| `slow` | 仅对启用中的渠道判定，`response_time > --slow-ms`。`response_time == 0` 表示从未测试过，不算慢 |
| `low_balance` | `0 < balance < --min-balance`。余额恰为 0 视为「该渠道类型不支持余额查询」，不告警 |
| `healthy` | 一条问题都没命中 |

一个渠道可能同时出现在多个分类里，但 `healthy` 只统计零问题的，所以 `healthy + 各类去重数 = total`，**不要**用 `healthy + len(disabled) + len(slow) + len(low_balance)` 去核对总数。

## 脚本取值

`--jq` 作用在 `data` 上，配 `--format ndjson` 拿裸值：

```bash
# 未启用的渠道数
new-api-cli channel +health --format ndjson --jq '.disabled | length'

# 逐条打印问题渠道
new-api-cli channel +health --format ndjson --jq -r '.disabled[] | "  #\(.id) \(.name) — \(.reason)"'

# 巡检脚本
bad=$(new-api-cli channel +health --format ndjson --jq '.disabled | length')
if [ "$bad" -gt 0 ]; then
  echo "有 $bad 个渠道未启用"
  exit 1
fi
```

## 之后做什么

`+health` 只告诉你哪里不对，不做修复。按分类走：

- `disabled` 里 `status=3`（自动禁用）→ `new-api-cli channel test <id>` 定位上游为什么报错，**别直接 enable**
- `disabled` 里 `status=2`（手动禁用）→ 确认是谁关的、为什么，再 `channel enable <id>`
- `slow` → 用 `channel update <id> --priority <低值>` 降优先级，或查 `new-api-cli log list --channel <id> --since 1h` 看是否集中失败
- `low_balance` → 先 `channel balance <id>` 刷新确认不是陈旧数据，再联系上游充值
