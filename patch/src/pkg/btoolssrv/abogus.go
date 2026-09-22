package btoolssrv

// ABogus 签名移植自 biliLive-tools DouYinRecorder/lib/sign.js，
// 其本身是 https://github.com/hua0512/rust-srec abogus.rs 的 TypeScript 实现。
// 纯算法：SM3 + RC4 + 自定义 Base64 + 字节置换，无混淆 JS 执行。
//
// 移植注意：JS 位运算会先 ToInt32/ToUint32，涉及超过 32 位的数值
// （如 b 最高 46 位）必须显式截断，见各处 uint32(...) 转换。

import (
	"math/rand"
	"strings"
)

const (
	abSalt        = "cus"
	abCharacter   = "Dkdpgh2ZmsQB80/MfvV36XI1R45-WUAlEixNLwoqYTOPuzKFjJnry79HbGcaStCe"
	abCharacter2  = "ckdp1h4ZKsUB80/Mfvw36XIgR25+WQAlEi7NLboqYTOPuzmFjJnryx9HVGDaStCe"
	abDefaultUA   = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36 Edg/130.0.0.0"
	abFingerprint = "1024|768|1049|843|0|30|0|0|1024|768|1280|800|1024|768|24|24|Win32"
)

// abBigArray 与 sign.js 中的 bigArray 完全一致（会被 transformBytes 原地打乱）。
var abBigArray = [256]int{
	121, 243, 55, 234, 103, 36, 47, 228, 30, 231, 106, 6, 115, 95, 78, 101, 250, 207, 198, 50,
	139, 227, 220, 105, 97, 143, 34, 28, 194, 215, 18, 100, 159, 160, 43, 8, 169, 217, 180, 120,
	247, 45, 90, 11, 27, 197, 46, 3, 84, 72, 5, 68, 62, 56, 221, 75, 144, 79, 73, 161, 178, 81,
	64, 187, 134, 117, 186, 118, 16, 241, 130, 71, 89, 147, 122, 129, 65, 40, 88, 150, 110, 219,
	199, 255, 181, 254, 48, 4, 195, 248, 208, 32, 116, 167, 69, 201, 17, 124, 125, 104, 96, 83,
	80, 127, 236, 108, 154, 126, 204, 15, 20, 135, 112, 158, 13, 1, 188, 164, 210, 237, 222, 98,
	212, 77, 253, 42, 170, 202, 26, 22, 29, 182, 251, 10, 173, 152, 58, 138, 54, 141, 185, 33,
	157, 31, 252, 132, 233, 235, 102, 196, 191, 223, 240, 148, 39, 123, 92, 82, 128, 109, 57, 24,
	38, 113, 209, 245, 2, 119, 153, 229, 189, 214, 230, 174, 232, 63, 52, 205, 86, 140, 66, 175,
	111, 171, 246, 133, 238, 193, 99, 60, 74, 91, 225, 51, 76, 37, 145, 211, 166, 151, 213, 206,
	0, 200, 244, 176, 218, 44, 184, 172, 49, 216, 93, 168, 53, 21, 183, 41, 67, 85, 224, 155, 226,
	242, 87, 177, 146, 70, 190, 12, 162, 19, 137, 114, 25, 165, 163, 192, 23, 59, 9, 94, 179, 107,
	35, 7, 142, 131, 239, 203, 149, 136, 61, 249, 14, 156,
}

var abSortIndex = []int{
	18, 20, 52, 26, 30, 34, 58, 38, 40, 53, 42, 21, 27, 54, 55, 31, 35, 57, 39, 41, 43, 22, 28,
	32, 60, 36, 23, 29, 33, 37, 44, 45, 59, 46, 47, 48, 49, 50, 24, 25, 65, 66, 70, 71,
}

var abSortIndex2 = []int{
	18, 20, 26, 30, 34, 38, 40, 42, 21, 27, 31, 35, 39, 41, 43, 22, 28, 32, 36, 23, 29, 33, 37,
	44, 45, 46, 47, 48, 49, 50, 24, 25, 52, 53, 54, 55, 57, 58, 59, 60, 65, 66, 70, 71,
}

// abSM3ToBytes 对应 CryptoUtility.sm3ToArray：输入可以是字符串或字节数组。
func abSM3ToBytes(input any) []byte {
	switch v := input.(type) {
	case string:
		h := sm3([]byte(v))
		return h[:]
	case []byte:
		h := sm3(v)
		return h[:]
	default:
		h := sm3(nil)
		return h[:]
	}
}

func abSM3HexToBytes(input any) []byte {
	// sign.js 里 paramsToArray 返回 sm3 的字节数组，
	// 随后 sm3ToArray(paramsHash1) 再对该“字节数组”求一次 sm3。
	// JS 中 paramsHash1 是 number[]，Buffer.from(number[]) 按字节解释，
	// 与直接对 32 字节原文再哈希一致。
	return abSM3ToBytes(input)
}

