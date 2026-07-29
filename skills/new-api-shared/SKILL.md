---
name: new-api-shared
version: 1.0.0
description: "new-api-cli 的通用规则：配置初始化与多实例（config init / --profile）、登录认证（auth login --token / -u）、两种令牌的区别、JSON 输出信封、退出码契约、high-risk-write 确认门禁、--jq/--format/--dry-run/分页。任何 new-api-cli 任务开始前先读本 skill。"
metadata:
  requires:
    bins: ["new-api-cli"]
  cliHelp: "new-api-cli --help"
---

# new-api-cli 共享规则

本 skill 说明用 new-api-cli 操作 New API 网关的通用契约。**任何其他 new-api-* skill 都以本文为前提。**

## 三层命令，按粒度选

```bash
new-api-cli channel +health                 # 1. 快捷命令（+ 前缀）— 一句话完成一件事，优先用
new-api-cli channel list --status enabled   # 2. 资源命令 — 与管理接口一一对应，有校验和投影
new-api-cli api GET /api/channel/           # 3. 通用调用 — 接口尚无命令时的兜底
```

规则：**有资源命令就不要用 `api`**。资源命令做了参数校验、风险标注与输出投影，`api` 一律按 high-risk-write 处理。

## 配置与认证

### 前置检查

不确定当前是否可用时，先跑这两条（都是只读）：

```bash
new-api-cli config show     # 看 base_url、profile、是否已登录（令牌已脱敏）
new-api-cli auth status     # 校验令牌并返回用户名、角色、额度
```

`auth status` 的退出码可直接分支：`0` 凭据有效，`3` 未配置，`4` 未登录/令牌失效，`5` 权限不足。

### 初始化配置

```bash
# 交互式（人类用户，只需一次）
new-api-cli config init

# 非交互（Agent / CI）— 必须给 --base-url，否则报退出码 6
new-api-cli config init --base-url https://api.example.com --profile-name prod --force
```

配置写入 `~/.new-api-cli/config.yaml`（可用 `NEW_API_CLI_HOME` 改写），令牌单独存 `credentials.json`（0600）。

### 登录

| 场景 | 命令 |
|---|---|
| CLI / CI / Agent（推荐） | `new-api-cli auth login --token <系统访问令牌>` |
| 临时排查 | `new-api-cli auth login -u admin`（密码交互式询问，不进 shell 历史） |
| 账号开了两步验证 | `new-api-cli auth login -u admin --code 123456` |
| 密码登录换长期令牌 | `new-api-cli auth login -u admin --generate-token` |
| 退出 | `new-api-cli auth logout` |

系统访问令牌在 New API 网页端「个人设置 → 系统访问令牌」生成。

**Agent 与 CI 完全不需要配置文件**，纯环境变量即可：

```bash
export NEW_API_BASE_URL="https://api.example.com"
export NEW_API_TOKEN="<系统访问令牌>"
new-api-cli channel list --jq '.[].name'
```

环境变量：`NEW_API_BASE_URL`、`NEW_API_TOKEN`、`NEW_API_PROFILE`、`NEW_API_USER_ID`、`NEW_API_CLI_HOME`。优先级：**flag > 环境变量 > 配置文件**。

### 必须区分的两种「令牌」

这是最容易搞错的一点：

| | 管什么 | 长什么样 | 谁用 |
|---|---|---|---|
| `auth` 域 | **管理后台令牌**（系统访问令牌 / PAT） | 一串 hex | CLI 自己，用来调管理接口 |
| `token` 域 | **API 调用令牌** | `sk-...` | 客户端，用来调模型 |

用户说"生成一个 key 给我调用模型" → `new-api-cli token create`。
用户说"CLI 登录不上了" → `auth` 域。

`auth token` 会生成新的系统访问令牌并**立即作废旧的**，包括其他机器、其他 CI 上正在用的那一份 —— 属于 high-risk-write。

### 多实例（profile）

一个 profile 对应一个 New API 实例：

```bash
new-api-cli config list                          # 看有哪些 profile、哪个是当前
new-api-cli config use prod                      # 切换默认
new-api-cli channel list --profile staging       # 单次调用临时切换，不改默认
new-api-cli config set timeout 120               # 改单个字段
```

## JSON 输出契约

`--format json`（默认）下，成功与错误的信封结构不同。

**成功** → **stdout**，退出码 0：

