// 悟法领域 (Module 658) 协议编解码，依据原版 Main.swf
// com.protocols.Mod_WuFaField / Mod_WuFaField_Base / com.datas.WuFaFieldData 还原。
//
// @author Ateng
// @since 2026-11-03
package protocol

import "fmt"

// 悟法领域结果码，对齐官方 Mod_WuFaField_Base。
const (
	WuFaResultSuccess           uint8 = 2  // SUCCESS
	WuFaResultFailed            uint8 = 3  // FAILED
	WuFaResultNotEnoughIngot    uint8 = 4  // NOT_ENOUGH_INGOT
	WuFaResultFunctionNotOpen   uint8 = 5  // FUNCTION_NO_OPEN
	WuFaResultNotEnoughResourse uint8 = 6  // NOT_ENOUGH_RESOURSE (官方拼写)
	WuFaResultCountLimit        uint8 = 7  // COUNT_LIMIT
	WuFaResultNotEnoughCount    uint8 = 8  // NOT_ENOUGH_COUNT
	WuFaResultOverGet           uint8 = 9  // OVER_GET
	WuFaResultItemPackFull      uint8 = 10 // ITEM_PACK_FULL
	WuFaResultNotEnoughResource uint8 = 11 // NOT_ENOUGH_RESOURCE
	WuFaResultWeekNumLimit      uint8 = 12 // WEEK_NUM_LIMT
	WuFaResultNotEnoughRecharge uint8 = 13 // NOT_ENOUGH_RECHARGE_INGOT
	WuFaResultIsBuy             uint8 = 14 // IS_BUY
	WuFaResultIsHave            uint8 = 15 // IS_HAVE
	WuFaResultFullGrade         uint8 = 16 // FULL_GRADE
	WuFaResultFieldTypeError    uint8 = 17 // FIELD_TYPE_ERROR
	WuFaResultNoEnoughDrawTimes uint8 = 18 // NO_ENOUGTH_DRAW_TIMES
)

// 悟法领域抽卡类型，对齐官方 Mod_WuFaField_Base。
const (
	WuFaDrawOne uint8 = 0 // DRAW_ONE
	WuFaDrawTen uint8 = 1 // DRAW_TEN
)

// WuFaFieldState 悟法领域单个秘馆的实时状态。
type WuFaFieldState struct {
	FieldType         int32 // 秘馆类型
	TodayCount        int32 // 今日已抽次数
	TotalCount        int32 // 累计抽卡次数
	CurrCountID       int32 // 当前累计奖励档位
	LeftFreeOneCount  int32 // 剩余免费单抽次数
	LeftFreeTenCount  int32 // 剩余免费十连次数
	WishGongFa        int32 // 心愿功法编号
	WishGongFaProb    int32 // 心愿功法概率
	LeftTotalTenCount int32 // 剩余累计十连次数
}

// WuFaItemState 悟法领域资源余量。
type WuFaItemState struct {
	ItemID       int32
	ItemNum      int32
	ExceptionNum int32
}

// WuFaPanel 悟法领域主面板 (Mod 658, Act 0 panel_info)。
//
// 注意：官方面板回包没有 result_code，只能通过完整解析结构判断有效性。
type WuFaPanel struct {
	Fields           []WuFaFieldState
	Items            []WuFaItemState
	GiftStartTime    int32
	GiftEndTime      int32
	HavePrivilege    int32
	BookStoreOpen    int32
	RuMenFieldActive int32
	PsychicTbldLevel int32
	PsychicResNum    int32
	IsLuckyDay       int32
	RestTimes        int32
	DiscountNum      int32
	IndexTime        int32
	IsSkip           int8
}

// WuFaAward 单次悟法抽卡产出的奖励条目。
type WuFaAward struct {
	ItemID        int32
	ItemNum       int32
	ChangeItemID  int32
	ChangeItemNum int32
	IsSynthesize  int32
	IsUpLevel     int32
	PsychicAddNum int32
	LuckyItemNum  int32
}

// WuFaDrawResult 悟法领域感悟抽卡回包 (Mod 658, Act 1 draw)。
type WuFaDrawResult struct {
	Result    uint8
	Awards    []WuFaAward
	RestTimes int32
}

