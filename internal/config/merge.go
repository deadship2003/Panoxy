// merge-conf 的核心:个人配置定向融合进基底配置。
// 原则(用户确认的决策表):
//
//	接管(个人):端口/密钥/external-controller、proxy-groups、rules、proxies
//	保留(基底):tun 模式段、routing-mark、dns(可 --dns mine)、external-ui/geo/ntp/sniffer/锚点
//	合并:proxy-providers(同名基底优先)、rule-providers(同名个人优先)
//	自动:rules 含进程规则 → find-process-mode=strict;个人 proxies 全部带入并
//	     追加进各组 proxies 末尾(select 组默认不变,面板中自行挑选)
package config

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

type MergeOpts struct {
	DNSMine     bool // 个人 dns 段接管(listen 仍强制 0.0.0.0:1053)
	NoWire      bool // 不把基底订阅接线进个人组
	NoProxyWire bool // 不把个人 proxies 追加进组
}

type MergeReport struct {
	Taken     []string // 接管(个人)
	Kept      []string // 保留(基底)
	Providers struct {
		BaseKept []string // 基底保留(含同名冲突)
		Personal []string // 个人新增
		Conflict []string // 同名,基底优先
	}
	RuleProvidersAdded []string
	PersonalProxies    []string
	Adjustments        []string // 自动调整(find-process-mode 等)
	Warnings           []string
}

// MergePersonal 把 src(个人配置)按决策表融合进 e(基底)。不落盘,由调用方 Save。
func (e *Editor) MergePersonal(src *Editor, opts MergeOpts) (*MergeReport, error) {
	tmB, tmS := e.topMap(), src.topMap()
	r := &MergeReport{}

	// 1) 接管:端口/密钥/控制器 + proxies + proxy-groups + rules
	for _, k := range []string{"mixed-port", "port", "socks-port", "secret", "external-controller"} {
		if v := mapGet(tmS, k); v != nil {
			mapSet(tmB, k, deepCopy(v))
			r.Taken = append(r.Taken, k)
		}
	}
	if v := mapGet(tmS, "proxies"); v != nil && v.Kind == yaml.SequenceNode {
		mapSet(tmB, "proxies", deepCopy(v))
		r.Taken = append(r.Taken, "proxies")
		for _, p := range v.Content { // 收集个人节点名(供接线)
			if n := mapGet(p, "name"); n != nil {
				r.PersonalProxies = append(r.PersonalProxies, n.Value)
			}
		}
	}
	if v := mapGet(tmS, "proxy-groups"); v != nil && v.Kind == yaml.SequenceNode {
		mapSet(tmB, "proxy-groups", deepCopy(v))
		r.Taken = append(r.Taken, "proxy-groups")
	}
	hasProcessRules := false
	if v := mapGet(tmS, "rules"); v != nil && v.Kind == yaml.SequenceNode {
		mapSet(tmB, "rules", deepCopy(v))
		r.Taken = append(r.Taken, "rules")
		for _, rule := range v.Content {
			if len(rule.Value) >= 8 && rule.Value[:8] == "PROCESS-" {
				hasProcessRules = true
			}
		}
	}

	// 2) 保留(基底):模式段/暗号/基础设施 —— 不动即为保留
	r.Kept = append(r.Kept, "tun/tproxy-port(模式段)", "routing-mark", "dns.listen", "external-ui", "geo*", "ntp", "sniffer", "profile")

	// 3) dns:默认基底;--dns mine 时接管但强制 listen
	if opts.DNSMine {
		if d := mapGet(tmS, "dns"); d != nil {
			dn := deepCopy(d)
			mapSet(tmB, "dns", dn)
			mapSet(dn, "listen", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "0.0.0.0:1053"})
			r.Taken = append(r.Taken, "dns(--dns mine,listen 强制 1053)")
		}
	}

	// 4) rule-providers 合并(同名个人优先)
	if rpS := mapGet(tmS, "rule-providers"); rpS != nil && rpS.Kind == yaml.MappingNode {
		rpB := mapGet(tmB, "rule-providers")
		if rpB == nil || rpB.Kind != yaml.MappingNode {
			rpB = &yaml.Node{Kind: yaml.MappingNode}
			mapSet(tmB, "rule-providers", rpB)
		}
		for i := 0; i+1 < len(rpS.Content); i += 2 {
			name, val := rpS.Content[i].Value, deepCopy(rpS.Content[i+1])
			mapSet(rpB, name, val) // 同名覆盖(个人优先)
			r.RuleProvidersAdded = append(r.RuleProvidersAdded, name)
		}
	}

	// 5) proxy-providers 合并(同名基底优先 —— 已导入的订阅含缓存)
	if ppS := mapGet(tmS, "proxy-providers"); ppS != nil && ppS.Kind == yaml.MappingNode {
		ppB := mapGet(tmB, "proxy-providers")
		if ppB == nil || ppB.Kind != yaml.MappingNode {
			ppB = &yaml.Node{Kind: yaml.MappingNode}
			mapSet(tmB, "proxy-providers", ppB)
		}
		if ppB.Kind == yaml.MappingNode {
			for i := 0; i+1 < len(ppB.Content); i += 2 {
				r.Providers.BaseKept = append(r.Providers.BaseKept, ppB.Content[i].Value)
			}
		}
		for i := 0; i+1 < len(ppS.Content); i += 2 {
			name, val := ppS.Content[i].Value, ppS.Content[i+1]
			if mapGet(ppB, name) != nil {
				r.Providers.Conflict = append(r.Providers.Conflict, name)
				continue // 基底优先
			}
			ppB.Content = append(ppB.Content,
				&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: name}, deepCopy(val))
			r.Providers.Personal = append(r.Providers.Personal, name)
		}
	}

	// 6) 自动调整:进程分流
	if hasProcessRules {
		mapSet(tmB, "find-process-mode", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "strict",
			LineComment: "merge-conf 自动:检测到 PROCESS- 规则(仅对本机流量生效,网关转发流量无进程信息)"})
		r.Adjustments = append(r.Adjustments, "find-process-mode → strict(个人 rules 含进程分流规则)")
	}

	// 7) 锚点清理:个人组接管后,pr/prd/use 锚点若无人引用则删除
	//    (保留 p: &p —— set-sub 生成 provider 条目仍依赖),避免后续接线走错锚点路径
	groups := mapGet(tmB, "proxy-groups")
	for _, a := range []string{"pr", "prd", "use"} {
		if groups != nil && nodeRefsAlias(groups, a) {
			continue
		}
		if mapGet(tmB, a) != nil {
			mapDel(tmB, a)
			r.Adjustments = append(r.Adjustments, fmt.Sprintf("移除未引用锚点 &%s(个人组未使用;保留 &p 供 set-sub)", a))
		}
	}
	return r, nil
}

