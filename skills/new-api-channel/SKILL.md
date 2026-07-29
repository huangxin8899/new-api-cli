---
name: new-api-channel
version: 1.0.0
description: "New API 上游渠道管理：查看/搜索渠道、新建与改配置、启停、连通性测试、余额刷新、按标签批量操作、修复渠道-模型关联表。当用户问「某模型调不通」「渠道被禁用了」「上游余额」「换 key」「加一个新上游」时使用。不负责：sk- 调用令牌（走 new-api-token）、模型元数据（走 new-api-model）。"
metadata:
  requires:
    bins: ["new-api-cli"]
  cliHelp: "new-api-cli channel --help"
---

# channel — 上游渠道

**开始前先读 [`../new-api-shared/SKILL.md`](../new-api-shared/SKILL.md)（认证、JSON 契约、确认门禁）。**

渠道是 New API 最核心也最危险的资源：它持有上游服务商的 API key，禁用一个渠道会立刻改变线上流量走向。全域需要**管理员**（role>=10），`channel key` 需要**超级管理员**（role=100）。

别名：`new-api-cli ch`。

## 快捷命令

| 命令 | 说明 |
|---|---|
| [`+health`](references/new-api-channel-health.md) | 渠道健康概览：一次拉全量，本地聚合出禁用的、响应慢的、余额不足的 |

排查类任务**先跑 `+health`**，它是只读的，不向上游发请求，一次就能定位问题面。

## 资源命令

| 命令 | 风险 | 说明 |
|---|---|---|
| `list` / `ls` | read | 列出渠道 |
| `search` | read | 按名称、分组或支持的模型搜索 |
| `get <id>` | read | 单个渠道详情（不含 key） |
| `models` | read | 模型清单 |
| `tag models <tag>` | read | 某标签下渠道支持的模型 |
| [`create`](references/new-api-channel-create.md) | write | 新建渠道 |
| [`update <id>`](references/new-api-channel-update.md) | write | 改配置（只改传入字段） |
| `enable <id>...` | write | 启用 |
| [`test`](references/new-api-channel-test.md) | write | 连通性测试（向上游发真实请求） |
| `balance [id]` | write | 刷新余额 |
| `tag set <id>...` | write | 批量打标签 |
| `tag enable <tag>` | write | 启用标签下全部渠道 |
| `fix` | write | 重建渠道-模型关联表 |
| `disable <id>...` | **high-risk-write** | 禁用，流量立即转移 |
| `delete [id...]` | **high-risk-write** | 删除，不可恢复 |
| `key <id>` | **high-risk-write** | 打印上游 API key 明文（超管） |
| `tag disable <tag>` | **high-risk-write** | 批量禁用一整个标签 |

## 渠道状态

`status` 字段的取值决定了大部分排查结论：

| 值 | 含义 | 处理 |
|---|---|---|
| 1 | 启用 | 正常 |
| 2 | 手动禁用 | 有人主动关的，确认原因再 `enable` |
| 3 | **自动禁用** | 上游报错触发 `auto_ban`。先 `channel test <id>` 确认恢复，再 `enable` |
| 0 | 未知 | 数据异常 |

`--status` 过滤只接受 `all` / `enabled` / `disabled`（`disabled` 含手动与自动）。区分 2 和 3 要看返回数据里的 `status`。

自动禁用是最常见的"渠道不见了"原因：**不要直接 enable**，那样上游一报错又会被禁。先 `test` 定位是 key 失效、余额耗尽还是模型下线。

## 查询

```bash
# 列表与过滤
new-api-cli channel list --format table
new-api-cli channel list --status disabled
new-api-cli channel list --all --group vip
new-api-cli channel list --sort-by response_time --sort-order desc
new-api-cli channel list --tag-mode                  # 按标签聚合返回

# 搜索：--keyword / --group / --model 至少给一个
new-api-cli channel search --model gpt-4o            # 哪些渠道承载这个模型
new-api-cli channel search --keyword openai

# 单个详情
new-api-cli channel get 7
```

`list` 与 `search` 都不返回 key，可安全展示。`get` 也不含 key。

**"某个模型调不通"的第一步永远是 `channel search --model <name>`** —— 它直接回答"有没有渠道承载这个模型、这些渠道是什么状态"。

## 模型清单

