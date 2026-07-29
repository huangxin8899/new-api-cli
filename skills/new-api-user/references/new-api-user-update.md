# user update — 更新用户

> 前置：先读 [`../../new-api-shared/SKILL.md`](../../new-api-shared/SKILL.md)。

风险：`write`。需要管理员。

## 命令

```bash
new-api-cli user update 42 --quota 5000000
new-api-cli user update 42 --group vip
new-api-cli user update 42 --display-name 小明 --email alice@example.com
new-api-cli user update 42 --password '<新密码>'
new-api-cli user update 42 --remark "2026-07 试用转正"
```

## 参数

| 参数 | 说明 |
|---|---|
| `--quota <n>` | 额度（原始 quota 数值）。**设为新值，不是增加** |
| `--group` | 分组，决定可用模型与倍率 |
| `--display-name` | 显示名 |
| `--role` | 1 普通 / 10 管理员 |
| `--status` | 1 启用 / 2 禁用 |
| `--password` | 重置密码（8-20 位） |
| `--remark` | 备注，仅管理员可见 |
| `--email` | 邮箱 |

至少给一个字段，否则以退出码 6 报「没有要更新的字段」。

## 读-改-写

服务端是整体替换语义，CLI 先 `GET /api/user/<id>` 取回当前对象再合并你的 flag。两个后果：

- 未传的字段保持原值
- **未显式传 `--password` 时该字段会被移除**，不会把空密码写回去

## 加额度不是覆盖

`--quota` 是赋值。要"加 100 万"必须先读：

```bash
cur=$(new-api-cli user get 42 --format ndjson --jq '.quota')
new-api-cli user update 42 --quota $((cur + 1000000))
```

直接 `--quota 1000000` 会把一个原本有 500 万额度的用户改成 100 万。**用户说"加额度"时务必先读当前值**，或者明确问清是"加"还是"设为"。

## role 与 status 的取舍

`--role` / `--status` 能达到和 `user manage` 一样的效果，但语义门禁不同：

| 目的 | 推荐 | 原因 |
|---|---|---|
| 提权 / 降权 | `user manage <id> promote\|demote --yes` | 走服务端专用校验（只有超管能做），风险标注也正确 |
| 封号 / 解封 | `user manage <id> disable\|enable --yes` | 同上，且不会顺带提交其他字段 |
| 改额度、分组、资料 | `user update` | manage 不支持这些字段 |

`user update --role 10` 绕过了 manage 的门禁检查，服务端仍可能拒绝，而且它是 `write` 而非 `high-risk-write` —— 提权这种事应该走 [manage](new-api-user-manage.md)。

## 重置密码

```bash
new-api-cli user update 42 --password '<新密码>'
```

密码 8-20 位。它会进 shell 历史与进程列表 —— 尽量让用户自己改，或改完后提示用户首次登录再修改一次。不要在对话中重复回显密码。

## 分组

分组决定用户能调哪些模型、按什么倍率计费。可用分组与倍率：

```bash
new-api-cli user groups            # 任何登录用户可看
```

改分组后建议验证：

```bash
new-api-cli --as-user 42 user models    # 以该用户视角看可用模型（管理员专用）
```

## 预演

```bash
new-api-cli user update 42 --quota 5000000 --dry-run
```

会打印读回来的完整请求体 —— 正好用来确认哪些字段将被提交，尤其是确认 `password` 已被移除。
