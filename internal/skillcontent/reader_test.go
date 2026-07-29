package skillcontent

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/huangxin8899/new-api-cli/errs"
)

// testFS 复刻真实的嵌入布局：以 skill 列表为根。
func testFS() fstest.MapFS {
	return fstest.MapFS{
		"new-api-shared/SKILL.md": &fstest.MapFile{Data: []byte(
			"---\nname: new-api-shared\nversion: 1.0.0\ndescription: \"共享规则\"\nmetadata:\n  cliHelp: \"new-api-cli --help\"\n---\n\n# 共享\n")},
		"new-api-channel/SKILL.md": &fstest.MapFile{Data: []byte(
			"---\nname: new-api-channel\nversion: 2.1.0\ndescription: \"渠道\"\n---\n\n# channel\n")},
		"new-api-channel/references/health.md": &fstest.MapFile{Data: []byte("# +health\n")},
		// 没有 SKILL.md 的目录不是 skill。
		"notaskill/readme.md": &fstest.MapFile{Data: []byte("x")},
	}
}

func TestListSkipsDirsWithoutSkillMD(t *testing.T) {
	skills, err := New(testFS()).List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(skills) != 2 {
		t.Fatalf("想要 2 个 skill，得到 %d: %+v", len(skills), skills)
	}
	// 按名称升序。
	if skills[0].Name != "new-api-channel" || skills[1].Name != "new-api-shared" {
		t.Errorf("排序不对: %s, %s", skills[0].Name, skills[1].Name)
	}
	if skills[0].Version != "2.1.0" || skills[0].Description != "渠道" {
		t.Errorf("frontmatter 解析不对: %+v", skills[0])
	}
	if got := skills[1].Metadata["cliHelp"]; got != "new-api-cli --help" {
		t.Errorf("metadata 想要 new-api-cli --help，得到 %v", got)
	}
}

func TestParseFrontmatterTolerance(t *testing.T) {
	cases := map[string]string{
		"没有 frontmatter": "# 标题\n",
		"未闭合":            "---\nname: x\n# 标题\n",
		"frontmatter 非法": "---\n: : :\n---\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			desc, version, meta := parseFrontmatter([]byte(content))
			if desc != "" || version != "" || meta != nil {
				t.Errorf("想要全零值，得到 %q %q %v", desc, version, meta)
			}
		})
	}
}

func TestReadSkillAndReference(t *testing.T) {
	r := New(testFS())

	data, err := r.ReadSkill("new-api-channel")
	if err != nil {
		t.Fatalf("ReadSkill: %v", err)
	}
	if want := "# channel\n"; !strings.Contains(string(data), want) {
		t.Errorf("SKILL.md 内容不含 %q", want)
	}

	data, path, err := r.ReadReference("new-api-channel", "references/health.md")
	if err != nil {
		t.Fatalf("ReadReference: %v", err)
	}
	if path != "references/health.md" {
		t.Errorf("path 想要 references/health.md，得到 %q", path)
	}
	if string(data) != "# +health\n" {
		t.Errorf("引用文件内容不对: %q", data)
	}
}

func TestPathGuardRejectsEscapes(t *testing.T) {
	r := New(testFS())
	// 跨 skill 的相对引用必须被拒绝 —— guidance 会教 Agent 改写成 skills read <other>/...。
	for _, bad := range []string{"../new-api-shared/SKILL.md", `..\new-api-shared\SKILL.md`, "/etc/passwd", ".", ".."} {
		if _, _, err := r.ReadReference("new-api-channel", bad); err == nil {
			t.Errorf("路径 %q 应该被拒绝", bad)
		}
	}
}

func TestUnknownSkillIsValidationError(t *testing.T) {
	r := New(testFS())
	for _, name := range []string{"nope", "notaskill", "", "..", "a/b"} {
		_, err := r.ReadSkill(name)
		if err == nil {
			t.Fatalf("skill %q 应该报错", name)
		}
		// 未知 skill 是用法问题，必须映射到退出码 6 而不是 1。
		if code := errs.ExitCodeOf(err); code != errs.ExitValidation {
			t.Errorf("skill %q 想要退出码 %d，得到 %d", name, errs.ExitValidation, code)
		}
	}
}

func TestListPath(t *testing.T) {
	r := New(testFS())

	entries, listed, err := r.ListPath("new-api-channel")
	if err != nil {
		t.Fatalf("ListPath: %v", err)
	}
	if listed != "new-api-channel" {
		t.Errorf("listed 想要 new-api-channel，得到 %q", listed)
	}
	// Path 带 skill 前缀，可以直接喂回 read。
	want := map[string]bool{"new-api-channel/SKILL.md": false, "new-api-channel/references": true}
	if len(entries) != len(want) {
		t.Fatalf("想要 %d 条，得到 %d: %+v", len(want), len(entries), entries)
	}
	for _, e := range entries {
		isDir, ok := want[e.Path]
		if !ok {
			t.Errorf("意外条目 %q", e.Path)
			continue
		}
		if e.IsDir != isDir {
			t.Errorf("%q 的 is_dir 想要 %v，得到 %v", e.Path, isDir, e.IsDir)
		}
	}

	if _, _, err := r.ListPath("new-api-channel/references"); err != nil {
		t.Errorf("列出子目录失败: %v", err)
	}
	// 对文件调用 list 要给出改用 read 的提示。
	if _, _, err := r.ListPath("new-api-channel/SKILL.md"); err == nil {
		t.Error("对文件调用 ListPath 应该报错")
	}
}

func TestSplitArg(t *testing.T) {
	cases := []struct{ arg, name, rest string }{
		{"new-api-channel", "new-api-channel", ""},
		{"new-api-channel/references/health.md", "new-api-channel", "references/health.md"},
		{"a/b/c", "a", "b/c"},
	}
	for _, c := range cases {
		name, rest := SplitArg(c.arg)
		if name != c.name || rest != c.rest {
			t.Errorf("SplitArg(%q) = (%q, %q)，想要 (%q, %q)", c.arg, name, rest, c.name, c.rest)
		}
	}
}
