---
name: new-api-model
version: 1.0.0
description: "New API 模型元数据管理：列出/搜索模型元数据、查看当前身份可调用的模型、找出缺元数据的模型、登记/更新/删除元数据（名称、描述、图标、标签、供应商、匹配规则）。当用户说「定价页缺模型描述」「登记新模型」「我能调哪些模型」时使用。不负责：渠道对模型的支持（走 new-api-channel）。"
metadata:
  requires:
    bins: ["new-api-cli"]
  cliHelp: "new-api-cli model --help"
---

# model — 模型元数据

**开始前先读 [`../new-api-shared/SKILL.md`](../new-api-shared/SKILL.md)（认证、JSON 契约、确认门禁）。**

别名：`new-api-cli models`。

## 先分清两件事

| | 谁负责 | 影响 |
|---|---|---|
| **模型能不能调用** | 渠道（`channel` 域） | 没有渠道承载 → 请求失败 |
| **模型的展示信息** | 元数据（本 skill） | 缺失只是定价页/模型列表少描述、图标、供应商，**调用照样成功** |

用户说"gpt-4o 调不通" → 去 [`../new-api-channel/SKILL.md`](../new-api-channel/SKILL.md)，不是这里。
用户说"定价页上这个模型没有说明" → 本 skill。

## 命令一览

| 命令 | 风险 | 权限 | 说明 |
|---|---|---|---|
| `list` / `ls` | read | 管理员 | 列出模型元数据 |
| `search` | read | 管理员 | 按关键字、供应商、状态搜索 |
| `get <id>` | read | 管理员 | 单条元数据详情 |
| `available` | read | 已登录 | **当前身份可调用的模型** |
| `missing` | read | 管理员 | 渠道已支持但缺元数据的模型 |
| `create` | write | 管理员 | 登记元数据 |
| `update <id>` | write | 管理员 | 更新元数据 |
| `delete <id>` | write | 管理员 | 删除元数据（不影响调用） |

`delete` 是 `write` 而非 high-risk —— 因为它只删展示信息。

## 我能调哪些模型

```bash
new-api-cli model available                # 走 /api/user/models，按当前用户分组解析
new-api-cli model available --dashboard    # 走 /api/models，仪表盘视角
```

回答"我能用哪些模型"用不带 `--dashboard` 的版本，它最贴近实际调用时的解析结果。

## 查询元数据

```bash
new-api-cli model list --format table
new-api-cli model list --all
new-api-cli model list --status 1               # 只看启用的
new-api-cli model search --keyword gpt-4
new-api-cli model search --vendor openai
new-api-cli model get 7
```

默认输出列：`id`、`model_name`、`vendor_id`、`status`、`sync_official`、`tags`、`matched_count`。

`matched_count` 是匹配到的渠道数 —— **为 0 表示登记了元数据但没有渠道承载它**，定价页会显示一个实际调不通的模型。

## 找出缺元数据的模型

```bash
new-api-cli model missing
```

列出渠道声明支持、但尚未登记元数据的模型。这些模型**可以正常调用**，只是在定价页与模型列表里缺描述、图标与供应商信息。

新增渠道后跑一次，把新模型补齐元数据，是常规收尾动作。

## 登记与更新

```bash
new-api-cli model create --model-name gpt-4o --vendor-id 1 --status 1
new-api-cli model create --model-name gpt-4o --description "OpenAI 旗舰多模态模型" --tags vision,chat --status 1

new-api-cli model update 7 --status 2
new-api-cli model update 7 --description "新描述" --tags chat
new-api-cli model update 7 --data '{"some_new_field": "v"}'
```

| 参数 | 说明 |
|---|---|
| `--model-name` | 模型名，如 `gpt-4o`。create 时必填 |
| `--description` | 描述 |
| `--icon` | 图标 |
| `--tags` | 标签，逗号分隔（覆盖语义） |
| `--vendor-id` | 供应商 ID，见 `new-api-cli api GET /api/vendors/` |
| `--endpoints` | 支持的端点，JSON 数组字符串 |
| `--status` | 1 启用 2 禁用 |
| `--sync-official` | 是否同步官方信息：1 是 0 否 |
| `--name-rule` | 匹配规则：0 精确 1 前缀 2 包含 3 后缀 |
| `--data` | 完整 JSON 请求体，支持 `@file` 与 `-` |

`update` 是读-改-写：先取回当前记录再合并你传入的 flag，未传的字段保持原值。至少给一个字段或 `--data`。

`--data` 与 flag 同时给时，**flag 覆盖 `--data`**（flag 在后应用）。

### --name-rule 匹配规则

决定这条元数据覆盖哪些实际模型名：

| 值 | 规则 | 例子 |
|---|---|---|
| 0 | 精确 | `gpt-4o` 只匹配 `gpt-4o` |
| 1 | 前缀 | `gpt-4` 匹配 `gpt-4o`、`gpt-4-turbo` |
| 2 | 包含 | `claude` 匹配 `claude-sonnet-4`、`anthropic/claude-3` |
| 3 | 后缀 | `-preview` 匹配所有预览版 |

用前缀/包含规则给一整个系列共享描述，省去逐个登记。但要注意匹配过宽会让不相关的模型套上错误的供应商与图标。

## 删除

```bash
new-api-cli model delete 7 --yes
```

只影响展示信息。删掉后该模型**依然能被调用**，只是定价页与模型列表里少了这条描述 —— 所以它不是 high-risk。

## 常见任务

| 用户诉求 | 命令 |
|---|---|
| 我能调哪些模型 | `model available` |
| 定价页缺模型说明 | `model missing` 找出来 → `model create` 逐个补 |
| 登记新模型 | `model create --model-name X --vendor-id N --status 1` |
| 隐藏某个模型 | `model update <id> --status 2` |
| 有哪些供应商 ID | `new-api-cli api GET /api/vendors/` |
| 元数据登记了但调不通 | 看 `matched_count`；为 0 就去渠道侧 `channel search --model X` |

## 不在本 skill 范围

- 渠道支持哪些模型、模型调不通 → [`../new-api-channel/SKILL.md`](../new-api-channel/SKILL.md)
- relay 侧可用模型清单与价格表 → [`../new-api-status/SKILL.md`](../new-api-status/SKILL.md)（`status models`、`status pricing`）
- 模型倍率（计费） → [`../new-api-option/SKILL.md`](../new-api-option/SKILL.md)（`ModelRatio`）
