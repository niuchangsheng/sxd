// Package dictionary 提供基于官方 SWF 构建的 data/gamedata.db 静态数据字典只读仓储。
//
// @author Ateng
// @since 2026-10-08
package dictionary

// Item 代表神仙道游戏内的物品或道具元数据。
type Item struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	TypeID       int    `json:"type_id"`
	Description  string `json:"description"`
	Quality      int    `json:"quality"`
	RequireLevel int    `json:"require_level"`
	Price        int    `json:"price"`
	Ingot        int    `json:"ingot"`
}

// NPC 代表城镇或副本中的 NPC 元数据及坐标。
type NPC struct {
	ID     int    `json:"id"`
	TownID int    `json:"town_id"`
	NPCID  int    `json:"npc_id"`
	Name   string `json:"name"`
	CoordX int    `json:"coord_x"`
	CoordY int    `json:"coord_y"`
}

// RoleType 代表游戏内的主角与招募伙伴元数据。
type RoleType struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	JobID   int    `json:"job_id"`
	Type    int    `json:"type"`
	Fame    int    `json:"fame"`
	StuntID int    `json:"stunt_id"`
	Gender  int    `json:"gender"`
}

// Mission 代表游戏内的普通与精英副本关卡元数据。
type Mission struct {
	ID          int    `json:"id"`
	SectionID   int    `json:"section_id"`
	MissionLock int    `json:"mission_lock"`
	Power       int    `json:"power"`
	MapID       int    `json:"map_id"`
	MapKey      int    `json:"map_key"`
	Name        string `json:"name"`
	Type        int    `json:"type"` // 0 普通关卡, 1 精英关卡
	IsBoss      bool   `json:"is_boss"`
	Monsters    string `json:"monsters"`
}

// NPCQuestAnswer 代表 NPC 结交每日问答的最优选项元数据 (来源 NpcFriendShipType.QuestList)。
type NPCQuestAnswer struct {
	ID         int    `json:"id"`
	Question   string `json:"question"`
	BestAnswer uint8  `json:"best_answer"` // 13=A, 14=B, 15=C
	BestAward  int32  `json:"best_award"`
}

// BeelzebubBoss 代表魔王试炼 Boss 元数据 (来源 BeelzeBubTrialsBossType.Bosses)。
type BeelzebubBoss struct {
	ID       int32  `json:"id"`
	Name     string `json:"name"`
	Sign     string `json:"sign"`
	Level    int32  `json:"level"`
	MaxTimes int32  `json:"max_times"`
	Secret   uint8  `json:"secret"` // 0=普通, 1=神秘层, 2=神秘层且显示等级
}

// FaxiangPrayAward 代表主角法相造物佛手祈愿炼化奖励 (来源 MainRoleFaXiangType.extraPrayAwardList)。
type FaxiangPrayAward struct {
	ID          int32 `json:"id"`
	Level       int32 `json:"level"`
	ItemID      int32 `json:"item_id"`
	ItemCount   int32 `json:"item_count"`
	CostDaoYuan int32 `json:"cost_dao_yuan"`
	CostTime    int32 `json:"cost_time"`
	AddExp      int32 `json:"add_exp"`
}
