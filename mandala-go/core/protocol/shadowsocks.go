package protocol

import "log"

// BuildShadowsocksPayload 构造 Shadowsocks 握手包
// 在 Mandala 架构中，Shadowsocks over TLS/WebSocket 只需要发送标准 SOCKS5 格式的目标地址
// 格式: [ATYP][ADDR][PORT]
func BuildShadowsocksPayload(targetHost string, targetPort int) ([]byte, error) {
	log.Printf("[Shadowsocks] 构造地址 Payload: %s:%d", targetHost, targetPort)
	
	// 直接复用 utils.go 中的 ToSocksAddr，它生成的正是 SS 需要的格式
	addr, err := ToSocksAddr(targetHost, targetPort)
	if err != nil {
		log.Printf("[Shadowsocks] 地址转换失败: %v", err)
		return nil, err
	}
	
	return addr, nil
}
