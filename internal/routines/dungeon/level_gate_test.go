package dungeon

import "testing"

func TestMissionRequiredLevel(t *testing.T) {
	cases := []struct {
		lock int
		want int
	}{
		{0, 0},
		{20080, 20},
		{21010, 21},
		{1020, 1},
	}
	for _, c := range cases {
		if got := missionRequiredLevel(c.lock); got != c.want {
			t.Errorf("missionRequiredLevel(%d) = %d, want %d", c.lock, got, c.want)
		}
	}
}

func TestMissionLevelLocked(t *testing.T) {
	cases := []struct {
		lock   int
		level  int
		locked bool
	}{
		{20080, 20, false}, // 20 级章节可在 20 级挑战
		{20080, 19, true},
		{21010, 20, true}, // 21 级章节需要 21 级
		{21010, 21, false},
		{0, 1, false},
		{20080, 0, false}, // 等级未知时不拦截
	}
	for _, c := range cases {
		if got := missionLevelLocked(c.lock, c.level); got != c.locked {
			t.Errorf("missionLevelLocked(%d, %d) = %v, want %v", c.lock, c.level, got, c.locked)
		}
	}
}