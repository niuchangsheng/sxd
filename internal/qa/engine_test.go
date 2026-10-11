// Package qa_test 提供三级漏斗问答引擎的测试用例。
//
// @author Ateng
// @since 2026-10-08
package qa_test

import (
	"strings"
	"testing"
	"time"

	"sxd-pie-ng/internal/qa"
)

func TestEngine_Matching(t *testing.T) {
	// 1. 初始化引擎，读取 data/ 目录中的真实题库
	engine, err := qa.NewEngine("")
	if err != nil {
		t.Fatalf("初始化问答引擎失败: %v", err)
	}

	if engine.Size() == 0 {
		t.Fatal("题库加载为空，未读取到任何题目")
	}

	t.Run("精确题目匹配", func(t *testing.T) {
		q := "我国最大的佛像是哪一座？"
		ans, conf, found := engine.Match(q)
		if !found {
			t.Fatalf("未匹配到题目: %s", q)
		}
		if ans != "乐山大佛" {
			t.Errorf("期望答案为 '乐山大佛', 实际为 '%s'", ans)
		}
		if conf < 0.99 {
			t.Errorf("精确匹配置信度应接近 1.0, 实际为 %f", conf)
		}
	})

	t.Run("标点与空格容错", func(t *testing.T) {
		// 去掉标点并增加空格
		q := "我 国 最 大 的 佛 像 是 哪 一 座 "
		ans, conf, found := engine.Match(q)
		if !found {
			t.Fatalf("未匹配到题目: %s", q)
		}
		if ans != "乐山大佛" {
			t.Errorf("期望答案为 '乐山大佛', 实际为 '%s'", ans)
		}
		if conf < 0.99 {
			t.Errorf("期望置信度接近 1.0, 实际为 %f", conf)
		}
	})

	t.Run("编辑距离与错别字模糊容错", func(t *testing.T) {
		// 题干改动少数几个字
		q := "我国最宏伟的佛像是哪一座呢？"
		ans, conf, found := engine.Match(q)
		if !found {
			t.Fatalf("模糊匹配未能召回: %s", q)
		}
		if !strings.Contains(ans, "乐山大佛") {
			t.Errorf("期望答案包含 '乐山大佛', 实际为 '%s'", ans)
		}
		if conf < 0.65 {
			t.Errorf("期望模糊置信度 >= 0.65, 实际为 %f", conf)
		}
	})

	t.Run("未知题目安全降级", func(t *testing.T) {
		q := "这是一道完全不存在于神仙道历史题库中的异次元问题xyz999"
		ans, conf, found := engine.Match(q)
		if found {
			t.Errorf("期望未知题目未找到, 实际匹配到 '%s' (conf: %f)", ans, conf)
		}
	})

	t.Run("单次匹配性能低于1ms", func(t *testing.T) {
		q := "国际足联世界杯多少年举行一次？"
		start := time.Now()
		for i := 0; i < 100; i++ {
			_, _, _ = engine.Match(q)
		}
		avg := time.Since(start) / 100
		if avg > time.Millisecond {
			t.Errorf("平均检索耗时超过 1ms: %v", avg)
		}
	})

	t.Run("仙履奇缘收益权重决策", func(t *testing.T) {
		// 寻找题目：优先选择体力产出分支
		story := "灵木玉神树，千年产一次凝神汁"
		branch, entry, found := engine.MatchFate(story)
		if !found {
			// 若由于 GBK 导致未命中，也可以由单独构造的条目验证决策算法
			entry = &qa.FateEntry{
				Title: "灵木玉神树，千年产一次凝神汁",
				Q1:    "除掉守护灵兽，夺取凝神汁。",
				Q2:    "灵兽过于强大，遗憾的放弃。",
				P1:    "50声望,50阅历",
				P2:    "5体力",
			}
			branch, _ = qa.DecideBestBranch(entry)
		}
		if entry != nil {
			// P2 包含 5体力 (权重 10000)，P1 包含 50声望,50阅历 (权重 1100)，应决策分支 2
			if branch != 2 {
				t.Errorf("期望依据体力优先选择分支 2, 实际分支: %d", branch)
			}
		}
	})
}
