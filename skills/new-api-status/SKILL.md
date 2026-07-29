---
name: new-api-status
version: 1.0.0
description: "New API 站点状态与公开信息：站点版本与配置探测（无需登录）、公告、关于页、模型价格表、relay 侧可用模型、运行时性能指标、集群实例列表。当用户问「站点通不通」「什么版本」「价格表」「集群有几个节点」时使用。"
metadata:
  requires:
    bins: ["new-api-cli"]
  cliHelp: "new-api-cli status --help"
---

# status — 站点状态

**开始前先读 [`../new-api-shared/SKILL.md`](../new-api-shared/SKILL.md)（认证、JSON 契约）。**

全部只读（`read`）。

## 命令

| 命令 | 权限 | 说明 |
|---|---|---|
| `status` | **无需登录** | 站点基本信息与前端配置 |
| `status notice` | **无需登录** | 站点公告 |
| `status about` | **无需登录** | 关于页内容 |
| `status pricing` | 已登录 | 模型价格表 |
| `status models` | 已登录 | relay 侧可用模型 |
| `status test` | 管理员 | 触发站点自检 |
| `status perf` | 超管 | 运行时性能指标 |
| `status instances` | 超管 | 集群实例列表 |

## 排查连通性的第一步

`status` 走 `/api/status`，**不需要认证**，所以它能把"站点不通"与"没登录/没权限"区分开：

```bash
new-api-cli status                                       # 用当前 profile 的地址
new-api-cli status --base-url https://api.example.com    # 不改配置，直接试一个地址
new-api-cli --insecure status                            # 自签名证书的私有部署
```

判读：

| 结果 | 结论 |
|---|---|
| 成功返回 | 地址对、站点活着。后续失败就是认证或权限问题 |
| 退出码 8（`network`） | 地址不通、DNS、TLS 或防火墙 |
| 退出码 7 但返回了 HTML/非预期结构 | 地址指向的不是 New API（可能是反代或落地页） |

`config init` 内部也是用这个接口做连通性探测的。

## 站点信息里看什么

```bash
new-api-cli status --jq '{version, system_name, start_time}'
new-api-cli status --format pretty                       # 完整结构
```

`version` 很重要：不同 New API 版本的接口字段集与设置项并不一致。遇到"某个字段没有""某个设置项不存在"时，先确认版本。

## 价格与可用模型

```bash
new-api-cli status pricing
new-api-cli status pricing --jq '.[:5]'
new-api-cli status models
new-api-cli status models --jq 'length'
```

`status models` 是 **relay 侧的可用性视角**（`/api/models`），和这两个都不一样：

| 命令 | 视角 |
|---|---|
| `status models` | relay 侧，网关整体暴露的模型 |
| `new-api-cli model available` | 按**当前用户分组**解析的可调用模型 |
| `new-api-cli model list` | 模型**元数据**管理（描述、图标、供应商） |
| `new-api-cli channel models` | **渠道**声明支持的模型 |

回答"我能调什么" → `model available`。回答"网关上有什么" → `status models`。排查"为什么调不通" → `channel search --model X`。

## 运行时指标与集群

```bash
new-api-cli status perf                    # 超管
new-api-cli status instances               # 超管
new-api-cli status test                    # 管理员，触发自检
```

`instances` 默认列：`node_name`、`version`、`start_time`、`last_heartbeat`。多节点部署时用它确认：

- 节点数对不对（有没有掉线的）
- **各节点 `version` 是否一致** —— 滚动升级中途版本不齐会导致行为不一致的诡异问题
- `last_heartbeat` 是否新鲜

```bash
new-api-cli status instances --format table
new-api-cli status instances --jq '[.[] | .version] | unique'    # 版本齐不齐
```

## 公告与关于页

```bash
new-api-cli status notice
new-api-cli status about
```

都不需要登录。用户问"站点有什么公告"时用 `notice`。

## 常见任务

| 用户诉求 | 命令 |
|---|---|
| 站点通不通 | `new-api-cli status` |
| 这个地址对不对 | `new-api-cli status --base-url <地址>` |
| 什么版本 | `new-api-cli status --jq '.version'` |
| 价格表 | `new-api-cli status pricing --format table` |
| 集群几个节点、版本齐不齐 | `new-api-cli status instances --format table` |
| 性能指标 | `new-api-cli status perf` |

## 不在本 skill 范围

- 改站点设置 → [`../new-api-option/SKILL.md`](../new-api-option/SKILL.md)
- 模型元数据 → [`../new-api-model/SKILL.md`](../new-api-model/SKILL.md)
- 渠道健康 → [`../new-api-channel/SKILL.md`](../new-api-channel/SKILL.md)（`channel +health`）
- CLI 自己的版本 → `new-api-cli version`
