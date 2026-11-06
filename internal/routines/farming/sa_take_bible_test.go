package farming

import "testing"

func TestNormalizeSaTakeBibleMsg(t *testing.T) {
	cases := map[uint8]uint8{
		11: 9,  // SUCCESS
		13: 11, // FULL_PROTECT_TIMES
		15: 13, // TAKE_BIBLE_STATUS
		18: 16, // NOT_ENOUGH_INGOT
		20: 18, // FULL_TAKE_BIBLE_TIMES
		22: 20, // PROTECT_TIME
		23: 21, // FULL_ROB_TIMES
		24: 22, // HAVE_CD_TIME
		40: 38, // FAILED
	}
	for sa, want := range cases {
		if got := normalizeSaTakeBibleMsg(sa); got != want {
			t.Fatalf("sa msg %d: got %d want %d", sa, got, want)
		}
	}
	// 未映射的码保持原值，避免误伤未知业务语义。
	if got := normalizeSaTakeBibleMsg(99); got != 99 {
		t.Fatalf("unexpected passthrough: %d", got)
	}
}
