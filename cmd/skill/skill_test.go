package skill

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/huangxin8899/new-api-cli/errs"
	"github.com/huangxin8899/new-api-cli/internal/cmdutil"
)

func testContentFS() fstest.MapFS {
	return fstest.MapFS{
		"new-api-shared/SKILL.md": &fstest.MapFile{Data: []byte(
			"---\nname: new-api-shared\nversion: 1.0.0\ndescription: \"共享规则\"\n---\n\n# 共享\n")},
		"new-api-channel/SKILL.md":             &fstest.MapFile{Data: []byte("---\nname: new-api-channel\ndescription: \"渠道\"\n---\n\n# channel\n")},
		"new-api-channel/references/health.md": &fstest.MapFile{Data: []byte("# +health\n")},
	}
}

// run 在进程内驱动 skills 命令树，返回 stdout、stderr 与退出码。
func run(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	streams := cmdutil.IOStreams{In: strings.NewReader(""), Out: &stdout, Err: &stderr}
	f := cmdutil.NewFactory(streams, &cmdutil.GlobalFlags{Format: "json"})
	f.SkillContent = testContentFS()

	cmd := NewCmd(f)
	cmd.SetArgs(args)
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	code := 0
	if err := cmd.Execute(); err != nil {
		code = errs.ExitCodeOf(err)
	}
	return stdout.String(), stderr.String(), code
}

func TestListEmitsEnvelope(t *testing.T) {
	stdout, _, code := run(t, "list")
	if code != 0 {
		t.Fatalf("退出码 %d，stdout=%s", code, stdout)
	}
	var env struct {
		OK   bool `json:"ok"`
		Data []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"data"`
		Meta struct{ Count int } `json:"meta"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("stdout 不是合法 JSON: %v\n%s", err, stdout)
	}
	if !env.OK || env.Meta.Count != 2 || len(env.Data) != 2 {
		t.Fatalf("信封不对: %s", stdout)
	}
	if env.Data[0].Name != "new-api-channel" || env.Data[0].Description != "渠道" {
		t.Errorf("首条不对: %+v", env.Data[0])
	}
}

// read 默认输出必须与文件逐字节一致 —— Agent 直接消费 stdout，
// 加信封或加提示会破坏它。
func TestReadWritesRawMarkdownToStdout(t *testing.T) {
	stdout, stderr, code := run(t, "read", "new-api-shared")
	if code != 0 {
		t.Fatalf("退出码 %d", code)
	}
	want := "---\nname: new-api-shared\nversion: 1.0.0\ndescription: \"共享规则\"\n---\n\n# 共享\n"
	if stdout != want {
		t.Errorf("stdout 不是原始 markdown:\n%q", stdout)
	}
	// 指引走 stderr，不污染管道。
	if !strings.Contains(stderr, "skills read new-api-shared") {
		t.Errorf("stderr 缺少读取指引: %q", stderr)
	}
}

// 读引用文件时不应附加指引 —— 只有 SKILL.md 需要那段导航说明。
func TestReadReferenceHasNoGuidance(t *testing.T) {
	stdout, stderr, code := run(t, "read", "new-api-channel/references/health.md")
	if code != 0 {
		t.Fatalf("退出码 %d", code)
	}
	if stdout != "# +health\n" {
		t.Errorf("stdout 不对: %q", stdout)
	}
	if strings.TrimSpace(stderr) != "" {
		t.Errorf("引用文件不该有 stderr 输出: %q", stderr)
	}
}

// 两参数形式与斜杠形式必须等价。
func TestReadTwoArgFormEquivalent(t *testing.T) {
	slash, _, _ := run(t, "read", "new-api-channel/references/health.md")
	twoArg, _, _ := run(t, "read", "new-api-channel", "references/health.md")
	if slash != twoArg {
		t.Errorf("两种写法结果不同:\n%q\n%q", slash, twoArg)
	}
}

func TestReadJSONWrapsContent(t *testing.T) {
	stdout, _, code := run(t, "read", "new-api-shared", "--json")
	if code != 0 {
		t.Fatalf("退出码 %d", code)
	}
	var env struct {
		OK   bool `json:"ok"`
		Data struct {
			Skill    string `json:"skill"`
			Path     string `json:"path"`
			Content  string `json:"content"`
			Guidance string `json:"guidance"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("stdout 不是合法 JSON: %v\n%s", err, stdout)
	}
	if !env.OK || env.Data.Skill != "new-api-shared" || env.Data.Path != "SKILL.md" {
		t.Fatalf("信封不对: %s", stdout)
	}
	if !strings.Contains(env.Data.Content, "# 共享") {
		t.Errorf("content 不含正文: %q", env.Data.Content)
	}
	if env.Data.Guidance == "" {
		t.Error("SKILL.md 的 JSON 输出应带 guidance")
	}
}

// 未知 skill 是用法问题，必须是退出码 6 而不是 1。
func TestUnknownSkillExitsValidation(t *testing.T) {
	_, _, code := run(t, "read", "nope")
	if code != errs.ExitValidation {
		t.Errorf("想要退出码 %d，得到 %d", errs.ExitValidation, code)
	}
}

// 未嵌入内容时要明确报错，而不是静默返回空列表。
func TestMissingEmbedIsReported(t *testing.T) {
	var stdout, stderr bytes.Buffer
	streams := cmdutil.IOStreams{In: strings.NewReader(""), Out: &stdout, Err: &stderr}
	f := cmdutil.NewFactory(streams, &cmdutil.GlobalFlags{Format: "json"})
	// 刻意不设 SkillContent。
	cmd := NewCmd(f)
	cmd.SetArgs([]string{"list"})
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("未嵌入内容时 list 应该报错")
	}
	if !strings.Contains(err.Error(), "未嵌入") {
		t.Errorf("错误信息应说明未嵌入: %v", err)
	}
}

func TestAllLeavesAreReadRisk(t *testing.T) {
	f := cmdutil.NewFactory(cmdutil.IOStreams{}, &cmdutil.GlobalFlags{})
	for _, sub := range NewCmd(f).Commands() {
		if got := cmdutil.RiskOf(sub); got != cmdutil.RiskRead {
			t.Errorf("%s 的风险等级想要 %s，得到 %s", sub.Name(), cmdutil.RiskRead, got)
		}
	}
}
