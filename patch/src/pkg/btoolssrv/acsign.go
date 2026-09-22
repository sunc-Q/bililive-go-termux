package btoolssrv

// get__ac_signature 移植自 biliLive-tools DouYinRecorder/lib/utils.js。
// 纯算术哈希，无混淆 JS。已用 utils.js 注释中的样例作为黄金用例：
//   ts=1740635781, site="www.douyin.com/", nonce="067bffadf00143f576ddf",
//   ua="Mozilla/5.0 ... Chrome/133.0.0.0 Safari/537.36 Edg/133.0.0.0"
//   => "_02B4Z6wo00f01HBw-fgAAIDA-rdLmXfuMxxwUP1AAHurb5"
//
// 注意 JS 语义：位运算先 ToInt32/ToUint32；b 是 46 位整数，
// 所有涉及 b 的移位/异或都必须先截断到 32 位再运算。

import (
	"fmt"
	"strconv"
	"strings"
)

// jsCalOneStr 对应 cal_one_str：k = ((k ^ ch) * 65599) >>> 0
func jsCalOneStr(s string, iv uint32) uint32 {
	k := iv
	for _, ch := range []rune(s) {
		x := k ^ uint32(ch)
		k = uint32(uint64(x) * 65599) // (x*65599) mod 2^32
	}
	return k
}

// jsCalOneStr3 对应 cal_one_str_3：k = (k * 65599 + ch) >>> 0
func jsCalOneStr3(s string, iv uint32) uint32 {
	k := iv
	for _, ch := range []rune(s) {
		k = uint32(uint64(k)*65599 + uint64(ch))
	}
	return k
}

// jsGetOneChr 对应 get_one_chr。
func jsGetOneChr(c int) byte {
	switch {
	case c < 26:
		return byte(c + 65) // 'A'..'Z'
	case c < 52:
		return byte(c + 71) // 'a'..'z'
	case c < 62:
		return byte(c - 4) // '0'..'9'
	default:
		return byte(c - 17) // '-' / '.'
	}
}

// jsEncNumToStr 对应 enc_num_to_str：从第 24 位开始每 6 位取一个字符，共 5 个。
// 入参为 JS 的 int32 语义值（可为负，算术移位）。
func jsEncNumToStr(x int32) string {
	var sb strings.Builder
	for i := 24; i >= 0; i -= 6 {
		sb.WriteByte(jsGetOneChr(int((x >> uint(i)) & 63)))
	}
	return sb.String()
}

// GetACSignature 对应 get__ac_signature(one_time_stamp, one_site, one_nonce, ua_n)。
func GetACSignature(timestamp int64, site, nonce, ua string) string {
	signHead := "_02B4Z6wo00f01"
	tsStr := strconv.FormatInt(timestamp, 10)

	// a = cal_one_str(site, cal_one_str(ts, 0)) % 65521
	a := jsCalOneStr(site, jsCalOneStr(tsStr, 0)) % 65521

	// bin32 = ToUint32(ts ^ ToInt32(a*65521)) 的 32 位二进制
	bin32 := fmt.Sprintf("%032b", uint32(int32(timestamp)^int32(uint32(uint64(a)*65521))))

	// b = parseInt("10000000110000" + bin32, 2) —— 最高 46 位
	b, err := strconv.ParseUint("10000000110000"+bin32, 2, 64)
	if err != nil {
		// 理论上不会发生（46 位 < 64 位）
		b = 0
	}
	bStr := strconv.FormatUint(b, 10) // 十进制字符串

	c := jsCalOneStr(bStr, 0)

	// d = enc_num_to_str(b >> 2) —— JS: ToInt32(b) 算术右移 2
	d := jsEncNumToStr(int32(uint32(b)) >> 2)

	// e = (b / 4294967296) >>> 0 —— 真除法（非移位）
	e := uint32(b / 0x100000000)

	// f = enc_num_to_str((b << 28) | (e >>> 4))
	f := jsEncNumToStr((int32(uint32(b)) << 28) | int32(e>>4))

	// g = 582085784 ^ b —— JS 异或按 int32
	g := int32(582085784) ^ int32(uint32(b))

	// h = enc_num_to_str((e << 26) | (g >>> 6))
	h := jsEncNumToStr((int32(uint32(e)) << 26) | int32(uint32(g)>>6))

	// i = get_one_chr(g & 63)
	iChr := jsGetOneChr(int(g & 63))

	// j = (cal_one_str(ua, c) % 65521 << 16) | cal_one_str(nonce, c) % 65521
	j := (int32(jsCalOneStr(ua, c)%65521) << 16) | int32(jsCalOneStr(nonce, c)%65521)

	k := jsEncNumToStr(j >> 2)

	// l = enc_num_to_str((j << 28) | ((524576 ^ b) >>> 4))
	l := jsEncNumToStr((j << 28) | int32((uint32(524576)^uint32(b))>>4))

	m := jsEncNumToStr(int32(a))

	n := signHead + d + f + h + string(iChr) + k + l + m

	// o = parseInt(cal_one_str_3(n, 0)).toString(16).slice(-2)
	o := strconv.FormatUint(uint64(jsCalOneStr3(n, 0)), 16)
	if len(o) > 2 {
		o = o[len(o)-2:]
	}

	return n + o
}
