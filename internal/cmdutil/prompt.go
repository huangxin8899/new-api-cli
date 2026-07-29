package cmdutil

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/huangxin8899/new-api-cli/errs"
	"golang.org/x/term"
)

// Prompter 负责交互式输入。所有提示写 stderr，保证 stdout 只有结果。
type Prompter struct {
	In  io.Reader
	Err io.Writer
}

// NewPrompter 基于 Factory 的流构造 Prompter。
func (f *Factory) NewPrompter() *Prompter {
	return &Prompter{In: f.IOStreams.In, Err: f.IOStreams.Err}
}

// Interactive 报告当前是否有真实终端可供提问。
// 管道、CI、Agent 调用下为 false —— 此时缺参数应报错而不是挂起等输入。
func (p *Prompter) Interactive() bool {
	f, ok := p.In.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

// Line 读取一行输入，回车留空时取 defaultValue。
func (p *Prompter) Line(label, defaultValue string) (string, error) {
	if defaultValue != "" {
		fmt.Fprintf(p.Err, "%s [%s]: ", label, defaultValue)
	} else {
		fmt.Fprintf(p.Err, "%s: ", label)
	}
	reader := bufio.NewReader(p.In)
	text, err := reader.ReadString('\n')
	if err != nil && text == "" {
		return "", errs.NewValidationError(errs.SubtypeMissingArgument,
			"读取输入失败: %v", err)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return defaultValue, nil
	}
	return text, nil
}

// Secret 读取一行不回显的输入（密码、令牌）。
func (p *Prompter) Secret(label string) (string, error) {
	f, ok := p.In.(*os.File)
	if !ok || !term.IsTerminal(int(f.Fd())) {
		// 非终端（管道输入）时退化成普通读取，仍然可用。
		return p.Line(label, "")
	}
	fmt.Fprintf(p.Err, "%s: ", label)
	raw, err := term.ReadPassword(int(f.Fd()))
	fmt.Fprintln(p.Err)
	if err != nil {
		return "", errs.NewValidationError(errs.SubtypeMissingArgument,
			"读取输入失败: %v", err)
	}
	return strings.TrimSpace(string(raw)), nil
}

// YesNo 询问一个是非题，默认否。
func (p *Prompter) YesNo(label string) bool {
	fmt.Fprintf(p.Err, "%s [y/N]: ", label)
	reader := bufio.NewReader(p.In)
	text, _ := reader.ReadString('\n')
	text = strings.ToLower(strings.TrimSpace(text))
	return text == "y" || text == "yes"
}
