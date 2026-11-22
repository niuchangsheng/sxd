// Package minigame 提供吉星高照、仙旅奇缘答题、喜从天降、刮刮卡、钓鱼等益智趣味活动。
//
// 本文件实现天帝宝库自选大奖与免费抽奖 (Mod_TreasureHouse_Base, Module 594) 自动化。
package minigame

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/dictionary"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/routines/kit"
	"sxd-pie-ng/internal/scheduler"
)

// AwardSelectorFunc 允许注入自定义交互选择器（例如命令行交互、Web交互回调或自动化测试桩）。
type AwardSelectorFunc func(ctx context.Context, pool *protocol.TreasureHousePool, dict dictionary.Repository) ([]protocol.TreasureAwardSelect, error)

// treasureHousePriority 天帝宝库调度优先级。
// Act 4 set_use_free_flag 会严格校验城镇/战场上下文，服务端在前置状态被污染
// （试炼建队、副本战斗、跨服战斗等）时会静默丢弃该请求；因此宝库必须排在
// 本轮所有场景/队伍类玩法之前执行。2026-11-22 实测优先级 200 时仍被推图
// (300)/结缘(250) 抢先，导致 Act 4 失败；提升到 450，登录后第一个执行。
var treasureHousePriority = scheduler.PhasePriority(scheduler.PhaseTownSensitive, 30)

// 天帝宝库非交互模式选奖策略。
const (
	// TreasureHouseSelectionRarity 按商品稀有度自动选择 (默认)
	TreasureHouseSelectionRarity = "rarity"
	// TreasureHouseSelectionExplicit 严格使用配置指定的奖励 ID
	TreasureHouseSelectionExplicit = "explicit"
	// TreasureHouseSelectionAuto 依据账号养成进度快照动态计算需求分并选奖
	TreasureHouseSelectionAuto = "auto"
)

// TreasureHouseConfig 天帝宝库自选大奖与开启抽奖配置。
type TreasureHouseConfig struct {
	// DictRepo 静态数据库字典仓储 (用于解析大奖条目道具名及说明)
	DictRepo dictionary.Repository
	// SelectionMode 非交互模式选奖策略: rarity(默认)|explicit
	SelectionMode string
	// PreferAwards 优先奖励条目 ID 列表 (按顺序匹配当前开放候选，命中即选).
	// 用于优先补齐账号当前最缺的资源，例如鸿蒙紫气、血魂石等。
	PreferAwards []int32
	// Interactive 是否开启终端交互式输入自选大奖
	Interactive bool
	// In 用户交互输入流，默认为 os.Stdin
	In io.Reader
	// Out 交互提示输出流，默认为 os.Stdout
	Out io.Writer
	// Selector 自定义大奖选择逻辑 (优先级高于终端输入)
	Selector AwardSelectorFunc
	// Awards 预设三档自选大奖列表 (非交互模式或交互跳过/默认时的值)
	Awards []protocol.TreasureAwardSelect
	// MaxDrawTimes 单次调度最大抽奖次数 (默认 10)
	MaxDrawTimes int
}

// DefaultTreasureHouseConfig 返回默认天帝宝库三档大奖配置 (对齐抓包基准: 50, 25, 42)。
func DefaultTreasureHouseConfig() TreasureHouseConfig {
	return TreasureHouseConfig{
		Awards: []protocol.TreasureAwardSelect{
			{TierID: 1, ItemID: 50},
			{TierID: 2, ItemID: 25},
			{TierID: 3, ItemID: 42},
		},
		MaxDrawTimes: 10,
	}
}

// formatAwardEntry 将候选道具条目 ID 格式化为直观的中文名称与数量描述。
func formatAwardEntry(id int32, awardMap map[int]*dictionary.WishpoolAward, dict dictionary.Repository) string {
	if awardMap != nil {
		if a, ok := awardMap[int(id)]; ok && a != nil {
			name := a.ItemName
			if name == "" {
				name = fmt.Sprintf("条目%d", a.ID)
			}
			desc := ""
			if a.Description != "" {
				desc = " (" + a.Description + ")"
			}
			if a.ItemNum > 1 {
				return fmt.Sprintf("%s x%d (ID: %d)%s", name, a.ItemNum, id, desc)
			}
			return fmt.Sprintf("%s (ID: %d)%s", name, id, desc)
		}
	}
	if dict != nil {
		if item, err := dict.GetItem(int(id)); err == nil && item != nil {
			return fmt.Sprintf("%s (道具ID: %d)", item.Name, id)
		}
	}
	return fmt.Sprintf("条目 ID: %d", id)
}

// readLineWithTimeout 携带超时与上下文感知地读取一行输入。
func readLineWithTimeout(ctx context.Context, reader *bufio.Reader, timeout time.Duration) (string, error) {
	type result struct {
		line string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		line, err := reader.ReadString('\n')
		ch <- result{line: line, err: err}
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-timer.C:
		return "", errors.New("输入等待超时")
	case res := <-ch:
		if res.err != nil && res.line == "" {
			return "", res.err
		}
		return strings.TrimSpace(res.line), nil
	}
}

