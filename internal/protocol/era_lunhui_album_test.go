package protocol_test

import (
	"encoding/hex"
	"testing"

	"sxd-pie-ng/internal/protocol"
)

// /tmp/lunhuitulu.pcapng 882/1 [tab=5] 任务列表。
const eraLunHuiTab5Hex = "00090000026600000002000000000000026300000002000000010000026a0000000a00000000000002680000000a00000000000002690000000a000000000000026500000002000000000000026b0000000a00000000000002640000000200000000000002670000000a0000000000000005"

func TestParseEraLunHuiTasks(t *testing.T) {
	raw, err := hex.DecodeString(eraLunHuiTab5Hex)
	if err != nil {
		t.Fatalf("hex: %v", err)
	}
	tasks, err := protocol.ParseEraLunHuiTasks(raw)
	if err != nil {
		t.Fatalf("ParseEraLunHuiTasks: %v", err)
	}
	if len(tasks) != 9 {
		t.Fatalf("任务数 = %d, want 9", len(tasks))
	}
	if tasks[0].ID != 614 || tasks[0].State != 2 || tasks[0].Claimed != 0 {
		t.Fatalf("任务[0] 不符: %+v", tasks[0])
	}
	if tasks[1].ID != 611 || tasks[1].Claimed != 1 {
		t.Fatalf("任务[1] 不符: %+v", tasks[1])
	}
}
