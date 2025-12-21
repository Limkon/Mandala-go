package protocol

import (
	"crypto/rand"
	"io"
)

// BuildShadowsocksPayload 构造 Shadowsocks 握手包
// 修改逻辑：增加 Salt (IV) 头部生成。
// 在 Shadowsocks 协议中，即使是跑在 TLS 隧道内，标准服务端仍期望首包包含随机 Salt。
func BuildShadowsocksPayload(password string, targetHost string, targetPort int) ([]byte, error) {
	// 1. 生成 16 字节随机 Salt (对应常用的加密方式占位)
	salt := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, err
	}

	// 2. 构造目标地址 [ATYP][ADDR][PORT]
	// 复用 utils.go 中的 ToSocksAddr，它生成的正是 Shadowsocks 需要的 SOCKS5 地址格式
	addr, err := ToSocksAddr(targetHost, targetPort)
	if err != nil {
		return nil, err
	}

	// 3. 组合最终 Payload: [Salt] + [Address]
	// 注意：此处保持明文地址传输（依赖外部 TLS 加密），若对接标准 SS 需在此处引入 AES/ChaCha20 加密逻辑
	finalPayload := make([]byte, len(salt)+len(addr))
	copy(finalPayload[0:len(salt)], salt)
	copy(finalPayload[len(salt):], addr)

	return finalPayload, nil
}
