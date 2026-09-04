// merge-conf 核心:叠加式融合(同名组字段级合并,非替换)。
//
// 融合策略(用户确认):
//
//	同名组:  字段级合并(proxies/use 并集,标量个人覆盖,个人新增字段带入)
//	新增组:  追加到末尾
//	基底组:  保留(不被删除,引用不断链)
//	规则:    个人前置(优先匹配)+ 基底兜底(MATCH 排最后,去重)
//	备份:    融合前 → <prog>-premerge 后缀;失败自动恢复;--rollback 手动回滚
package config

import (
	"fmt"
	"os"

	"github.com/deadship2003/panoxy/internal/constants"

	"gopkg.in/yaml.v3"
)

type MergeOpts struct {
	DNSMine     bool // 个人 dns 段接管(listen 仍强制 [::]:1053 双栈)
	NoWire      bool // 不把基底订阅接线进组
	NoProxyWire bool // 不把个人 proxies 追加进组
}

type MergeReport struct {
	GroupsMerged  []string // 同名融合
	GroupsAdded   []string // 个人新增
	GroupsKept    []string // 基底保留
	RulesPersonal int
	RulesBase     int
	RulesDeduped  int
	Taken         []string // 接管(个人)
	Kept          []string // 保留(基底)
	Providers     struct {
		BaseKept []string
		Personal []string
		Conflict []string
	}
	RuleProvidersAdded []string
	PersonalProxies    []string
	Adjustments        []string
	BackupPath         string // premerge 备份路径(空=未备份)
}

// PremergeBackup 融合前备份(供 --rollback 恢复)。
func PremergeBackup(confPath string) (string, error) {
	dst := confPath + constants.PremergeSuffix()
	if err := backupFile(confPath, constants.PremergeSuffix()); err != nil {
		return "", err
	}
	return dst, nil
}

// PremergeRestore 从 premerge 备份恢复。
func PremergeRestore(confPath string) error {
	if err := restoreFile(confPath, constants.PremergeSuffix()); err != nil {
		return fmt.Errorf("no premerge backup: %w", err)
	}
	return nil
}

// PremergeExists 判断 premerge 备份是否存在。
func PremergeExists(confPath string) bool {
	_, err := os.Stat(confPath + constants.PremergeSuffix())
	return err == nil
}

