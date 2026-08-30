package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/deadship2003/Panoxy/internal/constants"
	"github.com/deadship2003/Panoxy/internal/health"
	"github.com/deadship2003/Panoxy/internal/paths"
	"github.com/deadship2003/Panoxy/internal/statemode"
)

func runStatus(cmd *cobra.Command, args []string) error {
	p := paths.Get()
	v, _ := cmd.Flags().GetBool("detail")
	q, _ := cmd.Flags().GetBool("quiet")
	asJSON, _ := cmd.Flags().GetBool("json")

	r := health.Collect(p.Conf, p.Bin, p.UiStamp, p.LastUp, p.State)
	// Fix the stale-rule judgement: table exists + service active = normal; table exists + service inactive = truly stale.
	if r.Stale && r.Service == "active" {
		r.Stale = false // service is running, rules in the table is the normal state
	}

	// -q: exit code only (0 healthy, 1 degraded, 2 faulty).
	if q {
		if r.Service != "active" || !r.APIAlive {
			os.Exit(2)
		}
		if r.Nodes == 0 || r.Egress != "204" {
			os.Exit(1)
		}
		os.Exit(0)
	}
	if asJSON {
		b, _ := json.Marshal(r)
		fmt.Println(string(b))
		return nil
	}

	fmt.Printf("== %s v%s  (%s) ==\n", constants.ProgName, constants.Version, p.Root)
	fmt.Printf("service:  %s\n", r.Service)
	fmt.Printf("mode:     %s   firewall: %s", r.Mode, r.FwBackend)
	if r.Stale {
		fmt.Printf("  ⚠️ stale rules detected (restart %s self-heals)", constants.ProgName)
	}
	fmt.Println()
	for _, st := range r.Providers {
		mark := "✅"
		err := ""
		if st.Error != "" {
			mark, err = "⚠️", "  "+st.Error
		} else if st.Nodes == 0 {
			mark = "⚠️ 0 nodes"
		}
		fmt.Printf("sub:      %s %-16s %d nodes%s\n", mark, st.Name, st.Nodes, err)
	}
	if len(r.Providers) == 0 {
		fmt.Printf("sub:      - (config has no proxy-providers)\n")
	}
	fmt.Printf("kernel:   %s\n", r.CoreVer)
	fmt.Printf("ui:       %s   last upgrade: %s\n", r.UIVer, orUnknown(r.LastUp))
	fmt.Printf("api:      %s\n", orUnreachable(r.APIAlive, r.APIVer))
	fmt.Printf("egress:   %s (expect 204)\n", r.Egress)
	fmt.Printf("direct:   %s (expect 204)\n", r.Direct)
	fmt.Println("tip:      browser built-in DoH (443) cannot be hijacked by the kernel; domain-based routing does not apply to it, recommend disabling it")

	if v {
		fmt.Println("-- detail (--detail) --")
		if r.Mode == "tun" {
			fmt.Println("TUN stack: system (current config; for heavy BT/UDP, frequent drops, or old kernels, gvisor is recommended as a fallback)")
		} else {
			fmt.Println("TPROXY:   keeps the client source IP; note the IPv6/container hijack pitfalls (see README)")
		}
		if b, err := os.ReadFile(p.State); err == nil {
			fmt.Printf("state file: %s\n%s", p.State, indentLines(string(b)))
		}
		fmt.Printf("data plane (node/group selection) is done in the web UI; transport plane (tun/tproxy) via %s mode\n", constants.ProgName)
	}
	return nil
}

func runMode(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		p := paths.Get()
		fmt.Println(statemode.Read(p.State))
		return nil
	}
	return modeSwitch(args[0])
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}
func orUnreachable(ok bool, v string) string {
	if !ok {
		return "unreachable"
	}
	return v
}
func indentLines(s string) string {
	return "  " + strings.ReplaceAll(strings.TrimSpace(s), "\n", "\n  ")
}
