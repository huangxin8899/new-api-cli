# token update — 更新 API 调用令牌

> 前置：先读 [`../../new-api-shared/SKILL.md`](../../new-api-shared/SKILL.md)。

风险：`write`。普通用户即可（只能改自己的令牌）。

## 命令

```bash
new-api-cli token update 12 --name prod-v2
new-api-cli token update 12 --status 2                  # 停用
new-api-cli token update 12 --quota 1000000             # 补额度
new-api-cli token update 12 --expired-at never          # 取消过期
new-api-cli token update 12 --unlimited                 # 改为不限量
new-api-cli token update 12 --model-limits gpt-4o,o3    # 限制模型
new-api-cli token update 12 --model-limits ""           # 解除模型限制
new-api-cli token update 12 --allow-ips ""              # 解除 IP 限制
```

参数与 [create](new-api-token-create.md) 相同，外加 `--status`（1 启用 / 2 禁用）。至少给一个字段，否则以退出码 6 报「没有要更新的字段」。

## 读-改-写与 status_only 快路径

服务端的更新接口是**整体替换语义** —— 只提交改动字段会清空其余字段。CLI 因此先 `GET /api/token/<id>` 取回当前对象，把你传入的 flag 合并上去再整体提交。两个结果：

- **未传的字段一定保持原值**，你不用担心副作用
- **掩码后的 key 永远不会被写回**（CLI 在合并前把 `key` 删掉），否则真 key 会被替换成 `sk-1234****`

只改 `--status` 时走服务端的 `status_only=1` 快路径，不做读改写 —— 少一次请求，也避开整体替换的风险。所以**停用/启用令牌就只传 `--status`，别顺手带别的字段**。

## 各字段的语义

| 参数 | 语义 |
|---|---|
| `--quota` | 设为新值，**不是增加**。要"加 50 万"得先读当前值再相加 |
| `--unlimited` | true 后 `remain_quota` 不再生效 |
| `--model-limits` | **覆盖**。传空字符串同时把 `model_limits_enabled` 置为 false |
| `--allow-ips` | 覆盖。空字符串解除限制 |
| `--expired-at` | `never` 表示永不过期 |
| `--status` | 1 启用 2 禁用。3（额度耗尽）、4（已过期）由服务端根据额度与时间自动判定，不要手动写 |

加额度的正确做法：

```bash
cur=$(new-api-cli token get 12 --format ndjson --jq '.remain_quota')
new-api-cli token update 12 --quota $((cur + 500000))
```

## 按症状选操作

| `status` | 症状 | 修复 |
|---|---|---|
| 3 | 额度耗尽，调用报额度不足 | `--quota <新值>` 或 `--unlimited`；补上后状态自动恢复 |
| 4 | 已过期 | `--expired-at never` 或一个未来时间 |
| 2 | 被手动禁用 | `--status 1` |
| 1 但仍失败 | 可能是 `model_limits` 挡了，或 `allow_ips` 不含来源 IP | `token get <id> --jq '{model_limits_enabled, model_limits, allow_ips, group}'` 逐项核对 |

`status=1` 却报模型不可用，也可能根本不是令牌的问题 —— 查渠道侧：`new-api-cli channel search --model <模型名>`。

## 预演

```bash
new-api-cli token update 12 --quota 1000000 --dry-run
```

会打印完整请求体，包含读回来的全部字段 —— 正好能确认哪些字段将被提交。
