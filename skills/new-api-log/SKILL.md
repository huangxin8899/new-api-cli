---
name: new-api-log
version: 1.0.0
description: "New API 调用日志与用量统计：按时间/用户/模型/渠道/令牌/请求 ID 查日志，统计 RPM/TPM 与消耗额度，按日聚合与额度流水。当用户问「最近有多少错误」「某个请求为什么失败」「这个月花了多少」「谁用得最多」「某模型的调用量」时使用。"
metadata:
  requires:
    bins: ["new-api-cli"]
  cliHelp: "new-api-cli log --help"
---

# log / data — 调用日志与用量

**开始前先读 [`../new-api-shared/SKILL.md`](../new-api-shared/SKILL.md)（认证、JSON 契约、分页）。**

两个域，分工明确：

| 域 | 粒度 | 用途 |
|---|---|---|
| `log` | **逐条调用明细** | 排查某次失败、看某模型的具体请求 |
| `data` | **服务端预聚合的按日汇总** | 画趋势、月度对账、找用量大户 |

数据量差一个数量级：查"上个月花了多少"用 `data`，别用 `log --all` 拉几十万条。

## 命令一览

| 命令 | 权限 | 说明 |
|---|---|---|
| [`log list`](references/new-api-log-query.md) | 管理员 | 全站调用日志 |
| [`log self`](references/new-api-log-query.md) | 已登录 | 自己的调用日志 |
| `log stat` | 管理员 | 全站消耗额度与 RPM/TPM |
| `log self-stat` | 已登录 | 自己的消耗额度与 RPM/TPM |
| [`data list`](references/new-api-data-usage.md) | 管理员 | 全站按日消耗 |
| [`data users`](references/new-api-data-usage.md) | 管理员 | 按用户汇总消耗 |
| `data self` | 已登录 | 自己的按日消耗 |
| `data flow` | 混合 | 额度流水（充值、消耗、退款） |

全部只读（`read`）。

## 日志类型

`log --type` 的取值决定你看到什么：

| 值 | 含义 |
|---|---|
| `all` | 不过滤（默认） |
| `consume` | 模型调用消耗 —— 最常用 |
| `error` | **失败记录** —— 排障第一站 |
| `topup` | 充值 |
| `manage` | 管理操作 |
| `system` | 系统事件 |
| `refund` | 退款 |
| `login` | 登录 |

排查线上问题：`log list --type error --since 1h`。

## 时间范围

`--since`（相对）与 `--start`/`--end`（绝对）**互斥**，同时给会以退出码 6 拒绝。

```bash
--since 24h          # 最近 24 小时
--since 7d           # 最近 7 天
--start 2026-07-01 --end 2026-07-31
--start "2026-07-01 09:00:00"
--start 1782000000   # Unix 秒
```

`log` 系列不传时间就是全量（很慢，慎用 `--all`）。`data` 系列默认最近 7 天。

## 最短路径

```bash
# 最近一小时的失败
new-api-cli log list --type error --since 1h --format table

# 追一个具体请求
new-api-cli log list --request-id abc123

# 某用户最近一周用了什么
new-api-cli log list --username alice --since 7d --format table

# 某渠道的失败集中吗
new-api-cli log list --channel 7 --type error --since 1h

# 当前吞吐
new-api-cli log stat --since 1h

# 这个月各用户花了多少
new-api-cli data users --since 30d --format table

# 自己的用量（普通用户）
new-api-cli log self --since 7d
new-api-cli data self --since 30d
```

## 过滤参数

| 参数 | `list` | `self` | 说明 |
|---|---|---|---|
| `--type` | ✅ | ✅ | 日志类型 |
| `--since` / `--start` / `--end` | ✅ | ✅ | 时间范围 |
| `--model` | ✅ | ✅ | 模型名 |
| `--token-name` | ✅ | ✅ | 令牌名（对应 `token` 域的 `--name`） |
| `--group` | ✅ | ✅ | 分组 |
| `--request-id` | ✅ | ✅ | 请求 ID 精确查找 |
| `--upstream-request-id` | ✅ | ✅ | 上游请求 ID |
| `--username` | ✅ | ❌ | 按用户过滤（self 视图无意义） |
| `--channel` | ✅ | ❌ | 按渠道 ID 过滤 |

## 关键字段

日志条目常看这些：`created_at`、`username`、`token_name`、`model_name`、`quota`（本次消耗）、`prompt_tokens`、`completion_tokens`、`use_time`（耗时秒）、`channel_id`、`content`（失败时是错误详情）。

`log stat` 返回 `quota`（区间总消耗）、`rpm`、`tpm`。

## 权限分流

用户是普通用户（role=1）时，`log list` / `log stat` / `data list` / `data users` 都会返回退出码 5（`forbidden`）。这时改用 `self` 版本：

| 全站（管理员） | 自己（任何人） |
|---|---|
| `log list` | `log self` |
| `log stat` | `log self-stat` |
| `data list` | `data self` |
| `data flow` | `data flow --self` |

不确定权限时先 `new-api-cli auth status --jq '.role_name'`。

## 服务端限制

- **`data self` 单次查询跨度不超过 31 天**，超出直接报错 —— 要更长区间就分段查再合并
- `data flow` 系列把 0 当非法时间值，CLI 已保证两端都传实值
- `log` 系列单页上限 100 条；`--all` 会自动翻页，大区间务必配 `--limit` 设上限

## 不在本 skill 范围

- 渠道为什么失败（配置侧） → [`../new-api-channel/SKILL.md`](../new-api-channel/SKILL.md)
- 令牌额度与状态 → [`../new-api-token/SKILL.md`](../new-api-token/SKILL.md)
- 用户额度调整 → [`../new-api-user/SKILL.md`](../new-api-user/SKILL.md)
- 完整排障流程 → [`../new-api-troubleshoot/SKILL.md`](../new-api-troubleshoot/SKILL.md)
