# channel create — 新建渠道

> 前置：先读 [`../../new-api-shared/SKILL.md`](../../new-api-shared/SKILL.md)。

风险：`write`。需要管理员。

## 命令

```bash
# 最小可用
new-api-cli channel create --name openai-main --type 1 --key sk-xxx --models gpt-4o

# 推荐：key 用 @file，不进 shell 历史
new-api-cli channel create \
  --name openai-main --type 1 \
  --key @openai-key.txt \
  --models gpt-4o,gpt-4o-mini \
  --group default,vip \
  --priority 10

# Anthropic
new-api-cli channel create --name claude --type 14 --key @key.txt --models claude-sonnet-4

# 先预演
new-api-cli channel create --name test --type 1 --key sk-x --models gpt-4o --dry-run
```

## 必填参数

| 参数 | 说明 |
|---|---|
| `--name` | 渠道名称 |
| `--type` | 渠道类型编号（1=OpenAI，14=Anthropic，其余见站点文档） |
| `--key` | 上游 API key；支持 `@file` 与 `-`（标准输入） |
| `--models` | 支持的模型，逗号分隔 |

缺任何一个都会以退出码 6 拒绝，错误信封的 `params` 会列出缺哪些。

不知道 `--type` 该填什么时，看已有渠道：`new-api-cli channel list --jq '.[] | {name, type}'`。

## 可选参数

| 参数 | 默认 | 说明 |
|---|---|---|
| `--group` | `default` | 可用分组，逗号分隔 |
| `--base-url` | 该类型的默认地址 | 上游地址；反代或私有部署时必填 |
| `--priority` | 0 | 优先级，越大越优先 |
| `--weight` | 0 | 同优先级内的权重 |
| `--tag` | — | 标签，用于批量操作 |
| `--test-model` | — | 连通性测试用的模型 |
| `--model-mapping` | — | 模型重定向 JSON；支持 `@file` |
| `--remark` | — | 备注（≤255 字符） |
| `--auto-ban` | `true` | 上游报错时自动禁用 |
| `--status` | — | 1 启用 2 禁用 |
| `--setting` | — | 渠道额外设置 JSON；支持 `@file` |
| `--param-override` | — | 请求参数覆盖 JSON；支持 `@file` |
| `--data` | — | 补充字段 JSON，与 flag 合并（**flag 优先**） |

`--key`、`--model-mapping`、`--setting`、`--param-override` 都接受 `@文件` 与 `-`（标准输入）。

## --auto-ban 的取舍

默认开启：上游连续报错时服务端自动把渠道置为 status=3。

- **保持开启**（推荐）：故障渠道自动摘掉，避免请求持续失败
- **关闭**（`--auto-ban=false`）：适合"这是唯一承载某模型的渠道，宁可失败也别摘"的场景 —— 但要配合监控

自动禁用后不会自动恢复，需人工 `channel test` + `channel enable`。

## --data 兜底

本命令未覆盖的字段用 `--data` 传：

```bash
new-api-cli channel create --name x --type 1 --key @k.txt --models gpt-4o \
  --data '{"channel_ratio": 1.5, "other_field": "v"}'
```

同名字段以 flag 为准。

## 返回与后续

服务端的创建接口**不回传对象**，因此 CLI 用请求里的 `name` 与 `type` 兜底回显，并在 `stderr` 提示如何取 id：

```bash
new-api-cli channel search --keyword openai-main --jq '.[] | {id, name, status}'
```

拿到 id 后建议做一次连通性验证（会消耗少量上游额度）：

```bash
new-api-cli channel test <id>
```

如果新渠道声明的模型此前站点没有，还要跑一次关联表重建，否则可能报「无可用渠道」：

```bash
new-api-cli channel fix --yes
```

## 安全

- **绝不把 key 写在命令行里** —— 会进 shell 历史与 `ps` 输出。用 `--key @file.txt` 或 `printf '%s' "$KEY" | new-api-cli channel create ... --key -`
- CI 里用 `@file` 后记得 `rm -f` 清理
- 创建成功的返回里不含 key