// WireAfterMerge 融合后的接线:
//
//	a) 基底订阅未被个人组引用 → 追加进含 use: 的组(已有订阅不失效)
//	b) 个人 proxies 全部追加进各组 proxies: 末尾(select 默认不变,面板自行挑选)
func (e *Editor) WireAfterMerge(baseProviders, personalProxies []string, opts MergeOpts) (wired int) {
	if !opts.NoWire {
		for _, pn := range baseProviders {
			if !e.providerReferenced(pn) {
				wired += e.WireProvider(pn, true, nil)
			}
		}
	}
	if !opts.NoProxyWire {
		gl := mapGet(e.topMap(), "proxy-groups")
		if gl == nil || gl.Kind != yaml.SequenceNode {
			return
		}
		for _, g := range gl.Content {
			px := mapGet(g, "proxies")
			if px == nil || px.Kind != yaml.SequenceNode || len(px.Content) == 0 {
				continue // 只追加进已有 proxies 列表的组,不给纯 use 组强造列表
			}
			for _, n := range personalProxies {
				seqAppend(px, n)
			}
		}
	}
	return
}

// providerReferenced 组的 use 列表里是否已引用该 provider。
func (e *Editor) providerReferenced(name string) bool {
	gl := mapGet(e.topMap(), "proxy-groups")
	if gl == nil || gl.Kind != yaml.SequenceNode {
		return false
	}
	for _, g := range gl.Content {
		if use := mapGet(g, "use"); use != nil && use.Kind == yaml.SequenceNode {
			for _, u := range use.Content {
				if u.Value == name {
					return true
				}
			}
		}
	}
	return false
}

// nodeRefsAlias 子树中是否引用了指定锚点(AliasNode.Value == anchor)。
func nodeRefsAlias(n *yaml.Node, anchor string) bool {
	if n == nil {
		return false
	}
	if n.Kind == yaml.AliasNode && n.Value == anchor {
		return true
	}
	for _, c := range n.Content {
		if nodeRefsAlias(c, anchor) {
			return true
		}
	}
	return false
}

// deepCopy 深拷贝 yaml 节点(保留注释/风格;个人子树独立于源文档)。
func deepCopy(n *yaml.Node) *yaml.Node {
	if n == nil {
		return nil
	}
	c := *n
	c.Content = nil
	for _, ch := range n.Content {
		c.Content = append(c.Content, deepCopy(ch))
	}
	return &c
}