// promptUserAwardSelection 在控制台打印当前服务端刷新出的各档候选大奖池并引导用户交互选择。
func promptUserAwardSelection(
	ctx context.Context,
	in io.Reader,
	out io.Writer,
	pool *protocol.TreasureHousePool,
	dict dictionary.Repository,
	defaultAwards []protocol.TreasureAwardSelect,
) []protocol.TreasureAwardSelect {
	if in == nil {
		in = os.Stdin
	}
	if out == nil {
		out = os.Stdout
	}

	var awardMap map[int]*dictionary.WishpoolAward
	if dict != nil {
		awardMap, _ = dict.GetWishpoolAwards()
	}

	defaultMap := make(map[int]int32)
	for _, a := range defaultAwards {
		defaultMap[int(a.TierID)] = a.ItemID
	}
	if defaultMap[1] == 0 {
		defaultMap[1] = 50
	}
	if defaultMap[2] == 0 {
		defaultMap[2] = 25
	}
	if defaultMap[3] == 0 {
		defaultMap[3] = 42
	}

	fmt.Fprintln(out, "\n====================【天帝宝库 当前候选大奖池】====================")
	fmt.Fprintf(out, "服务端最新候选奖池刷新成功，共解析出 %d 项可选道具：\n", pool.TotalCount)

	printTierOptions := func(tier int, items []int32, defID int32) {
		defName := formatAwardEntry(defID, awardMap, dict)
		fmt.Fprintf(out, "\n【第 %d 档大奖候选列表】 (直接回车默认: %s):\n", tier, defName)
		for idx, itemID := range items {
			entryName := formatAwardEntry(itemID, awardMap, dict)
			fmt.Fprintf(out, "  [%2d] %s\n", idx+1, entryName)
		}
	}

	printTierOptions(1, pool.Tier1Items, defaultMap[1])
	printTierOptions(2, pool.Tier2Items, defaultMap[2])
	printTierOptions(3, pool.Tier3Items, defaultMap[3])
	fmt.Fprintln(out, "=================================================================")

	reader := bufio.NewReader(in)

	selectTier := func(tier int, items []int32, defID int32) int32 {
		defName := formatAwardEntry(defID, awardMap, dict)
		fmt.Fprintf(out, ">> 请选择【第 %d 档大奖】(输入 1-%d 序号、道具ID或名称, 直接回车默认选 [%s]): ", tier, len(items), defName)

		input, err := readLineWithTimeout(ctx, reader, 60*time.Second)
		if err != nil || input == "" {
			fmt.Fprintf(out, "已采纳默认设定: %s\n", defName)
			return defID
		}

		cleanInput := strings.TrimSpace(input)
		lower := strings.ToLower(cleanInput)

		// 1. 若以 id: 或 # 前缀指定道具 ID (如 id:14, #14)
		if strings.HasPrefix(lower, "id:") || strings.HasPrefix(lower, "id ") || strings.HasPrefix(lower, "#") {
			raw := strings.TrimLeft(lower, "id:# ")
			var targetID int32
			if _, err := fmt.Sscanf(raw, "%d", &targetID); err == nil {
				for _, itm := range items {
					if itm == targetID {
						fmt.Fprintf(out, "成功选定第 %d 档 (按ID): %s\n", tier, formatAwardEntry(itm, awardMap, dict))
						return itm
					}
				}
			}
		}

		// 2. 按纯数字解析
		var val int32
		if _, scanErr := fmt.Sscanf(cleanInput, "%d", &val); scanErr == nil {
			// 若数字在 1..len(items) 范围内，优先作为菜单项序号
			if val >= 1 && int(val) <= len(items) {
				picked := items[val-1]
				fmt.Fprintf(out, "成功选定第 %d 档 (序号 %d): %s\n", tier, val, formatAwardEntry(picked, awardMap, dict))
				return picked
			}

			// 若数字超出菜单序号范围，匹配条目 ID (例如 50, 25, 42)
			for _, itm := range items {
				if itm == val {
					fmt.Fprintf(out, "成功选定第 %d 档 (道具ID %d): %s\n", tier, val, formatAwardEntry(itm, awardMap, dict))
					return itm
				}
			}
		}

		// 3. 按道具名称匹配 (如输入 "鸿蒙紫气"、"血脉精华")
		for _, itm := range items {
			entryName := formatAwardEntry(itm, awardMap, dict)
			if strings.Contains(entryName, cleanInput) {
				fmt.Fprintf(out, "成功选定第 %d 档 (名称匹配): %s\n", tier, entryName)
				return itm
			}
		}

		fmt.Fprintf(out, "输入的编号不在本档候选池中，已采纳默认设定: %s\n", defName)
		return defID
	}

	t1 := selectTier(1, pool.Tier1Items, defaultMap[1])
	t2 := selectTier(2, pool.Tier2Items, defaultMap[2])
	t3 := selectTier(3, pool.Tier3Items, defaultMap[3])

	return []protocol.TreasureAwardSelect{
		{TierID: 1, ItemID: t1},
		{TierID: 2, ItemID: t2},
		{TierID: 3, ItemID: t3},
	}
}

