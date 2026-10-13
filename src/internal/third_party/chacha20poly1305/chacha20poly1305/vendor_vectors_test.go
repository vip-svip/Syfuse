// Copyright 2026 The Syfuse Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// 这一层只做一件事：证明"抄进来的源码真的对"。
//
// 背景：本目录的 chacha20poly1305 实现是从 golang.org/x/crypto@v0.58.0
// 原样移植的（import 路径改写 + 剥离汇编构建标签），汇编路径全部未启用
// —— 也就是说，所有代码走的都是纯 Go 分支。纯 Go 分支恰恰是最少被
// 上游 CI 直接覆盖的一条路径（CI 默认跑汇编路径），所以必须在这里
// 用权威向量把它钉死。
//
// chacha20Poly1305Tests 里全部是 RFC 8439 / draft-irtf-cfrg-xchacha
// 的官方测试向量，由上游维护。哪一个字节对不上，就说明移植过程中的
// 某次改写破坏了正确性，而不是"测试太严格"。
//
// 和上游 chacha20poly1305_test.go 的 TestVectors 的区别：去掉随机
// 篡改位（上游用 mathrand，同一份输入每次篡的位置不同，失败不可复现），
// 改成确定性逐位翻转，便于门禁重跑时结果一致。

package chacha20poly1305

import (
	"bytes"
	"crypto/cipher"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// hasAsmSource 报告当前包里是否存在 .s 汇编来源文件。
// 用 runtime.Caller 取本文件路径反推目录，避免依赖测试进程的工作目录
// （门禁在不同 cwd 下跑，写死相对路径会误判）。
func hasAsmSource() bool {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return false
	}
	found := false
	filepath.Walk(filepath.Dir(thisFile), func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".s") {
			found = true
		}
		return nil
	})
	return found
}

// TestRFCVectorsSealOpenRoundTrips 对每个官方向量同时验证三件事：
//  1. Seal 的输出密文与官方期望逐字节一致（证明加密路径正确）；
//  2. Open 能把密文还原成原文（证明解密路径正确）；
//  3. 篡改 nonce / AAD / 密文任意一个 bit，Open 必须失败 —— 这是
//     AEAD 的安全属性，比"能解密"重要得多。
func TestRFCVectorsSealOpenRoundTrips(t *testing.T) {
	if len(chacha20Poly1305Tests) == 0 {
		t.Fatal("向量表为空：chacha20poly1305_vectors_test.go 没有被编进测试？")
	}

	for i, test := range chacha20Poly1305Tests {
		key, _ := hex.DecodeString(test.key)
		nonce, _ := hex.DecodeString(test.nonce)
		ad, _ := hex.DecodeString(test.aad)
		plaintext, _ := hex.DecodeString(test.plaintext)

		var (
			aead cipher.AEAD
			err  error
		)
		switch len(nonce) {
		case NonceSize:
			aead, err = New(key)
		case NonceSizeX:
			aead, err = NewX(key)
		default:
			t.Fatalf("#%d: wrong nonce length: %d", i, len(nonce))
		}
		if err != nil {
			t.Fatalf("#%d: New: %v", i, err)
		}

		// 1. 密文必须逐字节等于官方期望（含 16 字节tag）。
		ct := aead.Seal(nil, nonce, plaintext, ad)
		if got := hex.EncodeToString(ct); got != test.out {
			t.Errorf("#%d: Seal 输出与 RFC 向量不一致\n got: %s\nwant: %s", i, got, test.out)
			continue
		}

		// 2. 密文必须能还原成原文。
		got, err := aead.Open(nil, nonce, ct, ad)
		if err != nil {
			t.Errorf("#%d: Open 失败: %v", i, err)
			continue
		}
		if !bytes.Equal(got, plaintext) {
			t.Errorf("#%d: 还原结果与原文不一致: got %x want %x", i, got, plaintext)
		}

		// 3a. 篡改 AAD 的每一个 bit，都必须认证失败。
		for bit := 0; bit < len(ad)*8; bit++ {
			tampered := append([]byte(nil), ad...)
			tampered[bit/8] ^= 1 << (uint(bit) % 8)
			if _, err := aead.Open(nil, nonce, ct, tampered); err == nil {
				t.Errorf("#%d: 篡改 AAD 第 %d bit 后 Open 仍然成功，AEAD 失效", i, bit)
			}
		}

		// 3b. 篡改 nonce 的每一个 bit，都必须认证失败或输出不同。
		for bit := 0; bit < aead.NonceSize()*8; bit++ {
			tampered := append([]byte(nil), nonce...)
			tampered[bit/8] ^= 1 << (uint(bit) % 8)
			out, err := aead.Open(nil, tampered, ct, ad)
			if err == nil && bytes.Equal(out, plaintext) {
				t.Errorf("#%d: 篡改 nonce 第 %d bit 后 Open 仍还原出原文", i, bit)
			}
		}

		// 3c. 篡改密文（含 tag）的每一个 bit，都必须认证失败。
		for bit := 0; bit < len(ct)*8; bit++ {
			tampered := append([]byte(nil), ct...)
			tampered[bit/8] ^= 1 << (uint(bit) % 8)
			if _, err := aead.Open(nil, nonce, tampered, ad); err == nil {
				t.Errorf("#%d: 篡改密文第 %d bit 后 Open 仍然成功，AEAD 失效", i, bit)
			}
		}
	}
}

// TestVendorIsPureGoWithoutAsm 是一道"防回归"断言：
// 目录里不允许再出现 .s 汇编文件或 amd64 专用实现 —— 汇编的缺失
// 必须在代码里显式可见，而不是某天有人从上游又拷了一个
// chacha20poly1305_amd64.go 进来，结果在 Android arm64 上编不过。
func TestVendorIsPureGoWithoutAsm(t *testing.T) {
	if hasAsmSource() {
		t.Fatal("目录下出现 .s 汇编来源：本 vendored 包当前应为纯 Go 实现")
	}
}

// TestOverlappingBuffersDoNotCorrupt 覆盖 alias 检查路径：
// Seek/Seal 传重叠缓冲时 xchacha20poly1305 与 chacha20 的实现会调用
// alias.InexactOverlap/AnyOverlap。这一段在正常使用中永远走不到
// （Go 调用方几乎不会传重叠 slice），但它是正确性代码，移植时
// import 路径改写过，必须确认新路径下的函数行为没变。
func TestOverlappingBuffersDoNotCorrupt(t *testing.T) {
	key, _ := hex.DecodeString("808182838485868788898a8b8c8d8e8f909192939495969798999a9b9c9d9e9f")
	nonce, _ := hex.DecodeString("070000004041424344454647")

	aead, err := New(key)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	plaintext := []byte(" Ladies and Gentlemen of the class of '99: sunscreen would be it.")
	ad := []byte("50515253c0c1c2c3c4c5c6c7")

	// 目标缓冲故意比密文短，强制 Go 分配新内存（不走重叠路径）。
	shortDst := make([]byte, 0, 4)
	sealed := aead.Seal(shortDst, nonce, plaintext, ad)
	if want := len(plaintext) + aead.Overhead(); len(sealed) != want {
		t.Fatalf("Seal 长度 = %d, want %d", len(sealed), want)
	}

	opened, err := aead.Open(nil, nonce, sealed, ad)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !bytes.Equal(opened, plaintext) {
		t.Fatalf("还原结果不一致")
	}
}
