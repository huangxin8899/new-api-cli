# channel update — 更新渠道

> 前置：先读 [`../../new-api-shared/SKILL.md`](../../new-api-shared/SKILL.md)。

风险：`write`（传 `--key` 时会额外要求确认）。需要管理员。

## 命令

```bash
new-api-cli channel update 7 --priority 10
new-api-cli channel update 7 --models gpt-4o,gpt-4o-mini,o3
new-api-cli channel update 7 --key @new-key.txt --yes
new-api-cli channel update 7 --group default,vip --tag openai-pool
new-api-cli channel update 7 --status 2                # 等价于 disable，但不触发 disable 的门禁
```

## 语义：只改传入的字段

只有**显式传入**的 flag 会被提交，其余字段服务端保持原值。CLI 用 `Flags().Changed()` 判定，所以 `--priority 0` 与不传 `--priority` 是两件不同的事。

参数集与 [create](new-api-channel-create.md) 相同，此处只列语义有坑的：

| 参数 | 语义 |
|---|---|
| `--models` | **整体覆盖，不是追加**。要加一个模型必须把原有的全写上 |
| `--group` | 同样整体覆盖 |
| `--key` | 替换上游凭证；会触发一次确认（非交互需 `--yes`） |
| `--tag` | 覆盖标签；批量打标签用 `channel tag set` |
| `--status` | 1 启用 2 禁用 |
| `--data` | 补充字段 JSON，与 flag 合并，flag 优先 |

至少要给一个字段，否则以退出码 6 报「没有要更新的字段」。

## 追加模型的正确做法

`--models` 是覆盖语义，先读再写：

```bash
# 1. 读当前模型列表
cur=$(new-api-cli channel get 7 --format ndjson --jq -r '.models')

# 2. 追加后提交
new-api-cli channel update 7 --models "$cur,o3"
```

或者一步用 jq 拼：

```bash
new-api-cli channel update 7 --models "$(new-api-cli channel get 7 --format ndjson --jq -r '.models'),o3"
```

改完模型集合后跑一次关联表重建：

```bash
new-api-cli channel fix --yes
```

## 换 key

```bash
# 推荐：@file
printf '%s' "$NEW_KEY" > key.txt
new-api-cli channel update 7 --key @key.txt --yes
rm -f key.txt

# 或标准输入
printf '%s' "$NEW_KEY" | new-api-cli channel update 7 --key - --yes
```

CLI 从不把掩码后的 key 写回服务端 —— 更新时只提交你显式传入的 key。

换完 key 后验证：

```bash
new-api-cli channel test 7
```

若渠道此前因 key 失效被自动禁用（status=3），换 key 后还需手动启用：

```bash
new-api-cli channel enable 7
```

## 调优先级与权重

New API 按 `priority` 降序选渠道，同优先级内按 `weight` 分流：

```bash
new-api-cli channel update 7 --priority 100     # 优先走这个
new-api-cli channel update 8 --priority 100 --weight 3   # 同级，权重更高
new-api-cli channel update 9 --priority 0       # 降为兜底
```

处理慢渠道时优先降 priority 而不是直接 disable —— 前者保留兜底能力。

## 预演

改动影响面不确定时先看请求体：

```bash
new-api-cli channel update 7 --models gpt-4o --dry-run
```

`--dry-run` 只打印 method / path / body，不发请求、不触发门禁。
