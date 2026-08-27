// Package config 以 yaml.v3 Node 模式增量编辑 /etc/clash.yaml:
// 只触碰 proxy-providers[NAME] 与各组 use 列表,保留注释/锚点/其他 provider,
// 绝不整块覆盖 —— 这是 set-sub "只做节点管理与融合" 语义的落点。
package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Editor 持有解析后的配置树;所有操作仅改内存,Save 落盘。
type Editor struct {
	root *yaml.Node // DocumentNode
	path string
}

// Load 解析配置文件为 Node 树(注释随节点保留)。
func Load(path string) (*Editor, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取配置失败: %w", err)
	}
	var root yaml.Node
	if err := yaml.Unmarshal(b, &root); err != nil {
		return nil, fmt.Errorf("解析 YAML 失败: %w", err)
	}
	if root.Kind != yaml.DocumentNode || len(root.Content) == 0 {
		return nil, fmt.Errorf("配置不是有效的 YAML 文档")
	}
	return &Editor{root: &root, path: path}, nil
}

// Save 编码落盘(缩进 2,与手写风格一致)。
// 归一化:yaml.v3 会把 merge 键显式输出为 "!!merge <<",这里还原为手写的裸 "<<"
// (下次解析时 resolve 仍会识别为 merge;避免整份配置因一个编辑产生全文件 diff 噪音)。
func (e *Editor) Save() error {
	normalizeMergeKeys(e.root)
	var buf []byte
	enc := yaml.NewEncoder(&nopWriter{&buf})
	enc.SetIndent(2)
	if err := enc.Encode(e.root); err != nil {
		return fmt.Errorf("编码 YAML 失败: %w", err)
	}
	enc.Close()
	// 反转义非 ASCII(yaml.v3 默认把 emoji 等转成 "\U0001F503",功能正确但可读性差;
	// 仅处理码点 ≥0x80 的 \U/\u 序列,字面反斜杠场景不受影响)
	out := unescapeNonASCII(string(buf))
	return os.WriteFile(e.path, []byte(out+"\n"), 0o644)
}

func unescapeNonASCII(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		c := s[i]
		if c == '\\' && i+1 < len(s) && (s[i+1] == 'U' || s[i+1] == 'u') {
			width := 8
			if s[i+1] == 'u' {
				width = 4
			}
			if i+2+width <= len(s) {
				if cp, ok := parseHex(s[i+2 : i+2+width]); ok && cp >= 0x80 {
					b.WriteRune(rune(cp))
					i += 2 + width
					continue
				}
			}
		}
		b.WriteByte(c)
		i++
	}
	return b.String()
}

func parseHex(s string) (int64, bool) {
	var v int64
	for i := 0; i < len(s); i++ {
		c := s[i]
		var d int64
		switch {
		case c >= '0' && c <= '9':
			d = int64(c - '0')
		case c >= 'a' && c <= 'f':
			d = int64(c-'a') + 10
		case c >= 'A' && c <= 'F':
			d = int64(c-'A') + 10
		default:
			return 0, false
		}
		v = v*16 + d
	}
	return v, true
}

func normalizeMergeKeys(n *yaml.Node) {
	if n == nil {
		return
	}
	if n.Kind == yaml.ScalarNode && n.Tag == "!!merge" {
		n.Tag = "!!str"
	}
	for _, c := range n.Content {
		normalizeMergeKeys(c)
	}
}

type nopWriter struct{ b *[]byte }

func (w *nopWriter) Write(p []byte) (int, error) { *w.b = append(*w.b, p...); return len(p), nil }

// topMap 返回顶层 MappingNode。
func (e *Editor) topMap() *yaml.Node {
	return e.root.Content[0]
}

// mapGet 在 mapping 中按键取值节点;不存在返回 nil。
func mapGet(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// mapSet 替换或追加键值(追加在末尾,保留原顺序与注释)。
func mapSet(m *yaml.Node, key string, val *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content[i+1] = val
			return
		}
	}
	m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, val)
}

// mapDel 删除键;不存在返回 false。
func mapDel(m *yaml.Node, key string) bool {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content = append(m.Content[:i], m.Content[i+2:]...)
			return true
		}
	}
	return false
}

// seqAppend 去重追加标量。
func seqAppend(s *yaml.Node, val string) {
	for _, c := range s.Content {
		if c.Value == val {
			return
		}
	}
	s.Content = append(s.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: val})
}

// seqRemove 移除标量;返回是否发生变更。
func seqRemove(s *yaml.Node, val string) bool {
	for i, c := range s.Content {
		if c.Value == val {
			s.Content = append(s.Content[:i], s.Content[i+1:]...)
			return true
		}
	}
	return false
}

// Providers 返回全部 provider 名称(保持配置顺序)。
func (e *Editor) Providers() []string {
	var out []string
	if pm := mapGet(e.topMap(), "proxy-providers"); pm != nil && pm.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(pm.Content); i += 2 {
			out = append(out, pm.Content[i].Value)
		}
	}
	return out
}

