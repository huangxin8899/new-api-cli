// new-api-cli — New API 命令行工具（Go 实现）。
//
// 让人类与 AI Agent 都能在终端里管理 New API 网关：渠道、令牌、用户、
// 日志、兑换码、模型与系统设置。
package main

import (
	"os"

	"github.com/huangxin8899/new-api-cli/cmd"
)

func main() {
	os.Exit(cmd.Execute())
}
