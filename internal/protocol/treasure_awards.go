package protocol

import "sxd-pie-ng/internal/dictionary"

// 天帝宝库自选大奖官方配置 (来源 data/gamedata.db :: wishpoolaward，
// 由 scripts/rebuild_dict.py 从官方 TreasureHouseTypeData.objHouseAward 提取)。
//
// 服务端 select_panel 只下发档位 quality 与候选奖励 id，不携带道具质量/数量等明细；
// 奖励 id → 道具/数量/稀有度统一从静态数据字典层查询。
type TreasureAward struct {
	// ID 奖励条目编号 (select_panel 下发与 set_award 使用的 award_id)
	ID int32
	// ItemID 实际发放的道具编号
	ItemID int32
	// ItemNum 发放数量
	ItemNum int32
	// Quality 档位品质: 1=珍品, 2=上品, 3=凡品
	Quality int32
	// Sort 官方展示排序 (越小越靠前)
	Sort int32
	// AwardType 奖励类型 (1=普通, 2=特殊)
	AwardType int32
	// FuncID 关联功能编号 (FunctionTypeData.key)
	FuncID int32
}

// TreasureAwardByID 从统一静态数据字典查询官方奖励条目；未知 id 或字典未加载返回 (零值, false)。
func TreasureAwardByID(id int32) (TreasureAward, bool) {
	repo := dictionary.Default()
	if repo == nil {
		return TreasureAward{}, false
	}
	a, err := repo.GetWishpoolAward(int(id))
	if err != nil || a == nil {
		return TreasureAward{}, false
	}
	return TreasureAward{
		ID:        int32(a.ID),
		ItemID:    int32(a.ItemID),
		ItemNum:   int32(a.ItemNum),
		Quality:   int32(a.Quality),
		Sort:      int32(a.Sort),
		AwardType: int32(a.AwardType),
		FuncID:    int32(a.FuncID),
	}, true
}

// TreasureAwardQualityName 返回档位品质中文名。
func TreasureAwardQualityName(quality int32) string {
	switch quality {
	case 1:
		return "珍品"
	case 2:
		return "上品"
	case 3:
		return "凡品"
	default:
		return "未知"
	}
}
