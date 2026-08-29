// Package config 以 yaml.v3 Node 模式增量编辑 /etc/clash.yaml:
// 只触碰 proxy-providers[NAME] 与各组 use 列表,保留注释/锚点/其他 provider,
// 绝不整块覆盖 —— 这是 set-sub "只做节点管理与融合" 语义的落点。
package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/deadship2003/panixy/internal/asset"

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

// SetProviderType 切换 provider 读取方式(file=true 时 type: file 读本地缓存、不刷新远程;
// false 时删除显式 type,回落到锚点 <<: *p 的 type: http 自动刷新)。
//
// 用于 mihomo 无法原生解析的订阅格式(sing-box/Surge/base64-Clash):set-sub 已把内容
// 归一化成 Clash YAML 写入缓存,必须切 file,否则 mihomo 重启时会重新拉原始 URL 再解析失败。
func (e *Editor) SetProviderType(name string, file bool) error {
	pm := mapGet(e.topMap(), "proxy-providers")
	if pm == nil || pm.Kind != yaml.MappingNode {
		return fmt.Errorf("配置缺少 proxy-providers 段")
	}
	entry := mapGet(pm, name)
	if entry == nil {
		return fmt.Errorf("provider %s 不存在", name)
	}
	if file {
		mapSet(entry, "type", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "file"})
	} else {
		mapDel(entry, "type")
	}
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

// PruneDerived 根据实际节点名剔除无匹配的派生组(带 filter 的地区/类型组),只保留有效分组。
// 被剔除的组名同步从锚点持有者(pr/prd)与 dns 组的 proxies 列表中移除,避免悬空引用。
// 返回剔除的组数。nodeNames 应覆盖全部 provider(含新导入订阅),避免误删仍被其他订阅命中的组。
func (e *Editor) PruneDerived(nodeNames []string) int {
	tm := e.topMap()
	gl := mapGet(tm, "proxy-groups")
	if gl == nil || gl.Kind != yaml.SequenceNode {
		return 0
	}
	var prune []string
	keep := make([]*yaml.Node, 0, len(gl.Content))
	for _, g := range gl.Content {
		f := mapGet(g, "filter")
		if f == nil || f.Value == "" {
			keep = append(keep, g) // 无 filter 的组(应用组/兜底组)不参与剪枝
			continue
		}
		re, err := regexp.Compile(f.Value)
		if err != nil {
			keep = append(keep, g) // 无效 filter 保留,交由 mihomo -t 报错
			continue
		}
		hit := false
		for _, n := range nodeNames {
			if re.MatchString(n) {
				hit = true
				break
			}
		}
		if name := mapGet(g, "name"); name != nil && !hit {
			prune = append(prune, name.Value)
		} else {
			keep = append(keep, g)
		}
	}
	if len(prune) == 0 {
		return 0
	}
	gl.Content = keep
	// 从锚点持有者与 dns 组的 proxies 移除被剔除的组名
	for _, holder := range []string{"pr", "prd"} {
		if hm := mapGet(tm, holder); hm != nil {
			if px := mapGet(hm, "proxies"); px != nil && px.Kind == yaml.SequenceNode {
				for _, p := range prune {
					seqRemove(px, p)
				}
			}
		}
	}
	for _, g := range gl.Content {
		if name := mapGet(g, "name"); name != nil && name.Value == "dns" {
			if px := mapGet(g, "proxies"); px != nil && px.Kind == yaml.SequenceNode {
				for _, p := range prune {
					seqRemove(px, p)
				}
			}
		}
	}
	return len(prune)
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

// SetMode 切换 tun/tproxy 配置变体(mode 命令用;tun 块与模板常量保持一致)。
func (e *Editor) SetMode(tproxy bool, tproxyPort int) {
	tm := e.topMap()
	if tproxy {
		mapDel(tm, "tun")
		mapSet(tm, "tproxy-port", &yaml.Node{
			Kind: yaml.ScalarNode, Tag: "!!int", Value: fmt.Sprint(tproxyPort),
			LineComment: "TPROXY 模式(mark/策略路由由 panixy 防火墙管理)",
		})
		return
	}
	mapDel(tm, "tproxy-port")
	tun := &yaml.Node{Kind: yaml.MappingNode}
	for _, kv := range asset.TunParams {
		tag := "!!str"
		if kv[1] == "true" || kv[1] == "1500" {
			tag = "!!bool"
			if kv[1] == "1500" {
				tag = "!!int"
			}
		}
		tun.Content = append(tun.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: kv[0]},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: kv[1]})
	}
	exc := &yaml.Node{Kind: yaml.SequenceNode}
	for _, cidr := range asset.TunRouteExclude {
		exc.Content = append(exc.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: cidr})
	}
	tun.Content = append(tun.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "route-exclude-address",
			LineComment: "排除回环/内网,防代理循环"}, exc)
	mapSet(tm, "tun", tun)
}

// SetPath 改变 Save 目标(预览/临时校验用)。
func (e *Editor) SetPath(p string) { e.path = p }

// Path 返回当前落盘路径。
func (e *Editor) Path() string { return e.path }
