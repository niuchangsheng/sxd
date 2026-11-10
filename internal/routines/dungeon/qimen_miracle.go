// 本文件实现奇门遁甲一/二 (Mod_MiracleFighters_Base / Mod_MiracleFightersTwo_Base)
// 的普通修炼与突破试炼。旧实现误用 486/485/584（法阵/法力星图/打坐），
// 已由本实现替换。
package dungeon

import (
	"context"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

type miracleFightersIDs struct {
	label     string
	info      uint32
	practice  uint32
	breakThru uint32
	oneKey    uint32
}

var (
	miracleFightersOneIDs = miracleFightersIDs{
		label:     "奇门遁甲一",
		info:      protocol.ActionIDMiracleFightersInfo,
		practice:  protocol.ActionIDMiracleFightersPractice,
		breakThru: protocol.ActionIDMiracleFightersBreakThru,
		oneKey:    protocol.ActionIDMiracleFightersOneKey,
	}
	miracleFightersTwoIDs = miracleFightersIDs{
		label:     "奇门遁甲二",
		info:      protocol.ActionIDMiracleFightersTwoInfo,
		practice:  protocol.ActionIDMiracleFightersTwoPractice,
		breakThru: protocol.ActionIDMiracleFightersTwoBreakThru,
		oneKey:    protocol.ActionIDMiracleFightersTwoOneKey,
	}
)

// 审计文档中历史探针使用过的 6 个条目编号；面板返回条目为空时作为兜底。
var miracleFightersFallbackEntries = []int32{587, 371, 2420, 73, 4059, 784}

func newQiMenMiracleRoutine() scheduler.ActivityRoutine {
	const callTimeout = 10 * time.Second
	return scheduler.NewBaseRoutine(
		"qimen_dunjia",
		"dungeon",
		scheduler.ScheduleCron,
		scheduler.PriorityNormal,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}
			open, fErr := session.IsFunctionOpen(ctx, protocol.FunctionIDMiracleFighters)
			if fErr != nil {
				slog.Info("奇门遁甲功能开放状态查询失败，跳过", "role_id", session.RoleID(), "err", fErr)
				return nil
			}
			if !open {
				slog.Info("奇门遁甲功能未开放，跳过", "role_id", session.RoleID(),
					"function_id", protocol.FunctionIDMiracleFighters)
				return nil
			}
			slog.Info("正在执行玩法任务: 奇门遁甲一/二修炼与突破", "role_id", session.RoleID())
			qimenPveChallenge(ctx, session)

			call := func(pkt *protocol.Packet, act uint32) *protocol.Packet {
				cCtx, cancel := context.WithTimeout(ctx, callTimeout)
				defer cancel()
				resp, err := session.Call(cCtx, pkt, act)
				if err != nil {
					slog.Info("奇门遁甲协议无回包", "role_id", session.RoleID(),
						"action", act, "err", err)
					return nil
				}
				return resp
			}

			for _, ids := range []miracleFightersIDs{miracleFightersOneIDs, miracleFightersTwoIDs} {
				runMiracleFightersModule(ctx, session, call, ids)
				if ctx.Err() != nil {
					return nil
				}
			}
			return nil
		},
	)
}