// abParamsToArray 对应 CryptoUtility.paramsToArray(param, addSalt)。
func abParamsToArray(param string, addSalt bool) []byte {
	processed := param
	if addSalt {
		processed = param + abSalt
	}
	return abSM3HexToBytes(processed)
}

// abTransformBytes 对应 CryptoUtility.transformBytes（bigArray 状态原地打乱）。
func abTransformBytes(bigArray []int, valuesList []int) []int {
	result := make([]int, 0, len(valuesList))
	indexB := bigArray[1]
	initialValue, valueE := 0, 0
	arrayLen := len(bigArray)
	for index := 0; index < len(valuesList); index++ {
		var sumInitial int
		if index == 0 {
			initialValue = bigArray[indexB]
			sumInitial = indexB + initialValue
			bigArray[1] = initialValue
			bigArray[indexB] = indexB
		} else {
			sumInitial = initialValue + valueE
		}
		sumInitialIdx := sumInitial % arrayLen
		valueF := bigArray[sumInitialIdx]
		result = append(result, valuesList[index]^valueF)
		nextIdx := (index + 2) % arrayLen
		valueE = bigArray[nextIdx]
		newSumInitialIdx := (indexB + valueE) % arrayLen
		initialValue = bigArray[newSumInitialIdx]
		bigArray[newSumInitialIdx], bigArray[nextIdx] = bigArray[nextIdx], bigArray[newSumInitialIdx]
		indexB = newSumInitialIdx
	}
	return result
}

// abBase64Encode 对应 CryptoUtility.base64Encode（alphabetIndex: 0/1）。
func abBase64Encode(bytes []byte, alphabetIndex int) string {
	alphabet := abCharacter
	if alphabetIndex == 1 {
		alphabet = abCharacter2
	}
	var sb strings.Builder
	for i := 0; i < len(bytes); i += 3 {
		b1 := bytes[i]
		b2 := 0
		if i+1 < len(bytes) {
			b2 = int(bytes[i+1])
		}
		b3 := 0
		if i+2 < len(bytes) {
			b3 = int(bytes[i+2])
		}
		combined := (int(b1) << 16) | (int(b2) << 8) | b3
		sb.WriteByte(alphabet[(combined>>18)&63])
		sb.WriteByte(alphabet[(combined>>12)&63])
		if i+1 < len(bytes) {
			sb.WriteByte(alphabet[(combined>>6)&63])
		}
		if i+2 < len(bytes) {
			sb.WriteByte(alphabet[combined&63])
		}
	}
	out := sb.String()
	for len(out)%4 != 0 {
		out += "="
	}
	return out
}

// abAbogusEncode 对应 CryptoUtility.abogusEncode。
func abAbogusEncode(values []int, alphabetIndex int) string {
	alphabet := abCharacter
	if alphabetIndex == 1 {
		alphabet = abCharacter2
	}
	var sb strings.Builder
	for i := 0; i < len(values); i += 3 {
		v1 := values[i]
		v2 := 0
		if i+1 < len(values) {
			v2 = values[i+1]
		}
		v3 := 0
		if i+2 < len(values) {
			v3 = values[i+2]
		}
		n := (v1 << 16) | (v2 << 8) | v3
		sb.WriteByte(alphabet[(n&0xfc0000)>>18])
		sb.WriteByte(alphabet[(n&0x03f000)>>12])
		if i+1 < len(values) {
			sb.WriteByte(alphabet[(n&0x0fc0)>>6])
		}
		if i+2 < len(values) {
			sb.WriteByte(alphabet[n&0x3f])
		}
	}
	out := sb.String()
	for len(out)%4 != 0 {
		out += "="
	}
	return out
}

// abRC4Encrypt 对应 CryptoUtility.rc4Encrypt（key 为字节数组，明文为字符串）。
func abRC4Encrypt(key []byte, plaintext string) []byte {
	var s [256]int
	for i := range s {
		s[i] = i
	}
	j := 0
	for i := 0; i < 256; i++ {
		j = (j + s[i] + int(key[i%len(key)])) & 0xff
		s[i], s[j] = s[j], s[i]
	}
	i := 0
	j = 0
	ct := make([]byte, 0, len(plaintext))
	for _, r := range []rune(plaintext) {
		charVal := int(r) // UA 为 ASCII，charCodeAt 等价
		i = (i + 1) & 0xff
		j = (j + s[i]) & 0xff
		s[i], s[j] = s[j], s[i]
		k := s[(s[i]+s[j])&0xff]
		ct = append(ct, byte(charVal^k))
	}
	return ct
}

// abGenerateRandomPrefix 对应 StringProcessor.generateRandomBytes(3) 的
// 字节序列（12 字节），直接作为 finalValues 前缀的数值数组。
func abGenerateRandomPrefix(n int) []int {
	result := make([]int, 0, n*4)
	for i := 0; i < n; i++ {
		rd := rand.Intn(10000)
		result = append(result,
			(rd&255&170)|1,
			(rd&255&85)|2,
			((rd>>8)&170)|5,
			((rd>>8)&85)|40,
		)
	}
	return result
}

