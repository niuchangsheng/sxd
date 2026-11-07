// Package daily 提供签到、邮件、在线奖励、绑定奖励与日常维护类玩法。
//
// 本文件实现孩子每日免费玩法：
//   - 每日活跃度任务 59「每日孩子学习」：学堂处于未完成状态时送孩子上学 (消耗培养金，不消耗元宝)；
//   - 每日活跃度任务 63「每日孩子任务」：领取已完成的孩子任务奖励。
//
// 线上实测：先调用活跃度面板 (Mod 0 Act 42) 会让同一会话后续的 Module 312/315
// 子面板请求被服务端静默丢弃；因此本任务把所有孩子操作放在前面，活跃度面板
// 只在最后做一次只读汇总。
package daily

import (
	"context"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// 孩子相关每日活跃度任务编号 (对齐官方 AssistantTypeData)。
const (
	// DailyTaskChildrenSchool 每日孩子学习
	DailyTaskChildrenSchool int32 = 59
	// DailyTaskChildrenMission 每日孩子任务
	DailyTaskChildrenMission int32 = 63
	// maxChildrenPerRun 单轮最多处理的孩子数量 (游戏最多 2 个)
	maxChildrenPerRun = 2
)

// childrenPriority 孩子日常调度优先级。
//
// 线上实测：同一会话调用过活跃度面板 (Mod 0 Act 42) 后，Module 312/315 的
// 子面板请求会被服务端静默丢弃。因此孩子任务必须排在所有会读取活跃度的
// 例程之前；天帝宝库 Act 4 对城镇上下文更敏感，使用 200 排在最前，孩子使用 120。
const childrenPriority scheduler.RoutinePriority = 120

// NewChildrenDailyRoutine 构造孩子每日学习与任务奖励领取任务。
//
// 全部动作只消耗培养金/免费次数，绝不调用任何元宝接口。
func NewChildrenDailyRoutine() scheduler.ActivityRoutine {
	const callTimeout = 10 * time.Second
	return scheduler.NewBaseRoutine(
		"children_daily",
		"daily",
		scheduler.ScheduleCron,
		childrenPriority,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}

			call := func(pkt *protocol.Packet, act uint32) *protocol.Packet {
				cCtx, cancel := context.WithTimeout(ctx, callTimeout)
				defer cancel()
				resp, err := session.Call(cCtx, pkt, act)
				if err != nil {
					slog.Info("孩子协议无回包", "role_id", session.RoleID(),
						"action", act, "err", err)
					return nil
				}
				return resp
			}

			// 1. 读取孩子主面板，获取孩子 role_id。
			infoResp := call(protocol.BuildChildrenInfoPacket(), protocol.ActionIDChildrenInfo)
			if infoResp == nil {
				slog.Info("孩子功能不可用或未开放，跳过", "role_id", session.RoleID())
				return nil
			}
			children, err := protocol.ParseChildrenPanel(infoResp.Payload)
			if err != nil {
				slog.Warn("解析孩子主面板失败，跳过", "role_id", session.RoleID(), "err", err)
				return nil
			}
			if children.Result != protocol.ChildrenResultSuccess {
				slog.Info("孩子主面板返回失败",
					"role_id", session.RoleID(),
					"result", children.Result,
					"result_text", protocol.ChildrenResultText(children.Result))
				return nil
			}
			if len(children.ChildIDs) == 0 {
				slog.Info("当前没有孩子，跳过", "role_id", session.RoleID())
				return nil
			}
			slog.Info("孩子面板解析完成",
				"role_id", session.RoleID(),
				"child_ids", children.ChildIDs)

			// 线上实测：孩子子面板请求紧跟主面板连续发送时会被静默丢弃，
			// 间隔一小段时间可稳定收到回包。
			time.Sleep(300 * time.Millisecond)

			for i, childID := range children.ChildIDs {
				if i >= maxChildrenPerRun || ctx.Err() != nil {
					break
				}
				// 线上实测：先调用孩子任务 (Module 315) 会让后续的学堂 (312)
				// 子面板请求被静默丢弃，因此学堂必须排在任务之前。
				attendChildrenSchool(ctx, session, childID, call)
				claimChildrenTasks(ctx, session, childID, call)
			}

			// 2. 只读汇总：孩子操作全部结束后再读活跃度，避免污染 312/315 会话状态。
			if actResp := call(protocol.BuildGameAssistantInfoPacket(), protocol.ActionIDGameAssistantInfo); actResp != nil {
				if panel, err := protocol.ParseGameAssistantInfo(actResp.Payload); err == nil {
					slog.Info("孩子日常完成",
						"role_id", session.RoleID(),
						"daily_activity", panel.DailyTotal(),
						"school_remain", panel.RemainingTimesFor(DailyTaskChildrenSchool),
						"mission_remain", panel.RemainingTimesFor(DailyTaskChildrenMission))
				}
			}
			return nil
		},
	)
}

