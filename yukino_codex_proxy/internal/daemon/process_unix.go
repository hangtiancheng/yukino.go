//go:build darwin || linux || freebsd || openbsd || netbsd || dragonfly

package daemon

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

func detach(cmd *exec.Cmd)          { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }
func processAlive(pid int) bool     { return pid > 0 && syscall.Kill(pid, 0) != syscall.ESRCH }
func interrupt(process *os.Process) { _ = process.Signal(os.Interrupt) }

func (m *Manager) lock(ctx context.Context) (func(), error) {
	if err := os.MkdirAll(m.Options.StateDir, 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(m.Options.StateDir, "manager.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			f.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}
