---
name: new-api-troubleshoot
version: 1.0.0
description: "New API 线上问题的端到端排查工作流：从「某模型调不通」「请求全失败」「变慢了」「额度不对」等模糊症状出发，按固定顺序定位到渠道、令牌、用户额度、站点设置或网络。当用户描述的是症状而不是具体命令时，先读本 skill。"
metadata:
  requires:
    bins: ["new-api-cli"]
  cliHelp: "new-api-cli --help"
---

# 排障工作流

**开始前先读 [`../new-api-shared/SKILL.md`](../new-api-shared/SKILL.md)（认证、JSON 契约、确认门禁）。**

用户描述的是**症状**而不是命令时走本 skill。这里只做定位，具体修复动作跳到对应域的 skill。

## 铁律

1. **先只读，后写入。** 定位阶段全部用 `read` 命令，不要边查边改。
2. **一次只改一件事。** 同时调倍率和换渠道，出了问题分不清是哪一个。
3. **改之前记原值。** 尤其 `option set` 与 `channel update --models`。
4. **区分"上游没钱"与"本站额度不足"** —— 前者查渠道余额，后者查令牌/用户额度。这是最常见的误判。

## 第 0 步：确认自己能查

```bash
new-api-cli auth status --jq '{username, role_name, base_url}'
```

| 退出码 | 含义 | 处理 |
|---|---|---|
| 0 | 凭据有效 | 继续 |
| 3 | 未配置 | `config init`，见共享 skill |
| 4 | 未登录/令牌失效 | `auth login --token <PAT>` |
| 5 | 权限不足 | 换管理员账号；普通用户只能用 `self` 系列 |
| 8 | 网络不通 | 跳到「站点整体不可用」 |

`role_name` 决定你能查到什么：普通用户看不到全站日志和渠道。

## 症状：某个模型调不通

最常见的问题。按顺序走，**不要跳步**：

```bash
# 1. 有渠道承载这个模型吗？状态如何？
new-api-cli channel search --model gpt-4o --format table
```

| 结果 | 结论 | 下一步 |
|---|---|---|
| 空 | 没有渠道配置这个模型 | 加渠道或给现有渠道加模型：[channel update](../new-api-channel/references/new-api-channel-update.md) |
| 有，但 `status != 1` | 渠道被禁用 | 见下面「渠道被禁用」 |
| 有且 `status = 1` | 配置看起来对 | 进第 2 步 |

```bash
# 2. 看实际失败原因
new-api-cli log list --model gpt-4o --type error --since 1h --format table
```

`content` 字段是上游/本站的错误原文，据它分流：

| `content` 关键词 | 根因 | 去哪 |
|---|---|---|
| 无可用渠道 / no available channel | 关联表与渠道配置不一致 | `channel fix --yes` |
| invalid_api_key、401 | 上游 key 失效 | [channel update](../new-api-channel/references/new-api-channel-update.md) 换 key |
| insufficient_quota、billing | **上游**余额耗尽 | `channel balance --all` |
| 额度不足、quota | **本站**令牌或用户额度耗尽 | [token](../new-api-token/SKILL.md) / [user](../new-api-user/SKILL.md) |
| model_not_found | 上游不提供该模型 | `channel models --fetch <id>` 核对 |
| rate_limit | 限流 | 上游限流，或站点 `RetryTimes` 设置 |
| timeout | 超时 | 见「变慢了」 |

```bash
# 3. 渠道状态正常、日志也没有相关错误 → 关联表问题
new-api-cli channel fix --yes
```

`abilities` 表记录"哪个渠道能承载哪个模型"，偶尔与渠道配置不一致，症状正是"配了却报无可用渠道"。

```bash
# 4. 调用方视角确认
new-api-cli model available --jq '.[] | select(. == "gpt-4o")'
```

不在列表里说明是**分组权限**问题：该用户/令牌的 group 不允许这个模型。查 `token get <id> --jq '{group, model_limits_enabled, model_limits}'` 与 `user get <id> --jq '.group'`。

## 症状：渠道被禁用了

```bash
new-api-cli channel +health --format table
new-api-cli channel get <id> --jq '{status, response_time, balance, auto_ban}'
```

`status` 的含义决定处理方式：

- **3 = 自动禁用** —— 上游报错触发 `auto_ban`。**绝对不要直接 enable**，先找根因：
  ```bash
  new-api-cli channel test <id>          # 看上游现在还报什么
  new-api-cli log list --channel <id> --type error --since 24h --format table
  ```
  修好根因（换 key / 充值 / 改模型列表）后再 `channel test <id>` → 通过了才 `channel enable <id>`。
- **2 = 手动禁用** —— 有人主动关的。先搞清为什么，再决定是否 `enable`。
  ```bash
  new-api-cli log list --type manage --since 7d --format table    # 找管理操作记录
  ```

详见 [channel test](../new-api-channel/references/new-api-channel-test.md)。

## 症状：请求全都失败

范围判断先做，别一头扎进单个渠道：

