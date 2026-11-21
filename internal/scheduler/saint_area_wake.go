// 本文件提供圣域（圣域竞技场 / 圣域修炼替身）关闭窗口的唤醒时间计算，
// 供「活动未开启时按开启时间精确唤醒」的统一约定使用。
package scheduler

import (
	"time"

	"sxd-pie-ng/internal/protocol"
)

// SaintAreaReopenTime 根据 0x000001?? 的圣域状态回包计算下次可尝试时间。
//
// 协议下发 CloseTimes 关闭窗口 [start, end]。仅当 now 确实落在某个窗口内时
// 才采用 end+1 分钟；否则回退 1 小时探测，避免错误时间戳导致长时间不唤醒。
func SaintAreaReopenTime(status *protocol.SaintAreaStatus, now time.Time) time.Time {
	if status != nil {
		for _, w := range status.CloseTimes {
			if w.StartTime <= 0 || w.EndTime <= 0 {
				continue
			}
			start := time.Unix(int64(w.StartTime), 0)
			end := time.Unix(int64(w.EndTime), 0)
			if !now.Before(start) && now.Before(end) {
				return end.Add(time.Minute)
			}
		}
	}
	return now.Add(time.Hour)
}
