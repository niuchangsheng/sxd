package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Writer 线程安全的日志文件写入器，按大小或时间周期自动轮转。
//
// 活动日志始终写入固定的 cfg.FileName；每次轮转时将其重命名为带时间戳的
// 历史文件，随后重新创建活动文件并清理超限的历史文件。
type Writer struct {
	cfg        Config
	path       string
	file       *os.File
	size       int64
	nextRotate time.Time
	now        func() time.Time
	mu         sync.Mutex
}

// newWriter 创建并打开日志文件写入器。
func newWriter(cfg Config) (*Writer, error) {
	cfg = cfg.normalize()
	if err := os.MkdirAll(cfg.Dir, 0o755); err != nil {
		return nil, fmt.Errorf("logging: 创建日志目录失败: %w", err)
	}

	w := &Writer{
		cfg:  cfg,
		path: filepath.Join(cfg.Dir, cfg.FileName),
		now:  time.Now,
	}
	if err := w.openLocked(); err != nil {
		return nil, err
	}
	return w, nil
}

// Write 写入日志内容，必要时先执行轮转。
func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file == nil {
		if err := w.openLocked(); err != nil {
			return 0, err
		}
	}
	if w.shouldRotateLocked(len(p)) {
		if err := w.rotateLocked(); err != nil {
			return 0, err
		}
	}

	n, err := w.file.Write(p)
	w.size += int64(n)
	return n, err
}

// Close 刷新并关闭活动日志文件，可重复调用。
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file == nil {
		return nil
	}
	_ = w.file.Sync()
	err := w.file.Close()
	w.file = nil
	return err
}

// openLocked 以追加模式打开活动日志文件，并从文件状态恢复大小与下次轮转时间。
func (w *Writer) openLocked() error {
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("logging: 打开日志文件失败: %w", err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return fmt.Errorf("logging: 读取日志文件状态失败: %w", err)
	}

	w.file = f
	w.size = info.Size()
	if w.cfg.RotateInterval > 0 {
		base := w.now()
		if w.size > 0 {
			base = info.ModTime()
		}
		w.nextRotate = base.Add(w.cfg.RotateInterval)
	} else {
		w.nextRotate = time.Time{}
	}
	return nil
}

// shouldRotateLocked 判断本次写入是否会触发大小或时间轮转。
func (w *Writer) shouldRotateLocked(incoming int) bool {
	// 空文件不轮转，避免产生无意义的历史文件。
	if w.size <= 0 {
		return false
	}
	if w.cfg.MaxSizeBytes > 0 && w.size+int64(incoming) > w.cfg.MaxSizeBytes {
		return true
	}
	if w.cfg.RotateInterval > 0 && !w.now().Before(w.nextRotate) {
		return true
	}
	return false
}

// rotateLocked 关闭活动文件、重命名为历史文件、清理过期文件并重新打开活动文件。
func (w *Writer) rotateLocked() error {
	now := w.now()
	if err := w.closeFileLocked(); err != nil {
		return fmt.Errorf("logging: 关闭日志文件失败: %w", err)
	}

	backup := w.uniqueBackupPathLocked(now)
	if err := os.Rename(w.path, backup); err != nil {
		if reopenErr := w.openLocked(); reopenErr != nil {
			return fmt.Errorf("logging: 轮转日志失败: %w (重新打开失败: %v)", err, reopenErr)
		}
		return fmt.Errorf("logging: 轮转日志失败: %w", err)
	}

	w.pruneLocked(now)
	return w.openLocked()
}

// closeFileLocked 刷新并关闭当前文件句柄。
func (w *Writer) closeFileLocked() error {
	if w.file == nil {
		return nil
	}
	_ = w.file.Sync()
	err := w.file.Close()
	w.file = nil
	return err
}

// uniqueBackupPathLocked 生成不与现有文件冲突的历史日志路径。
func (w *Writer) uniqueBackupPathLocked(now time.Time) string {
	ext := filepath.Ext(w.cfg.FileName)
	base := strings.TrimSuffix(w.cfg.FileName, ext)
	stamp := now.Format("20060102-150405")

	path := filepath.Join(w.cfg.Dir, base+"-"+stamp+ext)
	for i := 1; ; i++ {
		if _, err := os.Stat(path); err != nil {
			return path
		}
		path = filepath.Join(w.cfg.Dir, fmt.Sprintf("%s-%s-%d%s", base, stamp, i, ext))
	}
}

// pruneLocked 按保留天数与保留数量删除过期的历史日志文件。
func (w *Writer) pruneLocked(now time.Time) {
	ext := filepath.Ext(w.cfg.FileName)
	base := strings.TrimSuffix(w.cfg.FileName, ext)
	// 仅匹配轮转生成的时间戳文件，避免误删同目录下的其他日志。
	const stampPattern = "[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]-[0-9][0-9][0-9][0-9][0-9][0-9]"
	matches, err := filepath.Glob(filepath.Join(w.cfg.Dir, base+"-"+stampPattern+"*"+ext))
	if err != nil {
		return
	}

	type candidate struct {
		path string
		mod  time.Time
	}
	candidates := make([]candidate, 0, len(matches))
	for _, path := range matches {
		info, statErr := os.Stat(path)
		if statErr != nil || info.IsDir() {
			continue
		}
		candidates = append(candidates, candidate{path: path, mod: info.ModTime()})
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].mod.Equal(candidates[j].mod) {
			return candidates[i].path < candidates[j].path
		}
		return candidates[i].mod.Before(candidates[j].mod)
	})

	kept := candidates[:0]
	for _, c := range candidates {
		if w.cfg.MaxAge > 0 && now.Sub(c.mod) > w.cfg.MaxAge {
			_ = os.Remove(c.path)
			continue
		}
		kept = append(kept, c)
	}

	if w.cfg.MaxBackups > 0 && len(kept) > w.cfg.MaxBackups {
		for _, c := range kept[:len(kept)-w.cfg.MaxBackups] {
			_ = os.Remove(c.path)
		}
	}
}
