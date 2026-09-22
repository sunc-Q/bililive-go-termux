// Package btoolssrv 是 bililive-tools(btools) 中抖音解析能力的 Go 等价实现。
//
// 背景：Android 构建无法运行外部 Node 版 btools（tools_android.go 明确跳过），
// 而抖音解析强依赖 btools 的 /bgo/* HTTP 接口（src/live/douyin/douyin_btools.go）。
// 本包在 Android 上内建一个等价服务，监听 127.0.0.1:18110。
//
// 实现依据：bililive-tools 3.1.2-bgo.2（kira1928/biliLive-tools，
// renmu123/biliLive-tools 的分叉）打包产物的 sourcemap 还原源码：
//   - DouYinRecorder/lib/douyin_api.js（房间信息与取流）
//   - DouYinRecorder/lib/sign.js（ABogus，纯算法：SM3+RC4+自定义Base64）
//   - DouYinRecorder/lib/utils.js（__ac_signature，纯算术哈希）
//   - DouYinRecorder/lib/stream.js / loadBalancer/loadBalancer.js
//   - http/lib/routes/bgo.js（/bgo 三个端点）
//
// 签名均为纯算法（ABogus 源自 hua0512/rust-srec 的 Rust 实现的 TS 移植），
// 不含字节跳动混淆 JS 执行，因此可直接移植为 Go，无需 goja。
package btoolssrv

import "encoding/binary"

// sm3 计算 GB/T 32905-2016 SM3 摘要，返回 32 字节。
// 与 sm-crypto 的 sm3()（hex 输出）等价，这里直接返回原始字节。
func sm3(data []byte) [32]byte {
	var iv = [8]uint32{
		0x7380166f, 0x4914b2b9, 0x172442d7, 0xda8a0600,
		0xa96f30bc, 0x163138aa, 0xe38dee4d, 0xb0fb0e4e,
	}

	msgLen := uint64(len(data)) * 8
	// padding: 0x80 + zeros(至 56 mod 64) + 64-bit big-endian bit length
	padLen := (56 - (len(data)+1)%64 + 64) % 64
	buf := make([]byte, 0, len(data)+1+padLen+8)
	buf = append(buf, data...)
	buf = append(buf, 0x80)
	buf = append(buf, make([]byte, padLen)...)
	var lenBytes [8]byte
	binary.BigEndian.PutUint64(lenBytes[:], msgLen)
	buf = append(buf, lenBytes[:]...)

	v := iv
	for off := 0; off < len(buf); off += 64 {
		v = sm3Block(v, buf[off:off+64])
	}

	var out [32]byte
	for i, w := range v {
		binary.BigEndian.PutUint32(out[i*4:], w)
	}
	return out
}

func rotl(x uint32, n int) uint32 { return x<<uint(n) | x>>uint(32-n) }

func p0(x uint32) uint32 { return x ^ rotl(x, 9) ^ rotl(x, 17) }
func p1(x uint32) uint32 { return x ^ rotl(x, 15) ^ rotl(x, 23) }

func sm3Block(v [8]uint32, block []byte) [8]uint32 {
	var w [68]uint32
	for i := 0; i < 16; i++ {
		w[i] = binary.BigEndian.Uint32(block[i*4:])
	}
	for j := 16; j < 68; j++ {
		w[j] = p1(w[j-16]^w[j-9]^rotl(w[j-3], 15)) ^ rotl(w[j-13], 7) ^ w[j-6]
	}

	a, b, c, d, e, f, g, h := v[0], v[1], v[2], v[3], v[4], v[5], v[6], v[7]
	for j := 0; j < 64; j++ {
		var t uint32
		ff, gg := uint32(0), uint32(0)
		if j < 16 {
			t = 0x79cc4519
			ff = a ^ b ^ c
			gg = e ^ f ^ g
		} else {
			t = 0x7a879d8a
			ff = (a & b) | (a & c) | (b & c)
			gg = (e & f) | (^e & g)
		}
		ss1 := rotl(rotl(a, 12)+e+rotl(t, int(uint32(j)%32)), 7)
		ss2 := ss1 ^ rotl(a, 12)
		tt1 := ff + d + ss2 + (w[j] ^ w[j+4]) // TT1 用 W'[j] = W[j] ⊕ W[j+4]
		tt2 := gg + h + ss1 + w[j]
		d = c
		c = rotl(b, 9)
		b = a
		a = tt1
		h = g
		g = rotl(f, 19)
		f = e
		e = p0(tt2)
	}
	return [8]uint32{
		v[0] ^ a, v[1] ^ b, v[2] ^ c, v[3] ^ d,
		v[4] ^ e, v[5] ^ f, v[6] ^ g, v[7] ^ h,
	}
}
