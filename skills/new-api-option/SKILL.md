---
name: new-api-option
version: 1.0.0
description: "New API 站点系统设置读写：列出与按前缀过滤设置项、读单项、写入设置（模型倍率 ModelRatio、额度换算 QuotaPerUnit、自动禁用开关、登录方式等）。当用户说「改模型倍率」「关掉注册」「调整额度单位」「站点设置在哪」时使用。需要超级管理员。"
metadata:
  requires:
    bins: ["new-api-cli"]
  cliHelp: "new-api-cli option --help"
---

# option — 站点系统设置

**开始前先读 [`../new-api-shared/SKILL.md`](../new-api-shared/SKILL.md)（认证、JSON 契约、确认门禁）。**

读写站点系统设置。**全部子命令要求超级管理员（role=100）。** 别名：`options`、`setting`。

## 为什么要格外小心

设置项直接改变线上行为，而且**没有版本历史，改错了只能靠你记下的原值回滚**：

- 改 `ModelRatio` → 立刻改变计费
- 改 `PasswordLoginEnabled` 之类的登录开关 → 可能把自己锁在外面
- 改 `QuotaPerUnit` → 全站额度显示与换算全变

`option set` 是 **high-risk-write**，非交互环境必须 `--yes`。

**写入前一定先记录原值：**

```bash
new-api-cli option get ModelRatio --raw > ModelRatio.bak.json
```

## 命令

| 命令 | 风险 | 说明 |
|---|---|---|
| `list` / `ls` | read | 列出全部设置项 |
| `get <key>` | read | 读单项 |
| `set <key> [value]` | **high-risk-write** | 写入 |

## 读取

```bash
new-api-cli option list
new-api-cli option list --prefix Quota --format table    # 前缀过滤，不区分大小写
new-api-cli option get QuotaPerUnit
new-api-cli option get QuotaPerUnit --raw                # 只输出裸值，适合 $(...) 取用
new-api-cli option get ModelRatio --raw > ratio.json
```

`--raw` 直接把值写到 stdout（末尾带换行），不带信封 —— 用来存盘或做 shell 变量。

`get` 找不到 key 时返回退出码 9（`not_found`）。可能是拼错了，也可能是被服务端过滤了（见下）。

## 敏感项被过滤

服务端返回时会**过滤掉以 `Token` / `Secret` / `Key` / `api_key` 结尾的设置项**，所以：

- `list` 和 `get` 看不到它们的值
- 但**仍然可以用 `set` 写入**

也就是说这类项是只写不可读的。写入后无法通过 CLI 验证是否生效，只能通过实际功能确认。

## 写入

```bash
# 布尔
new-api-cli option set DisplayInCurrencyEnabled true --yes

# 数字
new-api-cli option set QuotaPerUnit 500000 --yes

# 长 JSON 用 --value-file
new-api-cli option set ModelRatio --value-file @ratio.json --yes

# 从标准输入
cat ratio.json | new-api-cli option set ModelRatio --value-file - --yes
```

值可以作位置参数给出，或用 `--value-file`（`@文件` / `-`）。两者**不能同时给**。

### 值的类型处理

CLI 会把命令行上的字符串还原成合适的 JSON 类型：

| 输入 | 提交为 | 原因 |
|---|---|---|
| `true` / `false`（不分大小写） | 布尔 | 服务端部分校验分支对字符串 `"true"` 与布尔 `true` 表现不同 |
| `500000`、`1.5` | 数字 | 往返一致时才转，`1.0`、`007` 保持字符串以免被悄悄改写 |
| JSON 对象/数组 | **字符串** | 服务端就是按字符串存 `ModelRatio` 的 |
| 其他 | 字符串 | — |

所以 `ModelRatio` 这类大 JSON 直接用 `--value-file @file.json` 就对了，不用自己转义。

## 改倍率的安全流程

```bash
# 1. 备份原值
new-api-cli option get ModelRatio --raw > ModelRatio.$(date +%s).bak.json

# 2. 基于备份编辑（用 jq 精确改一个模型，别手写整份）
jq '.["gpt-4o"] = 2.5' ModelRatio.*.bak.json > ratio.new.json

# 3. 预演，确认请求体
new-api-cli option set ModelRatio --value-file @ratio.new.json --yes --dry-run

# 4. 写入
new-api-cli option set ModelRatio --value-file @ratio.new.json --yes

# 5. 验证
new-api-cli option get ModelRatio --raw | jq '.["gpt-4o"]'
```

**不要手写整份 `ModelRatio`** —— 漏掉的模型会失去倍率配置。始终基于读回来的原值改。

## 常见设置项

不同 New API 版本的设置项集合不同，**先用 `list --prefix` 确认存在再写**：

| 前缀 / key | 关系到 |
|---|---|
| `QuotaPerUnit` | 额度与货币的换算（默认 500000 = $1） |
| `QuotaForNewUser` | 新用户初始额度 |
| `ModelRatio` | 各模型的计费倍率（JSON） |
| `CompletionRatio` | 输出 token 的额外倍率（JSON） |
| `GroupRatio` | 各分组的倍率（JSON） |
| `DisplayInCurrencyEnabled` | 前端按货币还是 quota 展示 |
| `AutomaticDisableChannelEnabled` | 上游报错时自动禁用渠道 |
| `AutomaticEnableChannelEnabled` | 恢复后自动启用渠道 |
| `PasswordLoginEnabled` / `PasswordRegisterEnabled` | 密码登录 / 注册开关 |
| `RetryTimes` | 失败重试次数 |
| `ChannelDisableThreshold` | 触发自动禁用的阈值 |

查某类设置：

```bash
new-api-cli option list --prefix Quota --format table
new-api-cli option list --prefix Automatic --format table
new-api-cli option list --prefix Ratio --format table
```

## 别把自己锁在外面

以下改动可能导致你无法继续操作站点：

- 关闭 `PasswordLoginEnabled` 而你只有密码登录 → 先确保有系统访问令牌可用（`auth status` 显示 `kind: pat`）
- 改动权限或认证相关开关 → 先在测试 profile 上验证

改这类设置前告知用户风险，并确认他们有备用登录方式。

## 常见任务

| 用户诉求 | 命令 |
|---|---|
| 额度单位是多少 | `option get QuotaPerUnit --raw` |
| 调某模型倍率 | 见上面「改倍率的安全流程」 |
| 关掉自动禁用渠道 | `option set AutomaticDisableChannelEnabled false --yes` |
| 关掉注册 | `option set PasswordRegisterEnabled false --yes` |
| 改新用户初始额度 | `option set QuotaForNewUser 500000 --yes` |
| 有哪些设置项 | `option list --format table`（很长，配 `--prefix` 收窄） |

## 不在本 skill 范围

- 站点状态、版本、公告（不需要超管） → [`../new-api-status/SKILL.md`](../new-api-status/SKILL.md)
- CLI 自己的配置（base_url、profile、超时） → [`../new-api-shared/SKILL.md`](../new-api-shared/SKILL.md) 的 `config` 一节；注意 `config set` 与 `option set` 是**完全不同**的两件事：前者改本机 CLI 配置，后者改服务端站点设置
- 渠道级的参数覆盖 → [`../new-api-channel/SKILL.md`](../new-api-channel/SKILL.md)（`--setting`、`--param-override`）
