// Package qa 提供基于内存三级漏斗（精准哈希、关键词倒排、Levenshtein 编辑距离）的高性能问答与题库检索系统。
//
// @author Ateng
// @since 2026-10-08
package qa

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

// Engine 定义智能题库问答引擎契约。
type Engine interface {
	// Match 执行三级漏斗智能问答检索，返回答案、置信度以及是否命中。
	Match(question string) (answer string, confidence float64, found bool)
	// MatchFate 根据仙履奇缘故事文本检索题库，并基于收益权重决策最优分支选项 (1 或 2)。
	MatchFate(story string) (bestBranch int, entry *FateEntry, found bool)
	// Size 返回当前引擎加载的题库去重条数。
	Size() int
}

// QABank 为 Engine 的语义契约别名。
type QABank = Engine

type entry struct {
	normalizedQ string
	answer      string
}

type memoryEngine struct {
	exactMap   map[string]string
	entries    []entry
	ngramIndex map[string][]int

	fateEntries []FateEntry
	fateMap     map[string]*FateEntry
	fateNgrams  map[string][]int

	mu sync.RWMutex
}

// Normalize 对题干文本执行清洗，去除所有标点、符号与空白字符，并转小写。
func Normalize(text string) string {
	var sb strings.Builder
	for _, r := range text {
		if unicode.IsPunct(r) || unicode.IsSpace(r) || unicode.IsSymbol(r) {
			continue
		}
		// 过滤常见全角标点与修饰符
		switch r {
		case '？', '！', '，', '。', '、', '“', '”', '’', '‘', '（', '）', '【', '】', '《', '》', '：', '；', '_', '-', '—':
			continue
		}
		sb.WriteRune(unicode.ToLower(r))
	}
	return sb.String()
}

// ResolveDataDir 探测题库文件所在的物理目录。
func ResolveDataDir(customDir string) (string, error) {
	if customDir != "" {
		if fi, err := os.Stat(customDir); err == nil && fi.IsDir() {
			return customDir, nil
		}
	}

	candidates := []string{
		"data",
		filepath.Join("..", "data"),
		filepath.Join("..", "..", "data"),
		filepath.Join("..", "..", "..", "data"),
		`D:\Software\疯玩神仙道`,
	}

	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && fi.IsDir() {
			return c, nil
		}
	}

	return "", errors.New("qa: 未找到题库数据目录，请确认已放置在 data/ 目录")
}

// NewEngine 构造并加载题库问答引擎。
func NewEngine(dataDir string) (Engine, error) {
	actualDir, err := ResolveDataDir(dataDir)
	if err != nil {
		return nil, err
	}

	eng := &memoryEngine{
		exactMap:   make(map[string]string),
		ngramIndex: make(map[string][]int),
		fateMap:    make(map[string]*FateEntry),
		fateNgrams: make(map[string][]int),
	}

	// 1. 扫描加载常见题库文件
	targetFiles := []string{"answers.txt", "scra.ini", "monkeyanswer.ini"}
	loadedCount := 0

	for _, fname := range targetFiles {
		fullPath := filepath.Join(actualDir, fname)
		if _, err := os.Stat(fullPath); err == nil {
			if fname == "answers.txt" {
				if entries, err := LoadAnswersFile(fullPath); err == nil && len(entries) > 0 {
					eng.loadFateEntries(entries)
					loadedCount++
				}
			} else {
				if err := eng.loadFile(fullPath); err == nil {
					loadedCount++
				}
			}
		}
	}

	if len(eng.exactMap) == 0 && len(eng.fateMap) == 0 {
		return nil, fmt.Errorf("qa: 题库文件解析完成但未提取到有效题目 (dataDir: %s)", actualDir)
	}

	// 2. 建立倒排索引用以模糊召回
	eng.buildNgramIndex()

	return eng, nil
}

func (e *memoryEngine) loadFile(path string) error {
	rawBytes, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	// 尝试优先按照 GBK 解码，若失败回退 UTF-8
	reader := transform.NewReader(bytes.NewReader(rawBytes), simplifiedchinese.GBK.NewDecoder())
	decoded, err := io.ReadAll(reader)
	if err != nil {
		decoded = rawBytes
	}

	scanner := bufio.NewScanner(bytes.NewReader(decoded))
	var currentQ string

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}

		// 格式形如 [问题题干]
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			currentQ = strings.Trim(line, "[]")
			continue
		}

		// 格式形如 答案=内容 或 答案: 内容
		if strings.HasPrefix(line, "答案=") || strings.HasPrefix(line, "答案:") {
			sepIdx := strings.IndexAny(line, "=:")
			ans := strings.TrimSpace(line[sepIdx+1:])
			if currentQ != "" && ans != "" {
				normQ := Normalize(currentQ)
				if normQ != "" && e.exactMap[normQ] == "" {
					e.exactMap[normQ] = ans
					e.entries = append(e.entries, entry{
						normalizedQ: normQ,
						answer:      ans,
					})
				}
			}
			currentQ = ""
		}
	}

	return scanner.Err()
}