// NewTreasureHouseRoutine 构造天帝宝库自选与开启抽奖任务 (Feature 16: 天帝宝库)。
// 支持先向服务端拉取刷新候选道具，再通过用户交互输入或自定义选择器选定一档、二档、三档大奖。
func NewTreasureHouseRoutine(cfgs ...TreasureHouseConfig) scheduler.ActivityRoutine {
	cfg := DefaultTreasureHouseConfig()
	if len(cfgs) > 0 {
		cfg = cfgs[0]
	}
	if len(cfg.Awards) == 0 {
		cfg.Awards = DefaultTreasureHouseConfig().Awards
	}
	if cfg.MaxDrawTimes <= 0 {
		cfg.MaxDrawTimes = 10
	}

	return scheduler.NewBaseRoutine(
		"treasure_house",
		"minigame",
		scheduler.ScheduleCron,
		// 天帝宝库的 set_use_free_flag (Act 4) 会严格校验角色的城镇上下文：
		// 若本轮先执行过魔王试炼等会创建队伍/战场的玩法，服务端会保留战场状态并
		// 静默丢弃 Act 4（实机 2026-11-06）。因此把宝库安排在每日任务批次最前，
		// 高于 PriorityHigh，确保设置免费标记与抽奖链路安全。
		treasureHousePriority,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}
			slog.Info("正在执行益智任务: 天帝宝库自选与开启抽奖",
				"role_id", session.RoleID(),
				"interactive", cfg.Interactive,
				"awards_count", len(cfg.Awards),
				"max_draw_times", cfg.MaxDrawTimes,
			)

			// 先查功能开放列表；未开放时不再发送会被静默丢弃的 594 业务请求。
			open, fErr := session.IsFunctionOpen(ctx, protocol.FunctionIDTreasureHouse)
			if fErr != nil {
				slog.Info("天帝宝库功能开放状态查询失败，跳过", "role_id", session.RoleID(), "err", fErr)
				return nil
			}
			if !open {
				slog.Info("天帝宝库功能未开放，跳过", "role_id", session.RoleID(), "function_id", protocol.FunctionIDTreasureHouse)
				return nil
			}

			// 0.1 不要在 Act4 之前发送任何 Mod 57 (魔王试炼) 请求。
			// 线上实测: 即便是只读的 get_player_team_info 也会让服务端进入
			// 试炼上下文，随后免费优先标记 set_use_free_flag (Act 4) 被静默
			// 丢弃，导致整轮免费抽奖被跳过。存在遗留队伍时由魔王试炼例程
			// 自身在开战前清理，无需在此重复处理。

			prevAct := session.LastActionID()
			if prevAct == 0 {
				prevAct = protocol.ActionIDTownEnter
			}
			caller := kit.NewCaller(ctx, session, 8*time.Second)
			call := func(pkt *protocol.Packet, action uint32, timeout time.Duration) *protocol.Packet {
				resp, cErr := caller.Call(pkt, action, timeout)
				if cErr != nil || resp == nil {
					// 调用方会记录具体业务上下文，这里只保留 Debug 级诊断，避免重复 WARN。
					slog.Debug("天帝宝库请求无回包",
						"action", fmt.Sprintf("0x%08X", action), "err", cErr)
					return nil
				}
				prevAct = action
				return resp
			}

			// 1. 主面板 (Mod 594, Act 1): 校验 result_code 并读取免费钥匙/当前自选
			mainResp := call(protocol.BuildTreasureHouseMainPanelPacket(prevAct),
				protocol.ActionIDTreasureHouseMainPanel, 8*time.Second)
			if mainResp == nil {
				if abortErr := caller.AbortErr(); abortErr != nil {
					return abortErr
				}
				return nil
			}
			mainInfo, pErr := protocol.ParseTreasureHouseMainPanelResult(mainResp.Payload)
			if pErr != nil {
				slog.Warn("解析天帝宝库主面板失败", "err", pErr)
				return nil
			}
			if mainInfo.Result != protocol.TreasureHouseResultSuccess {
				slog.Warn("天帝宝库主面板返回失败",
					"result", mainInfo.Result,
					"meaning", protocol.TreasureHouseResultName(mainInfo.Result))
				return nil
			}
			slog.Info("天帝宝库主面板",
				"role_id", session.RoleID(),
				"free_item_num", mainInfo.FreeItemNum,
				"use_free_flag", mainInfo.UseFreeFlag,
				"left_can_get_free", mainInfo.LeftCanGetFree,
				"total_times", mainInfo.TotalTimes,
				"wish_list", mainInfo.WishList)

			// 2. 领取今日免费钥匙 (Mod 594, Act 8)
			freeKeyNoItem := false
			if mainInfo.LeftCanGetFree > 0 {
				freeResp := call(protocol.BuildTreasureHouseGetFreeItemPacket(prevAct),
					protocol.ActionIDTreasureHouseGetFreeItem, 8*time.Second)
				if freeResp != nil {
					code, cErr := protocol.ParseTreasureHouseResultCode(freeResp.Payload)
					switch {
					case cErr != nil:
						slog.Warn("解析领取免费钥匙回包失败", "err", cErr)
					case code == protocol.TreasureHouseResultNoItem:
						// 今日已无免费钥匙可领，属于正常业务状态。
						freeKeyNoItem = true
						slog.Info("领取免费钥匙: 无可用道具",
							"result", code,
							"meaning", protocol.TreasureHouseResultName(code))
					case code != protocol.TreasureHouseResultSuccess:
						slog.Warn("领取免费钥匙失败",
							"result", code,
							"meaning", protocol.TreasureHouseResultName(code))
					default:
						slog.Info("已领取今日免费钥匙", "left_can_get_free", mainInfo.LeftCanGetFree)
						if refresh := call(protocol.BuildTreasureHouseMainPanelPacket(prevAct),
							protocol.ActionIDTreasureHouseMainPanel, 8*time.Second); refresh != nil {
							if mi, e := protocol.ParseTreasureHouseMainPanelResult(refresh.Payload); e == nil && mi.Result == protocol.TreasureHouseResultSuccess {
								mainInfo = mi
							}
						}
					}
				}
			}

			// 无免费钥匙且面板免费抽奖次数为 0 时，后续 Act 4 必然无免费次数，
			// 直接跳过，避免已知上下文丢包（2026-11-14 ncs 实测）。
			if freeKeyNoItem && mainInfo.FreeItemNum == 0 {
				slog.Info("天帝宝库无免费钥匙且无免费抽奖次数，跳过设置与抽奖",
					"role_id", session.RoleID())
				return nil
			}

			// 2.1 设置优先使用免费次数 (Mod 594, Act 4) 必须紧跟在主面板/领钥匙之后、
			// 任何养成面板采集之前发送。线上实测: Mod 57 请求以及动态选奖采集的
			// 养成面板请求都会让服务端进入其他上下文，随后 Act 4 被静默丢弃。
			// 2026-11-14 进一步验证：官方裸 1 字节与旧的 8 字节载荷均无回包，
			// 确认为服务端上下文/资格限制；设置失败时放弃抽奖，避免误扣元宝。
			flagResp := call(protocol.BuildTreasureHouseUseFreeFlagPacket(1, prevAct),
				protocol.ActionIDTreasureHouseSetUseFreeFlag, 8*time.Second)
			if flagResp == nil {
				if abortErr := caller.AbortErr(); abortErr != nil {
					return abortErr
				}
				// 该请求对上下文极敏感，10 分钟一轮会持续丢包；改为 1 小时低频探测。
				slog.Info("设置免费次数优先无回包，1 小时后再试（不消耗元宝）",
					"role_id", session.RoleID())
				return scheduler.NewCooldown(time.Hour, "天帝宝库设置免费次数优先无回包")
			}
			flagCode, fErr := protocol.ParseTreasureHouseResultCode(flagResp.Payload)
			if fErr != nil {
				slog.Warn("解析设置免费次数优先回包失败，跳过抽奖", "err", fErr)
				return nil
			}
			if flagCode == protocol.TreasureHouseResultNoFreeItem {
				// 今日无免费钥匙：服务端正常返回该业务码，不是协议失败。
				slog.Info("天帝宝库无免费次数，跳过抽奖以免误消耗元宝",
					"result", flagCode,
					"meaning", protocol.TreasureHouseResultName(flagCode))
				return nil
			}
			if flagCode != protocol.TreasureHouseResultSuccess {
				slog.Warn("设置免费次数优先失败，跳过抽奖以免误消耗元宝",
					"result", flagCode,
					"meaning", protocol.TreasureHouseResultName(flagCode))
				return nil
			}

			// 3. 自选面板 (Mod 594, Act 2): 只有 open_flag=1 的候选才能被服务端接受
			panelResp := call(protocol.BuildTreasureHouseInfoPacket(prevAct),
				protocol.ActionIDTreasureHouseSelectPanel, 8*time.Second)
			if panelResp == nil {
				if abortErr := caller.AbortErr(); abortErr != nil {
					return abortErr
				}
				return nil
			}
			panel, pErr := protocol.ParseTreasureHouseSelectPanelResult(panelResp.Payload)
			if pErr != nil {
				slog.Warn("解析天帝宝库自选面板失败", "err", pErr)
				return nil
			}
			if panel.Result != protocol.TreasureHouseResultSuccess {
				slog.Warn("天帝宝库自选面板返回失败",
					"result", panel.Result,
					"meaning", protocol.TreasureHouseResultName(panel.Result))
				return nil
			}

			offered := offeredAwardPool(panel)

			// 非交互模式默认按商品稀有度自动选择；显式模式下沿用配置值。
			var itemLookup func(int32) (*dictionary.Item, error)
			if cfg.DictRepo != nil {
				itemLookup = func(itemID int32) (*dictionary.Item, error) {
					return cfg.DictRepo.GetItem(int(itemID))
				}
			}
			selectedAwards := resolveTreasureAwards(cfg.Awards, mainInfo.WishList, offered)
			// 非交互模式选奖策略:
			//   auto     -> 采集账号养成进度并动态计算需求分 (每次刷新重新评估)
			//   explicit -> 使用配置奖励
			//   rarity   -> 稀有度/价值序 + 功能开放过滤 (默认)
			// 交互模式保留配置值作为回车默认。
			if !cfg.Interactive && cfg.Selector == nil {
				// 先取角色已开放功能集合，过滤掉所属系统未开放的奖励。
				var isFuncOpen func(int32) bool
				if openSet, fErr := session.RefreshPlayerFunctionsIfStale(ctx); fErr != nil {
					slog.Warn("获取功能开放列表失败，选奖时不做功能过滤",
						"role_id", session.RoleID(), "err", fErr)
				} else {
					isFuncOpen = func(functionID int32) bool { return openSet[functionID] }
				}

				switch cfg.SelectionMode {
				case TreasureHouseSelectionExplicit:
					// 保持 resolveTreasureAwards 的结果。
				case TreasureHouseSelectionAuto:
					snap := CollectNeedSnapshot(ctx, session, offered)
					if snap.hasProgressData() {
						needs := EvaluateTreasureNeeds(snap, offered)
						selectedAwards = SelectTreasureAwardsByNeed(offered, itemLookup, isFuncOpen, needs)
					} else {
						slog.Warn("账号养成进度采集全部失败，回退稀有度选奖", "role_id", session.RoleID())
						selectedAwards = SelectTreasureAwardsByRarity(offered, itemLookup, isFuncOpen, cfg.PreferAwards)
					}
				default:
					selectedAwards = SelectTreasureAwardsByRarity(offered, itemLookup, isFuncOpen, cfg.PreferAwards)
				}
			}
			slog.Info("天帝宝库当前可选大奖",
				"role_id", session.RoleID(),
				"tier1_options", offered.Tier1Items,
				"tier2_options", offered.Tier2Items,
				"tier3_options", offered.Tier3Items,
				"resolved_awards", selectedAwards)

			// 交互选奖：若配置了 Selector 则优先调用，否则若开启 Interactive 则通过交互式终端输入
			if cfg.Selector != nil {
				if chosen, cErr := cfg.Selector(ctx, offered, cfg.DictRepo); cErr == nil && len(chosen) >= 3 {
					selectedAwards = chosen
				}
			} else if cfg.Interactive {
				selectedAwards = promptUserAwardSelection(ctx, cfg.In, cfg.Out, offered, cfg.DictRepo, selectedAwards)
			}

			// 4. 设定三档自选大奖 (Mod 594, Act 3): 必须解析 result_code
			selResp := call(protocol.BuildTreasureHouseSelectPacket(selectedAwards, prevAct),
				protocol.ActionIDTreasureHouseSetAward, 8*time.Second)
			if selResp == nil {
				if abortErr := caller.AbortErr(); abortErr != nil {
					return abortErr
				}
				return nil
			}
			code, cErr := protocol.ParseTreasureHouseResultCode(selResp.Payload)
			if cErr != nil {
				slog.Warn("解析设定自选大奖回包失败", "err", cErr)
				return nil
			}
			if code != protocol.TreasureHouseResultSuccess {
				slog.Warn("设定天帝宝库自选大奖失败",
					"result", code,
					"meaning", protocol.TreasureHouseResultName(code),
					"awards", selectedAwards)
				return nil
			}
			slog.Info("天帝宝库自选大奖设定成功", "awards", selectedAwards)

			// 5. 消耗免费钥匙抽奖 (Mod 594, Act 6): 每次必须解析 result_code，失败立即熔断
			drawLimit := cfg.MaxDrawTimes
			if int(mainInfo.FreeItemNum) < drawLimit {
				drawLimit = int(mainInfo.FreeItemNum)
			}
			if drawLimit <= 0 {
				slog.Info("天帝宝库无免费钥匙，跳过抽奖以免消耗元宝",
					"role_id", session.RoleID(),
					"free_item_num", mainInfo.FreeItemNum)
				return nil
			}

			drawCount := 0
			for i := 0; i < drawLimit; i++ {
				drawResp := call(protocol.BuildTreasureHouseDrawPacket(prevAct),
					protocol.ActionIDTreasureHouseLottery, 8*time.Second)
				if drawResp == nil {
					if abortErr := caller.AbortErr(); abortErr != nil {
						return abortErr
					}
					break
				}
				lottery, lErr := protocol.ParseTreasureHouseLotteryResult(drawResp.Payload)
				if lErr != nil {
					slog.Warn("解析天帝宝库抽奖回包失败", "err", lErr)
					break
				}
				if lottery.Result != protocol.TreasureHouseResultSuccess {
					slog.Warn("天帝宝库抽奖失败，停止后续抽奖",
						"result", lottery.Result,
						"meaning", protocol.TreasureHouseResultName(lottery.Result),
						"draw_index", i+1)
					break
				}
				drawCount++
				slog.Info("天帝宝库抽奖成功",
					"award_id", lottery.AwardID,
					"draw_index", i+1)
				if !kit.Sleep(ctx, 50*time.Millisecond) {
					break
				}
			}

			// 7. 抽取结束后刷新主面板同步剩余钥匙与累计次数
			if refresh := call(protocol.BuildTreasureHouseMainPanelPacket(prevAct),
				protocol.ActionIDTreasureHouseMainPanel, 8*time.Second); refresh != nil {
				if mi, e := protocol.ParseTreasureHouseMainPanelResult(refresh.Payload); e == nil && mi.Result == protocol.TreasureHouseResultSuccess {
					slog.Info("天帝宝库抽奖后状态",
						"free_item_num", mi.FreeItemNum,
						"total_times", mi.TotalTimes,
						"wish_list", mi.WishList)
				}
			}

			slog.Info("天帝宝库抽奖流程完成", "role_id", session.RoleID(), "draw_count", drawCount)
			return nil
		},
	)
}

