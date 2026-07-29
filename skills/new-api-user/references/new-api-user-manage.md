# user manage — 启用/禁用/提权/降权/删除

> 前置：先读 [`../../new-api-shared/SKILL.md`](../../new-api-shared/SKILL.md)。

风险：**high-risk-write**。非交互环境必须显式 `--yes`。

## 命令

```bash
new-api-cli user manage 42 enable --yes
new-api-cli user manage 42 disable --yes
new-api-cli user manage 42 promote --yes
new-api-cli user manage 42 demote --yes
new-api-cli user manage 42 delete --yes
```

## 动作

| 动作 | 作用 | 权限 | 服务端限制 |
|---|---|---|---|
| `enable` | 启用（status → 1） | 管理员 | — |
| `disable` | 禁用（status → 2） | 管理员 | **无法禁用超级管理员** |
| `promote` | 提升为管理员（role → 10） | **仅超管** | — |
| `demote` | 降为普通用户（role → 1） | **仅超管** | — |
| `delete` | 删除（软删除） | 管理员 | **无法删除超级管理员** |

动作名以外的值会被本地拦下（退出码 6），错误里会列出可选值。

## 各动作的影响面

**disable** —— 该用户无法登录，其令牌也无法再调用。是"暂时停掉某人"的正确做法，可逆。

**delete** —— 服务端是软删除（数据库记录保留），但**该用户的全部令牌立即失效**，正在用这些 key 的调用会当场失败。不可通过 CLI 撤销。

**promote / demote** —— 改的是权限边界。提权后此人能看全站日志、改渠道、删用户。降权则相反：如果对方正在用管理员身份跑脚本，降权后那些脚本会开始报退出码 5。

## 执行前必须确认

这些操作都要用户明确同意。给用户看清对象是谁，别只报 id：

```bash
new-api-cli user get 42 --jq '{id, username, display_name, role, status}'
```

确认是对的人之后再执行。尤其 `delete` 与 `demote` —— 弄错对象后没有回退按钮。

## 遇到 confirm_required 时

非交互环境下不带 `--yes` 会得到退出码 6 与：

```json
{ "ok": false, "error": { "type": "validation", "subtype": "confirm_required", "message": "该操作不可撤销：对用户 #42 执行 disable", "hint": "确认无误后加 --yes 执行", "params": ["--yes"] } }
```

正确处理：把 `message` 里的操作描述给用户 → 等明确同意 → 在原命令末尾追加 `--yes` 重试。**不要**看到这个错误就自动补 `--yes`。

## 别把自己锁在外面

`demote` 掉最后一个超级管理员，或 `disable` 掉自己正在用的账号，会让你失去继续操作的权限。执行前先确认：

```bash
new-api-cli auth status --jq '{user_id, role_name}'   # 我是谁
new-api-cli user search --role 100 --format table     # 还有几个超管
```

## 与 delete 子命令的区别

`user manage <id> delete` 与 `user delete <id>` 都走软删除，效果一致：

- `manage ... delete` 走 `POST /api/user/manage`，与其他动作同一个入口
- `user delete <id>` 走 `DELETE /api/user/<id>`

批量处理多个动作时用 `manage` 保持一致；单纯删一个用哪个都行。
