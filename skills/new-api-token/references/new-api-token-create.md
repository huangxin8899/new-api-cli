# token create — 创建 API 调用令牌

> 前置：先读 [`../../new-api-shared/SKILL.md`](../../new-api-shared/SKILL.md)。

风险：`write`。普通用户即可。

## 命令

```bash
# 不限额度，永不过期
new-api-cli token create --name prod --unlimited

# 限额 + 有效期
new-api-cli token create --name trial --quota 500000 --expired-at 2026-12-31

# 只能调指定模型
new-api-cli token create --name gpt4-only --unlimited --model-limits gpt-4o,gpt-4o-mini

# 限制来源 IP
new-api-cli token create --name ci --quota 1000000 --allow-ips 203.0.113.5
```

## 参数

| 参数 | 必填 | 默认 | 说明 |
|---|---|---|---|
| `--name` | ✅ | — | 令牌名称（≤50 字符） |
| `--quota <n>` | | 0 | 剩余额度（原始 quota 数值） |
| `--unlimited` | | false | 无限额度；给了它 `--quota` 就没意义 |
| `--expired-at` | | 永不过期 | `never` \| Unix 秒 \| `2026-12-31` \| `2026-01-31T10:00:00Z` |
| `--group` | | 站点默认 | 分组，决定可用模型与倍率 |
| `--model-limits` | | 不限制 | 允许的模型，逗号分隔。**留空即不限制** |
| `--allow-ips` | | 不限制 | 允许的来源 IP，逗号分隔 |
| `--cross-group-retry` | | false | 跨分组重试（仅 `auto` 分组有效） |

`--quota 0` 且不带 `--unlimited` 会创建一个**立刻就用不了**的令牌 —— 用户没说额度时优先问清，或用 `--unlimited`。

`--model-limits` 一旦给值，服务端就把 `model_limits_enabled` 置为 true；清除限制要在 update 时传空字符串。

## 取回明文 key

服务端的创建接口**不回传令牌对象**，所以 CLI 只回显你提交的 `name`，并在 `stderr` 给出后续步骤。完整流程是三步：

```bash
# 1. 创建
new-api-cli token create --name prod --unlimited

# 2. 用名称找 id
new-api-cli token search --keyword prod --jq '.[] | {id, name, status}'

# 3. 取明文 key（high-risk-write，需确认）
new-api-cli token key <id> --yes
```

第 3 步会把 `sk-...` 明文打到 stdout。**只在用户明确要 key 时做**，并提醒终端与日志留存风险。

如果创建后不需要立刻给出 key（例如只是预备额度），就停在第 1 步。

## 命名建议

名称是后面唯一的检索锚点（创建接口不回 id），所以：

- 用可检索的唯一名字：`prod-web-2026q3` 而不是 `test`
- 同名令牌可以共存，会让 `search` 拿到多条，取 id 时容易搞错
- 名称会出现在调用日志的 `token_name` 里，便于后续按令牌归因用量

## 时间格式

`--expired-at` 接受：

```bash
--expired-at never                     # 永不过期（内部为 -1）
--expired-at 2026-12-31                # 日期
--expired-at "2026-12-31 23:59:59"     # 日期时间
--expired-at 2026-12-31T23:59:59Z      # RFC 3339
--expired-at 1798761599                # Unix 秒
```

不传 `--expired-at` 时默认永不过期。
