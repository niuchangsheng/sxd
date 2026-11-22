// Package client 提供特定区服角色的独立 TCP Socket 长连接会话、登录态维护与心跳保活状态机。
//
// 本文件承载 PlayerState 定义、本地角色资产快照读写、属性推送解析、
// 角色全量信息拉取 (FetchLatestPlayerInfo) 与任务列表同步 (SyncQuests)。
package client

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/protocol"
)

// PlayerState 纳管角色在游戏世界中的动态属性与资源状态。
type PlayerState struct {
	Level        int   `json:"level"`
	VIP          int   `json:"vip"`
	Stamina      int   `json:"stamina"`
	MaxStamina   int   `json:"max_stamina"`
	ExtraStamina int   `json:"extra_stamina"`
	Coins        int64 `json:"coins"`
	Ingots       int64 `json:"ingots"`
	StateSource  int64 `json:"state_source"`
	BagCapacity  int   `json:"bag_capacity"`
	TownID       int   `json:"town_id"`
	// TownMapID 角色当前所在城镇地图编号 (Mod_Town_Base.enter_town 入参)。
	TownMapID      int               `json:"town_map_id"`
	Quests         map[uint32]uint32 `json:"quests"`
	IsPracticing   bool              `json:"is_practicing"`
	PracticeExp    int64             `json:"practice_exp"`
	Experience     int64             `json:"experience"`
	MaxExperience  int64             `json:"max_experience"`
	PlayerID       int32             `json:"player_id"`
	OriginNickName string            `json:"origin_nick_name"`
	// OnlineGiftTime 下一次在线礼包可领取的绝对 Unix 时间戳。
	// 服务端 prop 81 下发的是剩余秒数, 收到时按客户端同款逻辑换算为 now+value。
	OnlineGiftTime int64 `json:"online_gift_time"`
	// IsGetOnlineGift 在线礼包状态 (update_player_data prop 82): -2 未同步 / -1 无礼包 / 0 倒计时中 / 1 可领取。
	IsGetOnlineGift int `json:"is_get_online_gift"`
}

// RoleID 获取会话绑定的角色唯一标识符。
func (s *RoleSession) RoleID() string {
	return s.cfg.RoleID
}

// RoleName 获取会话绑定的角色显示名称。
func (s *RoleSession) RoleName() string {
	return s.cfg.RoleName
}

// PlayerID 获取角色在游戏服务端的数字标识 (PlayerID)。
func (s *RoleSession) PlayerID() int32 {
	s.playerMu.RLock()
	defer s.playerMu.RUnlock()
	return s.playerState.PlayerID
}

// OriginNickName 获取角色带区服后缀的官方完整名称 (例如 "曹植始.s1")。
func (s *RoleSession) OriginNickName() string {
	s.playerMu.RLock()
	defer s.playerMu.RUnlock()
	if s.playerState.OriginNickName != "" {
		return s.playerState.OriginNickName
	}
	return s.RoleName()
}

// GetPlayerState 获取角色当前快照状态副本。
func (s *RoleSession) GetPlayerState() PlayerState {
	s.playerMu.RLock()
	defer s.playerMu.RUnlock()
	state := s.playerState
	if state.Quests != nil {
		qCopy := make(map[uint32]uint32, len(state.Quests))
		for k, v := range state.Quests {
			qCopy[k] = v
		}
		state.Quests = qCopy
	}
	return state
}

// GetQuests 获取当前角色身上已接任务的快照 (QuestID -> State: 0-进行中, 1-可交)。
func (s *RoleSession) GetQuests() map[uint32]uint32 {
	s.playerMu.RLock()
	defer s.playerMu.RUnlock()
	res := make(map[uint32]uint32, len(s.playerState.Quests))
	for k, v := range s.playerState.Quests {
		res[k] = v
	}
	return res
}

// SetQuestState 更新指定任务的状态。
func (s *RoleSession) SetQuestState(questID uint32, state uint32) {
	s.playerMu.Lock()
	defer s.playerMu.Unlock()
	if s.playerState.Quests == nil {
		s.playerState.Quests = make(map[uint32]uint32)
	}
	s.playerState.Quests[questID] = state
}