func runMiracleFightersModule(
	ctx context.Context,
	session *client.RoleSession,
	call func(*protocol.Packet, uint32) *protocol.Packet,
	ids miracleFightersIDs,
) {
	infoResp := call(protocol.BuildMiracleFightersInfoPacket(ids.info), ids.info)
	if infoResp == nil {
		slog.Info(ids.label+" 面板无回包，跳过", "role_id", session.RoleID())
		return
	}
	info, err := protocol.ParseMiracleFightersInfo(infoResp.Payload)
	if err != nil {
		slog.Warn(ids.label+" 面板解析失败", "role_id", session.RoleID(), "err", err)
		return
	}
	slog.Info(ids.label+" 面板",
		"role_id", session.RoleID(),
		"result", info.Result,
		"value", info.Value,
		"flag", info.Flag,
		"entries", info.Entries,
	)
	if info.Result != protocol.MiracleFightersResultSuccess {
		slog.Info(ids.label+" 暂不可用",
			"role_id", session.RoleID(),
			"result", info.Result,
			"result_text", protocol.MiracleFightersResultText(info.Result))
		return
	}

	entries := info.Entries
	if len(entries) == 0 {
		entries = miracleFightersFallbackEntries
	}

	// 每个模块只尝试一个条目的一次一键修炼 + 一次突破，避免重复刷请求；
	// 失败码按语义继续或结束。
	for _, entryID := range entries {
		if ctx.Err() != nil {
			return
		}
		if resp := call(protocol.BuildMiracleFightersOneKeyPacket(ids.oneKey, entryID), ids.oneKey); resp != nil {
			code, pErr := protocol.ParseMiracleFightersActionCode(resp.Payload)
			if pErr != nil {
				slog.Warn(ids.label+" 一键修炼回包解析失败", "role_id", session.RoleID(), "err", pErr)
				return
			}
			switch code {
			case protocol.MiracleFightersResultSuccess:
				slog.Info(ids.label+" 一键修炼成功", "role_id", session.RoleID(), "entry_id", entryID)
			case protocol.MiracleFightersResultTimesLimit:
				slog.Info(ids.label+" 修炼次数已用尽", "role_id", session.RoleID())
				return
			case protocol.MiracleFightersResultLevelUnopen, protocol.MiracleFightersResultFailed,
				protocol.MiracleFightersResultLimit, protocol.MiracleFightersResultIdError:
				slog.Info(ids.label+" 条目暂不可修炼，尝试下一条目",
					"role_id", session.RoleID(), "entry_id", entryID, "result", code)
				continue
			case protocol.MiracleFightersResultFuncUnopen:
				slog.Info(ids.label+" 功能未开放", "role_id", session.RoleID())
				return
			default:
				slog.Info(ids.label+" 一键修炼未成功",
					"role_id", session.RoleID(), "entry_id", entryID,
					"result", code, "result_text", protocol.MiracleFightersResultText(code))
				// 元宝/资源不足等失败直接结束该模块。
				return
			}
		}

		// 突破试炼（免费）。
		if resp := call(protocol.BuildMiracleFightersBreakThroughPacket(ids.breakThru, entryID), ids.breakThru); resp != nil {
			code, pErr := protocol.ParseMiracleFightersActionCode(resp.Payload)
			if pErr != nil {
				slog.Warn(ids.label+" 突破回包解析失败", "role_id", session.RoleID(), "err", pErr)
				return
			}
			switch code {
			case protocol.MiracleFightersResultSuccess:
				slog.Info(ids.label+" 突破试炼成功", "role_id", session.RoleID(), "entry_id", entryID)
				return
			case protocol.MiracleFightersResultTimesLimit:
				slog.Info(ids.label+" 突破次数已用尽", "role_id", session.RoleID())
				return
			case protocol.MiracleFightersResultLevelUnopen, protocol.MiracleFightersResultFailed,
				protocol.MiracleFightersResultLimit, protocol.MiracleFightersResultIdError:
				slog.Info(ids.label+" 条目暂不可突破，尝试下一条目",
					"role_id", session.RoleID(), "entry_id", entryID, "result", code)
				continue
			case protocol.MiracleFightersResultFuncUnopen:
				slog.Info(ids.label+" 功能未开放", "role_id", session.RoleID())
				return
			default:
				slog.Info(ids.label+" 突破未成功",
					"role_id", session.RoleID(), "entry_id", entryID,
					"result", code, "result_text", protocol.MiracleFightersResultText(code))
				return
			}
		}
	}
	slog.Info(ids.label+" 未找到可修炼/突破的条目", "role_id", session.RoleID())
}

// qimenPveChallenge 发起奇门遁甲一/二的 PvE 挑战 (253/1、394/1)。
//
// 官方客户端“挑战”按钮直接调 fight（空载荷），战斗由服务端结算；
// 返回 COMPLETE_ALL(3) 表示已全部通关，FUNCTION_NO_OPEN(1)/LEVEL_LIMIT(2)/
// CD_TIME(4) 分别对应未开放/等级不足/冷却，均不消耗元宝。
func qimenPveChallenge(ctx context.Context, session *client.RoleSession) {
	targets := []struct {
		label string
		act   uint32
	}{
		{"奇门遁甲一", protocol.ActionIDMiracleFightersPveFight},
		{"奇门遁甲二", protocol.ActionIDMiracleFightersPveTwoFight},
	}
	for _, t := range targets {
		if ctx.Err() != nil {
			return
		}
		cCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
		resp, err := session.Call(cCtx, protocol.NewPacket(t.act, nil), t.act)
		cancel()
		if err != nil || resp == nil {
			slog.Info(t.label+"试炼挑战无回包", "role_id", session.RoleID(), "err", err)
			continue
		}
		if len(resp.Payload) == 0 {
			continue
		}
		switch resp.Payload[0] {
		case protocol.MiraclePveSuccess:
			slog.Info(t.label+"试炼挑战成功", "role_id", session.RoleID(), "report_len", len(resp.Payload))
		case protocol.MiraclePveCompleteAll:
			slog.Info(t.label+"试炼已全部通关", "role_id", session.RoleID())
		case protocol.MiraclePveFuncNoOpen:
			slog.Info(t.label+"试炼未开放", "role_id", session.RoleID())
		case protocol.MiraclePveLevelLimit:
			slog.Info(t.label+"试炼等级不足", "role_id", session.RoleID())
		case protocol.MiraclePveCDTime:
			slog.Info(t.label+"试炼冷却中", "role_id", session.RoleID())
		default:
			slog.Info(t.label+"试炼返回未知结果", "role_id", session.RoleID(), "result", resp.Payload[0])
		}
	}
}
