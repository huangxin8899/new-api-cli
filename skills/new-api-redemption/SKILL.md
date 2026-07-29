---
name: new-api-redemption
version: 1.0.0
description: "New API 兑换码管理：批量签发兑换码、列出与搜索、改额度与状态、删除与清理失效码。当用户说「生成一批充值码」「兑换码发出去了要作废」「清理用过的码」时使用。"
metadata:
  requires:
    bins: ["new-api-cli"]
  cliHelp: "new-api-cli redemption --help"
---

# redemption — 兑换码

**开始前先读 [`../new-api-shared/SKILL.md`](../new-api-shared/SKILL.md)（认证、JSON 契约、确认门禁）。**

批量签发与管理兑换码。全域需要**管理员**。别名：`new-api-cli redeem`。

## 最重要的一条

**兑换码明文只在 `create` 的返回里出现一次，列表接口不会再给出。** 创建时立刻保存输出，丢了只能作废重发。

## 命令一览

| 命令 | 风险 | 说明 |
|---|---|---|
| `list` / `ls` | read | 列出兑换码 |
| `search <keyword>` | read | 按名称或码搜索 |
| `get <id>` | read | 单个详情 |
| `create` | write | 批量签发 |
| `update <id>` | write | 改名称、额度、状态、过期 |
| `delete <id>` | **high-risk-write** | 删除，不可恢复 |
| `prune` | **high-risk-write** | 清理全部已失效码 |

## 批量签发

```bash
new-api-cli redemption create --name 双十一 --quota 500000 --count 10
new-api-cli redemption create --name 试用 --quota 100000 --count 1 --expired-at 2026-12-31
```

| 参数 | 必填 | 默认 | 限制 |
|---|---|---|---|
| `--name` | ✅ | — | 批次名称，**1-20 字符** |
| `--quota` | ✅ | — | 每个码的额度，必须为正数 |
| `--count` | | 1 | 生成数量，**1-100** |
| `--expired-at` | | 永不过期 | `never` \| Unix 秒 \| `2026-12-31` |

超出 `--count` 上限或 `--quota <= 0` 会在本地拦下（退出码 6），不会发请求。要发 500 个码就跑 5 次。

返回的 `data` 是本次生成的**码明文数组**，`meta.count` 是数量，`stderr` 会提醒仅此一次可见。

存盘的正确做法：

```bash
new-api-cli redemption create --name 双十一 --quota 500000 --count 10 \
  --format ndjson --jq -r '.[]' > codes-双十一.txt
```

先存文件再给用户，避免明文只存在于终端回滚缓冲里。

## 额度换算

`--quota` 是原始整数。站点默认 500000 ≈ $1，但**必须实际读取**：

```bash
new-api-cli option get QuotaPerUnit --raw     # 需超管权限
```

用户说"发 10 个 5 美元的码"时，先拿到 `QuotaPerUnit` 再算 `--quota`。读不到就把换算关系问清楚，别按默认值硬算。

## 查询

```bash
new-api-cli redemption list --format table
new-api-cli redemption list --all
new-api-cli redemption search 双十一
new-api-cli redemption get 7
```

默认列：`id`、`name`、`status`、`quota`、`used_user_id`、`created_time`、`redeemed_time`、`expired_time`。

状态：

| 值 | 含义 |
|---|---|
| 1 | 未使用 |
| 2 | 已禁用 |
| 3 | 已使用 |

`used_user_id` 是兑换人的用户 ID，配合 `redeemed_time` 可以查"这个码谁用了、什么时候用的"。

## 更新

```bash
new-api-cli redemption update 7 --status 2                        # 作废
new-api-cli redemption update 7 --name 双十一-延期 --expired-at 2027-01-31
new-api-cli redemption update 7 --quota 1000000                   # 改额度（仅未使用的码有意义）
```

读-改-写：先取回当前记录再合并 flag，未传字段保持原值。码明文（`key`）不参与更新，不会被覆盖成空。

**作废一个已发出去的码用 `--status 2`，不是 `delete`** —— 前者保留审计记录，后者记录也没了。

## 删除与清理

```bash
new-api-cli redemption delete 7 --yes      # 删单个
new-api-cli redemption prune --yes         # 清理全部已失效
```

`prune` 的失效判定由**服务端**做（已兑换、已禁用、已过期），CLI 不做本地筛选。返回的 `data` 是被删除的条数。

跑 `prune` 前先确认范围：

```bash
new-api-cli redemption list --all --format table --jq '[.[] | select(.status != 1)]'
```

删除不可恢复，兑换记录也一并消失 —— 需要留存对账数据时改用 `--status 2` 逐个禁用。

## 常见任务

| 用户诉求 | 命令 |
|---|---|
| 发 10 个充值码 | `redemption create --name <批次> --quota <额度> --count 10`（立即存盘） |
| 某个码发错了要作废 | `redemption search <名称>` 取 id → `redemption update <id> --status 2` |
| 这批码用掉几个了 | `redemption search <批次名> --all --jq '[.[] | .status] | group_by(.) | map({status: .[0], count: length})'` |
| 谁用了这个码 | `redemption get <id> --jq '{used_user_id, redeemed_time}'` |
| 清理用过的码 | 先 list 确认 → `redemption prune --yes` |
| 延长有效期 | `redemption update <id> --expired-at 2027-01-31` |

## 不在本 skill 范围

- 直接给用户加额度（不走兑换码） → [`../new-api-user/SKILL.md`](../new-api-user/SKILL.md)（`user update --quota`）
- 充值记录与额度流水 → [`../new-api-log/SKILL.md`](../new-api-log/SKILL.md)（`log list --type topup`、`data flow`）
- `sk-` 调用令牌 → [`../new-api-token/SKILL.md`](../new-api-token/SKILL.md)
