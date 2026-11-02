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

	t.Run("仙旅奇缘 GBK 题库加载与精确命中", func(t *testing.T) {
		// answers.txt 为 GBK 编码 JSON，必须完整转码后命中；否则题库会退化为乱码。
		story := "此地出现女娲大神造人剩下的七彩神土，传闻使用神土可创造第三种人类。"
		branch, entry, found := engine.MatchFate(story)
		if !found || entry == nil {
			t.Fatalf("仙旅奇缘题库未命中线上真实故事: %s", story)
		}
		if entry.P1 != "50声望,30阅历" || entry.P2 != "10阅历" {
			t.Errorf("题库条目收益数据异常: p1=%q p2=%q", entry.P1, entry.P2)
		}
		// P1 (声望 1000 + 阅历 100) 优于 P2 (阅历 100)，应选择分支 1。
		if branch != 1 {
			t.Errorf("期望分支 1, 实际分支: %d", branch)
		}
	})

	t.Run("仙旅奇缘体力优先收益权重决策", func(t *testing.T) {
		// P2 包含 5体力 (权重 10000)，P1 仅 10声望 (权重 1000)，应优先选择体力分支 2。
		story := "传闻此处出现了一妖艳女子，吸引了众多散仙与她饮酒作乐，夜夜笙歌。"
		branch, entry, found := engine.MatchFate(story)
		if !found || entry == nil {
			t.Fatalf("仙旅奇缘题库未命中线上真实故事: %s", story)
		}
		if branch != 2 {
			t.Errorf("期望依据体力优先选择分支 2, 实际分支: %d", branch)
		}
	})
}