// MergePersonal 叠加式融合:同名组合并 + 新增追加 + 基底保留。
func (e *Editor) MergePersonal(src *Editor, opts MergeOpts) (*MergeReport, error) {
	tmB, tmS := e.topMap(), src.topMap()
	r := &MergeReport{}

	// 1) 标量接管:端口/密钥/控制器
	for _, k := range []string{"mixed-port", "port", "socks-port", "secret", "external-controller"} {
		if v := mapGet(tmS, k); v != nil {
			mapSet(tmB, k, deepCopy(v))
			r.Taken = append(r.Taken, k)
		}
	}

	// 2) proxies:追加(基底通常无)
	if v := mapGet(tmS, "proxies"); v != nil && v.Kind == yaml.SequenceNode {
		basePx := mapGet(tmB, "proxies")
		if basePx == nil || basePx.Kind != yaml.SequenceNode {
			basePx = &yaml.Node{Kind: yaml.SequenceNode}
			mapSet(tmB, "proxies", basePx)
		}
		for _, p := range v.Content {
			basePx.Content = append(basePx.Content, deepCopy(p))
			if n := mapGet(p, "name"); n != nil {
				r.PersonalProxies = append(r.PersonalProxies, n.Value)
			}
		}
		r.Taken = append(r.Taken, "proxies (appended)")
	}

	// 3) proxy-groups:同名融合 + 新增追加 + 基底保留
	if v := mapGet(tmS, "proxy-groups"); v != nil && v.Kind == yaml.SequenceNode {
		baseGroups := mapGet(tmB, "proxy-groups")
		if baseGroups == nil || baseGroups.Kind != yaml.SequenceNode {
			baseGroups = &yaml.Node{Kind: yaml.SequenceNode}
			mapSet(tmB, "proxy-groups", baseGroups)
		}

		// 建立基底组名→节点索引
		baseIdx := map[string]int{}
		for i, g := range baseGroups.Content {
			if n := mapGet(g, "name"); n != nil {
				baseIdx[n.Value] = i
			}
		}

		// 逐个处理个人组
		for _, pg := range v.Content {
			pn := mapGet(pg, "name")
			if pn == nil {
				continue
			}
			if bi, ok := baseIdx[pn.Value]; ok {
				// 同名:字段级融合
				mergeGroupNodes(baseGroups.Content[bi], pg)
				r.GroupsMerged = append(r.GroupsMerged, pn.Value)
			} else {
				// 新增:追加到末尾
				baseGroups.Content = append(baseGroups.Content, deepCopy(pg))
				r.GroupsAdded = append(r.GroupsAdded, pn.Value)
			}
		}

		// 记录基底保留的组(未被个人覆盖的)
		for _, g := range baseGroups.Content {
			if n := mapGet(g, "name"); n != nil {
				found := false
				for _, m := range r.GroupsMerged {
					if m == n.Value {
						found = true
						break
					}
				}
				if !found {
					r.GroupsKept = append(r.GroupsKept, n.Value)
				}
			}
		}
		r.Taken = append(r.Taken, "proxy-groups (merged)")
	}

	// 4) rules:个人前置 + 基底兜底(去重,MATCH 排最后)
	if v := mapGet(tmS, "rules"); v != nil && v.Kind == yaml.SequenceNode {
		baseRules := mapGet(tmB, "rules")
		var baseList []string
		if baseRules != nil && baseRules.Kind == yaml.SequenceNode {
			for _, rn := range baseRules.Content {
				baseList = append(baseList, rn.Value)
			}
		}

		var merged []string
		seen := map[string]bool{}
		for _, rn := range v.Content {
			if !seen[rn.Value] {
				merged = append(merged, rn.Value)
				seen[rn.Value] = true
			}
		}
		r.RulesPersonal = len(v.Content)

		var matchRule string
		for _, br := range baseList {
			if len(br) > 6 && br[:6] == "MATCH," {
				matchRule = br
				continue
			}
			if !seen[br] {
				merged = append(merged, br)
				seen[br] = true
			} else {
				r.RulesDeduped++
			}
		}
		if matchRule != "" && !seen[matchRule] {
			merged = append(merged, matchRule)
		}
		r.RulesBase = len(baseList)

		newRules := &yaml.Node{Kind: yaml.SequenceNode}
		for _, rs := range merged {
			newRules.Content = append(newRules.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: rs})
		}
		mapSet(tmB, "rules", newRules)
		r.Taken = append(r.Taken, "rules (personal-first + base fallback)")
	}

	// 5) 保留(基底):模式段/暗号/基础设施
	r.Kept = append(r.Kept, "tun/tproxy-port (mode block)", "routing-mark", "dns.listen", "external-ui", "geo*", "ntp", "sniffer", "profile")

	// 6) dns:默认基底;--dns mine 时接管但强制 listen
	if opts.DNSMine {
		if d := mapGet(tmS, "dns"); d != nil {
			dn := deepCopy(d)
			mapSet(tmB, "dns", dn)
			mapSet(dn, "listen", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "[::]:1053"})
			r.Taken = append(r.Taken, "dns (--dns mine, listen forced to [::]:1053)")
		}
	}

	// 7) rule-providers 合并(同名个人优先)
	if rpS := mapGet(tmS, "rule-providers"); rpS != nil && rpS.Kind == yaml.MappingNode {
		rpB := mapGet(tmB, "rule-providers")
		if rpB == nil || rpB.Kind != yaml.MappingNode {
			rpB = &yaml.Node{Kind: yaml.MappingNode}
			mapSet(tmB, "rule-providers", rpB)
		}
		for i := 0; i+1 < len(rpS.Content); i += 2 {
			name := rpS.Content[i].Value
			mapSet(rpB, name, deepCopy(rpS.Content[i+1]))
			r.RuleProvidersAdded = append(r.RuleProvidersAdded, name)
		}
	}

	// 8) proxy-providers 合并(同名基底优先)
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
			name := ppS.Content[i].Value
			if mapGet(ppB, name) != nil {
				r.Providers.Conflict = append(r.Providers.Conflict, name)
				continue
			}
			ppB.Content = append(ppB.Content,
				&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: name}, deepCopy(ppS.Content[i+1]))
			r.Providers.Personal = append(r.Providers.Personal, name)
		}
	}

	// 9) 占位退场
	ppNow := mapGet(tmB, "proxy-providers")
	var retired []string
	if ppNow != nil && ppNow.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(ppNow.Content); i += 2 {
			pn, pv := ppNow.Content[i].Value, ppNow.Content[i+1]
			if u := mapGet(pv, "url"); u != nil && u.Value == PlaceholderURL {
				if len(ppNow.Content) > 2 {
					retired = append(retired, pn)
				}
			}
		}
		for _, pn := range retired {
			mapDel(ppNow, pn)
		}
	}
	if len(retired) > 0 {
		r.Adjustments = append(r.Adjustments, fmt.Sprintf("removed placeholder subscription %v (real subscription is in place)", retired))
		// 清理所有对已退场 provider 的引用:组的 use 列表 + 顶层锚点定义(pr/prd/use)
		// (merge key 的 use 在锚点定义里,不在组的直接 Content 中)
		cleanupRefs := func(m *yaml.Node) {
			if m == nil || m.Kind != yaml.MappingNode {
				return
			}
			useNode := mapGet(m, "use")
			if useNode == nil || useNode.Kind != yaml.SequenceNode {
				return
			}
			var keep []*yaml.Node
			for _, u := range useNode.Content {
				isRetired := false
				for _, rn := range retired {
					if u.Value == rn {
						isRetired = true
						break
					}
				}
				if !isRetired {
					keep = append(keep, u)
				}
			}
			useNode.Content = keep
		}
		// 清理组(直接 use 列表)
		gl := mapGet(tmB, "proxy-groups")
		if gl != nil && gl.Kind == yaml.SequenceNode {
			for _, g := range gl.Content {
				cleanupRefs(g)
			}
		}
		// 清理顶层锚点定义(pr/prd/use 内的 use 列表)
		for _, anchor := range []string{"pr", "prd", "use"} {
			cleanupRefs(mapGet(tmB, anchor))
		}
	}

	// 10) 进程分流
	hasProcess := false
	if rules := mapGet(tmB, "rules"); rules != nil && rules.Kind == yaml.SequenceNode {
		for _, rule := range rules.Content {
			if len(rule.Value) >= 8 && rule.Value[:8] == "PROCESS-" {
				hasProcess = true
				break
			}
		}
	}
	if hasProcess {
		mapSet(tmB, "find-process-mode", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "strict"})
		r.Adjustments = append(r.Adjustments, "find-process-mode → strict (PROCESS- rule detected)")
	}

	return r, nil
}

