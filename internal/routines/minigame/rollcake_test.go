// Package minigame 提供吉星高照、仙履奇缘答题、刮刮卡、钓鱼等益智趣味活动。
//
// @author Ateng
// @since 2026-11-02
package minigame

import "testing"

func TestShouldRerollRollCake(t *testing.T) {
	cases := []struct {
		name       string
		dice       []int8
		isLastRoll bool
		want       bool
	}{
		{"两吉非最后", []int8{4, 4, 1, 2, 3, 5}, false, true},
		{"三吉非最后", []int8{1, 2, 3, 4, 4, 4}, false, true},
		{"一吉非最后", []int8{4, 1, 2, 3, 5, 6}, false, false},
		{"零吉非最后", []int8{1, 2, 3, 5, 6, 1}, false, false},
		{"六吉非最后", []int8{4, 4, 4, 4, 4, 4}, false, true},
		{"一吉但今日最后一次", []int8{4, 1, 2, 3, 5, 6}, true, true},
		{"零吉但今日最后一次", []int8{1, 2, 3, 5, 6, 1}, true, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldRerollRollCake(tc.dice, tc.isLastRoll); got != tc.want {
				t.Errorf("shouldRerollRollCake(%v, last=%v) = %v, want %v", tc.dice, tc.isLastRoll, got, tc.want)
			}
		})
	}
}

func TestCountRollCakeJi(t *testing.T) {
	cases := []struct {
		dice []int8
		want int
	}{
		{[]int8{4, 4, 4, 1, 2, 3}, 3},
		{[]int8{1, 2, 3, 5, 6, 1}, 0},
		{[]int8{4, 4, 4, 4, 4, 4}, 6},
		{nil, 0},
	}

	for _, tc := range cases {
		if got := countRollCakeJi(tc.dice); got != tc.want {
			t.Errorf("countRollCakeJi(%v) = %d, want %d", tc.dice, got, tc.want)
		}
	}
}

func TestAllRollCakeJi(t *testing.T) {
	if !allRollCakeJi([]int8{4, 4, 4, 4, 4, 4}) {
		t.Error("六吉应判定为全吉")
	}
	if allRollCakeJi([]int8{4, 4, 4, 4, 4, 3}) {
		t.Error("五吉一非吉不应判定为全吉")
	}
	if allRollCakeJi(nil) {
		t.Error("空骰子不应判定为全吉")
	}
}
