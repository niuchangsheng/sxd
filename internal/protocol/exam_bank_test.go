// Package protocol_test 提供金榜题名题库解密与选项匹配的测试用例。
package protocol_test

import (
	"bytes"
	"crypto/cipher"
	"crypto/des"
	"encoding/base64"
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func encryptExamForTest(t *testing.T, plain, key string) string {
	t.Helper()
	block, err := des.NewCipher([]byte(key)[:8])
	if err != nil {
		t.Fatalf("create des: %v", err)
	}
	pad := 8 - len(plain)%8
	if pad == 0 {
		pad = 8
	}
	pt := append([]byte(plain), bytes.Repeat([]byte{byte(pad)}, pad)...)
	ct := make([]byte, len(pt))
	mode := cipher.NewCBCEncrypter(block, []byte(key)[:8])
	mode.CryptBlocks(ct, pt)
	return base64.StdEncoding.EncodeToString(ct)
}

func TestDecryptExamString(t *testing.T) {
	key := "testkey1"
	cipherText := encryptExamForTest(t, "李白字什么？", key)
	got, err := protocol.DecryptExamString(cipherText, key)
	if err != nil {
		t.Fatalf("解密失败: %v", err)
	}
	if got != "李白字什么？" {
		t.Errorf("解密结果期望 %q, got %q", "李白字什么？", got)
	}
	// 空密文返回空
	if s, err := protocol.DecryptExamString("", key); err != nil || s != "" {
		t.Errorf("空密文应返回空, got %q err=%v", s, err)
	}
	// 非法 Base64 报错
	if _, err := protocol.DecryptExamString("!!!", key); err == nil {
		t.Error("非法密文应返回错误")
	}
}

func TestChooseExamOption(t *testing.T) {
	options := [4]string{"太白", "子美", "杜甫", "居易"}
	if got := protocol.ChooseExamOption(options, "太白"); got != 0 {
		t.Errorf("期望选项 0, got %d", got)
	}
	if got := protocol.ChooseExamOption(options, "子美"); got != 1 {
		t.Errorf("期望选项 1, got %d", got)
	}
	if got := protocol.ChooseExamOption(options, "不存在的答案"); got != 0 {
		t.Errorf("无匹配应回退 0, got %d", got)
	}
}

func TestLookupExamQuestion(t *testing.T) {
	q, ok := protocol.LookupExamQuestion(1)
	if !ok {
		t.Fatal("题库缺少 id=1")
	}
	if q.Question == "" || q.AnswerA == "" {
		t.Errorf("题库密文字段为空: %+v", q)
	}
}
