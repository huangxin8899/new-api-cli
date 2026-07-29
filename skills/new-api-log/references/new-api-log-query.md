# log list / self — 查询调用日志

> 前置：先读 [`../../new-api-shared/SKILL.md`](../../new-api-shared/SKILL.md)。

风险：`read`。`list` 需管理员，`self` 任何登录用户可用。

## 命令

```bash
# 全站（管理员）
new-api-cli log list --since 24h
new-api-cli log list --type error --since 1h --format table
new-api-cli log list --username alice --model gpt-4o --since 7d
new-api-cli log list --channel 7 --type error --since 1h
new-api-cli log list --request-id abc123
new-api-cli log list --token-name prod --since 24h

# 自己（普通用户）
new-api-cli log self --since 7d
new-api-cli log self --type error --since 1h
```

## 过滤参数

| 参数 | 说明 |
|---|---|
| `--type` | `all`（默认）\| `consume` \| `error` \| `topup` \| `manage` \| `system` \| `refund` \| `login` |
| `--since` | 相对区间，如 `24h`、`7d`。**与 `--start` 互斥** |
| `--start` / `--end` | 绝对区间：Unix 秒、`2026-07-31`、`2026-07-31 10:00:00` |
| `--model` | 模型名 |
| `--token-name` | 令牌名 |
| `--group` | 分组 |
| `--request-id` | 请求 ID 精确查找 |
| `--upstream-request-id` | 上游请求 ID |
| `--username` | 用户名（仅 `list`） |
| `--channel` | 渠道 ID（仅 `list`） |

`--since` 与 `--start` 同时给会以退出码 6 拒绝 —— 这是刻意的，避免两个都传却只有一个生效的静默歧义。

分页参数：`--page`、`--page-size`（上限 100）、`--all`、`--limit`、`--columns`。

## 默认输出列

`created_at`、`username`、`token_name`、`model_name`、`quota`、`prompt_tokens`、`completion_tokens`、`use_time`、`channel_id`。

`--columns` 可换列，`--format table` 给人看：

```bash
new-api-cli log list --since 1h --columns created_at,username,model_name,quota,use_time --format table
```

## 排障：定位一次失败

```bash
# 1. 最近的失败有哪些
new-api-cli log list --type error --since 1h --format table

# 2. 拿到 request_id 后看详情
new-api-cli log list --request-id <id> --format pretty
```

失败记录的 `content` 字段是**上游返回的错误详情**，是判断根因的关键。常见模式：

| `content` 关键词 | 含义 | 下一步 |
|---|---|---|
| 无可用渠道 / no available channel | 没有渠道承载该模型，或关联表不一致 | `channel search --model X`，必要时 `channel fix --yes` |
| invalid_api_key / 401 | 上游 key 失效 | `channel update <id> --key @new.txt --yes` |
| insufficient_quota / billing | 上游余额耗尽 | `channel balance --all` |
| 额度不足 / quota | **用户或令牌**额度耗尽（不是上游） | `token get <id>`、`user get <id>` |
| rate_limit | 限流 | 上游或本站限流配置 |
| timeout / context deadline | 超时 | `channel +health --slow-ms 3000` 找慢渠道 |

注意区分"上游余额不足"与"本站额度不足"：前者查渠道，后者查令牌/用户。

## 按维度归因

```bash
# 失败集中在哪个渠道
new-api-cli log list --type error --since 1h --all --jq '[.[] | .channel_id] | group_by(.) | map({channel: .[0], count: length})'

# 失败集中在哪个模型
new-api-cli log list --type error --since 1h --all --jq '[.[] | .model_name] | group_by(.) | map({model: .[0], count: length})'

# 谁的调用最多
new-api-cli log list --since 24h --all --jq '[.[] | .username] | group_by(.) | map({user: .[0], count: length}) | sort_by(-.count)'
```

集中在单个渠道 → 渠道配置问题。散布在所有渠道 → 站点或上游整体问题，或本站额度/限流配置。

## 吞吐与消耗

```bash
new-api-cli log stat --since 1h              # 全站：quota、rpm、tpm
new-api-cli log stat --since 1h --model gpt-4o
new-api-cli log self-stat --since 24h        # 自己的
```

`stat` 接受与 `list` 相同的过滤参数，返回区间内的总消耗与 RPM/TPM，比拉全量明细快得多。**只要用户问的是"多少/多快"而不是"具体哪几条"，就用 `stat`。**

## 大区间的取数策略

```bash
# 先看有多少
new-api-cli log list --since 7d --page-size 1 --jq 'length'    # 看 meta.total 更准

# 有上限地取回
new-api-cli log list --type error --since 7d --all --limit 500
```

不带 `--limit` 的 `--all` 在大站点上会拉很久。结果被截断时 `stderr` 有提示。

需要长区间的**汇总**而非明细时，改用 `data` 域：见 [data 用量统计](new-api-data-usage.md)。

## 告警脚本

```bash
#!/bin/bash
errors=$(new-api-cli log list --type error --since 1h --all --format ndjson --jq 'length')
if [ "$errors" -gt 10 ]; then
  echo "警告：最近 1 小时有 $errors 条错误日志"
  new-api-cli log list --type error --since 1h --all --format ndjson \
    --jq -r '[.[] | .model_name] | group_by(.) | map("\(.[0]): \(length)") | .[]'
  exit 1
fi
```