// offeredAwardPool 将自选面板中 open_flag=1 的候选大奖整理为三档奖池。
// 服务端只接受当前开放的候选，配置里未开放的道具会被 set_award 拒绝。
func offeredAwardPool(panel *protocol.TreasureHouseSelectPanel) *protocol.TreasureHousePool {
	pool := &protocol.TreasureHousePool{}
	if panel == nil {
		return pool
	}
	for _, q := range panel.Qualities {
		for _, c := range q.Awards {
			if c.OpenFlag == 0 {
				continue
			}
			switch q.Quality {
			case 1:
				pool.Tier1Items = append(pool.Tier1Items, c.AwardID)
			case 2:
				pool.Tier2Items = append(pool.Tier2Items, c.AwardID)
			case 3:
				pool.Tier3Items = append(pool.Tier3Items, c.AwardID)
			default:
				continue
			}
			pool.TotalCount++
		}
	}
	return pool
}

// containsAwardID 判断道具条目 ID 是否在候选列表中。
func containsAwardID(items []int32, id int32) bool {
	for _, item := range items {
		if item == id {
			return true
		}
	}
	return false
}

// treasureItemTypePriority 方案 A 的档内价值序:
// 自选包/礼包(42100) > 稀有养成材料(31000) > 货币(28000) > 宝石/随机包(10011) > 令牌/玉牌(10006)。
func treasureItemTypePriority(typeID int) int {
	switch typeID {
	case 42100:
		return 5
	case 31000:
		return 4
	case 28000:
		return 3
	case 10011:
		return 2
	case 10006:
		return 1
	default:
		return 0
	}
}

