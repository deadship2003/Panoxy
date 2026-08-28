// 进度显示:TTY 下单行刷新进度条,非 TTY 下按里程碑打印(不污染管道/日志)。
package logx

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// Progress 渲染下载/复制进度。
type Progress struct {
	label string
	total int64
	last  time.Time
	marks map[int]bool // 非 TTY 里程碑
	isTTY bool
}

func NewProgress(label string, total int64) *Progress {
	fi, err := os.Stderr.Stat()
	isTTY := err == nil && fi.Mode()&os.ModeCharDevice != 0
	return &Progress{label: label, total: total, isTTY: isTTY, marks: map[int]bool{}}
}

// Update 上报已传输字节数。
func (p *Progress) Update(n int64) {
	if p == nil {
		return
	}
	now := time.Now()
	if p.isTTY {
		if now.Sub(p.last) < 100*time.Millisecond && n < p.total {
			return // 节流 100ms
		}
		p.last = now
		fmt.Fprint(os.Stderr, "\r"+p.render(n))
		return
	}
	if p.total <= 0 {
		return
	}
	pct := int(n * 100 / p.total)
	for _, m := range []int{0, 25, 50, 75} {
		if pct >= m && !p.marks[m] {
			p.marks[m] = true
			Info("%s: %d%%(%s)", p.label, pct, humanBytes(n, p.total))
		}
	}
}

// Done 结束(成功/失败都要调,补换行/终点)。
func (p *Progress) Done(err error) {
	if p == nil {
		return
	}
	if p.isTTY {
		fmt.Fprint(os.Stderr, "\r"+p.render(p.total)+"\n")
	}
	if err != nil {
		Warn("%s 失败: %v", p.label, err)
	} else {
		Info("%s 完成(%s)", p.label, humanBytes(p.total, p.total))
	}
}

func (p *Progress) render(n int64) string {
	pct := 0
	if p.total > 0 {
		pct = int(n * 100 / p.total)
	}
	if pct > 100 {
		pct = 100
	}
	const width = 24
 filled := pct * width / 100
	bar := strings.Repeat("█", filled) + strings.Repeat("─", width-filled)
	return fmt.Sprintf("  %s %3d%% [%s] %s", p.label, pct, bar, humanBytes(n, p.total))
}

func humanBytes(n, total int64) string {
	f := func(v int64) string {
		switch {
		case v >= 1<<20:
			return fmt.Sprintf("%.1fMB", float64(v)/(1<<20))
		case v >= 1<<10:
			return fmt.Sprintf("%.0fKB", float64(v)/(1<<10))
		default:
			return fmt.Sprintf("%dB", v)
		}
	}
	if total > 0 {
		return f(n) + "/" + f(total)
	}
	return f(n)
}