// RemoveQuest 从身上移除已完成或放弃的任务。
func (s *RoleSession) RemoveQuest(questID uint32) {
	s.playerMu.Lock()
	defer s.playerMu.Unlock()
	delete(s.playerState.Quests, questID)
}

// GetStamina 获取当前总剩余体力 (基础体力 + 存储/额外体力)。
func (s *RoleSession) GetStamina() int {
	s.playerMu.RLock()
	defer s.playerMu.RUnlock()
	return s.playerState.Stamina + s.playerState.ExtraStamina
}

// GetBaseStamina 获取基础体力池数值 (基准上限 300)。
func (s *RoleSession) GetBaseStamina() int {
	s.playerMu.RLock()
	defer s.playerMu.RUnlock()
	return s.playerState.Stamina
}

// GetExtraStamina 获取存储/额外体力池数值。
func (s *RoleSession) GetExtraStamina() int {
	s.playerMu.RLock()
	defer s.playerMu.RUnlock()
	return s.playerState.ExtraStamina
}

// SetStamina 设置角色当前基础体力。
func (s *RoleSession) SetStamina(val int) {
	s.playerMu.Lock()
	defer s.playerMu.Unlock()
	s.playerState.Stamina = val
}

// SetExtraStamina 设置角色当前存储/额外体力。
func (s *RoleSession) SetExtraStamina(val int) {
	s.playerMu.Lock()
	defer s.playerMu.Unlock()
	s.playerState.ExtraStamina = val
}

// ConsumeStamina 扣除角色体力并返回扣除后的总剩余体力。
// 服务端扫荡遵循优先扣除存储/额外体力池，耗尽后再扣除基础体力池的业务机制。
func (s *RoleSession) ConsumeStamina(amount int) (int, error) {
	s.playerMu.Lock()
	defer s.playerMu.Unlock()
	total := s.playerState.Stamina + s.playerState.ExtraStamina
	if total < amount {
		return total, errors.New("client: stamina not enough")
	}
	if s.playerState.ExtraStamina >= amount {
		s.playerState.ExtraStamina -= amount
	} else {
		rem := amount - s.playerState.ExtraStamina
		s.playerState.ExtraStamina = 0
		s.playerState.Stamina -= rem
	}
	return s.playerState.Stamina + s.playerState.ExtraStamina, nil
}

// AddStamina 增加角色基础体力并返回增加后的总剩余体力。
func (s *RoleSession) AddStamina(amount int) int {
	s.playerMu.Lock()
	defer s.playerMu.Unlock()
	s.playerState.Stamina += amount
	return s.playerState.Stamina + s.playerState.ExtraStamina
}

// AddRewards 增加角色经验与铜钱收益。
func (s *RoleSession) AddRewards(exp, coins int64) {
	s.playerMu.Lock()
	defer s.playerMu.Unlock()
	s.playerState.Coins += coins
}

// UpdatePlayerState 线程安全地原子更新角色状态。
func (s *RoleSession) UpdatePlayerState(fn func(*PlayerState)) {
	s.playerMu.Lock()
	defer s.playerMu.Unlock()
	if fn != nil {
		fn(&s.playerState)
	}
}

