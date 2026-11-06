package protocol_test

import (
	"encoding/hex"
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestBuildLibraryUseStuntSkillPacketLayout(t *testing.T) {
	pkt, err := protocol.BuildLibraryUseStuntSkillPacket(1234)
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	if pkt.ActionID != protocol.ActionIDLibraryUseStuntSkill {
		t.Fatalf("unexpected action: 0x%08X", pkt.ActionID)
	}
	if got := hex.EncodeToString(pkt.Payload); got != "04d2" {
		t.Fatalf("unexpected payload: %s", got)
	}

	if _, err := protocol.BuildLibraryUseStuntSkillPacket(0); err == nil {
		t.Error("expected zero stunt id to fail")
	}
	if _, err := protocol.BuildLibraryUseStuntSkillPacket(40000); err == nil {
		t.Error("expected out-of-range stunt id to fail")
	}
}

func TestBuildLibraryArtifactStuntInfoPacket(t *testing.T) {
	pkt := protocol.BuildLibraryArtifactStuntInfoPacket()
	if pkt.ActionID != 0x00550005 {
		t.Fatalf("unexpected artifact stunt action: 0x%08X", pkt.ActionID)
	}
}

func TestParseLibraryMasterPracticeStuntInfoResult(t *testing.T) {
	infos := []protocol.LibraryStuntInfo{
		{RoleJobID: 1, RoleStuntID: 1234, NextRoleStuntID: 1235, IsActive: 1},
		{RoleJobID: 15, RoleStuntID: 0, NextRoleStuntID: 5678, IsActive: 1, IsUse: 1},
	}
	got, err := protocol.ParseLibraryMasterPracticeStuntInfoResult(
		protocol.BuildLibraryMasterPracticeStuntInfoResultPacket(infos).Payload)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(got))
	}
	if got[0].RoleJobID != 1 || got[0].RoleStuntID != 1234 || got[0].NextRoleStuntID != 1235 || got[0].IsActive != 1 {
		t.Errorf("unexpected first entry: %+v", got[0])
	}
	if got[1].RoleJobID != 15 || got[1].RoleStuntID != 0 || got[1].NextRoleStuntID != 5678 || got[1].IsUse != 1 {
		t.Errorf("unexpected second entry: %+v", got[1])
	}
}

func TestParseLibraryUseStuntSkillResult(t *testing.T) {
	result, roleID, stuntID, err := protocol.ParseLibraryUseStuntSkillResult(
		protocol.BuildLibraryUseStuntSkillResultPacket(protocol.LibraryResultSuccess, 600, 1234).Payload)
	if err != nil {
		t.Fatalf("parse success failed: %v", err)
	}
	if result != protocol.LibraryResultSuccess || roleID != 600 || stuntID != 1234 {
		t.Fatalf("unexpected success result: %d %d %d", result, roleID, stuntID)
	}

	result, _, _, err = protocol.ParseLibraryUseStuntSkillResult(
		protocol.BuildLibraryUseStuntSkillResultPacket(protocol.LibraryResultNoHave, 0, 0).Payload)
	if err != nil {
		t.Fatalf("parse failure code failed: %v", err)
	}
	if result != protocol.LibraryResultNoHave {
		t.Fatalf("expected NO_HAVE, got %d", result)
	}
}
