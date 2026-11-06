// Package main 提供神仙道助手服务端主入口与运行时生命周期管理。
//
// 本文件实现多角色/多区服隔离的玩法开关管理器。
//
// @author Ateng
// @since 2026-11-06
package main

import (
	"fmt"
	"sync"
	"time"

	"sxd-pie-ng/internal/routines"
	"sxd-pie-ng/internal/scheduler"
	"sxd-pie-ng/internal/web"
)

// roleRoutineState 单个角色的玩法开关与调度器状态。
type roleRoutineState struct {
	roleID   string
	serverID string
	enabled  map[string]bool
	sched    *scheduler.RoleScheduler
	factory  *routines.RegistryFactory
}

// routineManager 管理按角色隔离的玩法开关。
//
// 全局开关 (roleID 为空) 会同步到所有角色；指定 roleID 时只修改该角色，
// 避免 s0/s1 或同区服多角色的开关互相影响。
type routineManager struct {
	mu    sync.RWMutex
	base  map[string]bool
	roles map[string]*roleRoutineState
	order []string
}

func newRoutineManager(base map[string]bool) *routineManager {
	rm := &routineManager{
		base:  make(map[string]bool, len(base)),
		roles: make(map[string]*roleRoutineState),
	}
	for k, v := range base {
		rm.base[k] = v
	}
	return rm
}

// registerRole 登记一个已装配调度器的角色及其生效开关。
func (rm *routineManager) registerRole(
	roleID, serverID string,
	sched *scheduler.RoleScheduler,
	enabled map[string]bool,
	factory *routines.RegistryFactory,
) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if _, exists := rm.roles[roleID]; exists {
		return
	}
	copied := make(map[string]bool, len(enabled))
	for k, v := range enabled {
		copied[k] = v
	}
	rm.roles[roleID] = &roleRoutineState{
		roleID:   roleID,
		serverID: serverID,
		enabled:  copied,
		sched:    sched,
		factory:  factory,
	}
	rm.order = append(rm.order, roleID)
}

// ListRoutines 返回玩法状态。roleID 为空时返回聚合视图，并在 Roles 中给出各角色明细。
func (rm *routineManager) ListRoutines(roleID string) []web.RoutineStatus {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	defs := routines.GetAllDefinitions()
	if roleID != "" {
		st := rm.roles[roleID]
		if st == nil {
			return []web.RoutineStatus{}
		}
		res := make([]web.RoutineStatus, 0, len(defs))
		for _, d := range defs {
			enabled := routineEnabled(st, d)
			state, cooling := routineState(st, d.ID, enabled)
			res = append(res, web.RoutineStatus{
				ID:           d.ID,
				Name:         d.Name,
				Domain:       d.Domain,
				Schedule:     d.Schedule.String(),
				Description:  d.Description,
				Enabled:      enabled,
				State:        state,
				CoolingUntil: cooling,
			})
		}
		return res
	}

	res := make([]web.RoutineStatus, 0, len(defs))
	for _, d := range defs {
		enabledAny := false
		state := "Disabled"
		cooling := ""
		roleStates := make([]web.RoleRoutineStatus, 0, len(rm.order))

		for _, rid := range rm.order {
			st := rm.roles[rid]
			enabled := routineEnabled(st, d)
			roleState, roleCooling := routineState(st, d.ID, enabled)
			if enabled {
				enabledAny = true
			}
			if roleState == "Cooling" && cooling == "" {
				state = "Cooling"
				cooling = roleCooling
			} else if roleState == "Active" && state != "Cooling" {
				state = "Active"
			}
			roleStates = append(roleStates, web.RoleRoutineStatus{
				RoleID:       st.roleID,
				ServerID:     st.serverID,
				Enabled:      enabled,
				State:        roleState,
				CoolingUntil: roleCooling,
			})
		}

		if len(rm.order) == 0 {
			// 尚未装配任何角色时回退到全局默认开关。
			if v, ok := rm.base[d.ID]; ok {
				enabledAny = v
			} else {
				enabledAny = d.DefaultOn
			}
			if enabledAny {
				state = "Active"
			}
		}

		res = append(res, web.RoutineStatus{
			ID:           d.ID,
			Name:         d.Name,
			Domain:       d.Domain,
			Schedule:     d.Schedule.String(),
			Description:  d.Description,
			Enabled:      enabledAny,
			State:        state,
			CoolingUntil: cooling,
			Roles:        roleStates,
		})
	}
	return res
}

