// Package qa 提供基于内存三级漏斗（精准哈希、关键词倒排、Levenshtein 编辑距离）的高性能问答与题库检索系统。
//
// @author Ateng
// @since 2026-10-11
package qa

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

// FateEntry 代表仙旅奇缘中单条题目的故事与分支收益数据。
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
// 严格对齐原版 01.ini [仙旅奇缘] 优先级规则：体力 > 声望 > 阅历 > 铜钱。
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

// ParseAnswersJSON 从字节流中解析 GBK/UTF-8 编码的仙旅奇缘 JSON 数组。
func ParseAnswersJSON(rawBytes []byte) ([]FateEntry, error) {
	var entries []FateEntry

	// 1. 优先按 UTF-8 直接解析，避免 GBK 解码器误伤已是 UTF-8 的题库。
	if utf8.Valid(rawBytes) {
		if err := json.Unmarshal(rawBytes, &entries); err == nil {
			return entries, nil
		}
	}

	// 2. GBK 题库需完整流式转码，单次 Read 可能返回 io.ErrShortBuffer 导致整包退化为乱码。
	decoded, err := io.ReadAll(transform.NewReader(bytes.NewReader(rawBytes), simplifiedchinese.GBK.NewDecoder()))
	if err == nil {
		if uErr := json.Unmarshal(decoded, &entries); uErr == nil {
			return entries, nil
		}
	}

	// 3. 最后回退原始字节，兼容编码标记异常的 UTF-8 题库。
	if err := json.Unmarshal(rawBytes, &entries); err != nil {
		return nil, fmt.Errorf("qa: failed to unmarshal answers.txt: %v", err)
	}

	return entries, nil
}

// LoadAnswersFile 从物理文件加载仙旅奇缘题库。
func LoadAnswersFile(path string) ([]FateEntry, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseAnswersJSON(raw)
}
