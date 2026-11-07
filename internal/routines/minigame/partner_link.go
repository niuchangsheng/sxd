// Package minigame 提供吉星高照、仙旅奇缘答题、喜从天降、刮刮卡、钓鱼等益智趣味活动。
//
// 本文件实现「灵结」(Mod_PartnerLink_Base, Module 290) 自动化。
package minigame

import (
	"context"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// maxPartnerLinkActivations 单轮最多激活的灵结数量。
const maxPartnerLinkActivations = 10

// NewPartnerLinkRoutine 构造灵结日常任务。
//
// 官方 com.datas.PartnerLinkData: get_info 返回灵结等级、伙伴灵契与各灵结类型状态。
// 对 is_active=NO 的灵结调用 activate 激活；激活消耗伙伴灵契，不涉及元宝。
// 进阶 (advance) 可能涉及道缘/铜钱/元宝多种消耗路径，自动化暂不执行。
func NewPartnerLinkRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"partner_link",
		"minigame",
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
			slog.Info("正在执行益智任务: 灵结激活", "role_id", session.RoleID())

			// 先查功能开放列表；未开放时不再发送会被静默丢弃的灵结面板请求。
			open, fErr := session.IsFunctionOpen(ctx, protocol.FunctionIDPartnerLink)
			if fErr != nil {
				slog.Info("灵结功能开放状态查询失败，跳过", "role_id", session.RoleID(), "err", fErr)
				return nil
			}
			if !open {
				slog.Info("灵结功能未开放，跳过", "role_id", session.RoleID(), "function_id", protocol.FunctionIDPartnerLink)
				return nil
			}

			call := func(pkt *protocol.Packet, act uint32) (*protocol.Packet, error) {
				cCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				defer cancel()
				return session.Call(cCtx, pkt, act)
			}

			resp, err := call(protocol.BuildPartnerLinkInfoPacket(), protocol.ActionIDPartnerLinkInfo)
			if err != nil || resp == nil {
				slog.Info("灵结功能不可用或未开放，跳过", "role_id", session.RoleID(), "err", err)
				return nil
			}
			info, err := protocol.ParsePartnerLinkInfoResult(resp.Payload)
			if err != nil {
				slog.Info("解析灵结面板失败，跳过", "role_id", session.RoleID(), "err", err)
				return nil
			}
			slog.Info("灵结面板",
				"role_id", session.RoleID(),
				"level", info.Level,
				"partners", len(info.PartnerInfoList),
				"links", len(info.Links),
			)

			activated := 0
			for _, link := range info.Links {
				if link.IsActive == protocol.PartnerLinkYes {
					continue
				}
				if activated >= maxPartnerLinkActivations {
					break
				}
				if err := ctx.Err(); err != nil {
					return nil
				}
				aResp, aErr := call(
					protocol.BuildPartnerLinkActivatePacket(link.LinkTypeID),
					protocol.ActionIDPartnerLinkActivate,
				)
				if aErr != nil || aResp == nil {
					slog.Info("激活灵结无回包", "role_id", session.RoleID(), "link_type_id", link.LinkTypeID, "err", aErr)
					return nil
				}
				got, pErr := protocol.ParsePartnerLinkActivateResult(aResp.Payload)
				if pErr != nil {
					slog.Warn("解析灵结激活回包失败", "role_id", session.RoleID(), "err", pErr)
					return nil
				}
				switch got.Result {
				case protocol.PartnerLinkResultSuccess:
					activated++
					slog.Info("灵结激活成功",
						"role_id", session.RoleID(),
						"link_type_id", link.LinkTypeID,
						"level", got.Level,
					)
				case protocol.PartnerLinkResultLackOfLinkSoul:
					slog.Info("灵契不足，停止激活灵结", "role_id", session.RoleID())
					return nil
				case protocol.PartnerLinkResultFunctionNotOpen:
					slog.Info("灵结功能未开放", "role_id", session.RoleID())
					return nil
				case protocol.PartnerLinkResultNotEnoughIngot,
					protocol.PartnerLinkResultNotEnoughDaoYuan,
					protocol.PartnerLinkResultNotEnoughCoin:
					slog.Info("灵结激活资源不足，停止", "role_id", session.RoleID(), "result", got.Result)
					return nil
				case protocol.PartnerLinkResultFailed:
					slog.Info("灵结激活失败", "role_id", session.RoleID(), "link_type_id", link.LinkTypeID)
					return nil
				default:
					slog.Info("灵结激活未成功", "role_id", session.RoleID(), "link_type_id", link.LinkTypeID, "result", got.Result)
					return nil
				}
				select {
				case <-ctx.Done():
					return nil
				case <-time.After(200 * time.Millisecond):
				}
			}
			slog.Info("灵结巡检完成", "role_id", session.RoleID(), "activated", activated)
			return nil
		},
	)
}
