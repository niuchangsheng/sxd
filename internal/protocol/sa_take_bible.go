// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现圣域取经路况解析 (Mod_SaTakeBible_Base.open_take_bible, Module 395 / 0x018B)。
//
// 圣域取经与仙界取经报文大体一致，但 open_take_bible 的队伍条目多出 2 个 Byte，
// 尾部少 1 个 Byte；直接复用仙界解析器会在 last_rob 列表处错位。
package protocol

import "fmt"

// ParseSaTakeBibleRoadResult 解析圣域取经路况快照。
//
// 官方 schema:
//
//	[ [Int player_id, UByte protection, Int start_time, Int arrival_time, Short distance,
//	   Int protect_player_id, Byte sequence_id, Byte is_deeds_so, Byte extra1, Byte extra2],
//	  Int protect_player_id, Short can_rob_times, Byte can_protect_times,
//	  Byte can_take_bible_times, Byte quickly_times, UByte type, Int time,
//	  [Byte seq, Short remain_recapture_time, String last_rob_nickname,
//	   String last_rob_stage_name, String last_rob_server_name, Short rob_role_id],
//	  Byte buff, Byte extra ]
func ParseSaTakeBibleRoadResult(payload []byte) (*StTakeBibleCrossRoadInfo, error) {
	r := NewReader(payload)
	res := &StTakeBibleCrossRoadInfo{}

	teamCount, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read sa road team count: %v", ErrInvalidPayload, err)
	}
	res.Teams = make([]StTakeBibleCrossRoadTeam, 0, teamCount)
	for i := 0; i < int(teamCount); i++ {
		var t StTakeBibleCrossRoadTeam
		if t.PlayerID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read sa road_team[%d].player_id: %v", ErrInvalidPayload, i, err)
		}
		if t.Protection, err = r.ReadUint8(); err != nil {
			return nil, fmt.Errorf("%w: failed to read sa road_team[%d].protection: %v", ErrInvalidPayload, i, err)
		}
		if t.StartTime, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read sa road_team[%d].start_time: %v", ErrInvalidPayload, i, err)
		}
		if t.ArrivalTime, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read sa road_team[%d].arrival_time: %v", ErrInvalidPayload, i, err)
		}
		if t.Distance, err = r.ReadInt16(); err != nil {
			return nil, fmt.Errorf("%w: failed to read sa road_team[%d].distance: %v", ErrInvalidPayload, i, err)
		}
		if t.ProtectPlayerID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read sa road_team[%d].protect_player_id: %v", ErrInvalidPayload, i, err)
		}
		if t.SequenceID, err = r.ReadInt8(); err != nil {
			return nil, fmt.Errorf("%w: failed to read sa road_team[%d].sequence_id: %v", ErrInvalidPayload, i, err)
		}
		if t.IsDeedsSo, err = r.ReadInt8(); err != nil {
			return nil, fmt.Errorf("%w: failed to read sa road_team[%d].is_deeds_so: %v", ErrInvalidPayload, i, err)
		}
		// 圣域版本额外携带两个标志位 (是否本队/敌对状态)，自动化不消费。
		if _, err = r.ReadUint8(); err != nil {
			return nil, fmt.Errorf("%w: failed to read sa road_team[%d].extra1: %v", ErrInvalidPayload, i, err)
		}
		if _, err = r.ReadUint8(); err != nil {
			return nil, fmt.Errorf("%w: failed to read sa road_team[%d].extra2: %v", ErrInvalidPayload, i, err)
		}
		res.Teams = append(res.Teams, t)
	}

	if res.ProtectPlayerID, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read sa road protect_player_id: %v", ErrInvalidPayload, err)
	}
	if res.CanRobTimes, err = r.ReadInt16(); err != nil {
		return nil, fmt.Errorf("%w: failed to read sa road can_rob_times: %v", ErrInvalidPayload, err)
	}
	if res.CanProtectTimes, err = r.ReadInt8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read sa road can_protect_times: %v", ErrInvalidPayload, err)
	}
	if res.CanTakeBibleTimes, err = r.ReadInt8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read sa road can_take_bible_times: %v", ErrInvalidPayload, err)
	}
	if res.QuicklyTimes, err = r.ReadInt8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read sa road quickly_times: %v", ErrInvalidPayload, err)
	}
	if res.Type, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read sa road type: %v", ErrInvalidPayload, err)
	}
	if res.Time, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read sa road time: %v", ErrInvalidPayload, err)
	}

	lastRobCount, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read sa last rob count: %v", ErrInvalidPayload, err)
	}
	res.LastRobs = make([]StTakeBibleCrossLastRob, 0, lastRobCount)
	for i := 0; i < int(lastRobCount); i++ {
		var l StTakeBibleCrossLastRob
		if l.SequenceID, err = r.ReadInt8(); err != nil {
			return nil, fmt.Errorf("%w: failed to read sa last_rob[%d].sequence_id: %v", ErrInvalidPayload, i, err)
		}
		if l.RemainRecaptureTime, err = r.ReadInt16(); err != nil {
			return nil, fmt.Errorf("%w: failed to read sa last_rob[%d].remain_recapture_time: %v", ErrInvalidPayload, i, err)
		}
		if l.LastRobNickname, err = r.ReadString(); err != nil {
			return nil, fmt.Errorf("%w: failed to read sa last_rob[%d].nickname: %v", ErrInvalidPayload, i, err)
		}
		if l.LastRobStageName, err = r.ReadString(); err != nil {
			return nil, fmt.Errorf("%w: failed to read sa last_rob[%d].stage: %v", ErrInvalidPayload, i, err)
		}
		if l.LastRobServerName, err = r.ReadString(); err != nil {
			return nil, fmt.Errorf("%w: failed to read sa last_rob[%d].server: %v", ErrInvalidPayload, i, err)
		}
		if l.RobRoleID, err = r.ReadInt16(); err != nil {
			return nil, fmt.Errorf("%w: failed to read sa last_rob[%d].role_id: %v", ErrInvalidPayload, i, err)
		}
		res.LastRobs = append(res.LastRobs, l)
	}
	if res.Buff, err = r.ReadInt8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read sa road buff: %v", ErrInvalidPayload, err)
	}
	// 尾部还可能有 1 个扩展 Byte，存在则消费，不存在不视为错误。
	_, _ = r.ReadInt8()
	return res, nil
}