func treasureItemQuality(item *dictionary.Item) int {
	if item == nil {
		return 0
	}
	return item.Quality
}

func treasureItemTypeID(item *dictionary.Item) int {
	if item == nil {
		return 0
	}
	return item.TypeID
}

// betterTreasureAward 判断候选 a 是否优于 b。
//
// 排序键 (逐级比较):
//  1. 道具品质 (稀有度) 降序
//  2. 方案 A 类型价值序 (自选包 > 稀有材料 > 货币 > 宝石包 > 令牌) 降序
//  3. 发放数量降序
//  4. 官方展示 sort 升序
//  5. 奖励条目 id 升序 (保证结果稳定)
func betterTreasureAward(a protocol.TreasureAward, ia *dictionary.Item, b protocol.TreasureAward, ib *dictionary.Item) bool {
	if qa, qb := treasureItemQuality(ia), treasureItemQuality(ib); qa != qb {
		return qa > qb
	}
	if pa, pb := treasureItemTypePriority(treasureItemTypeID(ia)), treasureItemTypePriority(treasureItemTypeID(ib)); pa != pb {
		return pa > pb
	}
	if a.ItemNum != b.ItemNum {
		return a.ItemNum > b.ItemNum
	}
	if a.Sort != b.Sort {
		return a.Sort < b.Sort
	}
	return a.ID < b.ID
}

