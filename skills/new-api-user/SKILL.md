---
name: new-api-user
version: 1.0.0
description: "New API 站点用户管理：查看自身信息与可用模型/分组、列出与搜索用户、创建用户、启用/禁用/提权/降权/删除、改额度与分组。当用户说「加个账号」「封了这个人」「给某人加额度」「提为管理员」「我能用哪些模型」时使用。不负责：sk- 调用令牌（走 new-api-token）。"
metadata:
  requires:
    bins: ["new-api-cli"]
  cliHelp: "new-api-cli user --help"
---

# user — 用户

**开始前先读 [`../new-api-shared/SKILL.md`](../new-api-shared/SKILL.md)（认证、JSON 契约、确认门禁）。**

`self` / `models` / `groups` 任何已登录用户都能用；其余全部需要**管理员**（role>=10），提权降权需要**超级管理员**（role=100）。

## 命令一览

| 命令 | 风险 | 权限 | 说明 |
|---|---|---|---|
| `self` / `me` | read | 已登录 | 当前登录用户详情 |
| `models` | read | 已登录 | 当前用户可调用的模型 |
| `groups` | read | 已登录 | 可用分组及其倍率 |
| `list` / `ls` | read | 管理员 | 列出全部用户 |
| `search` | read | 管理员 | 按关键字、角色、分组、状态搜索 |
| `get <id>` | read | 管理员 | 单个用户详情 |
| `create` | write | 管理员 | 创建用户 |
| [`update <id>`](references/new-api-user-update.md) | write | 管理员 | 改额度、分组、显示名、密码等 |
| [`manage <id> <action>`](references/new-api-user-manage.md) | **high-risk-write** | 管理员/超管 | 启用/禁用/提权/降权/删除 |
| `delete <id>` | **high-risk-write** | 管理员 | 删除用户，其令牌立即失效 |

## 角色

| 值 | 角色 | 能做什么 |
|---|---|---|
| 1 | 普通用户 | 管自己的令牌、看自己的日志与用量 |
| 10 | 管理员 | 渠道、用户、日志全站视图 |
| 100 | 超级管理员 | 加上系统设置、提权降权、取渠道 key、性能指标 |

创建用户时**只能创建比自己角色低的**：管理员（10）创建不了管理员，得超管来。

## 自身信息

```bash
new-api-cli user self
new-api-cli user self --jq '{username, role, quota, used_quota, group}'
new-api-cli user models      # 我能调哪些模型
new-api-cli user groups      # 有哪些分组、倍率多少
```

用户问"我能用哪些模型" → `user models`（按当前用户分组解析，最准）。
用户问"为什么我调 gpt-4o 报错" → 先 `user models` 看在不在列表里；不在就是分组权限问题，在就往渠道侧查。

## 查询用户

```bash
new-api-cli user list --format table
new-api-cli user list --all
new-api-cli user list --sort-by quota --sort-order desc     # 谁额度最多
new-api-cli user list --sort-by used_quota --sort-order desc # 谁用得最多

new-api-cli user search --keyword alice        # 用户名/显示名/邮箱
new-api-cli user search --role 10              # 有哪些管理员
new-api-cli user search --status 2             # 有哪些被禁用的
new-api-cli user search --group vip
new-api-cli user get 42
```

关键字段：`id`、`username`、`display_name`、`role`、`status`（1 启用 2 禁用）、`group`、`quota`（剩余额度）、`used_quota`（已用）、`request_count`。

## 创建用户

```bash
new-api-cli user create --username alice --password 'S3cret!pass'
new-api-cli user create --username bob --password 'S3cret!pass' --display-name 小明 --role 1
```

| 参数 | 必填 | 说明 |
|---|---|---|
| `--username` | ✅ | 用户名（≤20 字符） |
| `--password` | ✅ | 密码（**8-20 位**） |
| `--display-name` | | 显示名，默认同用户名 |
| `--role` | | 1 普通 / 10 管理员（须低于自己的角色） |

密码会出现在 shell 历史与进程列表里。给用户建账号时**建议让用户自己改密码**，或提示他们首次登录后修改。不要在对话里回显密码明文超过必要范围。

新用户的初始额度由站点设置 `QuotaForNewUser` 决定；要单独给额度用 `user update <id> --quota <n>`。

## 额度

`quota` 是无单位整数，换算靠 `QuotaPerUnit`（超管权限可读）：

```bash
new-api-cli option get QuotaPerUnit --raw       # 默认 500000 = $1
```

读不到时向用户报原始数值并说明是站点内部单位，别按默认值硬算。

## 常见任务

| 用户诉求 | 命令 |
|---|---|
| 有哪些管理员 | `user search --role 10 --format table` |
| 加个账号 | `user create --username x --password '...'` |
| 给某人加额度 | `user update <id> --quota <新值>`（覆盖，不是增加） |
| 把某人放进 vip 分组 | `user update <id> --group vip` |
| 封号 | `user manage <id> disable --yes` |
| 解封 | `user manage <id> enable --yes` |
| 提为管理员 | `user manage <id> promote --yes`（超管） |
| 删号 | `user manage <id> delete --yes` 或 `user delete <id> --yes` |
| 谁用得最多 | `user list --all --sort-by used_quota --sort-order desc --format table` |
| 某人用了什么 | `new-api-cli log list --username alice --since 7d`（走 new-api-log） |
| 重置某人密码 | `user update <id> --password '<新密码>'` |

## 不在本 skill 范围

- `sk-` 调用令牌 → [`../new-api-token/SKILL.md`](../new-api-token/SKILL.md)
- 某用户的调用明细与用量 → [`../new-api-log/SKILL.md`](../new-api-log/SKILL.md)
- 分组倍率的设置（不是查看） → [`../new-api-option/SKILL.md`](../new-api-option/SKILL.md)
- 兑换码充值 → [`../new-api-redemption/SKILL.md`](../new-api-redemption/SKILL.md)
