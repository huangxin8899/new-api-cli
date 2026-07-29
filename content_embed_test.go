package main

import (
	"io/fs"
	"os"
	"path"
	"regexp"
	"strings"
	"testing"

	"github.com/huangxin8899/new-api-cli/internal/skillcontent"
	"gopkg.in/yaml.v3"
)

// skillsFS 返回与运行时一致的视图：以 skill 列表为根。
func skillsFS(t *testing.T) fs.FS {
	t.Helper()
	sub, err := fs.Sub(embeddedSkillsFS, "skills")
	if err != nil {
		t.Fatalf("装配嵌入内容失败: %v", err)
	}
	return sub
}

// 嵌入是白名单，很容易在新增 skill 后漏掉 —— 这里守住"每个 skills/ 子目录
// 都被打进二进制"。
func TestEverySkillDirIsEmbedded(t *testing.T) {
	onDisk, err := os.ReadDir("skills")
	if err != nil {
		t.Fatalf("读 skills 目录失败: %v", err)
	}
	embedded := map[string]bool{}
	for _, s := range mustList(t) {
		embedded[s.Name] = true
	}
	for _, e := range onDisk {
		if !e.IsDir() {
			continue
		}
		if !embedded[e.Name()] {
			t.Errorf("skills/%s 未被嵌入或缺少 SKILL.md", e.Name())
		}
	}
	if len(embedded) == 0 {
		t.Fatal("没有嵌入任何 skill")
	}
}

// frontmatter 是 Agent 选 skill 的唯一依据，缺字段会让它无从判断相关性。
func TestFrontmatterIsComplete(t *testing.T) {
	fsys := skillsFS(t)
	for _, s := range mustList(t) {
		t.Run(s.Name, func(t *testing.T) {
			data, err := fs.ReadFile(fsys, s.Name+"/SKILL.md")
			if err != nil {
				t.Fatalf("读 SKILL.md 失败: %v", err)
			}
			var fm struct {
				Name        string `yaml:"name"`
				Version     string `yaml:"version"`
				Description string `yaml:"description"`
			}
			block, ok := frontmatterBlock(data)
			if !ok {
				t.Fatal("缺少 frontmatter")
			}
			if err := yaml.Unmarshal([]byte(block), &fm); err != nil {
				t.Fatalf("frontmatter 不是合法 YAML: %v", err)
			}
			// name 必须与目录名一致，否则 skills read <name> 找不到。
			if fm.Name != s.Name {
				t.Errorf("frontmatter name %q 与目录名 %q 不一致", fm.Name, s.Name)
			}
			if fm.Version == "" {
				t.Error("缺少 version")
			}
			if len(fm.Description) < 20 {
				t.Errorf("description 太短，Agent 无法判断相关性: %q", fm.Description)
			}
		})
	}
}

var mdLink = regexp.MustCompile(`\]\(([^)#]+\.md)(#[^)]*)?\)`)

// skill 文档之间大量交叉引用，写错的链接会让 Agent 读到 not_found 后放弃。
// 这里把每个相对链接都解析成嵌入 FS 里的实际路径来验证。
func TestAllRelativeLinksResolve(t *testing.T) {
	fsys := skillsFS(t)
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".md") {
			return err
		}
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		for _, m := range mdLink.FindAllStringSubmatch(string(data), -1) {
			target := m[1]
			// 只校验相对链接；http(s) 链接不在此列。
			if strings.Contains(target, "://") {
				continue
			}
			resolved := path.Join(path.Dir(p), target)
			if _, statErr := fs.Stat(fsys, resolved); statErr != nil {
				t.Errorf("%s 中的链接 %q 解析到 %q，但该文件不存在", p, target, resolved)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("遍历失败: %v", err)
	}
}

// 每个 skill 都以 new-api-shared 为前提，正文里必须给出指向它的入口 ——
// 否则 Agent 会漏掉认证、退出码与确认门禁这些通用契约。
func TestEverySkillPointsAtShared(t *testing.T) {
	fsys := skillsFS(t)
	for _, s := range mustList(t) {
		if s.Name == "new-api-shared" {
			continue
		}
		data, err := fs.ReadFile(fsys, s.Name+"/SKILL.md")
		if err != nil {
			t.Fatalf("读 %s 失败: %v", s.Name, err)
		}
		if !strings.Contains(string(data), "new-api-shared/SKILL.md") {
			t.Errorf("%s 没有引用 new-api-shared", s.Name)
		}
	}
}

// 命令示例里的二进制名必须是 new-api-cli —— 这些 skill 是从飞书 cli 的
// 结构改写来的，容易残留 lark-cli。
func TestNoForeignBinaryNames(t *testing.T) {
	fsys := skillsFS(t)
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".md") {
			return err
		}
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		for _, bad := range []string{"lark-cli", "lark_cli"} {
			if strings.Contains(string(data), bad) {
				t.Errorf("%s 残留了 %q", p, bad)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("遍历失败: %v", err)
	}
}

func mustList(t *testing.T) []skillcontent.SkillInfo {
	t.Helper()
	skills, err := skillcontent.New(skillsFS(t)).List()
	if err != nil {
		t.Fatalf("List 失败: %v", err)
	}
	return skills
}

// frontmatterBlock 取出 --- 之间的 YAML 块。
func frontmatterBlock(data []byte) (string, bool) {
	lines := strings.Split(string(data), "\n")
	if strings.TrimRight(lines[0], "\r") != "---" {
		return "", false
	}
	var block []string
	for _, ln := range lines[1:] {
		if strings.TrimRight(ln, "\r") == "---" {
			return strings.Join(block, "\n"), true
		}
		block = append(block, ln)
	}
	return "", false
}