```bash
new-api-cli channel models                # 本站支持的全部模型
new-api-cli channel models --enabled      # 只有当前有启用渠道承载的模型
new-api-cli channel models --fetch 7      # 向渠道 7 的上游拉它真实支持的模型
```

`--fetch` 会真的请求上游，用来核对"渠道声明支持的模型"与"上游实际支持的模型"是否一致 —— 配错模型名是"无可用渠道"的常见原因。

## 写操作

新建与更新的字段较多，分别见 [create](references/new-api-channel-create.md) 与 [update](references/new-api-channel-update.md)。启停与删除：

```bash
new-api-cli channel enable 7
new-api-cli channel enable 7 8 9                 # 多个 id 走批量接口

new-api-cli channel disable 7 --yes              # high-risk：流量立即转移
new-api-cli channel delete 7 --yes               # high-risk：不可恢复
new-api-cli channel delete --disabled --yes      # 清理全部已禁用渠道
```

`delete` 的 `--disabled` 不能与显式 id 同时给。删除后走该渠道的模型若无其他渠道承接，相关请求会**立即开始失败** —— 删之前先 `channel search --model <该渠道的模型>` 确认有替代。

## 余额

```bash
new-api-cli channel balance 7        # 刷新单个
new-api-cli channel balance --all    # 刷新全部
```

余额为 0 通常表示**该渠道类型不支持余额查询**，不是真的没钱 —— `+health` 的低余额判定因此跳过了 0 值。

## 标签批量操作

标签把一组渠道当一个池子管理，适合"某个供应商整体出问题"的场景：

```bash
new-api-cli channel tag set 7 8 9 --tag openai-pool   # 打标签（--tag "" 清除）
new-api-cli channel tag models openai-pool            # 看这个池子支持哪些模型
new-api-cli channel tag enable openai-pool
new-api-cli channel tag disable openai-pool --yes     # high-risk：整池下线
```

`tag set` 的 `--tag` 必填（传空字符串表示清除）。

## 取上游 key（high-risk-write）

```bash
new-api-cli channel key 7 --yes
```

这是整个 CLI 最敏感的读操作：拿到的是上游服务商的凭证，会**明文打印到 stdout**。服务端要求 role=100，可能还要二次验证。

**默认不要执行这条命令**，即使用户说"看看渠道配置"。只有用户明确要求"取出上游 key"时才做，并在输出前提醒共享终端与日志留存风险。

## 修复渠道-模型关联表

```bash
new-api-cli channel fix --yes
```

New API 用一张 `abilities` 表记录"哪个渠道能承载哪个模型"。这张表偶尔会与渠道配置不一致，症状是**渠道明明配了某模型，请求却报「无可用渠道」**。`fix` 全表重算，渠道多时耗时较长。

判断顺序：先 `channel search --model X` 看有无渠道 → 有且是启用状态却仍报无可用渠道 → 才 `fix`。

## 排障速查

| 症状 | 排查路径 |
|---|---|
| 某模型报「无可用渠道」 | `channel search --model X` → 有没有渠道？状态是否启用？→ 都正常则 `channel fix --yes` |
| 渠道突然不工作 | `channel get <id>` 看 `status`；=3 是自动禁用 → `channel test <id>` 定位原因 |
| 请求变慢 | `channel +health --slow-ms 3000` 找慢渠道 → `channel update <id> --priority` 降权重 |
| 上游报余额不足 | `channel balance --all` 刷新 → `channel +health` 看低余额清单 |
| 换了上游 key | `channel update <id> --key @key.txt`（用 @file 避免进 shell 历史） |
| 加了新模型但调不通 | `channel models --fetch <id>` 核对上游真实支持 → `channel update <id> --models ...` → `channel fix` |
| 整个供应商挂了 | `channel tag disable <tag> --yes` 整池下线，恢复后 `tag enable` |

## 不在本 skill 范围

- `sk-` 调用令牌 → [`../new-api-token/SKILL.md`](../new-api-token/SKILL.md)
- 模型元数据（描述、图标、供应商） → [`../new-api-model/SKILL.md`](../new-api-model/SKILL.md)
- 调用日志与失败明细 → [`../new-api-log/SKILL.md`](../new-api-log/SKILL.md)
- 渠道类型编号的完整对照表 → 站点文档，或 `new-api-cli api GET /api/channel/` 看现有渠道的 `type`