// ToggleRoutine 切换玩法开关。roleID 为空时对所有角色生效。
func (rm *routineManager) ToggleRoutine(roleID, id string, enabled bool) error {
	if id == "" {
		return fmt.Errorf("routine id 不能为空")
	}

	rm.mu.Lock()
	defer rm.mu.Unlock()

	if roleID != "" {
		st, ok := rm.roles[roleID]
		if !ok {
			return fmt.Errorf("未知角色 %q", roleID)
		}
		rm.applyToggleLocked(st, id, enabled)
		return nil
	}

	rm.base[id] = enabled
	for _, rid := range rm.order {
		rm.applyToggleLocked(rm.roles[rid], id, enabled)
	}
	return nil
}

// ApplyPreset 应用预设。roleID 为空时对所有角色生效。
func (rm *routineManager) ApplyPreset(roleID, preset string) error {
	defs := routines.GetAllDefinitions()

	valueFor := func(d routines.RoutineDefinition) (bool, error) {
		switch preset {
		case "all":
			return true, nil
		case "none":
			return false, nil
		case "recommended":
			return d.DefaultOn, nil
		default:
			return false, fmt.Errorf("未知预设 %q", preset)
		}
	}

	rm.mu.Lock()
	defer rm.mu.Unlock()

	if roleID != "" {
		st, ok := rm.roles[roleID]
		if !ok {
			return fmt.Errorf("未知角色 %q", roleID)
		}
		for _, d := range defs {
			v, err := valueFor(d)
			if err != nil {
				return err
			}
			rm.applyToggleLocked(st, d.ID, v)
		}
		return nil
	}

	for _, d := range defs {
		v, err := valueFor(d)
		if err != nil {
			return err
		}
		rm.base[d.ID] = v
		for _, rid := range rm.order {
			rm.applyToggleLocked(rm.roles[rid], d.ID, v)
		}
	}
	return nil
}

// applyToggleLocked 在持有 rm.mu 的前提下同步单个角色的开关。
// 若玩法在启动时未注册 (默认关闭)，首次开启时补注册，保证控制台开关真实生效。
func (rm *routineManager) applyToggleLocked(st *roleRoutineState, id string, enabled bool) {
	st.enabled[id] = enabled
	if st.sched == nil {
		return
	}
	if st.sched.SetRoutineEnabled(id, enabled) {
		return
	}
	if !enabled || st.factory == nil {
		return
	}
	rt := st.factory.CreateRoutine(id)
	if rt == nil {
		return
	}
	if err := st.sched.Register(rt); err == nil {
		_ = st.sched.SetRoutineEnabled(id, true)
	}
}

func routineEnabled(st *roleRoutineState, d routines.RoutineDefinition) bool {
	if st == nil {
		return d.DefaultOn
	}
	if v, ok := st.enabled[d.ID]; ok {
		return v
	}
	return d.DefaultOn
}

func routineState(st *roleRoutineState, id string, enabled bool) (string, string) {
	if !enabled {
		return "Disabled", ""
	}
	if st != nil && st.sched != nil {
		if _, _, coolingUntil, found := st.sched.GetRoutineState(id); found {
			if coolingUntil.After(time.Now()) {
				return "Cooling", coolingUntil.Format("15:04:05")
			}
		}
	}
	return "Active", ""
}
