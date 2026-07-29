# data — 按日聚合的用量统计

> 前置：先读 [`../../new-api-shared/SKILL.md`](../../new-api-shared/SKILL.md)。

风险：`read`。`list` / `users` 需管理员，`self` 任何登录用户可用。

服务端预聚合的按日/按模型汇总。数据量比 `log` 小几个数量级，适合趋势、对账、找用量大户。**"这个月花了多少"用它，不要用 `log --all`。**

## 命令

```bash
new-api-cli data list --since 30d --format table        # 全站按日消耗
new-api-cli data list --username alice --since 30d      # 只看某人
new-api-cli data users --since 30d --format table       # 按用户汇总
new-api-cli data self --since 30d                       # 自己的按日消耗
new-api-cli data flow --since 30d                       # 额度流水
new-api-cli data flow --self                            # 自己的流水（普通用户）
```

## 时间参数

所有子命令共享，**默认最近 7 天**：

| 参数 | 默认 | 说明 |
|---|---|---|
| `--since` | `7d` | 相对区间，如 `24h`、`30d` |
| `--start` | — | 起始时间，覆盖 `--since` |
| `--end` | 当前 | 结束时间 |

格式：Unix 秒、`2026-07-31`、`2026-07-31 10:00:00`。结束早于起始会以退出码 6 拒绝。

**`data self` 服务端限制单次跨度不超过 31 天**，超出直接报错。要更长区间就按月分段查再合并。

## 各子命令的粒度

| 命令 | 一行代表 | 常用列 |
|---|---|---|
| `data list` | 某天 × 某用户 × 某模型 | `created_at`、`user_id`、`username`、`model_name`、`count`、`quota`、`token_used` |
| `data users` | 某个用户（区间内汇总） | `user_id`、`username`、`count`、`quota`、`token_used` |
| `data self` | 某天 × 某模型（当前用户） | 同 `data list` |
| `data flow` | 一笔额度变动 | 视站点版本而定，用 `--format pretty` 先看结构 |

`--columns` 可换列。`data flow` 没有默认列，先跑 `--format pretty` 看有哪些字段再定。

## 典型问答

```bash
# 这个月总共花了多少（原始 quota）
new-api-cli data list --since 30d --all --format ndjson --jq '[.[] | .quota] | add'

# 谁用得最多，前 10
new-api-cli data users --since 30d --format ndjson --jq 'sort_by(-.quota) | .[:10] | .[] | {username, quota, count}'

# 哪个模型消耗最大
new-api-cli data list --since 30d --format ndjson \
  --jq 'group_by(.model_name) | map({model: .[0].model_name, quota: ([.[].quota] | add)}) | sort_by(-.quota)'

# 按天的调用趋势
new-api-cli data list --since 14d --format ndjson \
  --jq 'group_by(.created_at) | map({day: .[0].created_at, count: ([.[].count] | add)})'

# 导出月度用量给财务
new-api-cli data users --since 30d --format csv > usage-$(date +%Y%m).csv
```

## 额度换算

`quota` 是无单位整数。换算靠站点设置（超管权限可读）：

```bash
per=$(new-api-cli option get QuotaPerUnit --raw)     # 默认 500000 = $1
total=$(new-api-cli data list --since 30d --all --format ndjson --jq '[.[] | .quota] | add')
echo "约 \$$(echo "scale=2; $total / $per" | bc)"
```

读不到 `QuotaPerUnit`（不是超管）时，向用户报**原始 quota 并说明这是站点内部单位** —— 不要按默认值 500000 硬算美元后当作事实报出去。

## data 还是 log

| 问题 | 用哪个 |
|---|---|
| 上个月花了多少 | `data list` / `data users` |
| 谁用得最多 | `data users` |
| 按天的趋势 | `data list` |
| 某次请求为什么失败 | `log list --request-id` |
| 最近一小时有多少错误 | `log list --type error --since 1h` |
| 当前 RPM/TPM | `log stat --since 1h` |
| 某模型的具体调用参数与耗时 | `log list --model X` |

`data` 只有消耗汇总，**没有失败原因**。排障永远从 `log` 起步。

## 权限降级

普通用户跑 `data list` / `data users` 会得到退出码 5（`forbidden`）。改用：

```bash
new-api-cli data self --since 30d
new-api-cli data flow --self --since 30d
```

`data flow` 不带 `--self` 时按全站视角走，`--username` 过滤需要管理员。
