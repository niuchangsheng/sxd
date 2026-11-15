// 本文件持久化结缘宝箱「当天无法开启」的箱型，避免对需要元宝/条件的箱型
// 反复发送必被静默丢弃的 232/34。与 st_union_ticket_state.json 同样跨进程。
package social

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const jieyuanSkipStatePath = "data/runtime/jieyuan_skip_state.json"

var jieyuanSkipMu sync.Mutex

type jieyuanSkipState struct {
	Date  string  `json:"date"`
	Items []int32 `json:"items"`
}

func loadJieyuanSkipLocked() jieyuanSkipState {
	var st jieyuanSkipState
	b, err := os.ReadFile(jieyuanSkipStatePath)
	if err != nil {
		return st
	}
	_ = json.Unmarshal(b, &st)
	if st.Date != time.Now().Format("2006-01-02") {
		return jieyuanSkipState{}
	}
	return st
}

// jiYuanBoxSkippedToday 判断箱型今天是否已被标记为不可开启。
func jiYuanBoxSkippedToday(itemID int32) bool {
	jieyuanSkipMu.Lock()
	defer jieyuanSkipMu.Unlock()
	st := loadJieyuanSkipLocked()
	for _, id := range st.Items {
		if id == itemID {
			return true
		}
	}
	return false
}

// markJiYuanBoxSkipped 记录箱型今天不可开启（跨进程持久化）。
func markJiYuanBoxSkipped(itemID int32) {
	jieyuanSkipMu.Lock()
	defer jieyuanSkipMu.Unlock()
	st := loadJieyuanSkipLocked()
	st.Date = time.Now().Format("2006-01-02")
	for _, id := range st.Items {
		if id == itemID {
			return
		}
	}
	st.Items = append(st.Items, itemID)
	_ = os.MkdirAll(filepath.Dir(jieyuanSkipStatePath), 0o755)
	b, _ := json.Marshal(st)
	_ = os.WriteFile(jieyuanSkipStatePath, b, 0o644)
}
