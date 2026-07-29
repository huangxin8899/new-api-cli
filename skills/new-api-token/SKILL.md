---
name: new-api-token
version: 1.0.0
description: "New API 的 sk- 调用令牌管理：创建/列出/搜索令牌、取明文 key、改额度与过期时间、限制模型与 IP、删除。当用户说「给我一个 key 调模型」「令牌额度用完了」「令牌过期」「限制某个 key 只能用某模型」时使用。不负责：CLI 自己的登录令牌（走 new-api-shared 的 auth 域）。"
metadata:
  requires:
    bins: ["new-api-cli"]
  cliHelp: "new-api-cli token --help"
---

# token — API 调用令牌（`sk-...`）

**开始前先读 [`../new-api-shared/SKILL.md`](../new-api-shared/SKILL.md)（认证、JSON 契约、确认门禁）。**

管理**当前登录用户**的 API 调用令牌 —— 客户端拿它调模型的那个 `sk-...`。普通用户权限即可，看到的也只是自己的令牌。

## 先分清两种令牌

| | 这个 skill（`token` 域） | `auth` 域 |
|---|---|---|
| 管什么 | **API 调用令牌** `sk-...` | **管理后台令牌**（系统访问令牌 / PAT） |
| 谁用 | 客户端 / SDK，用来调模型 | CLI 自己，用来调管理接口 |
| 用户会怎么说 | "给我个 key"、"额度用完了"、"key 泄露了要换" | "CLI 登录不上"、"令牌过期要重新登录" |

**用户说"生成一个 key" → 本 skill。** 用户说"CLI 认证失败" → `auth`，见共享 skill。

## 命令一览

| 命令 | 风险 | 说明 |
|---|---|---|
| `list` / `ls` | read | 列出我的令牌（key 为掩码） |
| `search` | read | 按名称或 key 片段搜索 |
| `get <id>` | read | 单个详情（key 为掩码） |
| [`create`](references/new-api-token-create.md) | write | 创建令牌 |
| [`update <id>`](references/new-api-token-update.md) | write | 改额度、过期、限制、状态 |
| `key <id>...` | **high-risk-write** | 取明文 key |
| `delete <id>...` | **high-risk-write** | 删除，使用该 key 的调用立即失败 |

## 查询

```bash
new-api-cli token list --format table
new-api-cli token list --all
new-api-cli token search --keyword prod
new-api-cli token search --token 1a2b        # 按 key 片段找
new-api-cli token get 12
```

列表与详情里的 `key` 已经是掩码，可以安全展示给用户。

关键字段：

| 字段 | 含义 |
|---|---|
| `status` | 1 启用；2 禁用；3 额度耗尽；4 已过期 |
| `remain_quota` | 剩余额度（原始 quota 数值） |
| `unlimited_quota` | true 时 `remain_quota` 无意义 |
| `expired_time` | Unix 秒；`-1` 表示永不过期 |
| `model_limits_enabled` / `model_limits` | 是否限制模型、限制哪些 |
| `group` | 令牌所属分组，决定可用模型与倍率 |

用户抱怨"key 突然不能用了"，先看 `status`：3 是额度耗尽（补额度），4 是过期（改 `--expired-at`），2 是被禁用。

## 取明文 key（high-risk-write）

```bash
new-api-cli token key 12 --yes
new-api-cli token key 12 13 14 --yes     # 多个走批量接口
```

明文 key 等同调用凭证，会**原样打印到 stdout**。

只在用户明确索取时执行。执行前提醒：共享终端、CI 日志、聊天记录里留存的 key 都等于泄露。key 泄露的处置是**删掉重建**，不是改名。

## 删除

```bash
new-api-cli token delete 12 --yes
new-api-cli token delete 12 13 --yes
```

删除即刻生效，使用该 key 的调用会当场失败。想临时停用而不是永久删除，用 `token update <id> --status 2`。

## 额度单位

`--quota` 和 `remain_quota` 都是原始整数 quota。换算靠站点设置：

```bash
new-api-cli option get QuotaPerUnit --raw     # 默认 500000 = $1，需超管权限
```

拿不到 `QuotaPerUnit`（非超管）时，直接向用户报原始 quota 并说明这是站点内部单位，**不要按默认值硬算美元**。

## 常见任务

| 用户诉求 | 命令 |
|---|---|
| 给我一个不限量的 key | `token create --name <名字> --unlimited` → `token search --keyword <名字>` 取 id → `token key <id> --yes` |
| 给测试用的限量 key | `token create --name trial --quota 500000 --expired-at 2026-12-31` |
| 这个 key 还剩多少额度 | `token get <id> --jq '{remain_quota, unlimited_quota, status}'` |
| key 额度用完了 | `token update <id> --quota <新值>`（`status=3` 会随额度恢复） |
| key 过期了 | `token update <id> --expired-at never` |
| 只让这个 key 调 gpt-4o | `token update <id> --model-limits gpt-4o` |
| 限制来源 IP | `token update <id> --allow-ips 1.2.3.4,5.6.7.8` |
| key 泄露了 | `token delete <id> --yes` 然后重新 `create` |
| 临时停用 | `token update <id> --status 2` |

## 不在本 skill 范围

- CLI 登录、系统访问令牌 → [`../new-api-shared/SKILL.md`](../new-api-shared/SKILL.md) 的认证一节
- 别人的令牌 / 用户额度 → [`../new-api-user/SKILL.md`](../new-api-user/SKILL.md)
- 令牌用了多少、调了什么 → [`../new-api-log/SKILL.md`](../new-api-log/SKILL.md)（`--token-name` 过滤）
- 兑换码 → [`../new-api-redemption/SKILL.md`](../new-api-redemption/SKILL.md)
