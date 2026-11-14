package protocol

// 本文件提供神仙卡牌收藏集 (Module 859) 卡包商城的静态配置。
//
// 数据来源: 2026 客户端 Main.swf 中 com.assist.server.source.CultivationCardGameTypeData.objShop。
// shop_info (859/7) 只回传 [shop_id, left_number]；价格、货币与礼包内容均在静态表内。
// 货币: 1747=铜钱, 10442=活动积分, 3345=元宝。自动化只允许购买铜钱条目。

const (
	// CultivationCardCostCoin 铜钱货币道具编号
	CultivationCardCostCoin int32 = 1747
	// CultivationCardCostPoint 活动积分货币道具编号
	CultivationCardCostPoint int32 = 10442
	// CultivationCardCostIngot 元宝货币道具编号
	CultivationCardCostIngot int32 = 3345
)

// CultivationCardShopGood 表示卡包商城静态商品。
type CultivationCardShopGood struct {
	ID          int32
	Type        int32 // 1=周礼包 2=超级礼包
	Limit       int32
	ItemID      int32
	ItemNum     int32
	CostItemID  int32
	CostItemNum int32
	StartTS     int64
}

// CultivationCardShopGoods 卡包商城全部静态商品 (按 id 升序)。
var CultivationCardShopGoods = []CultivationCardShopGood{
	{ID: 1, Type: 1, Limit: 1, ItemID: 10444, ItemNum: 1, CostItemID: 1747, CostItemNum: 1000000, StartTS: 1771257600},
	{ID: 2, Type: 1, Limit: 20, ItemID: 10444, ItemNum: 1, CostItemID: 10442, CostItemNum: 200, StartTS: 1771257600},
	{ID: 3, Type: 1, Limit: 1, ItemID: 10445, ItemNum: 1, CostItemID: 1747, CostItemNum: 5000000, StartTS: 1771257600},
	{ID: 4, Type: 2, Limit: 20, ItemID: 10444, ItemNum: 1, CostItemID: 3345, CostItemNum: 20, StartTS: 1771257600},
	{ID: 5, Type: 2, Limit: 20, ItemID: 10445, ItemNum: 1, CostItemID: 3345, CostItemNum: 100, StartTS: 1771257600},
	{ID: 6, Type: 1, Limit: 3, ItemID: 10445, ItemNum: 1, CostItemID: 10442, CostItemNum: 1000, StartTS: 1771257600},
	{ID: 7, Type: 1, Limit: 1, ItemID: 10446, ItemNum: 1, CostItemID: 1747, CostItemNum: 1000000, StartTS: 1778515200},
	{ID: 8, Type: 1, Limit: 20, ItemID: 10446, ItemNum: 1, CostItemID: 10442, CostItemNum: 200, StartTS: 1778515200},
	{ID: 9, Type: 1, Limit: 1, ItemID: 10447, ItemNum: 1, CostItemID: 1747, CostItemNum: 5000000, StartTS: 1778515200},
	{ID: 10, Type: 2, Limit: 20, ItemID: 10446, ItemNum: 1, CostItemID: 3345, CostItemNum: 20, StartTS: 1778515200},
	{ID: 11, Type: 2, Limit: 20, ItemID: 10447, ItemNum: 1, CostItemID: 3345, CostItemNum: 100, StartTS: 1778515200},
	{ID: 12, Type: 1, Limit: 3, ItemID: 10447, ItemNum: 1, CostItemID: 10442, CostItemNum: 1000, StartTS: 1778515200},
	{ID: 13, Type: 1, Limit: 1, ItemID: 10448, ItemNum: 1, CostItemID: 1747, CostItemNum: 1000000, StartTS: 1785772800},
	{ID: 14, Type: 1, Limit: 20, ItemID: 10448, ItemNum: 1, CostItemID: 10442, CostItemNum: 200, StartTS: 1785772800},
	{ID: 15, Type: 1, Limit: 1, ItemID: 10449, ItemNum: 1, CostItemID: 1747, CostItemNum: 5000000, StartTS: 1785772800},
	{ID: 16, Type: 2, Limit: 20, ItemID: 10448, ItemNum: 1, CostItemID: 3345, CostItemNum: 20, StartTS: 1785772800},
	{ID: 17, Type: 2, Limit: 20, ItemID: 10449, ItemNum: 1, CostItemID: 3345, CostItemNum: 100, StartTS: 1785772800},
	{ID: 18, Type: 1, Limit: 3, ItemID: 10449, ItemNum: 1, CostItemID: 10442, CostItemNum: 1000, StartTS: 1785772800},
	{ID: 19, Type: 1, Limit: 1, ItemID: 10450, ItemNum: 1, CostItemID: 1747, CostItemNum: 1000000, StartTS: 1793030400},
	{ID: 20, Type: 1, Limit: 20, ItemID: 10450, ItemNum: 1, CostItemID: 10442, CostItemNum: 200, StartTS: 1793030400},
	{ID: 21, Type: 1, Limit: 1, ItemID: 10451, ItemNum: 1, CostItemID: 1747, CostItemNum: 5000000, StartTS: 1793030400},
	{ID: 22, Type: 2, Limit: 20, ItemID: 10450, ItemNum: 1, CostItemID: 3345, CostItemNum: 20, StartTS: 1793030400},
	{ID: 23, Type: 2, Limit: 20, ItemID: 10451, ItemNum: 1, CostItemID: 3345, CostItemNum: 100, StartTS: 1793030400},
	{ID: 24, Type: 1, Limit: 3, ItemID: 10451, ItemNum: 1, CostItemID: 10442, CostItemNum: 1000, StartTS: 1793030400},
}

// GetCultivationCardShopGood 按 shop_id 查询静态商品。
func GetCultivationCardShopGood(shopID int32) *CultivationCardShopGood {
	for i := range CultivationCardShopGoods {
		if CultivationCardShopGoods[i].ID == shopID {
			return &CultivationCardShopGoods[i]
		}
	}
	return nil
}

// IsCultivationCardCoinGood 判断商品是否以铜钱结算（自动化只买这类商品）。
func IsCultivationCardCoinGood(shopID int32) bool {
	g := GetCultivationCardShopGood(shopID)
	return g != nil && g.CostItemID == CultivationCardCostCoin
}
