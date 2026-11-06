package protocol

// 天帝宝库自选大奖官方配置 (对齐 com.assist.server.source.TreasureHouseTypeData.objHouseAward)。
//
// 服务端 select_panel 只下发档位 quality 与候选奖励 id，不携带道具质量/数量等明细；
// 奖励 id → 道具/数量/稀有度必须查此官方表。Pieb.db.wishpoolaward 为老版本数据
// (仅 35 条且 id 映射与现行客户端不一致)，不能用于奖励解析。
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

// treasureAwards 官方奖励条目表 (49 条，id 1~51，缺 13/16)。
var treasureAwards = map[int32]TreasureAward{
	1:  {ID: 1, ItemID: 4611, ItemNum: 1, Quality: 1, Sort: 2, AwardType: 1, FuncID: 149},
	2:  {ID: 2, ItemID: 4239, ItemNum: 1, Quality: 1, Sort: 3, AwardType: 1, FuncID: 113},
	3:  {ID: 3, ItemID: 5852, ItemNum: 1, Quality: 1, Sort: 4, AwardType: 1, FuncID: 190},
	4:  {ID: 4, ItemID: 6651, ItemNum: 1, Quality: 1, Sort: 5, AwardType: 1, FuncID: 3},
	5:  {ID: 5, ItemID: 6759, ItemNum: 2, Quality: 1, Sort: 6, AwardType: 1, FuncID: 96},
	6:  {ID: 6, ItemID: 7697, ItemNum: 2, Quality: 1, Sort: 7, AwardType: 1, FuncID: 221},
	7:  {ID: 7, ItemID: 7089, ItemNum: 2, Quality: 1, Sort: 8, AwardType: 1, FuncID: 204},
	8:  {ID: 8, ItemID: 6910, ItemNum: 3, Quality: 1, Sort: 1, AwardType: 2, FuncID: 209},
	9:  {ID: 9, ItemID: 2010, ItemNum: 1, Quality: 1, Sort: 9, AwardType: 1, FuncID: 96},
	10: {ID: 10, ItemID: 5183, ItemNum: 40, Quality: 1, Sort: 10, AwardType: 1, FuncID: 192},
	11: {ID: 11, ItemID: 3055, ItemNum: 1, Quality: 1, Sort: 11, AwardType: 1, FuncID: 130},
	12: {ID: 12, ItemID: 4954, ItemNum: 1, Quality: 1, Sort: 12, AwardType: 1, FuncID: 92},
	14: {ID: 14, ItemID: 6499, ItemNum: 2, Quality: 1, Sort: 13, AwardType: 1, FuncID: 205},
	15: {ID: 15, ItemID: 5117, ItemNum: 40, Quality: 1, Sort: 14, AwardType: 1, FuncID: 96},
	17: {ID: 17, ItemID: 5323, ItemNum: 2, Quality: 2, Sort: 3, AwardType: 1, FuncID: 180},
	18: {ID: 18, ItemID: 2407, ItemNum: 3, Quality: 2, Sort: 4, AwardType: 1, FuncID: 60},
	19: {ID: 19, ItemID: 1787, ItemNum: 2, Quality: 2, Sort: 5, AwardType: 1, FuncID: 148},
	20: {ID: 20, ItemID: 2343, ItemNum: 50, Quality: 2, Sort: 6, AwardType: 1, FuncID: 109},
	21: {ID: 21, ItemID: 347, ItemNum: 250, Quality: 2, Sort: 7, AwardType: 1, FuncID: 0},
	22: {ID: 22, ItemID: 3054, ItemNum: 1, Quality: 2, Sort: 8, AwardType: 1, FuncID: 130},
	23: {ID: 23, ItemID: 6909, ItemNum: 2, Quality: 2, Sort: 9, AwardType: 1, FuncID: 209},
	24: {ID: 24, ItemID: 5398, ItemNum: 2, Quality: 2, Sort: 10, AwardType: 1, FuncID: 184},
	25: {ID: 25, ItemID: 6910, ItemNum: 1, Quality: 2, Sort: 1, AwardType: 2, FuncID: 209},
	26: {ID: 26, ItemID: 2397, ItemNum: 15, Quality: 2, Sort: 11, AwardType: 1, FuncID: 196},
	27: {ID: 27, ItemID: 5822, ItemNum: 3, Quality: 2, Sort: 12, AwardType: 1, FuncID: 190},
	28: {ID: 28, ItemID: 4760, ItemNum: 3, Quality: 2, Sort: 13, AwardType: 1, FuncID: 164},
	29: {ID: 29, ItemID: 3964, ItemNum: 1, Quality: 2, Sort: 14, AwardType: 1, FuncID: 140},
	30: {ID: 30, ItemID: 7298, ItemNum: 5, Quality: 2, Sort: 2, AwardType: 1, FuncID: 82},
	31: {ID: 31, ItemID: 2395, ItemNum: 16, Quality: 3, Sort: 4, AwardType: 1, FuncID: 104},
	32: {ID: 32, ItemID: 3783, ItemNum: 8, Quality: 3, Sort: 5, AwardType: 1, FuncID: 139},
	33: {ID: 33, ItemID: 2471, ItemNum: 15, Quality: 3, Sort: 6, AwardType: 1, FuncID: 48},
	34: {ID: 34, ItemID: 5397, ItemNum: 4, Quality: 3, Sort: 7, AwardType: 1, FuncID: 184},
	35: {ID: 35, ItemID: 1743, ItemNum: 200, Quality: 3, Sort: 8, AwardType: 1, FuncID: 78},
	36: {ID: 36, ItemID: 3706, ItemNum: 150, Quality: 3, Sort: 9, AwardType: 1, FuncID: 78},
	37: {ID: 37, ItemID: 1739, ItemNum: 1000, Quality: 3, Sort: 10, AwardType: 1, FuncID: 0},
	38: {ID: 38, ItemID: 1411, ItemNum: 4, Quality: 3, Sort: 1, AwardType: 1, FuncID: 2},
	39: {ID: 39, ItemID: 4519, ItemNum: 150, Quality: 3, Sort: 11, AwardType: 1, FuncID: 159},
	40: {ID: 40, ItemID: 2403, ItemNum: 400, Quality: 3, Sort: 12, AwardType: 1, FuncID: 91},
	41: {ID: 41, ItemID: 1747, ItemNum: 18000000, Quality: 3, Sort: 13, AwardType: 1, FuncID: 0},
	42: {ID: 42, ItemID: 1740, ItemNum: 20, Quality: 3, Sort: 2, AwardType: 1, FuncID: 0},
	43: {ID: 43, ItemID: 4975, ItemNum: 6, Quality: 3, Sort: 3, AwardType: 1, FuncID: 164},
	44: {ID: 44, ItemID: 4099, ItemNum: 5, Quality: 3, Sort: 14, AwardType: 1, FuncID: 146},
	45: {ID: 45, ItemID: 2452, ItemNum: 40, Quality: 3, Sort: 15, AwardType: 1, FuncID: 92},
	46: {ID: 46, ItemID: 7074, ItemNum: 3, Quality: 1, Sort: 1, AwardType: 2, FuncID: 209},
	47: {ID: 47, ItemID: 7074, ItemNum: 1, Quality: 2, Sort: 1, AwardType: 2, FuncID: 209},
	48: {ID: 48, ItemID: 7640, ItemNum: 3, Quality: 1, Sort: 1, AwardType: 2, FuncID: 209},
	49: {ID: 49, ItemID: 7640, ItemNum: 1, Quality: 2, Sort: 1, AwardType: 2, FuncID: 209},
	50: {ID: 50, ItemID: 9085, ItemNum: 2, Quality: 1, Sort: 15, AwardType: 1, FuncID: 256},
	51: {ID: 51, ItemID: 9212, ItemNum: 3, Quality: 2, Sort: 15, AwardType: 1, FuncID: 256},
}

// TreasureAwardByID 查询官方奖励条目；未知 id 返回 (零值, false)。
func TreasureAwardByID(id int32) (TreasureAward, bool) {
	a, ok := treasureAwards[id]
	return a, ok
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
