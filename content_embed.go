package main

import (
	"embed"
	"fmt"
	"io/fs"
	"os"

	"github.com/huangxin8899/new-api-cli/cmd"
)

// embeddedSkillsFS 把 Agent 可读的 skill 文档打进二进制，让它与 CLI 版本
// 同步发布：每个 skill 的 SKILL.md 与 references/。这是白名单 —— 新增内容
// 类型必须显式加进 embed 列表才会被打包。
// embed 指令必须写在根包里，因为 go:embed 无法向上跨出所在目录。
//
//go:embed skills/*/SKILL.md skills/*/references
var embeddedSkillsFS embed.FS

// init 把嵌入内容接进 CLI。它只在 `go build .` 时参与编译，不进入单文件
// 预览构建（`go build ./main.go`），后者保持自包含、不携带文档。
// 装配失败只在 stderr 警告而不 panic —— 文档缺失不该让整个 CLI 不可用。
func init() {
	sub, err := fs.Sub(embeddedSkillsFS, "skills")
	if err != nil {
		fmt.Fprintln(os.Stderr, "警告：skill 内容装配失败，skills 命令不可用:", err)
		return
	}
	cmd.SetEmbeddedSkillContent(sub)
}
