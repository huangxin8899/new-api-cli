# new-api-cli

[New API](https://github.com/QuantumNous/new-api) 网关的命令行工具 —— 让人类和 AI Agent 都能在终端里管理渠道、令牌、用户、日志与系统设置。

## 为什么用它

- **Agent 原生** — 统一 JSON 信封、稳定退出码契约、`--jq` 过滤、`--dry-run` 预演，Agent 无需额外适配
- **风险分级** — 每条命令标注 `read` / `write` / `high-risk-write`，破坏性操作强制确认门禁
- **三层调用** — 快捷命令 → 资源命令 → 通用调用，覆盖从日常运维到未实现接口的全部场景
- **多实例** — 一套配置管理 dev / staging / prod，`--profile` 随时切换
- **凭据隔离** — 配置与令牌分文件存放，令牌文件 0600，输出中一律脱敏

## 安装

需要 Go 1.23+。

```bash
git clone <repo-url>
cd new-api-cli
go build -o new-api-cli .
```

可选：装到 PATH 里。

```bash
# Linux / macOS
sudo mv new-api-cli /usr/local/bin/

# Windows（PowerShell，管理员）
move new-api-cli.exe C:\Windows\System32\
```

## 快速开始

### 人类用户

```bash
# 1. 初始化站点配置（交互式引导，只需一次）
new-api-cli config init

# 2. 登录 —— 推荐用系统访问令牌
#    在 New API 网页端「个人设置 → 系统访问令牌」生成
new-api-cli auth login --token <你的系统访问令牌>

# 3. 验证
new-api-cli auth status

# 4. 开始使用
new-api-cli channel +health --format table
```

### AI Agent / CI

无需配置文件，纯环境变量驱动：

```bash
export NEW_API_BASE_URL="https://api.example.com"
export NEW_API_TOKEN="<系统访问令牌>"

new-api-cli channel list --jq '.[].name'
```

## 认证

两种凭据，按场景选：

| 类型 | 获取方式 | 有效期 | 适用场景 |
| --- | --- | --- | --- |
| **系统访问令牌（推荐）** | 网页端个人设置页，或 `auth token` | 长期 | CLI、CI、Agent |
| **密码登录** | `auth login -u <用户名>` | 短期，会过期 | 临时排查 |

```bash
# 系统访问令牌（推荐）
new-api-cli auth login --token 1234abcd...

# 用户名密码（密码交互式询问，不进 shell 历史）
new-api-cli auth login -u admin

# 开启了两步验证的账号
new-api-cli auth login -u admin --code 123456

# 密码登录后换成长期令牌
new-api-cli auth login -u admin --generate-token

# 查看 / 退出
new-api-cli auth status
new-api-cli auth whoami
new-api-cli auth logout
```

> **注意区分两种「令牌」**：`auth` 管理的是**管理后台令牌**（操作 CLI 用）；`token` 命令管理的是**API 调用令牌**（`sk-...`，给客户端调模型用）。

## 配置与多实例

配置存放在 `~/.new-api-cli/`（可用 `NEW_API_CLI_HOME` 改写）：

```
~/.new-api-cli/
├── config.yaml        # 站点连接配置，可分享
└── credentials.json   # 令牌，权限 0600，永不打印
```

一个 profile 对应一个 New API 实例：

```bash
# 新增第二个实例
new-api-cli config init --profile-name prod --base-url https://prod.example.com

# 查看与切换
new-api-cli config list
new-api-cli config use prod
new-api-cli config show          # 令牌已脱敏

# 单次调用临时切换，不改默认
new-api-cli channel list --profile staging

# 改单个字段
new-api-cli config set timeout 120
new-api-cli config set insecure true
```

### 环境变量

| 变量 | 作用 |
| --- | --- |
| `NEW_API_BASE_URL` | 站点地址 |
| `NEW_API_TOKEN` | 系统访问令牌 |
| `NEW_API_PROFILE` | 使用哪个 profile |
| `NEW_API_USER_ID` | 以指定用户身份调用（管理员） |
| `NEW_API_CLI_HOME` | 配置目录 |

优先级：**命令行 flag > 环境变量 > 配置文件**。

## 三层命令调用

### 1. 快捷命令（`+` 前缀）

把高频排查动作收敛成一次调用：

```bash
# 渠道健康概览：禁用的、响应慢的、余额不足的
new-api-cli channel +health
new-api-cli channel +health --slow-ms 3000 --format table
```

### 2. 资源命令

与管理接口一一对应，做了参数校验、风险标注与输出投影：

```bash
new-api-cli channel list --status enabled
new-api-cli token create --name prod --unlimited
new-api-cli log list --since 24h --model gpt-4o
```

### 3. 通用调用

接口还没有对应命令时的兜底，覆盖全部端点：

```bash
new-api-cli api GET /api/channel/
new-api-cli api POST /api/channel/ --data @channel.json
new-api-cli api PUT /api/option/ --data '{"key":"AutoDisable","value":"true"}'
```

路径可省略 `/api` 前缀；`/v1`、`/mj`、`/suno`、`/pg` 开头的路径保持原样（relay 侧接口）。

## 命令总览

| 域 | 说明 | 权限 |
| --- | --- | --- |
| `config` | 站点配置与 profile 管理 | 本地 |
| `auth` | 登录、登出、身份查看 | — |
| `channel` | 上游渠道：增删改查、启停、连通性测试、余额刷新、标签批量操作 | 管理员 |
| `token` | API 调用令牌（`sk-...`）管理 | 用户 |
| `user` | 用户管理：创建、启停、提权、额度 | 管理员 |
| `model` | 模型元数据与可用模型 | 混合 |
| `redemption` | 兑换码批量签发与管理 | 管理员 |
| `log` | 调用日志与用量统计 | 混合 |
| `data` | 额度消耗的按日聚合统计 | 混合 |
| `option` | 站点系统设置 | 超级管理员 |
| `status` | 站点状态、公告、定价、性能指标 | 混合 |
| `api` | 通用 HTTP 调用 | 取决于目标接口 |

运行 `new-api-cli <域> --help` 查看该域的全部子命令。

## 常用操作

### 渠道

```bash
# 列表与搜索
new-api-cli channel list --format table
new-api-cli channel list --status disabled
new-api-cli channel search --model gpt-4o
new-api-cli channel search --keyword openai

# 新建（--key 支持 @file 避免密钥进 shell 历史）
new-api-cli channel create \
  --name openai-main --type 1 \
  --key @openai-key.txt \
  --models gpt-4o,gpt-4o-mini \
  --group default,vip

# 更新（只改传入的字段）
new-api-cli channel update 7 --priority 10
new-api-cli channel update 7 --models gpt-4o,gpt-4o-mini,o3

# 启停（disable 会立即转移流量，需 --yes）
new-api-cli channel enable 7
new-api-cli channel disable 7 --yes

# 连通性测试（会向上游发真实请求，消耗少量额度）
new-api-cli channel test 7
new-api-cli channel test 7 --model gpt-4o

# 余额与模型
new-api-cli channel balance --all
new-api-cli channel models --enabled
new-api-cli channel models --fetch 7      # 向上游拉真实支持的模型

# 按标签批量操作
new-api-cli channel tag set 7 8 9 --tag openai-pool
new-api-cli channel tag disable openai-pool --yes

# 修复渠道-模型关联表（出现「无可用渠道」时）
new-api-cli channel fix --yes
```

### API 令牌

```bash
new-api-cli token list --format table
new-api-cli token search --keyword prod

# 创建
new-api-cli token create --name prod --unlimited
new-api-cli token create --name trial --quota 500000 --expired-at 2026-12-31

# 取明文 key（敏感操作，需确认）
new-api-cli token key 12

# 更新与删除
new-api-cli token update 12 --status 2
new-api-cli token delete 12 --yes
```

### 用户

```bash
new-api-cli user self
new-api-cli user list --format table
new-api-cli user search --keyword alice
new-api-cli user search --role 10

new-api-cli user create --username alice --password 'S3cret!pass'

# 管理动作：enable | disable | promote | demote | delete
new-api-cli user manage 42 disable --yes
new-api-cli user manage 42 promote --yes

new-api-cli user update 42 --quota 5000000 --group vip
```

### 日志与统计

```bash
# 全站日志（管理员）
new-api-cli log list --since 24h
new-api-cli log list --type error --since 1h
new-api-cli log list --username alice --model gpt-4o --since 7d
new-api-cli log list --request-id abc123

# 自己的日志（普通用户）
new-api-cli log self --since 7d

# 用量统计
new-api-cli log stat --since 24h
new-api-cli log self-stat --since 24h

# 按日聚合
new-api-cli data list --since 30d --format table
new-api-cli data users --since 30d
new-api-cli data self
new-api-cli data flow --self
```

时间参数支持相对（`24h`、`7d`）与绝对（`2026-01-31`、`2026-01-31 10:00:00`、Unix 秒）两种写法。

### 兑换码

```bash
# 批量签发 —— 返回的码明文只此一次可见
new-api-cli redemption create --name 双十一 --quota 500000 --count 10

new-api-cli redemption list --format table
new-api-cli redemption update 7 --status 2
new-api-cli redemption prune --yes     # 清理已失效
```

### 系统设置

```bash
new-api-cli option list --prefix Quota
new-api-cli option get QuotaPerUnit
new-api-cli option get ModelRatio --raw           # 只输出裸值

# 写入（高危，影响线上行为）
new-api-cli option set DisplayInCurrencyEnabled true --yes
new-api-cli option set ModelRatio --value-file @ratio.json --yes
```

> 服务端会过滤掉以 `Token` / `Secret` / `Key` / `api_key` 结尾的敏感项，`list` 看不到它们的值，但仍可用 `set` 写入。

### 站点状态

```bash
new-api-cli status                    # 公开接口，无需登录
new-api-cli status --base-url https://api.example.com   # 排查连通性
new-api-cli status pricing
new-api-cli status perf
new-api-cli status instances
```

## 输出格式

```bash
--format json      # 完整 JSON 信封（默认，Agent 友好）
--format table     # 对齐表格，给人看
--format pretty    # 缩进键值树，适合看单条记录
--format ndjson    # 每行一条记录，适合管道流式处理
--format csv       # 逗号分隔值，适合导入表格软件
```

### JSON 输出契约

这是 Agent 依赖的稳定接口，改动即为破坏性变更。

**成功** → **stdout**，退出码 0：

```json
{
  "ok": true,
  "data": [ { "id": 1, "name": "openai-main" } ],
  "meta": { "count": 1, "total": 42, "page": 1, "page_size": 20 }
}
```

**失败** → **stderr**，退出码非 0：

```json
{
  "ok": false,
  "error": {
    "type": "auth",
    "subtype": "forbidden",
    "message": "权限不足",
    "hint": "管理接口需要管理员（role>=10）",
    "params": ["--profile"]
  }
}
```

`stdout` 只承载结果，进度与警告一律走 `stderr` —— 因此 `new-api-cli ... | jq` 永远拿到干净的 JSON。

### 退出码

| 码 | 含义 |
| --- | --- |
| 0 | 成功 |
| 1 | 通用 / 内部错误 |
| 2 | 用法错误（参数写错） |
| 3 | 未配置或配置损坏 |
| 4 | 未登录或令牌失效 |
| 5 | 已登录但权限不足 |
| 6 | 本地参数校验失败 |
| 7 | 服务端返回错误 |
| 8 | 网络不可达 |
| 9 | 资源不存在 |

## 进阶用法

### jq 过滤

```bash
# 提取字段
new-api-cli channel list --jq '.[].name'

# 组合投影
new-api-cli channel list --jq '.[] | {id, name, status}'

# 条件筛选
new-api-cli channel list --all --jq '[.[] | select(.status != 1) | .name]'

# 计数
new-api-cli channel list --all --jq 'length'
```

### 自动翻页

```bash
new-api-cli channel list --all                  # 取回全部
new-api-cli log list --all --limit 500          # 最多 500 条
new-api-cli api GET /api/channel/ --page-all    # 通用调用也支持
```

服务端单页上限 100 条，`--page-size` 超过会被截断。

### 预演请求

```bash
# 只打印将要发出的请求，不做任何改动
new-api-cli channel delete 7 --yes --dry-run
```

### 破坏性操作的确认门禁

标注 `high-risk-write` 的命令在交互终端会询问确认；在非交互环境（Agent、CI、管道）必须显式传 `--yes`，否则以退出码 6 拒绝执行 —— **确认发生在发出请求之前**。

```bash
new-api-cli channel delete 7          # 交互确认
new-api-cli channel delete 7 --yes    # 非交互放行
```

### 以其他用户身份调用

```bash
new-api-cli --as-user 42 token list    # 管理员专用
```

### 私有部署与自签名证书

```bash
new-api-cli --insecure channel list
new-api-cli config set insecure true   # 持久化到 profile
```

### 调试

```bash
new-api-cli channel list --verbose     # 请求详情写到 stderr
```

### Shell 补全

```bash
# bash
new-api-cli completion bash > /etc/bash_completion.d/new-api-cli

# zsh
new-api-cli completion zsh > "${fpath[1]}/_new-api-cli"

# fish
new-api-cli completion fish > ~/.config/fish/completions/new-api-cli.fish

# powershell
new-api-cli completion powershell | Out-String | Invoke-Expression
```

## 脚本示例

### 巡检不健康的渠道

```bash
#!/bin/bash
set -euo pipefail

# --format ndjson 让 --jq 输出裸值而非信封，适合脚本取值
bad=$(new-api-cli channel +health --format ndjson --jq '.disabled | length')

if [ "$bad" -gt 0 ]; then
  echo "有 $bad 个渠道未启用："
  new-api-cli channel +health --format ndjson --jq -r '.disabled[] | "  #\(.id) \(.name) — \(.reason)"'
  exit 1
fi
```

### 错误日志告警

```bash
#!/bin/bash
errors=$(new-api-cli log list --type error --since 1h --all --format ndjson --jq 'length')
if [ "$errors" -gt 10 ]; then
  echo "警告：最近 1 小时有 $errors 条错误日志"
  exit 1
fi
```

### CI 里轮换渠道密钥

```yaml
- name: 轮换上游密钥
  env:
    NEW_API_BASE_URL: ${{ secrets.NEW_API_URL }}
    NEW_API_TOKEN: ${{ secrets.NEW_API_TOKEN }}
  run: |
    printf '%s' "${{ secrets.OPENAI_KEY }}" > key.txt
    new-api-cli channel update 7 --key @key.txt --yes
    rm -f key.txt
```

### 导出月度用量

```bash
new-api-cli data users --since 30d --format csv > usage-$(date +%Y%m).csv
```

## 安全提示

- **优先用系统访问令牌**，别在命令行写密码 —— `-p` 会进入 shell 历史与进程列表
- **`--key` / `--value-file` 支持 `@file` 与 `-`**，用它们传密钥，避免密钥出现在命令行
- `channel key`、`token key` 会把凭证**明文打印到 stdout**，注意共享终端与日志留存
- `auth token` 生成新令牌会**立即作废旧令牌**，包括其他机器上正在用的那一份
- `credentials.json` 为 0600；`config show` / `auth list` 中令牌一律脱敏
- 服务端返回的字段在渲染前会被清理掉 ANSI 转义与控制字符，避免终端注入

## 开发

```bash
go build ./...        # 构建
go vet ./...          # 静态检查
go test ./...         # 全部测试
go test ./cmd/ -v     # 端到端测试（带模拟服务端）
```

### 项目结构

```
.
├── main.go                 # 入口
├── cmd/                    # 命令树，一个子目录一个域
│   ├── root.go             # 全局 flag、分组、错误分发
│   └── e2e_test.go         # 端到端测试（模拟 New API 服务端）
├── internal/
│   ├── client/             # HTTP 客户端：信封解析、错误翻译、翻页
│   ├── config/             # 配置与凭据存储
│   ├── output/             # 渲染：JSON/table/csv/ndjson/pretty、jq、净化
│   ├── cmdutil/            # Factory、风险等级、参数解析、列表脚手架
│   └── build/              # 编译期版本信息
└── errs/                   # 类型化错误与退出码契约
```

### 注入版本信息

```bash
go build -ldflags "\
  -X github.com/QuantumNous/new-api-cli/internal/build.Version=v1.0.0 \
  -X github.com/QuantumNous/new-api-cli/internal/build.Commit=$(git rev-parse --short HEAD) \
  -X github.com/QuantumNous/new-api-cli/internal/build.Date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  -o new-api-cli .
```

### 两个必须知道的服务端行为

写新命令时容易踩的两个坑，客户端层已统一处理：

1. **业务失败也返回 HTTP 200**，靠 body 里的 `success:false` 区分 —— 只看状态码会把失败当成功。
2. **多数更新接口是整体替换语义** —— 只提交改动字段会清空其余字段。因此更新命令先读取当前对象，把 flag 合并上去后再整体提交，并且**绝不把掩码后的 key 写回**。