// applyPlayerUpdateData 解析并应用服务器主动下发的角色属性变更 (Module 0, Action 3)。
//
// 2026 网关采用 [Uint16 条数][Uint8 属性ID][Int64 数值] 批量布局, 与旧版
// [Uint8 属性ID][Int32 数值] 重复布局完全不同。旧实现按 5 字节步长解析会把
// 条数头当成属性 ID, 将 0x03190000 之类的错位值写入等级/体力, 造成状态污染,
// 因此这里统一走 protocol.ParsePlayerUpdateData 并逐字段做合法性校验。
func (s *RoleSession) applyPlayerUpdateData(payload []byte) {
	updates, modern := protocol.ParsePlayerUpdateData(payload)
	if len(updates) == 0 {
		return
	}

	s.playerMu.Lock()
	defer s.playerMu.Unlock()

	for _, u := range updates {
		if !modern {
			s.applyLegacyPlayerProp(u)
			continue
		}
		switch u.Prop {
		case protocol.PlayerPropLevel2026:
			if u.Value >= 1 && u.Value <= 999 {
				s.playerState.Level = int(u.Value)
			}
		case protocol.PlayerPropIngot2026:
			if u.Value >= 0 && u.Value <= 1_000_000_000_000 {
				if s.playerState.Ingots != u.Value {
					slog.Info("元宝余额变更",
						"role_id", s.cfg.RoleID,
						"before", s.playerState.Ingots,
						"after", u.Value,
						"delta", u.Value-s.playerState.Ingots,
					)
				}
				s.playerState.Ingots = u.Value
			}
		case protocol.PlayerPropCoins2026:
			if u.Value >= 0 && u.Value <= 1_000_000_000_000_000 {
				if s.playerState.Coins != u.Value {
					slog.Info("铜钱余额变更",
						"role_id", s.cfg.RoleID,
						"before", s.playerState.Coins,
						"after", u.Value,
						"delta", u.Value-s.playerState.Coins,
					)
				}
				s.playerState.Coins = u.Value
			}
		case protocol.PlayerPropPower2026:
			if u.Value >= 0 && u.Value <= 1_000_000 {
				s.playerState.Stamina = int(u.Value)
			}
		case protocol.PlayerPropMaxPower2026:
			if u.Value >= 0 && u.Value <= 1_000_000 {
				s.playerState.MaxStamina = int(u.Value)
			}
		case protocol.PlayerPropExperience2026:
			if u.Value >= 0 {
				s.playerState.Experience = u.Value
				slog.Info("角色总经验同步更新", "role_id", s.cfg.RoleID, "experience", u.Value)
			}
		case protocol.PlayerPropMaxExperience2026:
			if u.Value >= 0 {
				s.playerState.MaxExperience = u.Value
				slog.Info("角色升级所需经验更新", "role_id", s.cfg.RoleID, "max_experience", u.Value)
			}
		case protocol.PlayerPropVIPLevel2026:
			if u.Value >= 0 && u.Value <= 100 {
				s.playerState.VIP = int(u.Value)
			}
		case protocol.PlayerPropPackEmptyNum2026:
			if u.Value >= 0 && u.Value <= 100_000 {
				s.playerState.BagCapacity = int(u.Value)
			}
		case protocol.PlayerPropOnlineGiftTime:
			if u.Value >= 0 {
				// 客户端 PlayerData 语义: onlineGiftTime = 剩余秒数 + 当前时间
				s.playerState.OnlineGiftTime = time.Now().Unix() + u.Value
			}
		case protocol.PlayerPropIsGetOnlineGift:
			if u.Value >= -1 && u.Value <= 1 {
				s.playerState.IsGetOnlineGift = int(u.Value)
			}
		}
	}
}

// applyLegacyPlayerProp 应用旧版 pie.exe 属性 ID 的变更, 仅供兼容布局使用。
func (s *RoleSession) applyLegacyPlayerProp(u protocol.PlayerPropUpdate) {
	switch u.Prop {
	case protocol.PlayerPropPower:
		if u.Value >= 0 && u.Value <= 1_000_000 {
			s.playerState.Stamina = int(u.Value)
		}
	case protocol.PlayerPropMaxPower:
		if u.Value >= 0 && u.Value <= 1_000_000 {
			s.playerState.MaxStamina = int(u.Value)
		}
	case protocol.PlayerPropCoins:
		if u.Value >= 0 {
			s.playerState.Coins = u.Value
		}
	case protocol.PlayerPropIngot:
		if u.Value >= 0 {
			s.playerState.Ingots = u.Value
		}
	case protocol.PlayerPropLevel:
		if u.Value >= 1 && u.Value <= 999 {
			s.playerState.Level = int(u.Value)
		}
	case protocol.PlayerPropExperience:
		if u.Value >= 0 {
			s.playerState.Experience = u.Value
		}
	case protocol.PlayerPropMaxExperience:
		if u.Value >= 0 {
			s.playerState.MaxExperience = u.Value
		}
	case protocol.PlayerPropVIPLevel:
		if u.Value >= 0 && u.Value <= 100 {
			s.playerState.VIP = int(u.Value)
		}
	case protocol.PlayerPropPackEmptyNum:
		if u.Value >= 0 && u.Value <= 100_000 {
			s.playerState.BagCapacity = int(u.Value)
		}
	}
}