```json
{ "ok": true, "data": [ { "id": 1, "name": "openai-main" } ], "meta": { "count": 1, "total": 42, "page": 1, "page_size": 20 } }
```

**失败** → **stderr**，退出码非 0：

```json
{ "ok": false, "error": { "type": "auth", "subtype": "forbidden", "message": "权限不足", "hint": "管理接口需要管理员（role>=10）", "params": ["--profile"] } }
```

**判断成功用 `ok == true` 或进程退出码 0。** 不要用 `code == 0` —— 成功信封没有顶层 `code`，`code` 只出现在错误信封的 `error` 里，含义是 New API 的业务错误码。按 New API 原始格式 `{"success":true}` 或 `{"code":0}` 判断会把成功误判为失败，在封装写操作时尤其危险。

`stdout` 只承载结果，进度、警告、摘要一律走 `stderr` —— 所以 `new-api-cli ... | jq` 永远拿到干净 JSON。

### 错误分类与退出码

`error.type` 决定退出码，Agent 据此分支：

| type | subtype 举例 | 退出码 | 该怎么做 |
|---|---|---|---|
| `config` | `not_configured`、`profile_not_found` | 3 | 引导 `config init` 或换 `--profile` |
| `auth` | `not_logged_in`、`token_invalid`、`token_expired` | 4 | 引导 `auth login` |
| `auth` | `forbidden`、`need_admin`、`need_root` | 5 | 权限不够，换管理员账号；**不要重试** |
| `validation` | `invalid_argument`、`missing_argument`、`confirm_required` | 6 | 按 `hint` 与 `params` 改参数后重试 |
| `api` | `not_found` | 9 | 资源不存在，先查列表拿正确 id |
| `api` | `server_error`、`rate_limited` | 7 | 读 `message`；限流可退避重试 |
| `network` | `timeout`、`connection_refused`、`tls` | 8 | 查 `--base-url`；自签名证书加 `--insecure` |
| `internal` | — | 1 | CLI 缺陷，报告给用户 |

错误信封里的 `hint` 是给出可执行的下一步，`params` 指出该改哪个 flag —— 遇错先读这两个字段，别猜。

## 高风险操作的确认门禁

命令 `--help` 顶部会标注风险等级：

- `read` — 只读，随便调
- `write` — 会改服务端数据
- `high-risk-write` — 不可逆或立即影响线上流量

`high-risk-write` 在交互终端会询问确认；在非交互环境（Agent、CI、管道）**必须显式传 `--yes`**，否则以退出码 6、`subtype: confirm_required` 拒绝执行。确认发生在发出请求**之前**。

遇到 `confirm_required` 时的正确流程：

1. **不要**默认加 `--yes` 静默重试 —— 那等于禁用门禁
2. 把要执行的操作、影响面告诉用户，等明确同意
3. 用户同意 → 在**原 argv 末尾追加 `--yes`** 重试
4. 用户拒绝 → 终止，不要改写参数绕过

想先让用户 review 具体请求，加 `--dry-run`：它不触发门禁，只打印将要发出的 method / path / body，不做任何改动。

```bash
new-api-cli channel delete 7 --yes --dry-run    # 预演，安全
```

### 各域的 high-risk-write 一览

| 命令 | 为什么危险 |
|---|---|
| `channel disable` / `delete` | 流量立即转移或开始失败 |
| `channel key` | 打印上游服务商凭证明文 |
| `channel tag disable` | 批量禁用一整个标签下的渠道 |
| `token key` / `token delete` | 打印调用凭证 / 使用该 key 的调用立即失败 |
| `user manage` / `user delete` | 提权降权、删号，用户令牌立即失效 |
| `option set` | 直接改线上行为（计费、登录开关） |
| `redemption delete` / `prune` | 兑换码不可恢复 |
| `config remove` | 删除 profile 与其令牌 |
| `auth token` | 作废其他机器上的系统访问令牌 |
| `api`（通用调用） | 方法由调用方决定，一律按最高风险处理 |

## 输出格式与过滤

```bash
--format json      # 完整 JSON 信封（默认，Agent 友好）
--format table     # 对齐表格，给人看
--format pretty    # 缩进键值树，适合看单条记录
--format ndjson    # 每行一条记录，适合管道流式处理
--format csv       # 导入表格软件
```

`--jq`（`-q`）用 jq 表达式过滤：