// HasAnchorP 检查配置中是否存在锚点 &p(set-sub 的前置要求)。
func (e *Editor) HasAnchorP() bool {
	v := mapGet(e.topMap(), "p")
	return v != nil && v.Anchor == "p"
}

// ProviderURL 返回指定 provider 的 url(用于 status 展示与占位符检测)。
func (e *Editor) ProviderURL(name string) (string, bool) {
	pm := mapGet(e.topMap(), "proxy-providers")
	if pm == nil {
		return "", false
	}
	p := mapGet(pm, name)
	if p == nil {
		return "", false
	}
	if u := mapGet(p, "url"); u != nil {
		return u.Value, true
	}
	return "", true
}

// SetProvider 写入/更新 provider:url + path,条目复用锚点 <<: *p;
// 已有条目仅改 url/path 两键,其余键与注释保持原样。
func (e *Editor) SetProvider(name, url, cacheRelPath string) error {
	if !e.HasAnchorP() {
		return fmt.Errorf("配置缺少锚点 &p(set-sub 依赖它生成 provider 条目;基础模板自带)")
	}
	tm := e.topMap()
	pm := mapGet(tm, "proxy-providers")
	if pm == nil || pm.Kind != yaml.MappingNode {
		pm = &yaml.Node{Kind: yaml.MappingNode}
		mapSet(tm, "proxy-providers", pm)
	}
	entry := mapGet(pm, name)
	if entry == nil {
		entry = &yaml.Node{
			Kind: yaml.MappingNode,
			Content: []*yaml.Node{
				{Kind: yaml.ScalarNode, Tag: "!!merge", Value: "<<"},
				{Kind: yaml.AliasNode, Value: "p"},
			},
		}
		pm.Content = append(pm.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: name}, entry)
	}
	mapSet(entry, "url", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: url})
	mapSet(entry, "path", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: cacheRelPath})
	return nil
}

// RemoveProvider 删除 provider 条目。
func (e *Editor) RemoveProvider(name string) bool {
	pm := mapGet(e.topMap(), "proxy-providers")
	if pm == nil {
		return false
	}
	return mapDel(pm, name)
}

// WireProvider 把 name 融合进 use 列表(add=true)或移除(add=false)。
//
// 融合规则(基础模板优化路径优先):
//  1. 顶层锚点持有者 pr/prd/use 的 use 序列 —— 一处修改,全部消费组生效
//  2. 无锚点持有者时(自定义配置):遍历 proxy-groups,凡 use 非空的组逐个追加
//
// groups 非空时(--group):只改指定组,组内无 use 键则显式创建(覆盖 merge 语义)。
func (e *Editor) WireProvider(name string, add bool, groups []string) int {
	tm := e.topMap()
	changed := 0
	if len(groups) == 0 {
		// 路径 1:锚点持有者
		for _, holder := range []string{"pr", "prd", "use"} {
			hm := mapGet(tm, holder)
			if hm == nil {
				continue
			}
			if use := mapGet(hm, "use"); use != nil && use.Kind == yaml.SequenceNode {
				if add {
					seqAppend(use, name)
				} else {
					seqRemove(use, name)
				}
				changed++
			}
		}
		if changed > 0 {
			return changed
		}
		// 路径 2:自定义配置,遍历组
		if gl := mapGet(tm, "proxy-groups"); gl != nil && gl.Kind == yaml.SequenceNode {
			for _, g := range gl.Content {
				if use := mapGet(g, "use"); use != nil && use.Kind == yaml.SequenceNode && len(use.Content) > 0 {
					if add {
						seqAppend(use, name)
					} else {
						seqRemove(use, name)
					}
					changed++
				}
			}
		}
		return changed
	}
	// --group 显式指定
	gl := mapGet(tm, "proxy-groups")
	if gl == nil || gl.Kind != yaml.SequenceNode {
		return 0
	}
	for _, g := range gl.Content {
		gname := mapGet(g, "name")
		if gname == nil {
			continue
		}
		for _, want := range groups {
			if gname.Value != want {
				continue
			}
			use := mapGet(g, "use")
			if use == nil || use.Kind != yaml.SequenceNode {
				use = &yaml.Node{Kind: yaml.SequenceNode}
				mapSet(g, "use", use)
			}
			if add {
				seqAppend(use, name)
			} else {
				seqRemove(use, name)
			}
			changed++
		}
	}
	return changed
}

// Backup / Restore 事务配套:修改前备份,失败恢复。
func Backup(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return os.WriteFile(path+".panixy-bak", b, 0o644)
}

func Restore(path string) error {
	b, err := os.ReadFile(path + ".panixy-bak")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func ClearBackup(path string) { os.Remove(path + ".panixy-bak") }