// FetchLatestPlayerInfo 主动向服务端拉取最新全量角色资产（包括经验、体力、铜钱等）并刷新本地状态快照。
func (s *RoleSession) FetchLatestPlayerInfo(ctx context.Context) (*protocol.PlayerLoginResult, error) {
	pkt := protocol.BuildPlayerGetInfoPacket(0)
	timeout := s.callTimeout(8*time.Second, 300*time.Millisecond)
	fCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := s.Call(fCtx, pkt, protocol.ActionIDPlayerGetInfo)
	if err != nil {
		return nil, fmt.Errorf("fetch latest player info: %w", err)
	}
	if resp == nil {
		return nil, errors.New("empty player info response")
	}

	infoRes, err := protocol.ParsePlayerLoginResult(resp.Payload)
	if err != nil {
		return nil, fmt.Errorf("parse player info: %w", err)
	}

	s.UpdatePlayerState(func(ps *PlayerState) {
		if infoRes.Level > 0 {
			ps.Level = int(infoRes.Level)
		}
		if infoRes.HasCoins {
			ps.Coins = infoRes.Coins
		}
		if infoRes.HasIngots {
			ps.Ingots = int64(infoRes.Ingots)
		}
		if infoRes.HasStamina {
			ps.Stamina = int(infoRes.Stamina)
		}
		if infoRes.HasExtraStamina {
			ps.ExtraStamina = int(infoRes.ExtraStamina)
		}
		if infoRes.MaxStamina > 0 {
			ps.MaxStamina = int(infoRes.MaxStamina)
		}
		if infoRes.VIP > 0 {
			ps.VIP = int(infoRes.VIP)
		}
		if infoRes.Experience > 0 {
			ps.Experience = infoRes.Experience
		}
		if infoRes.MaxExperience > 0 {
			ps.MaxExperience = infoRes.MaxExperience
		}
		if infoRes.TownMapID > 0 {
			ps.TownMapID = int(infoRes.TownMapID)
		}
	})

	return infoRes, nil
}

// EnterTown 切换/复位角色当前城镇场景 (Module 1, Action 0)。
//

// SyncQuests 主动向服务端发送 Mod 3, Act 9 协议全量拉取当前已接任务列表并同步至内存状态快照。
func (s *RoleSession) SyncQuests(ctx context.Context) ([]protocol.PlayerQuestItem, error) {
	questListPkt := protocol.BuildGetPlayerQuestListPacket(s.LastActionID())
	timeout := s.callTimeout(8*time.Second, 300*time.Millisecond)
	qCtx, qCancel := context.WithTimeout(ctx, timeout)
	defer qCancel()

	qResp, qErr := s.Call(qCtx, questListPkt, protocol.ActionIDQuestList)
	if qErr != nil {
		return nil, qErr
	}
	if qResp == nil {
		return nil, errors.New("empty quest list response")
	}

	items, pErr := protocol.ParsePlayerQuestListResponse(qResp.Payload)
	if pErr != nil {
		return nil, pErr
	}

	s.playerMu.Lock()
	s.playerState.Quests = make(map[uint32]uint32, len(items))
	for _, it := range items {
		s.playerState.Quests[uint32(it.QuestID)] = uint32(it.State)
	}
	s.playerMu.Unlock()

	slog.Info("成功同步服务端全量任务列表", "role_id", s.RoleID(), "total_quests", len(items))
	return items, nil
}
