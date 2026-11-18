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
	"strings"
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

// jieyuanSkipKey 生成「角色 + 箱型」维度的跳过键。
//
// 历史版本只按箱型 ID 记录，导致 s0 的不可开启标记会误伤 s1（同一箱型
// 在另一账号上其实可以开箱，2026-11-18 实测 s1 被 s0 的 3937 标记挡住）。
func jieyuanSkipKey(roleID string, itemID int32) string {
	return roleID + ":" + strconv.FormatInt(int64(itemID), 10)
}

// jiYuanBoxSkippedToday 判断该角色的箱型是否仍在跳过有效期内。
func jiYuanBoxSkippedToday(roleID string, itemID int32) bool {
	jieyuanSkipMu.Lock()
	defer jieyuanSkipMu.Unlock()
	st := loadJieyuanSkipLocked()
	exp, ok := st.ExpireAt[jieyuanSkipKey(roleID, itemID)]
	return ok && time.Now().Unix() < exp
}

// jiYuanBoxSkipExpiry 返回该角色箱型的跳过到期时间；未标记或已过期返回 false。
func jiYuanBoxSkipExpiry(roleID string, itemID int32) (time.Time, bool) {
	jieyuanSkipMu.Lock()
	defer jieyuanSkipMu.Unlock()
	st := loadJieyuanSkipLocked()
	exp, ok := st.ExpireAt[jieyuanSkipKey(roleID, itemID)]
	if !ok {
		return time.Time{}, false
	}
	expiry := time.Unix(exp, 0)
	if !time.Now().Before(expiry) {
		return time.Time{}, false
	}
	return expiry, true
}

// markJiYuanBoxSkipped 记录该角色的箱型在 TTL 内不可开启（跨进程持久化）。
func markJiYuanBoxSkipped(roleID string, itemID int32) {
	jieyuanSkipMu.Lock()
	defer jieyuanSkipMu.Unlock()
	st := loadJieyuanSkipLocked()
	if st.ExpireAt == nil {
		st.ExpireAt = map[string]int64{}
	}
	now := time.Now()
	// 顺带清理已过期项，避免文件无限增长；旧版无角色前缀的键一并清掉。
	for k, exp := range st.ExpireAt {
		if now.Unix() >= exp || !strings.Contains(k, ":") {
			delete(st.ExpireAt, k)
		}
	}
	st.ExpireAt[jieyuanSkipKey(roleID, itemID)] = now.Add(jieyuanSkipTTL).Unix()
	_ = os.MkdirAll(filepath.Dir(jieyuanSkipStatePath), 0o755)
	b, _ := json.Marshal(st)
	_ = os.WriteFile(jieyuanSkipStatePath, b, 0o644)
}
