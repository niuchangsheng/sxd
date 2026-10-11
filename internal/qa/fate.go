// Package qa 提供基于内存三级漏斗（精准哈希、关键词倒排、Levenshtein 编辑距离）的高性能问答与题库检索系统。
//
// @author Ateng
// @since 2026-10-11
package qa

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

// FateEntry 代表仙履奇缘中单条题目的故事与分支收益数据。
type FateEntry struct {
	Title string `json:"title"`
	Q1    string `json:"q1"`
	Q2    string `json:"q2"`
	P1    string `json:"p1"` // 如 "50声望,50阅历"
	P2    string `json:"p2"` // 如 "5体力"
	G1    string `json:"g1"`
	G2    string `json:"g2"`
}

// ScoreFateReward 计算收益文本的加权分值。
// 严格对齐原版 01.ini [仙履奇缘] 优先级规则：体力 > 声望 > 阅历 > 铜钱。
func ScoreFateReward(reward string) int {
	score := 0
	if strings.Contains(reward, "体力") {
		score += 10000
	}
	if strings.Contains(reward, "声望") {
		score += 1000
	}
	if strings.Contains(reward, "阅历") {
		score += 100
	}
	if strings.Contains(reward, "铜钱") {
		score += 10
	}
	return score
}

// DecideBestBranch 根据两个选项的收益分值，决策最优分支编号 (1 或 2)。
func DecideBestBranch(entry *FateEntry) (int, string) {
	if entry == nil {
		return 1, "默认首选分支"
	}

	score1 := ScoreFateReward(entry.P1)
	score2 := ScoreFateReward(entry.P2)

	if score1 >= score2 {
		return 1, fmt.Sprintf("分支1收益更优 (选项1: %s [分值:%d] >= 选项2: %s [分值:%d])", entry.P1, score1, entry.P2, score2)
	}
	return 2, fmt.Sprintf("分支2收益更优 (选项2: %s [分值:%d] > 选项1: %s [分值:%d])", entry.P2, score2, entry.P1, score1)
}

// ParseAnswersJSON 从字节流中解析 GBK/UTF-8 编码的仙履奇缘 JSON 数组。
func ParseAnswersJSON(rawBytes []byte) ([]FateEntry, error) {
	// 尝试 GBK 转码
	reader := transform.NewReader(strings.NewReader(string(rawBytes)), simplifiedchinese.GBK.NewDecoder())
	var decoded []byte
	buf := make([]byte, len(rawBytes)*2)
	n, err := reader.Read(buf)
	if err == nil {
		decoded = buf[:n]
	} else {
		decoded = rawBytes
	}

	var entries []FateEntry
	if err := json.Unmarshal(decoded, &entries); err != nil {
		// 若 GBK 失败尝试直接用原始字节解析 (兼容 UTF-8)
		if uErr := json.Unmarshal(rawBytes, &entries); uErr != nil {
			return nil, fmt.Errorf("qa: failed to unmarshal answers.txt: %v", err)
		}
	}

	return entries, nil
}

// LoadAnswersFile 从物理文件加载仙履奇缘题库。
func LoadAnswersFile(path string) ([]FateEntry, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseAnswersJSON(raw)
}