```bash
new-api-cli channel list --jq '.[].name'                                  # 提取字段
new-api-cli channel list --jq '.[] | {id, name, status}'                   # 组合投影
new-api-cli channel list --all --jq '[.[] | select(.status != 1) | .name]' # 条件筛选
new-api-cli channel list --all --jq 'length'                               # 计数
```

`--jq` 作用在 `data` 上（不是整个信封）。脚本里要取裸值时配 `--format ndjson`，避免值被包在信封里：

```bash
count=$(new-api-cli channel +health --format ndjson --jq '.disabled | length')
```

给人看结果时优先 `--format table`；给自己后续处理时用 json + `--jq`。

## 分页

```bash
new-api-cli channel list                 # 默认第 1 页，20 条
new-api-cli channel list --page 2 --page-size 50
new-api-cli channel list --all           # 自动翻页取回全部
new-api-cli log list --all --limit 500   # 最多 500 条
new-api-cli api GET /api/channel/ --page-all
```

服务端单页上限 100 条，`--page-size` 超过会被截断。数据量未知时先不带 `--all` 看 `meta.total` 再决定。结果被 `--limit` 截断时 `stderr` 会有提示。

## 安全规则

- **不要在命令行写密码**：`-p` 会进 shell 历史与进程列表，用 `--token` 或让它交互式询问
- **密钥用 `@file` 或 `-` 传**：`--key`、`--value-file`、`--data` 都支持 `@文件` 与 `-`（标准输入）
- **不要把令牌明文回显给用户**，除非用户明确索取；`channel key`、`token key` 会把凭证打到 stdout，注意共享终端与日志留存
- 更新类命令是**读-改-写**：只提交改动字段会被服务端整体替换语义清空其余字段，CLI 已代为处理，但这也意味着**掩码后的 key 绝不会被写回**
- 服务端返回的字段在渲染前会清掉 ANSI 转义与控制字符，防终端注入

## 两个必须知道的服务端行为

1. **业务失败也返回 HTTP 200**，靠 body 里的 `success:false` 区分。CLI 客户端层已统一翻译成错误信封，所以你只看 `ok` / 退出码就够了。
2. **多数更新接口是整体替换语义**。`channel update` / `token update` / `user update` / `model update` / `redemption update` 都是先读当前对象再合并 flag 提交 —— 因此**不要**用 `api PUT` 手搓更新，除非你自己完整回填了所有字段。

## 排障与调试

```bash
new-api-cli status                                       # 公开接口，无需登录，验证站点通不通
new-api-cli status --base-url https://api.example.com    # 不改配置，直接试一个地址
new-api-cli channel list --verbose                       # 请求详情写到 stderr
new-api-cli --insecure channel list                      # 自签名证书的私有部署
```

## 意图路由

| 用户意图 | 去哪个 skill |
|---|---|
| 上游渠道、连通性、"某模型没法调用"、余额 | [`../new-api-channel/SKILL.md`](../new-api-channel/SKILL.md) |
| `sk-` 调用令牌、额度、过期 | [`../new-api-token/SKILL.md`](../new-api-token/SKILL.md) |
| 站点用户、提权、封号、额度 | [`../new-api-user/SKILL.md`](../new-api-user/SKILL.md) |
| 调用日志、用量统计、错误排查、成本 | [`../new-api-log/SKILL.md`](../new-api-log/SKILL.md) |
| 模型元数据、定价页缺信息 | [`../new-api-model/SKILL.md`](../new-api-model/SKILL.md) |
| 兑换码批量签发 | [`../new-api-redemption/SKILL.md`](../new-api-redemption/SKILL.md) |
| 站点系统设置、倍率、开关 | [`../new-api-option/SKILL.md`](../new-api-option/SKILL.md) |
| 站点状态、公告、定价、集群实例 | [`../new-api-status/SKILL.md`](../new-api-status/SKILL.md) |
| 接口没有对应命令 | [`../new-api-api/SKILL.md`](../new-api-api/SKILL.md) |
| "线上出问题了"、不知道从哪查 | [`../new-api-troubleshoot/SKILL.md`](../new-api-troubleshoot/SKILL.md) |

## 额度单位

New API 的额度是无单位整数 `quota`。换算靠站点设置 `QuotaPerUnit`（默认 500000 = $1）：

```bash
new-api-cli option get QuotaPerUnit --raw
```

给用户报数字时要么给原始 quota，要么先读 `QuotaPerUnit` 换算成美元，**不要凭默认值硬编码**。
