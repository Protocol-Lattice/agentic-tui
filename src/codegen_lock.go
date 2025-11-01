package src

import (
	"context"
	"fmt"
	"path/filepath"
	"time"
)

func (m *model) withCodegenLock(fn func()) {
	if fn == nil {
		return
	}
	m.codegenLocal.Lock()
	defer m.codegenLocal.Unlock()

	if m.lockDir == "" {
		fn()
		return
	}

	ctx := m.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	lockPath := filepath.Join(m.lockDir, "codegen")
	warned := false
	release, err := acquireDirLock(ctx, lockPath, func(wait time.Duration) {
		if warned || wait < 500*time.Millisecond {
			return
		}
		warned = true
		m.mu.Lock()
		m.output += m.style.subtle.Render("⏳ Waiting for shared code generation lock...\n")
		m.mu.Unlock()
		m.renderOutput(true)
	})
	if err != nil {
		m.mu.Lock()
		m.output += m.style.error.Render(fmt.Sprintf("⚠️ code generation lock: %v\n", err))
		m.mu.Unlock()
		m.renderOutput(true)
		fn()
		return
	}
	if warned {
		m.mu.Lock()
		m.output += m.style.success.Render("🔓 Shared code generation lock acquired.\n")
		m.mu.Unlock()
		m.renderOutput(true)
	}
	defer func() {
		if err := release(); err != nil {
			m.mu.Lock()
			m.output += m.style.subtle.Render(fmt.Sprintf("⚠️ release shared lock: %v\n", err))
			m.mu.Unlock()
			m.renderOutput(true)
		}
	}()

	fn()
}