// treasureAwardCandidate 候选奖励及其道具元数据。
type treasureAwardCandidate struct {
	award protocol.TreasureAward
	item  *dictionary.Item
}

// pickBestTreasureAward 从候选中按方案 A 规则选出最优项。
func pickBestTreasureAward(candidates []treasureAwardCandidate) (treasureAwardCandidate, bool) {
	var best treasureAwardCandidate
	found := false
	for _, c := range candidates {
		if !found || betterTreasureAward(c.award, c.item, best.award, best.item) {
			best = c
			found = true
		}
	}
	return best, found
}

// SelectTreasureAwardsByNeed 依据账号养成需求分选择三档奖励。
//
// 需求分由 EvaluateAwardNeed 结合实机面板 (法阵/龙珠/血脉/藏经阁/丹魂/圣器/
// 兵法/混沌虚空/幻兽) 与道具持有量计算；同分时回退稀有度/价值序。
func SelectTreasureAwardsByNeed(
	offered *protocol.TreasureHousePool,
	lookup func(int32) (*dictionary.Item, error),
	isFuncOpen func(int32) bool,
	needs map[int32]AwardNeed,
) []protocol.TreasureAwardSelect {
	tiers := []struct {
		tier    int32
		options []int32
	}{
		{1, offered.Tier1Items},
		{2, offered.Tier2Items},
		{3, offered.Tier3Items},
	}

	resolved := make([]protocol.TreasureAwardSelect, 0, len(tiers))
	for _, t := range tiers {
		var best *treasureAwardCandidate
		var bestNeed AwardNeed
		var skipped []string
		for _, id := range t.options {
			award, ok := protocol.TreasureAwardByID(id)
			if !ok {
				continue
			}
			if isFuncOpen != nil && award.FuncID != 0 && !isFuncOpen(award.FuncID) {
				skipped = append(skipped, fmt.Sprintf("%d(func=%d)", award.ID, award.FuncID))
				continue
			}
			var item *dictionary.Item
			if lookup != nil {
				if it, err := lookup(award.ItemID); err == nil {
					item = it
				}
			}
			cand := treasureAwardCandidate{award: award, item: item}
			need := needs[id]
			if best == nil || need.Score > bestNeed.Score ||
				(need.Score == bestNeed.Score && betterTreasureAward(cand.award, cand.item, best.award, best.item)) {
				candCopy := cand
				best = &candCopy
				bestNeed = need
			}
		}

		if best == nil {
			if len(t.options) > 0 {
				resolved = append(resolved, protocol.TreasureAwardSelect{TierID: t.tier, ItemID: t.options[0]})
				slog.Warn("天帝宝库该档无可用的动态选奖候选，退回首个开放候选",
					"tier", t.tier, "award_id", t.options[0])
			}
			continue
		}

		itemName := ""
		if best.item != nil {
			itemName = best.item.Name
		}
		slog.Info("天帝宝库按账号需求选定大奖",
			"tier", t.tier,
			"tier_quality", protocol.TreasureAwardQualityName(best.award.Quality),
			"award_id", best.award.ID,
			"item_id", best.award.ItemID,
			"item_name", itemName,
			"item_num", best.award.ItemNum,
			"need_score", bestNeed.Score,
			"reason", bestNeed.Reason,
			"skipped_function_closed", skipped,
			"candidates", t.options,
		)
		resolved = append(resolved, protocol.TreasureAwardSelect{TierID: t.tier, ItemID: best.award.ID})
	}
	return resolved
}