// WuFaResultText 返回悟法领域结果码的可读文本，用于日志与诊断。
func WuFaResultText(code uint8) string {
	switch code {
	case WuFaResultSuccess:
		return "成功"
	case WuFaResultFailed:
		return "失败"
	case WuFaResultNotEnoughIngot:
		return "元宝不足"
	case WuFaResultFunctionNotOpen:
		return "功能未开启"
	case WuFaResultNotEnoughResourse, WuFaResultNotEnoughResource:
		return "资源不足"
	case WuFaResultCountLimit:
		return "次数已达上限"
	case WuFaResultNotEnoughCount:
		return "剩余次数不足"
	case WuFaResultOverGet:
		return "已领取"
	case WuFaResultItemPackFull:
		return "背包已满"
	case WuFaResultWeekNumLimit:
		return "周次数已达上限"
	case WuFaResultNotEnoughRecharge:
		return "充值元宝不足"
	case WuFaResultIsBuy:
		return "已购买"
	case WuFaResultIsHave:
		return "已拥有"
	case WuFaResultFullGrade:
		return "已满级"
	case WuFaResultFieldTypeError:
		return "秘馆类型错误"
	case WuFaResultNoEnoughDrawTimes:
		return "感悟次数不足"
	default:
		return fmt.Sprintf("未知结果码(%d)", code)
	}
}

// ParseWuFaPanel 解析悟法领域面板回包 (Mod 658, Act 0 panel_info)。
func ParseWuFaPanel(payload []byte) (*WuFaPanel, error) {
	r := NewReader(payload)

	fieldCount, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("悟法面板秘馆数量读取失败: %w", err)
	}
	panel := &WuFaPanel{
		Fields: make([]WuFaFieldState, 0, fieldCount),
	}
	for i := 0; i < int(fieldCount); i++ {
		values, readErr := readWuFaInt32Values(r, 9)
		if readErr != nil {
			return nil, fmt.Errorf("悟法面板秘馆[%d]解析失败: %w", i, readErr)
		}
		panel.Fields = append(panel.Fields, WuFaFieldState{
			FieldType:         values[0],
			TodayCount:        values[1],
			TotalCount:        values[2],
			CurrCountID:       values[3],
			LeftFreeOneCount:  values[4],
			LeftFreeTenCount:  values[5],
			WishGongFa:        values[6],
			WishGongFaProb:    values[7],
			LeftTotalTenCount: values[8],
		})
	}

	itemCount, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("悟法面板资源数量读取失败: %w", err)
	}
	panel.Items = make([]WuFaItemState, 0, itemCount)
	for i := 0; i < int(itemCount); i++ {
		values, readErr := readWuFaInt32Values(r, 3)
		if readErr != nil {
			return nil, fmt.Errorf("悟法面板资源[%d]解析失败: %w", i, readErr)
		}
		panel.Items = append(panel.Items, WuFaItemState{
			ItemID:       values[0],
			ItemNum:      values[1],
			ExceptionNum: values[2],
		})
	}

	values, err := readWuFaInt32Values(r, 11)
	if err != nil {
		return nil, fmt.Errorf("悟法面板公共字段解析失败: %w", err)
	}
	panel.GiftStartTime = values[0]
	panel.GiftEndTime = values[1]
	panel.HavePrivilege = values[2]
	panel.BookStoreOpen = values[3]
	panel.RuMenFieldActive = values[4]
	panel.PsychicTbldLevel = values[5]
	panel.PsychicResNum = values[6]
	panel.IsLuckyDay = values[7]
	panel.RestTimes = values[8]
	panel.DiscountNum = values[9]
	panel.IndexTime = values[10]

	panel.IsSkip, err = r.ReadInt8()
	if err != nil {
		return nil, fmt.Errorf("悟法面板 is_skip 解析失败: %w", err)
	}
	return panel, nil
}

// ParseWuFaDrawResult 解析悟法领域感悟抽卡回包 (Mod 658, Act 1 draw)。
func ParseWuFaDrawResult(payload []byte) (*WuFaDrawResult, error) {
	r := NewReader(payload)

	result, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("悟法抽卡结果码读取失败: %w", err)
	}
	awardCount, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("悟法抽卡奖励数量读取失败: %w", err)
	}

	draw := &WuFaDrawResult{
		Result: result,
		Awards: make([]WuFaAward, 0, awardCount),
	}
	for i := 0; i < int(awardCount); i++ {
		values, readErr := readWuFaInt32Values(r, 8)
		if readErr != nil {
			return nil, fmt.Errorf("悟法抽卡奖励[%d]解析失败: %w", i, readErr)
		}
		draw.Awards = append(draw.Awards, WuFaAward{
			ItemID:        values[0],
			ItemNum:       values[1],
			ChangeItemID:  values[2],
			ChangeItemNum: values[3],
			IsSynthesize:  values[4],
			IsUpLevel:     values[5],
			PsychicAddNum: values[6],
			LuckyItemNum:  values[7],
		})
	}

	draw.RestTimes, err = r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("悟法抽卡剩余次数解析失败: %w", err)
	}
	return draw, nil
}

func readWuFaInt32Values(r *Reader, n int) ([]int32, error) {
	values := make([]int32, n)
	for i := range values {
		v, err := r.ReadInt32()
		if err != nil {
			return nil, err
		}
		values[i] = v
	}
	return values, nil
}
