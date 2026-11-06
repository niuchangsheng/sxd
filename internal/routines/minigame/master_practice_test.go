package minigame

import (
	"testing"

	"sxd-pie-ng/internal/dictionary"
	"sxd-pie-ng/internal/protocol"
)

func TestMasterPracticeDirectionCandidates_PrefersMainRoleJob(t *testing.T) {
	repo, err := dictionary.NewRepository("../../../data/Pieb.db")
	if err != nil {
		t.Skipf("Pieb.db unavailable: %v", err)
	}
	defer repo.Close()

	panel := &protocol.MasterPracticePanel{Jobs: []protocol.MasterPracticeJobInfo{
		{JobID: 1}, {JobID: 2}, {JobID: 3}, {JobID: 5}, {JobID: 6}, {JobID: 15, TotalStars: 1},
	}}
	// 主角 role 511 = 飞羽男天师 (JobId 15)，伙伴 role 606 = 魔林月如 (JobId 2)。
	deploy := &protocol.MasterPracticeDeployInfo{
		Roles: []protocol.MasterPracticeDeployRole{
			{PlayerRoleID: 227943, IsMainRole: 1, RoleID: 511, IsDeploy: 1},
			{PlayerRoleID: 1035439, RoleID: 606, IsDeploy: 1},
		},
		Grids: []protocol.MasterPracticeDeployGrid{
			{GridTypeID: 1, PlayerRoleID: 227943},
			{GridTypeID: 9, PlayerRoleID: 1035439},
		},
	}
	got := masterPracticeDirectionCandidates(panel, deploy, repo)
	if len(got) < 2 || got[0] != 15 || got[1] != 1 {
		t.Fatalf("expected main job 15 then partner job 1, got %v", got)
	}
}

func TestMasterPracticeDirectionCandidates_FallsBackToActivePanelJob(t *testing.T) {
	panel := &protocol.MasterPracticePanel{Jobs: []protocol.MasterPracticeJobInfo{
		{JobID: 1}, {JobID: 2}, {JobID: 15, TotalStars: 3},
	}}
	got := masterPracticeDirectionCandidates(panel, nil, nil)
	if len(got) == 0 || got[0] != 15 {
		t.Fatalf("expected active job 15 first, got %v", got)
	}
}