```bash
# 1. 站点还活着吗（无需登录）
new-api-cli status --jq '{version, system_name}'

# 2. 渠道全景
new-api-cli channel +health --format table

# 3. 失败分布在哪
new-api-cli log list --type error --since 15m --all \
  --jq '[.[] | .channel_id] | group_by(.) | map({channel: .[0], count: length})'
```

| 分布 | 结论 |
|---|---|
| 集中在一个渠道 | 该渠道的配置或上游问题 |
| 散布在所有渠道 | 站点侧问题：设置改动、额度、限流，或多节点版本不一致 |
| 全部渠道都 disabled | 可能是 `AutomaticDisableChannelEnabled` 配合上游集体故障 |

站点侧继续查：

```bash
new-api-cli log list --type manage --since 24h --format table    # 最近有人改了什么
new-api-cli status instances --jq '[.[] | .version] | unique'    # 多节点版本齐不齐（超管）
new-api-cli option list --prefix Automatic --format table        # 自动禁用相关开关（超管）
```

**滚动升级中途版本不齐**会导致行为不一致的诡异问题，`unique` 结果超过一个元素就要怀疑它。

## 症状：变慢了

```bash
new-api-cli channel +health --slow-ms 3000 --format table
new-api-cli log stat --since 1h            # 当前 RPM/TPM
new-api-cli log list --since 1h --all --columns model_name,use_time,channel_id --format table
```

慢渠道的处理优先**降优先级**而不是禁用 —— 保留兜底能力：

```bash
new-api-cli channel update <id> --priority 0
```

`response_time == 0` 表示从未测试过，不是"极快"。要拿到真实数字得跑 `channel test <id>`（消耗少量上游额度）。

## 症状：额度/计费不对

先确定说的是哪一层额度：

```bash
new-api-cli option get QuotaPerUnit --raw        # 换算单位（超管）
new-api-cli user get <id> --jq '{quota, used_quota, group}'
new-api-cli token get <id> --jq '{remain_quota, unlimited_quota, status, group}'
new-api-cli data users --since 30d --format table # 谁用了多少
new-api-cli log list --username <u> --since 24h --format table
```

三层额度容易混：

| 层 | 字段 | 耗尽的表现 |
|---|---|---|
| 上游服务商 | `channel.balance` | 日志里 `insufficient_quota`、`billing` |
| 本站用户 | `user.quota` | 日志里「额度不足」 |
| 本站令牌 | `token.remain_quota` | `token.status = 3` |

计费数字对不上时查倍率（超管）：

```bash
new-api-cli option get ModelRatio --raw | jq '.["gpt-4o"]'
new-api-cli option get CompletionRatio --raw | jq '.["gpt-4o"]'
new-api-cli option get GroupRatio --raw
```

**报数字给用户时，`quota` 是无单位整数。** 要么给原始值，要么先读 `QuotaPerUnit` 换算，不要按默认 500000 硬算。

## 症状：站点整体不可用

```bash
new-api-cli status                                        # 无需登录
new-api-cli status --base-url https://api.example.com     # 换地址试
new-api-cli --insecure status                             # 自签名证书
new-api-cli status --verbose                              # 请求详情到 stderr
new-api-cli config show                                   # 当前 base_url 对不对
```

| 退出码 | 含义 | 检查 |
|---|---|---|
| 8 + `connection_refused` | 端口不通 | 服务是否在跑、防火墙 |
| 8 + `timeout` | 慢或被墙 | 网络路径、`--timeout` |
| 8 + `tls` | 证书问题 | 自签名加 `--insecure`；或证书过期 |
| 7 但结构不像 New API | 地址指向别处 | 反代配置、落地页 |

`status` 通了但其他命令报 4/5 → 不是站点问题，是认证或权限，回第 0 步。

## 症状：CLI 自己有问题

```bash
new-api-cli version
new-api-cli config show                    # profile、base_url、是否已登录
new-api-cli config list                    # 是不是连错了实例
new-api-cli auth list                      # 各 profile 的登录态
new-api-cli <命令> --verbose                # 请求详情
new-api-cli <命令> --dry-run               # 只看将要发什么
```

多实例环境下"命令没报错但数据不对"，八成是 `--profile` 连到了另一个站点。`config show` 的 `base_url` 是第一个要核对的东西。

## 定位完成后

修复动作按域跳转：

| 根因在 | 去哪 |
|---|---|
| 渠道配置、key、模型列表、启停 | [`../new-api-channel/SKILL.md`](../new-api-channel/SKILL.md) |
| 令牌额度、过期、模型限制 | [`../new-api-token/SKILL.md`](../new-api-token/SKILL.md) |
| 用户额度、分组、封禁 | [`../new-api-user/SKILL.md`](../new-api-user/SKILL.md) |
| 站点设置、倍率、开关 | [`../new-api-option/SKILL.md`](../new-api-option/SKILL.md) |
| 模型元数据缺失 | [`../new-api-model/SKILL.md`](../new-api-model/SKILL.md) |
| 接口没有对应命令 | [`../new-api-api/SKILL.md`](../new-api-api/SKILL.md) |

修复是写操作，**改动前把定位结论和将要执行的命令告诉用户，等确认再执行** —— high-risk-write 还需要 `--yes`。
