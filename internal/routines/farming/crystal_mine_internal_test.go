package farming

import (
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestPickStMineMountain(t *testing.T) {
	// 优先选择有空位的最高等级矿脉。
	if got := pickStMineMountain(&protocol.StMineInfo{
		Level: 3,
		Mountains: []protocol.StMineMountain{
			{Level: 1, UsedCount: 1, TotalCount: 2},
			{Level: 4, UsedCount: 2, TotalCount: 2},
			{Level: 2, UsedCount: 0, TotalCount: 2},
		},
	}); got != 2 {
		t.Fatalf("应选择有空位的最高等级矿脉 2, got %d", got)
	}
	// 全部满员时回退到与角色矿山等级一致的矿脉。
	if got := pickStMineMountain(&protocol.StMineInfo{
		Level: 3,
		Mountains: []protocol.StMineMountain{
			{Level: 1, UsedCount: 2, TotalCount: 2},
			{Level: 3, UsedCount: 2, TotalCount: 2},
		},
	}); got != 3 {
		t.Fatalf("满员时应回退到等级 3, got %d", got)
	}
	// 无列表时返回 1。
	if got := pickStMineMountain(&protocol.StMineInfo{}); got != 1 {
		t.Fatalf("空列表应返回 1, got %d", got)
	}
}
