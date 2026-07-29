// Package skillcontent 读取编译期嵌入的 skill 内容。
//
// 注入的 fs.FS 以 skill 列表为根，条目形如 "new-api-channel/SKILL.md"。
// 内容随二进制一起发布，因此 Agent 读到的说明与 CLI 版本天然同步。
package skillcontent

import (
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/huangxin8899/new-api-cli/errs"
	"gopkg.in/yaml.v3"
)

// Reader 从注入的文件系统读取 skill 内容。
type Reader struct {
	fsys fs.FS
}

// New 基于给定文件系统构造 Reader。
func New(fsys fs.FS) *Reader { return &Reader{fsys: fsys} }

// SkillInfo 是 list 输出的单条 skill 摘要。
type SkillInfo struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Version     string         `json:"version,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// DirEntry 的 Path 带 skill 前缀（如 "new-api-channel/references/x.md"），
// 可以直接喂回 read。
type DirEntry struct {
	Path  string `json:"path"`
	IsDir bool   `json:"is_dir"`
}

// List 列出全部 skill。
func (r *Reader) List() ([]SkillInfo, error) {
	entries, err := fs.ReadDir(r.fsys, ".")
	if err != nil {
		return nil, errs.NewInternalError("读取嵌入的 skill 列表失败: %v", err)
	}
	out := make([]SkillInfo, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		// 没有 SKILL.md 的目录不是 skill，跳过。
		if info, ok := r.skillInfo(e.Name()); ok {
			out = append(out, info)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (r *Reader) skillInfo(name string) (SkillInfo, bool) {
	data, err := fs.ReadFile(r.fsys, name+"/SKILL.md")
	if err != nil {
		return SkillInfo{}, false
	}
	desc, version, metadata := parseFrontmatter(data)
	return SkillInfo{Name: name, Description: desc, Version: version, Metadata: metadata}, true
}

// ListPath 列出 "<name>" 或 "<name>/<sub>" 下的一层目录内容（不递归），
// 返回条目与实际列出的路径。
func (r *Reader) ListPath(arg string) ([]DirEntry, string, error) {
	name, sub := SplitArg(arg)
	if err := r.ensureSkill(name); err != nil {
		return nil, "", err
	}
	dir := name
	if sub != "" {
		cleaned, err := cleanSubPath(sub)
		if err != nil {
			return nil, "", err
		}
		dir = name + "/" + cleaned
		info, err := fs.Stat(r.fsys, dir)
		if err != nil {
			return nil, "", errs.NewValidationError(errs.SubtypeInvalidArgument,
				"skill %q 中不存在路径 %q", name, sub).
				WithHint("用 new-api-cli skills list %s 查看该 skill 的文件", name)
		}
		if !info.IsDir() {
			return nil, "", errs.NewValidationError(errs.SubtypeInvalidArgument,
				"%q 是文件而不是目录", sub).
				WithHint("用 new-api-cli skills read %s/%s 读取它", name, cleaned)
		}
	}
	entries, err := fs.ReadDir(r.fsys, dir)
	if err != nil {
		return nil, "", errs.NewInternalError("读取嵌入的 skill 内容失败: %v", err)
	}
	out := make([]DirEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, DirEntry{Path: dir + "/" + e.Name(), IsDir: e.IsDir()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, dir, nil
}

// SplitArg 在第一个 '/' 处切分 "<name>/<rest>"；没有分隔符时整体是 skill 名。
func SplitArg(arg string) (name, rest string) {
	name, rest, _ = strings.Cut(arg, "/")
	return name, rest
}

// parseFrontmatter 尽力解析 frontmatter；缺失或无法解析时返回零值而非报错 ——
// 内容是给人和 Agent 读的，一处元数据写坏不该让整个 list 失败。
func parseFrontmatter(skillMD []byte) (description, version string, metadata map[string]any) {
	lines := strings.Split(string(skillMD), "\n")
	if strings.TrimRight(lines[0], "\r") != "---" {
		return "", "", nil
	}
	block := make([]string, 0, len(lines))
	closed := false
	for _, ln := range lines[1:] {
		if strings.TrimRight(ln, "\r") == "---" {
			closed = true
			break
		}
		block = append(block, ln)
	}
	if !closed {
		return "", "", nil
	}
	var fm struct {
		Description string         `yaml:"description"`
		Version     string         `yaml:"version"`
		Metadata    map[string]any `yaml:"metadata"`
	}
	if err := yaml.Unmarshal([]byte(strings.Join(block, "\n")), &fm); err != nil {
		return "", "", nil
	}
	return fm.Description, fm.Version, fm.Metadata
}

// ReadSkill 返回 skill 的 SKILL.md 内容。
func (r *Reader) ReadSkill(name string) ([]byte, error) {
	if err := r.ensureSkill(name); err != nil {
		return nil, err
	}
	data, err := fs.ReadFile(r.fsys, name+"/SKILL.md")
	if err != nil {
		return nil, errs.NewInternalError("读取嵌入的 skill 内容失败: %v", err)
	}
	return data, nil
}

func (r *Reader) ensureSkill(name string) error {
	if name == "" || strings.ContainsAny(name, `/\`) || name == "." || name == ".." {
		return unknownSkill(name)
	}
	info, err := fs.Stat(r.fsys, name)
	if err != nil || !info.IsDir() {
		return unknownSkill(name)
	}
	// 没有 SKILL.md 的目录不是 skill —— List 会跳过它，read/list 也必须
	// 报"未知 skill"（用法问题，退出码 6）而不是内部错误。
	if _, err := fs.Stat(r.fsys, name+"/SKILL.md"); err != nil {
		return unknownSkill(name)
	}
	return nil
}

func unknownSkill(name string) error {
	return errs.NewValidationError(errs.SubtypeInvalidArgument, "未知的 skill %q", name).
		WithHint("用 new-api-cli skills list 查看可用的 skill")
}

// cleanSubPath 归一化相对路径，拒绝绝对路径与 ".." 逃逸。
// relpath 必须非空（skill 根目录的情况由调用方处理）。
func cleanSubPath(relpath string) (string, error) {
	cleaned := path.Clean(relpath)
	// path.Clean 只认 '/'，Windows 风格的 "..\" 前缀会残留，需显式拒绝。
	if relpath == "" || path.IsAbs(relpath) || cleaned == "." ||
		cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.HasPrefix(cleaned, `..\`) {
		return "", errs.NewValidationError(errs.SubtypeInvalidArgument,
			"非法路径 %q：必须是不含 '..' 的相对路径", relpath)
	}
	return cleaned, nil
}

// ReadReference 返回 <name>/<relpath> 的内容与归一化后的路径。
func (r *Reader) ReadReference(name, relpath string) ([]byte, string, error) {
	if err := r.ensureSkill(name); err != nil {
		return nil, "", err
	}
	cleaned, err := cleanSubPath(relpath)
	if err != nil {
		return nil, "", err
	}
	full := name + "/" + cleaned
	info, err := fs.Stat(r.fsys, full)
	if err != nil {
		return nil, "", errs.NewValidationError(errs.SubtypeInvalidArgument,
			"skill %q 中不存在引用文件 %q", name, relpath).
			WithHint("用 new-api-cli skills list %s 查看该 skill 的文件", name)
	}
	if info.IsDir() {
		return nil, "", errs.NewValidationError(errs.SubtypeInvalidArgument,
			"%q 是目录而不是文件", relpath)
	}
	data, err := fs.ReadFile(r.fsys, full)
	if err != nil {
		return nil, "", errs.NewInternalError("读取嵌入的 skill 内容失败: %v", err)
	}
	return data, cleaned, nil
}
