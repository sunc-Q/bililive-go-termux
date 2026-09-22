package btoolssrv

import (
	"encoding/hex"
	"testing"
)

// SM3 标准测试向量（GB/T 32905-2016 附录 A）
func TestSM3Vectors(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"abc", "66c7f0f462eeedd9d1f2d46bdc10e4e24167c4875cf2f7a2297da02b8f4ba8e0"},
		{"abcdabcdabcdabcdabcdabcdabcdabcdabcdabcdabcdabcdabcdabcdabcdabcd",
			"debe9ff92275b8a138604889c18e5a4d6fdb70e5387e5765293dcba39c0c5732"},
	}
	for _, c := range cases {
		got := sm3([]byte(c.in))
		if hex.EncodeToString(got[:]) != c.want {
			t.Errorf("sm3(%q) = %x, want %s", c.in, got, c.want)
		}
	}
}

// __ac_signature 黄金用例：来自 biliLive-tools utils.js 源码注释中的
// 真实签名样例（该样例由抖音页面真实生成）。
func TestGetACSignatureGolden(t *testing.T) {
	ts := int64(1740635781)
	site := "www.douyin.com/"
	nonce := "067bffadf00143f576ddf"
	ua := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36 Edg/133.0.0.0"
	got := GetACSignature(ts, site, nonce, ua)
	want := "_02B4Z6wo00f01HBw-fgAAIDA-rdLmXfuMxxwUP1AAHurb5"
	if got != want {
		t.Errorf("GetACSignature = %q, want %q", got, want)
	}
}
