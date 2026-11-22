package protocol_test

import (
	"encoding/binary"
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestPacket_EffectivePrevActMode(t *testing.T) {
	var nilPkt *protocol.Packet
	if got := nilPkt.EffectivePrevActMode(); got != protocol.PrevActNone {
		t.Fatalf("nil packet mode = %v, want PrevActNone", got)
	}

	cases := []struct {
		name string
		pkt  *protocol.Packet
		want protocol.PrevActMode
	}{
		{
			name: "plain packet",
			pkt:  protocol.NewPacket(0x00010000, nil),
			want: protocol.PrevActNone,
		},
		{
			name: "legacy HasPrevAct",
			pkt:  &protocol.Packet{ActionID: 0x00010000, HasPrevAct: true},
			want: protocol.PrevActAutoBusiness,
		},
		{
			name: "legacy PrevActFromLastSent",
			pkt:  &protocol.Packet{ActionID: 0x00010000, PrevActFromLastSent: true},
			want: protocol.PrevActAutoBusiness,
		},
		{
			name: "explicit overrides legacy flags",
			pkt: &protocol.Packet{
				ActionID:            0x00040000,
				HasPrevAct:          true,
				PrevActFromLastSent: true,
				PrevActMode:         protocol.PrevActExplicit,
			},
			want: protocol.PrevActExplicit,
		},
		{
			name: "NewPacketWithAutoPrevAct",
			pkt:  protocol.NewPacketWithAutoPrevAct(0x03530005, []byte{1, 2}),
			want: protocol.PrevActAutoBusiness,
		},
		{
			name: "NewPacketWithExplicitPrevAct",
			pkt:  protocol.NewPacketWithExplicitPrevAct(0x03530005, []byte{1, 2}, 0x00020029),
			want: protocol.PrevActExplicit,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.pkt.EffectivePrevActMode(); got != tc.want {
				t.Fatalf("EffectivePrevActMode() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestNewPacketWithExplicitPrevAct_PayloadLayout(t *testing.T) {
	pkt := protocol.NewPacketWithExplicitPrevAct(0x03530005, []byte{0xAA, 0xBB}, 0x00020029)
	if len(pkt.Payload) != 6 {
		t.Fatalf("len(Payload) = %d, want 6", len(pkt.Payload))
	}
	if pkt.Payload[0] != 0xAA || pkt.Payload[1] != 0xBB {
		t.Fatalf("args mismatch: %X", pkt.Payload[:2])
	}
	if got := binary.BigEndian.Uint32(pkt.Payload[2:]); got != 0x00020029 {
		t.Fatalf("prevAct = 0x%08X, want 0x00020029", got)
	}
}
