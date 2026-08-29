// Package locker 提供 flock 文件互斥:写命令(deploy/install/sub import/del/upgrade/
// rollback/uninstall/mode)加锁,读命令(status/log/check/sub list)不加。
package locker

import (
	"fmt"
	"os"
	"sync"
	"syscall"
)

type Locker struct {
	f  *os.File
	ok bool
	re bool // 进程内重入(deploy→install 同进程复用已持锁)
}

var (
	mu     sync.Mutex
	locked bool
)

// Lock 获取互斥锁;被占用返回错误(不等待)。进程内可重入(deploy 内调 install)。
func Lock(path string) (*Locker, error) {
	mu.Lock()
	if locked {
		mu.Unlock()
		return &Locker{re: true}, nil
	}
	mu.Unlock()
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
	mu.Lock()
	locked = true
	mu.Unlock()
	return &Locker{f: f, ok: true}, nil
}

func (l *Locker) Unlock() {
	if l == nil || l.re {
		return // 重入持有者不解锁,由最外层释放
	}
	if !l.ok {
		return
	}
	syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	l.f.Close()
	l.ok = false
	mu.Lock()
	locked = false
	mu.Unlock()
}