// claimChildrenTasks 领取孩子已完成任务的奖励。
//
// 对未完成/已领取的任务服务端返回 NOT_COMPLETE/HAD_GET，均为无副作用分支。
func claimChildrenTasks(
	ctx context.Context,
	session *client.RoleSession,
	childID int32,
	call func(*protocol.Packet, uint32) *protocol.Packet,
) {
	resp := call(protocol.BuildChildrenTaskInfoPacket(childID), protocol.ActionIDChildrenTaskInfo)
	if resp == nil {
		return
	}
	tasks, err := protocol.ParseChildrenTaskInfo(resp.Payload)
	if err != nil {
		slog.Warn("解析孩子任务列表失败", "role_id", session.RoleID(), "child_id", childID, "err", err)
		return
	}
	for _, task := range tasks {
		if ctx.Err() != nil {
			return
		}
		awardResp := call(protocol.BuildChildrenTaskAwardPacket(childID, task.TaskID), protocol.ActionIDChildrenTaskAward)
		if awardResp == nil {
			continue
		}
		code, err := protocol.ParseChildrenResultCode(awardResp.Payload)
		if err != nil {
			continue
		}
		switch code {
		case protocol.ChildrenTaskResultSuccess:
			slog.Info("孩子任务奖励领取成功",
				"role_id", session.RoleID(), "child_id", childID, "task_id", task.TaskID)
		case protocol.ChildrenTaskResultNotComplete, protocol.ChildrenTaskResultHadGet,
			protocol.ChildrenTaskResultHadComplete:
			slog.Info("孩子任务暂不可领取",
				"role_id", session.RoleID(), "child_id", childID, "task_id", task.TaskID,
				"result", code, "result_text", protocol.ChildrenTaskResultText(code))
		default:
			slog.Info("孩子任务奖励领取未成功",
				"role_id", session.RoleID(), "child_id", childID, "task_id", task.TaskID,
				"result", code, "result_text", protocol.ChildrenTaskResultText(code))
		}
	}
}

// attendChildrenSchool 在学堂处于未完成状态时送孩子上学。
func attendChildrenSchool(
	ctx context.Context,
	session *client.RoleSession,
	childID int32,
	call func(*protocol.Packet, uint32) *protocol.Packet,
) {
	resp := call(protocol.BuildChildrenSchoolInfoPacket(childID), protocol.ActionIDChildrenSchoolInfo)
	if resp == nil {
		return
	}
	schools, err := protocol.ParseChildrenSchoolInfo(resp.Payload)
	if err != nil {
		slog.Warn("解析孩子学堂列表失败", "role_id", session.RoleID(), "child_id", childID, "err", err)
		return
	}
	for _, school := range schools {
		if ctx.Err() != nil {
			return
		}
		if school.State != protocol.ChildrenResultUndo {
			continue
		}
		attendResp := call(protocol.BuildChildrenAttendSchoolPacket(childID, school.SchoolID), protocol.ActionIDChildrenAttendSchool)
		if attendResp == nil {
			continue
		}
		code, err := protocol.ParseChildrenResultCode(attendResp.Payload)
		if err != nil {
			continue
		}
		switch code {
		case protocol.ChildrenResultSuccess:
			slog.Info("孩子上学成功",
				"role_id", session.RoleID(), "child_id", childID, "school_id", school.SchoolID)
			return
		case protocol.ChildrenResultOnSchool:
			slog.Info("孩子已在学堂", "role_id", session.RoleID(), "child_id", childID, "school_id", school.SchoolID)
			return
		case protocol.ChildrenResultLackOfGrowthGold:
			slog.Info("培养金不足，停止孩子上学", "role_id", session.RoleID(), "child_id", childID)
			return
		case protocol.ChildrenResultTimesLimit:
			slog.Info("孩子上学次数已达上限", "role_id", session.RoleID(), "child_id", childID)
			return
		default:
			slog.Info("孩子上学未成功",
				"role_id", session.RoleID(), "child_id", childID,
				"school_id", school.SchoolID, "result", code,
				"result_text", protocol.ChildrenResultText(code))
		}
	}
}