// SelectTreasureAwardsByRarity 按商品稀有度与方案 A 价值序为三档各选一个开放候选。
//
// 服务端 select_panel 只下发档位与候选奖励 id，奖励 id 对应的道具、数量与品质
// 必须查官方 TreasureAward 表；道具元数据 (品质/类型/名称) 取自静态字典。
//
// isFuncOpen 用于过滤奖励所属功能未开放的候选 (FuncID==0 视为通用道具，始终保留)；
// 传 nil 表示不做功能过滤。若某档全部候选都被过滤，则按稀有度兜底，保证 set_award 能完成。
// pickPreferredTreasureAward 按优先清单为指定档位选择奖励。
// preferred 中的奖励条目必须属于该档、在当前开放候选中且所属功能已开放。
func pickPreferredTreasureAward(tier int32, options, preferred []int32, isFuncOpen func(int32) bool) (protocol.TreasureAward, bool) {
	inTier := make(map[int32]bool, len(options))
	for _, id := range options {
		inTier[id] = true
	}
	for _, id := range preferred {
		if !inTier[id] {
			continue
		}
		award, ok := protocol.TreasureAwardByID(id)
		if !ok || award.Quality != tier {
			continue
		}
		if isFuncOpen != nil && award.FuncID != 0 && !isFuncOpen(award.FuncID) {
			continue
		}
		return award, true
	}
	return protocol.TreasureAward{}, false
}