// mergeGroupNodes 字段级合并同名组:个人字段覆盖/新增,proxies/use 取并集。
func mergeGroupNodes(base, personal *yaml.Node) {
	if base == nil || personal == nil || base.Kind != yaml.MappingNode || personal.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(personal.Content); i += 2 {
		key := personal.Content[i].Value
		val := personal.Content[i+1]

		if key == "proxies" || key == "use" {
			baseVal := mapGet(base, key)
			if baseVal == nil || baseVal.Kind != yaml.SequenceNode {
				base.Content = append(base.Content, personal.Content[i], deepCopy(val))
				continue
			}
			// 并集:个人在前(优先),基底原有追加在后,去重
			added := map[string]bool{}
			var newList []*yaml.Node
			for _, pv := range val.Content {
				if !added[pv.Value] {
					newList = append(newList, deepCopy(pv))
					added[pv.Value] = true
				}
			}
			for _, bv := range baseVal.Content {
				if !added[bv.Value] {
					newList = append(newList, bv)
					added[bv.Value] = true
				}
			}
			baseVal.Content = newList
		} else {
			mapSet(base, key, deepCopy(val))
		}
	}
}

// WireAfterMerge 融合后接线。
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
				continue
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

// deepCopy 深拷贝 yaml 节点。
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
