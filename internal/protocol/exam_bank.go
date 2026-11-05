// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件内嵌金榜题名题库 (来自官方 Templet.swf 的 StJinBangTiMingTypeData)：
// 题目与选项均为 Base64 密文，运行时用服务端下发的 decipher_key 做 DES-CBC 解密。
package protocol

import (
	"compress/gzip"
	"crypto/cipher"
	"crypto/des"
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"unicode"
)

//go:embed exam_questions.json.gz
var examBankFS embed.FS

// ExamQuestionCipher 表示题库中的一道题 (密文)。
type ExamQuestionCipher struct {
	// Question 题干密文
	Question string `json:"q"`
	// Sign 图片标识
	Sign string `json:"s"`
	// AnswerA 选项 A 密文
	AnswerA string `json:"a"`
	// AnswerB 选项 B 密文
	AnswerB string `json:"b"`
	// AnswerC 选项 C 密文
	AnswerC string `json:"c"`
	// AnswerD 选项 D 密文
	AnswerD string `json:"d"`
}

var (
	examBankOnce sync.Once
	examBank     map[string]ExamQuestionCipher
	examBankErr  error
)

// LookupExamQuestion 按 question_id 查询题库密文。
func LookupExamQuestion(id int32) (ExamQuestionCipher, bool) {
	examBankOnce.Do(func() {
		f, err := examBankFS.Open("exam_questions.json.gz")
		if err != nil {
			examBankErr = err
			return
		}
		defer f.Close()
		zr, err := gzip.NewReader(f)
		if err != nil {
			examBankErr = err
			return
		}
		defer zr.Close()
		raw, err := io.ReadAll(zr)
		if err != nil {
			examBankErr = err
			return
		}
		var bank map[string]ExamQuestionCipher
		if err := json.Unmarshal(raw, &bank); err != nil {
			examBankErr = err
			return
		}
		examBank = bank
	})
	if examBankErr != nil {
		return ExamQuestionCipher{}, false
	}
	q, ok := examBank[fmt.Sprintf("%d", id)]
	return q, ok
}

// DecryptExamString 使用服务端 decipher_key 解密题库字符串 (DES-CBC + PKCS#7)。
func DecryptExamString(cipherBase64, key string) (string, error) {
	if cipherBase64 == "" || key == "" {
		return "", nil
	}
	keyBytes := []byte(key)
	if len(keyBytes) < 8 {
		return "", fmt.Errorf("%w: exam key too short (%d)", ErrInvalidPayload, len(keyBytes))
	}
	iv := append([]byte(nil), keyBytes[:8]...)
	block, err := des.NewCipher(keyBytes[:8])
	if err != nil {
		return "", fmt.Errorf("%w: create des cipher: %v", ErrInvalidPayload, err)
	}
	ct, err := base64.StdEncoding.DecodeString(cipherBase64)
	if err != nil {
		return "", fmt.Errorf("%w: decode exam cipher: %v", ErrInvalidPayload, err)
	}
	if len(ct) == 0 || len(ct)%block.BlockSize() != 0 {
		return "", fmt.Errorf("%w: invalid exam cipher length %d", ErrInvalidPayload, len(ct))
	}
	mode := cipher.NewCBCDecrypter(block, iv)
	pt := make([]byte, len(ct))
	mode.CryptBlocks(pt, ct)
	// PKCS#7 去填充
	if n := int(pt[len(pt)-1]); n > 0 && n <= block.BlockSize() && n <= len(pt) {
		pt = pt[:len(pt)-n]
	}
	return string(pt), nil
}

// NormalizeExamText 清洗题干/选项文本，便于与题库答案匹配。
func NormalizeExamText(text string) string {
	var sb strings.Builder
	for _, r := range text {
		if unicode.IsPunct(r) || unicode.IsSpace(r) || unicode.IsSymbol(r) {
			continue
		}
		sb.WriteRune(unicode.ToLower(r))
	}
	return sb.String()
}

// ChooseExamOption 根据已知答案选择 A-D 选项下标 (0..3)；无匹配时返回 0。
func ChooseExamOption(options [4]string, knownAnswer string) int {
	target := NormalizeExamText(knownAnswer)
	if target == "" {
		return 0
	}
	for i, opt := range options {
		norm := NormalizeExamText(opt)
		if norm == target || strings.Contains(norm, target) || strings.Contains(target, norm) {
			return i
		}
	}
	return 0
}