func (e *memoryEngine) buildNgramIndex() {
	for idx, ent := range e.entries {
		runes := []rune(ent.normalizedQ)
		// 构建 2-gram 倒排索引
		for i := 0; i < len(runes)-1; i++ {
			gram := string(runes[i : i+2])
			e.ngramIndex[gram] = append(e.ngramIndex[gram], idx)
		}
	}
}

func (e *memoryEngine) Size() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.exactMap)
}

func (e *memoryEngine) Match(question string) (string, float64, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	normTarget := Normalize(question)
	if normTarget == "" {
		return "", 0, false
	}

	// 1. 第一级：标点与空白归一化精准查表
	if ans, ok := e.exactMap[normTarget]; ok {
		return ans, 1.0, true
	}

	// 2. 第二级：基于 2-gram 倒排索引快速聚合高频候选集
	targetRunes := []rune(normTarget)
	if len(targetRunes) < 2 {
		return "", 0, false
	}

	candidateScores := make(map[int]int)
	for i := 0; i < len(targetRunes)-1; i++ {
		gram := string(targetRunes[i : i+2])
		for _, idx := range e.ngramIndex[gram] {
			candidateScores[idx]++
		}
	}

	if len(candidateScores) == 0 {
		return "", 0, false
	}

	// 3. 第三级：针对候选集计算 Levenshtein 编辑距离相似度
	bestIdx := -1
	bestSimilarity := 0.0

	for idx, score := range candidateScores {
		// 粗筛：重合度过低的候选直接跳过
		if score < 2 && len(targetRunes) > 4 {
			continue
		}

		ent := e.entries[idx]
		candRunes := []rune(ent.normalizedQ)
		dist := levenshteinDistance(targetRunes, candRunes)
		maxLen := max(len(targetRunes), len(candRunes))
		sim := 1.0 - float64(dist)/float64(maxLen)

		if sim > bestSimilarity {
			bestSimilarity = sim
			bestIdx = idx
		}
	}

	// 阈值控制：相似度 >= 0.65 方可召回
	if bestSimilarity >= 0.65 && bestIdx >= 0 {
		return e.entries[bestIdx].answer, bestSimilarity, true
	}

	return "", 0, false
}

func levenshteinDistance(s1, s2 []rune) int {
	len1, len2 := len(s1), len(s2)
	dp := make([]int, len2+1)
	for j := 0; j <= len2; j++ {
		dp[j] = j
	}

	for i := 1; i <= len1; i++ {
		prev := dp[0]
		dp[0] = i
		for j := 1; j <= len2; j++ {
			temp := dp[j]
			cost := 0
			if s1[i-1] != s2[j-1] {
				cost = 1
			}
			dp[j] = min(dp[j]+1, min(dp[j-1]+1, prev+cost))
			prev = temp
		}
	}

	return dp[len2]
}

func (e *memoryEngine) loadFateEntries(entries []FateEntry) {
	e.fateEntries = append(e.fateEntries, entries...)
	for idx, ent := range e.fateEntries {
		normTitle := Normalize(ent.Title)
		if normTitle != "" {
			e.fateMap[normTitle] = &e.fateEntries[idx]
		}
		runes := []rune(normTitle)
		for i := 0; i < len(runes)-1; i++ {
			gram := string(runes[i : i+2])
			e.fateNgrams[gram] = append(e.fateNgrams[gram], idx)
		}
	}
}

// MatchFate 根据仙履奇缘故事文本检索题库，并基于收益权重决策最优分支选项 (1 或 2)。
func (e *memoryEngine) MatchFate(story string) (int, *FateEntry, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	normTarget := Normalize(story)
	if normTarget == "" {
		return 1, nil, false
	}

	// 1. 精确哈希匹配
	if ent, ok := e.fateMap[normTarget]; ok {
		branch, _ := DecideBestBranch(ent)
		return branch, ent, true
	}

	// 2. 2-gram 倒排索引模糊匹配
	targetRunes := []rune(normTarget)
	if len(targetRunes) < 2 {
		return 1, nil, false
	}

	candidateScores := make(map[int]int)
	for i := 0; i < len(targetRunes)-1; i++ {
		gram := string(targetRunes[i : i+2])
		for _, idx := range e.fateNgrams[gram] {
			candidateScores[idx]++
		}
	}

	bestIdx := -1
	bestSimilarity := 0.0

	for idx, score := range candidateScores {
		if score < 2 && len(targetRunes) > 4 {
			continue
		}
		ent := e.fateEntries[idx]
		candRunes := []rune(Normalize(ent.Title))
		dist := levenshteinDistance(targetRunes, candRunes)
		maxLen := max(len(targetRunes), len(candRunes))
		sim := 1.0 - float64(dist)/float64(maxLen)

		if sim > bestSimilarity {
			bestSimilarity = sim
			bestIdx = idx
		}
	}

	if bestSimilarity >= 0.50 && bestIdx >= 0 {
		ent := &e.fateEntries[bestIdx]
		branch, _ := DecideBestBranch(ent)
		return branch, ent, true
	}

	return 1, nil, false
}