func SelectTreasureAwardsByRarity(
	offered *protocol.TreasureHousePool,
	lookup func(int32) (*dictionary.Item, error),
	isFuncOpen func(int32) bool,
	preferred []int32,
) []protocol.TreasureAwardSelect {
	tiers := []struct {
		tier    int32
		options []int32
	}{
		{1, offered.Tier1Items},
		{2, offered.Tier2Items},
		{3, offered.Tier3Items},
	}

	resolved := make([]protocol.TreasureAwardSelect, 0, len(tiers))
	for _, t := range tiers {
		// 1. 优先清单: 账号当前最缺且仍在候选池中的奖励直接命中。
		if preferredAward, ok := pickPreferredTreasureAward(t.tier, t.options, preferred, isFuncOpen); ok {
			var preferredItem *dictionary.Item
			if lookup != nil {
				if it, err := lookup(preferredAward.ItemID); err == nil {
					preferredItem = it
				}
			}
			itemName := ""
			if preferredItem != nil {
				itemName = preferredItem.Name
			}
			slog.Info("天帝宝库按优先清单选定大奖",
				"tier", t.tier,
				"tier_quality", protocol.TreasureAwardQualityName(preferredAward.Quality),
				"award_id", preferredAward.ID,
				"item_id", preferredAward.ItemID,
				"item_name", itemName,
				"item_num", preferredAward.ItemNum,
				"func_id", preferredAward.FuncID,
				"preferred", preferred,
			)
			resolved = append(resolved, protocol.TreasureAwardSelect{TierID: t.tier, ItemID: preferredAward.ID})
			continue
		}

		// 2. 常规: 按功能开放过滤 + 稀有度/价值序选择。
		var available, fallback []treasureAwardCandidate
		var skipped []string

		for _, id := range t.options {
			award, ok := protocol.TreasureAwardByID(id)
			if !ok {
				slog.Warn("天帝宝库候选奖励未收录于官方表，跳过",
					"tier", t.tier, "award_id", id)
				continue
			}
			var item *dictionary.Item
			if lookup != nil {
				if it, err := lookup(award.ItemID); err == nil {
					item = it
				}
			}
			cand := treasureAwardCandidate{award: award, item: item}
			fallback = append(fallback, cand)
			if isFuncOpen != nil && award.FuncID != 0 && !isFuncOpen(award.FuncID) {
				skipped = append(skipped, fmt.Sprintf("%d(func=%d)", award.ID, award.FuncID))
				continue
			}
			available = append(available, cand)
		}

		pool := available
		if len(pool) == 0 && len(fallback) > 0 {
			slog.Warn("天帝宝库该档候选对应功能均未开放，按稀有度兜底",
				"tier", t.tier, "skipped", skipped)
			pool = fallback
		} else if len(skipped) > 0 {
			slog.Info("天帝宝库跳过所属功能未开放的候选",
				"tier", t.tier, "skipped", skipped)
		}

		best, found := pickBestTreasureAward(pool)
		if !found {
			// 官方表未收录任何候选时，退回服务端首个开放候选，保持流程可用。
			if len(t.options) > 0 {
				resolved = append(resolved, protocol.TreasureAwardSelect{TierID: t.tier, ItemID: t.options[0]})
				slog.Warn("天帝宝库该档无官方奖励元数据，退回首个开放候选",
					"tier", t.tier, "award_id", t.options[0])
			}
			continue
		}

		itemName := ""
		if best.item != nil {
			itemName = best.item.Name
		}
		slog.Info("天帝宝库按稀有度选定大奖",
			"tier", t.tier,
			"tier_quality", protocol.TreasureAwardQualityName(best.award.Quality),
			"award_id", best.award.ID,
			"item_id", best.award.ItemID,
			"item_name", itemName,
			"item_num", best.award.ItemNum,
			"item_quality", treasureItemQuality(best.item),
			"type_priority", treasureItemTypePriority(treasureItemTypeID(best.item)),
			"func_id", best.award.FuncID,
			"skipped_function_closed", skipped,
			"candidates", t.options,
		)
		resolved = append(resolved, protocol.TreasureAwardSelect{TierID: t.tier, ItemID: best.award.ID})
	}
	return resolved
}

// resolveTreasureAwards 解析最终三档自选大奖:
// 优先采用配置值；若配置值不在当前开放候选中，则回退到主面板当前自选；最后回退到该档首个开放候选。
func resolveTreasureAwards(configured, current []protocol.TreasureAwardSelect, offered *protocol.TreasureHousePool) []protocol.TreasureAwardSelect {
	configuredMap := make(map[int32]int32, len(configured))
	for _, a := range configured {
		configuredMap[a.TierID] = a.ItemID
	}
	currentMap := make(map[int32]int32, len(current))
	for _, a := range current {
		currentMap[a.TierID] = a.ItemID
	}

	tiers := []struct {
		tier    int32
		options []int32
	}{
		{1, offered.Tier1Items},
		{2, offered.Tier2Items},
		{3, offered.Tier3Items},
	}

	resolved := make([]protocol.TreasureAwardSelect, 0, len(tiers))
	for _, t := range tiers {
		id := configuredMap[t.tier]
		if len(t.options) > 0 && !containsAwardID(t.options, id) {
			if cur := currentMap[t.tier]; containsAwardID(t.options, cur) {
				id = cur
			} else {
				id = t.options[0]
			}
		}
		if id == 0 && len(t.options) > 0 {
			id = t.options[0]
		}
		resolved = append(resolved, protocol.TreasureAwardSelect{TierID: t.tier, ItemID: id})
	}
	return resolved
}

// --------------------------------------------------------------------------------
// 喜从天降 / 五福临门 (Mod_FindImmortal_Base, Module 68)
// --------------------------------------------------------------------------------
