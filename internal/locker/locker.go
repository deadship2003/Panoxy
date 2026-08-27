// Package locker 提供 flock 文件互斥:写命令(deploy/install/set-sub/sub-rm/upgrade/
// rollback/uninstall/mode)加锁,读命令(status/log/check/sub-list)不加。
package locker

import (
	"fmt"
	"os"
	"syscall"
)

type Locker struct {
	f  *os.File
	ok bool
}

// Lock 获取互斥锁;被占用返回错误(不等待)。
func Lock(path string) (*Locker, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		// 锁文件不可写(如只读环境):降级为无锁,行为与 bash 版一致
		fmt.Fprintf(os.Stderr, "[panixy] WARN 锁文件不可用(%v),继续无锁运行\n", err)
		return &Locker{}, nil
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("另一个 panixy 实例正在运行,请稍后再试")
	}
	return &Locker{f: f, ok: true}, nil
}

func (l *Locker) Unlock() {
	if l == nil || !l.ok {
		return
	}
	syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	l.f.Close()
	l.ok = false
}
