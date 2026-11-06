// Package main 验证按角色隔离的玩法开关管理器。
//
// @author Ateng
// @since 2026-11-06
package main

import (
	"testing"

	"sxd-pie-ng/internal/routines"
	"sxd-pie-ng/internal/scheduler"
	"sxd-pie-ng/internal/web"
)

func newTestRoleScheduler(t *testing.T, roleID string) *scheduler.RoleScheduler {
	t.Helper()
	return scheduler.NewRoleScheduler(roleID, nil, scheduler.NewJitter(scheduler.JitterConfig{}))
}

func TestRoutineManager_PerRoleToggleIsolation(t *testing.T) {
	factory := routines.NewRegistryFactory(nil, nil)
	s0 := newTestRoleScheduler(t, "s0-遁一")
	s1 := newTestRoleScheduler(t, "s1-小号")

	enabled0 := map[string]bool{"treasure_house": true, "arena": false}
	enabled1 := map[string]bool{"treasure_house": false, "arena": false}
	factory.RegisterAll(s0, enabled0)
	factory.RegisterAll(s1, enabled1)

	rm := newRoutineManager(map[string]bool{"treasure_house": true})
	rm.registerRole("s0-遁一", "s0", s0, enabled0, factory)
	rm.registerRole("s1-小号", "s1", s1, enabled1, factory)

	// 聚合视图：任一角色启用即视为运行中，并给出每角色明细。
	agg := findRoutine(t, rm.ListRoutines(""), "treasure_house")
	if !agg.Enabled {
		t.Error("expected aggregate treasure_house enabled because s0 has it on")
	}
	if len(agg.Roles) != 2 {
		t.Fatalf("expected 2 role details, got %d", len(agg.Roles))
	}

	// 仅切换 s1，s0 不受影响。
	if err := rm.ToggleRoutine("s1-小号", "treasure_house", true); err != nil {
		t.Fatalf("toggle s1 failed: %v", err)
	}
	agg = findRoutine(t, rm.ListRoutines(""), "treasure_house")
	for _, r := range agg.Roles {
		if !r.Enabled {
			t.Errorf("expected role %s enabled after toggling s1", r.RoleID)
		}
	}

	// 全局开关同步所有角色。
	if err := rm.ToggleRoutine("", "arena", true); err != nil {
		t.Fatalf("global toggle failed: %v", err)
	}
	for _, roleID := range []string{"s0-遁一", "s1-小号"} {
		arena := findRoutine(t, rm.ListRoutines(roleID), "arena")
		if !arena.Enabled {
			t.Errorf("expected arena enabled for %s after global toggle", roleID)
		}
	}
}

func TestRoutineManager_RoleQueryAndUnknownRole(t *testing.T) {
	factory := routines.NewRegistryFactory(nil, nil)
	s0 := newTestRoleScheduler(t, "s0-遁一")
	rm := newRoutineManager(nil)
	rm.registerRole("s0-遁一", "s0", s0, map[string]bool{"herb_garden": true}, factory)

	list := rm.ListRoutines("s0-遁一")
	herb := findRoutine(t, list, "herb_garden")
	if !herb.Enabled {
		t.Error("expected herb_garden enabled for s0")
	}
	if len(herb.Roles) != 0 {
		t.Error("role-specific query should not include aggregate Roles detail")
	}
	if got := rm.ListRoutines("missing-role"); len(got) != 0 {
		t.Errorf("expected empty list for unknown role, got %d entries", len(got))
	}
	if err := rm.ToggleRoutine("missing-role", "herb_garden", true); err == nil {
		t.Error("expected error when toggling unknown role")
	}
}

func TestRoutineManager_EnableRegistersRoutineAtRuntime(t *testing.T) {
	factory := routines.NewRegistryFactory(nil, nil)
	sched := newTestRoleScheduler(t, "s0-遁一")
	enabled := map[string]bool{"herb_garden": true, "arena": false}
	factory.RegisterAll(sched, enabled)

	rm := newRoutineManager(enabled)
	rm.registerRole("s0-遁一", "s0", sched, enabled, factory)

	if _, _, _, found := sched.GetRoutineState("arena"); found {
		t.Fatal("arena should not be registered while disabled at startup")
	}
	if err := rm.ToggleRoutine("s0-遁一", "arena", true); err != nil {
		t.Fatalf("toggle arena failed: %v", err)
	}
	if _, _, _, found := sched.GetRoutineState("arena"); !found {
		t.Fatal("expected arena to be registered dynamically after enabling")
	}
}

func TestRoutineManager_ApplyPresetPerRole(t *testing.T) {
	factory := routines.NewRegistryFactory(nil, nil)
	s0 := newTestRoleScheduler(t, "s0-遁一")
	s1 := newTestRoleScheduler(t, "s1-小号")
	rm := newRoutineManager(nil)
	rm.registerRole("s0-遁一", "s0", s0, map[string]bool{}, factory)
	rm.registerRole("s1-小号", "s1", s1, map[string]bool{}, factory)

	if err := rm.ApplyPreset("s0-遁一", "all"); err != nil {
		t.Fatalf("apply preset s0 failed: %v", err)
	}
	if !findRoutine(t, rm.ListRoutines("s0-遁一"), "arena").Enabled {
		t.Error("expected all routines enabled for s0")
	}
	if findRoutine(t, rm.ListRoutines("s1-小号"), "arena").Enabled {
		t.Error("expected s1 untouched by role-specific preset")
	}

	if err := rm.ApplyPreset("", "none"); err != nil {
		t.Fatalf("apply global preset failed: %v", err)
	}
	if findRoutine(t, rm.ListRoutines("s0-遁一"), "arena").Enabled {
		t.Error("expected arena disabled after global none preset")
	}
	if err := rm.ApplyPreset("", "bogus"); err == nil {
		t.Error("expected error for unknown preset")
	}
}

func findRoutine(t *testing.T, list []web.RoutineStatus, id string) web.RoutineStatus {
	t.Helper()
	for _, r := range list {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("routine %q not found in list", id)
	return web.RoutineStatus{}
}
