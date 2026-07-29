---
name: new-api-api
version: 1.0.0
description: "new-api-cli 的通用 HTTP 调用兜底：按方法与路径直接访问 New API 任意接口，支持 --data/--params（内联 JSON / @file / stdin）、--page-all 自动翻页、--raw 透传原始响应、-o 写文件。仅当目标接口没有对应资源命令时使用。"
metadata:
  requires:
    bins: ["new-api-cli"]
  cliHelp: "new-api-cli api --help"
---

# api — 通用调用（兜底）

**开始前先读 [`../new-api-shared/SKILL.md`](../new-api-shared/SKILL.md)（认证、JSON 契约、确认门禁）。**

风险：**high-risk-write** —— 方法由调用方决定，可能是 DELETE，所以一律按最高风险标注，非交互环境需 `--yes`。

## 先确认真的需要它

`api` 是三层调用的最后一层。**有资源命令就用资源命令**：

| 别写 | 该写 |
|---|---|
| `api GET /api/channel/` | `channel list` |
| `api POST /api/token/` | `token create` |
| `api PUT /api/user/` | `user update <id>` |
| `api DELETE /api/channel/7` | `channel delete 7 --yes` |

资源命令做了参数校验、风险分级、输出投影，还有**读-改-写**处理。用 `api PUT` 手搓更新是最容易出事的操作：多数更新接口是整体替换语义，漏字段就会清空数据。

`api` 的合理场景：

- 接口尚无对应命令（如 `/api/vendors/`、`/api/group/`）
- 需要传 CLI 未覆盖的字段
- 需要原始响应字节（`--raw`）
- 探索性调试

找命令：`new-api-cli --help`，然后 `new-api-cli <域> --help`。

## 命令

```bash
new-api-cli api GET /api/channel/
new-api-cli api GET /api/channel/ --params '{"p":1,"page_size":10}'
new-api-cli api GET /channel/ --page-all --jq '.[].name'
new-api-cli api POST /api/channel/ --data @channel.json --yes
new-api-cli api PUT /api/option/ --data '{"key":"AutoDisable","value":"true"}' --yes
new-api-cli api GET /api/status --raw
new-api-cli api GET /api/pricing -o pricing.json
```

支持的方法：`GET`、`POST`、`PUT`、`PATCH`、`DELETE`、`HEAD`。

## 路径规则

- **`/api` 前缀可省略**：`/channel/` 与 `/api/channel/` 等价
- 以 `/v1`、`/mj`、`/suno`、`/pg` 开头的路径**保持原样** —— 这些是 relay 侧接口
- 末尾斜杠有意义：New API 的列表接口通常是 `/api/channel/`（带斜杠）

## 参数

| 参数 | 说明 |
|---|---|
| `--data` | 请求体 JSON：内联 \| `@文件` \| `-`（标准输入） |
| `--params` | 查询参数 JSON：同上 |
| `--raw` | 透传原始响应，不解析 `success`/`data` 信封 |
| `--page-all` | 自动翻页取回全部 items（**仅 GET**） |
| `--limit <n>` | 配合 `--page-all`，最多取回多少条（0 = 不限） |
| `-o, --output <file>` | 把原始响应写入文件（隐含 `--raw`） |

约束（都会在本地拦下，退出码 6）：

- `--page-all` 只能用于 GET
- `--raw` 与 `--page-all` 互斥（raw 不解析信封，无法翻页）
- `--data` 与 `--params` 不能同时从标准输入读

`--data` 会先在本地校验是合法 JSON，避免把明显错误的载荷发出去。

## --params 的摊平规则

`--params` 接受 JSON 对象，摊平成查询串：

| JSON 值 | 结果 |
|---|---|
| `{"p": 1}` | `?p=1` |
| `{"ok": true}` | `?ok=true` |
| `{"ids": [1,2,3]}` | `?ids=1&ids=2&ids=3`（重复键） |
| `{"x": null}` | 跳过 |

## --raw 与信封

默认（不带 `--raw`）走标准信封：解析服务端的 `success`/`data`，输出统一的 `{"ok":true,"data":...}`，错误翻译成类型化信封与退出码。

`--raw` 直接把响应字节写到 stdout，**不加信封、不翻译错误**。用于：

- 接口返回的不是标准信封结构
- 需要逐字节保存（图片、文件）
- 调试服务端到底返回了什么

```bash
new-api-cli api GET /api/status --raw | jq .
new-api-cli api GET /api/pricing -o pricing.json      # 文件权限 0600
```

`-o` 隐含 `--raw`，成功后输出写入路径与字节数。

## 探索未知接口的流程

```bash
# 1. 先只读探路
new-api-cli api GET /api/vendors/ --format pretty

# 2. 看清结构后决定要改什么，先预演
new-api-cli api POST /api/vendors/ --data '{"name":"openai"}' --yes --dry-run

# 3. 确认请求体无误再真发
new-api-cli api POST /api/vendors/ --data '{"name":"openai"}' --yes
```

**写操作一律先 `--dry-run`。** 它打印完整的 method / path / body / base_url，不发请求也不触发门禁。

## 写操作的正确姿势

手搓 PUT 时必须自己完成读-改-写，否则会清空未提交的字段：

```bash
# 错：只提交一个字段，其余字段被整体替换清空
new-api-cli api PUT /api/token/ --data '{"id":12,"name":"new"}' --yes

# 对：先读回完整对象，改一个字段再整体提交
new-api-cli api GET /api/token/12 --format ndjson \
  | jq '.name = "new" | del(.key)' \
  | new-api-cli api PUT /api/token/ --data - --yes
```

注意 `del(.key)` —— 读回来的 `key` 是掩码，写回去会把真 key 覆盖成 `sk-1234****`。

**这正是资源命令替你处理的事情。** 能用 `token update 12 --name new` 就别手搓。

## 翻页

```bash
new-api-cli api GET /api/channel/ --page-all
new-api-cli api GET /api/log/ --page-all --limit 500
```

服务端单页上限 100 条。`--page-all` 会自动翻到底，大表务必配 `--limit`。

## 常用未覆盖接口

| 用途 | 命令 |
|---|---|
| 供应商列表（`model --vendor-id` 要用） | `api GET /api/vendors/` |
| 分组列表 | `api GET /api/group/` |
| 站点原始状态 | `api GET /api/status --raw` |

具体有哪些接口取决于站点版本 —— 用 `status --jq '.version'` 确认版本后查对应的 New API 文档。