type abogus struct {
	bigArray   []int
	userAgent  string
	browserFp  string
	options    [3]int
	pageId     int
	aid        int
	uaKey      []byte
}

func newAbogus() *abogus {
	arr := make([]int, len(abBigArray))
	copy(arr, abBigArray[:])
	return &abogus{
		bigArray:  arr,
		userAgent: abDefaultUA,
		browserFp: abFingerprint, // sign.js 随机指纹改为固定值，便于问题复现（长度一致即可）
		options:   [3]int{0, 1, 14},
		pageId:    0,
		aid:       6383,
		uaKey:     []byte{0x00, 0x01, 0x0e},
	}
}

// generateAbogus 返回 finalParams（已含 &a_bogus=...）。
// 对应 sign.js 的 ABogus.generateAbogus(params, body)。
func (a *abogus) generateAbogus(params, body string) string {
	abDir := map[int]int{
		8: 3, 18: 44, 66: 0, 69: 0, 70: 0, 71: 0,
	}
	startEncryption := nowMillis()
	// Hash(Hash(params))
	paramsHash1 := abParamsToArray(params, true)
	array1 := abSM3ToBytes(paramsHash1)
	// Hash(Hash(body))
	bodyHash1 := abParamsToArray(body, true)
	array2 := abSM3ToBytes(bodyHash1)
	// Hash(Base64(RC4(user_agent)))
	rc4Ua := abRC4Encrypt(a.uaKey, a.userAgent)
	uaB64 := abBase64Encode(rc4Ua, 1)
	array3 := abSM3ToBytes(uaB64)
	endEncryption := nowMillis()

	abDir[20] = int(uint32(startEncryption)>>24) & 255
	abDir[21] = int(uint32(startEncryption)>>16) & 255
	abDir[22] = int(uint32(startEncryption)>>8) & 255
	abDir[23] = int(uint32(startEncryption)) & 255
	abDir[24] = int(startEncryption / 0x100000000)
	abDir[25] = int(startEncryption / 0x10000000000)
	abDir[26] = (a.options[0] >> 24) & 255
	abDir[27] = (a.options[0] >> 16) & 255
	abDir[28] = (a.options[0] >> 8) & 255
	abDir[29] = a.options[0] & 255
	abDir[30] = (a.options[1] / 256) & 255
	abDir[31] = a.options[1] % 256
	abDir[32] = (a.options[1] >> 24) & 255
	abDir[33] = (a.options[1] >> 16) & 255
	abDir[34] = (a.options[2] >> 24) & 255
	abDir[35] = (a.options[2] >> 16) & 255
	abDir[36] = (a.options[2] >> 8) & 255
	abDir[37] = a.options[2] & 255
	abDir[38] = int(array1[21])
	abDir[39] = int(array1[22])
	abDir[40] = int(array2[21])
	abDir[41] = int(array2[22])
	abDir[42] = int(array3[23])
	abDir[43] = int(array3[24])
	abDir[44] = int(uint32(endEncryption)>>24) & 255
	abDir[45] = int(uint32(endEncryption)>>16) & 255
	abDir[46] = int(uint32(endEncryption)>>8) & 255
	abDir[47] = int(uint32(endEncryption)) & 255
	abDir[48] = abDir[8]
	abDir[49] = int(endEncryption / 0x100000000)
	abDir[50] = int(endEncryption / 0x10000000000)
	abDir[51] = (a.pageId >> 24) & 255
	abDir[52] = (a.pageId >> 16) & 255
	abDir[53] = (a.pageId >> 8) & 255
	abDir[54] = a.pageId & 255
	abDir[55] = a.pageId
	abDir[56] = a.aid
	abDir[57] = a.aid & 255
	abDir[58] = (a.aid >> 8) & 255
	abDir[59] = (a.aid >> 16) & 255
	abDir[60] = (a.aid >> 24) & 255
	abDir[64] = len(a.browserFp)
	abDir[65] = len(a.browserFp)

	sortedValues := make([]int, 0, len(abSortIndex))
	for _, idx := range abSortIndex {
		v := abDir[idx]
		sortedValues = append(sortedValues, v)
	}
	fpArray := make([]int, 0, len(a.browserFp))
	for _, r := range a.browserFp {
		fpArray = append(fpArray, int(r))
	}
	abXor := 0
	for idx, key := range abSortIndex2 {
		v := abDir[key]
		if idx == 0 {
			abXor = v
		} else {
			abXor ^= v
		}
	}
	allValues := make([]int, 0, len(sortedValues)+len(fpArray)+1)
	allValues = append(allValues, sortedValues...)
	allValues = append(allValues, fpArray...)
	allValues = append(allValues, abXor)

	transformedValues := abTransformBytes(a.bigArray, allValues)
	randomPrefix := abGenerateRandomPrefix(3)
	finalValues := make([]int, 0, len(randomPrefix)+len(transformedValues))
	finalValues = append(finalValues, randomPrefix...)
	finalValues = append(finalValues, transformedValues...)

	abogusStr := abAbogusEncode(finalValues, 0)
	return params + "&a_bogus=" + abogusStr
}
