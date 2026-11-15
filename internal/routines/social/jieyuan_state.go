// 本文件持久化结缘宝箱「暂不可开启」的箱型，避免对需要元宝/条件或已到
// 开启上限的箱型反复发送必被静默丢弃的 232/34。与 st_union_ticket_state.json
// 同样跨进程持久化；采用有效期（默认 6 小时）而非整日，便于游戏侧 05:00
// 等非 00:00 重置后自动重试。
package social

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

const (
	jieyuanSkipStatePath = "data/runtime/jieyuan_skip_state.json"
	// jieyuanSkipTTL 单个箱型标记的有效期。
	jieyuanSkipTTL = 6 * time.Hour
)

var jieyuanSkipMu sync.Mutex

type jieyuanSkipState struct {
	// ExpireAt 箱型 ID -> 过期时间(unix 秒)。
	ExpireAt map[string]int64 `json:"expire_at"`
}

func loadJieyuanSkipLocked() jieyuanSkipState {
	st := jieyuanSkipState{}
	b, err := os.ReadFile(jieyuanSkipStatePath)
	if err != nil {
		return st
	}
	_ = json.Unmarshal(b, &st)
	if st.ExpireAt == nil {
		st.ExpireAt = map[string]int64{}
	}
	return st
}

// jiYuanBoxSkippedToday 判断箱型是否仍在跳过有效期内。
func jiYuanBoxSkippedToday(itemID int32) bool {
	jieyuanSkipMu.Lock()
	defer jieyuanSkipMu.Unlock()
	st := loadJieyuanSkipLocked()
	exp, ok := st.ExpireAt[strconv.FormatInt(int64(itemID), 10)]
	return ok && time.Now().Unix() < exp
}

// markJiYuanBoxSkipped 记录箱型在 TTL 内不可开启（跨进程持久化）。
func markJiYuanBoxSkipped(itemID int32) {
	jieyuanSkipMu.Lock()
	defer jieyuanSkipMu.Unlock()
	st := loadJieyuanSkipLocked()
	if st.ExpireAt == nil {
		st.ExpireAt = map[string]int64{}
	}
	now := time.Now()
	// 顺带清理已过期项，避免文件无限增长。
	for k, exp := range st.ExpireAt {
		if now.Unix() >= exp {
			delete(st.ExpireAt, k)
		}
	}
	st.ExpireAt[strconv.FormatInt(int64(itemID), 10)] = now.Add(jieyuanSkipTTL).Unix()
	_ = os.MkdirAll(filepath.Dir(jieyuanSkipStatePath), 0o755)
	b, _ := json.Marshal(st)
	_ = os.WriteFile(jieyuanSkipStatePath, b, 0o644)
}
