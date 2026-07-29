# channel test — 连通性测试

> 前置：先读 [`../../new-api-shared/SKILL.md`](../../new-api-shared/SKILL.md)。

风险：`write`。需要管理员。

向上游发一次**真实请求**验证渠道可用性。会消耗上游少量额度，并更新渠道的 `response_time` 与 `test_time`。

## 命令

```bash
new-api-cli channel test 7                # 测单个
new-api-cli channel test 7 --model gpt-4o # 指定测试用的模型
new-api-cli channel test --all --yes      # 测全部（要确认）
```

| 参数 | 说明 |
|---|---|
| `--model <name>` | 用哪个模型测；不传则用渠道的 `test_model`，再退回渠道第一个模型 |
| `--all` | 测全部渠道，不能与具体 id 同时给 |

## --all 的代价

`--all` 会遍历所有渠道，每个都发真实请求：

- 渠道多时**耗时较长**（串行）
- 消耗每个上游的额度
- **失败的渠道可能被服务端自动禁用**（取决于该渠道的 `auto_ban` 设置）

因此它要求确认。不要为了"看看整体情况"跑 `--all` —— 那是 `channel +health` 的活，后者只读且免费。

`--all` 的合理场景：批量换 key 后集中验证、上游宣布故障恢复后确认。

## 解读结果

成功返回里带耗时；失败时错误信封的 `message` 是上游原文，这是定位问题的关键：

| 上游报错关键词 | 含义 | 处理 |
|---|---|---|
| `invalid_api_key`、`Incorrect API key`、401 | key 失效或被撤销 | `channel update <id> --key @new.txt --yes` |
| `insufficient_quota`、`billing` | 上游余额耗尽 | 充值；`channel balance <id>` 刷新确认 |
| `model_not_found`、`does not exist` | 该模型上游不提供 | `channel models --fetch <id>` 核对，改 `--models` |
| `rate_limit` | 上游限流 | 通常不是配置问题，稍后重试或降优先级 |
| 超时 / `connection` | 网络或 base_url 不对 | 检查 `--base-url`，反代是否正常 |

## 与自动禁用的关系

渠道 `status=3` 表示被自动禁用。恢复流程：

```bash
new-api-cli channel test 7        # 1. 先测，确认上游真的恢复了
new-api-cli channel enable 7      # 2. 通过后才启用
```

**顺序不能反。** 直接 `enable` 会让故障渠道重新接流量，报错后又被自动禁用，中间那批请求白白失败。

如果 `test` 仍然失败，先修根因（换 key、充值、改模型列表），修完再测。

## 测试与日志配合

`test` 只给单次结果。要看这个渠道近期的真实失败情况：

```bash
new-api-cli log list --channel 7 --type error --since 1h --format table
```

零星失败可能是上游抖动，持续失败才是配置问题。
