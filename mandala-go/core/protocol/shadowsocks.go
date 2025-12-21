package protocol

import (
	"fmt"
	"net"
)

// BuildShadowsocksPayload 构造 Shadowsocks 握手包
// 格式: [ATYP][ADDR][PORT]
// 修复：内置地址生成逻辑，确保不依赖外部可能有问题的实现
func BuildShadowsocksPayload(targetHost string, targetPort int) ([]byte, error) {
	ip := net.ParseIP(targetHost)
	var buf []byte
	
	if ip != nil {
		if ip4 := ip.To4(); ip4 != nil {
			buf = make([]byte, 1+4+2)
			buf[0] = 0x01 // IPv4
			copy(buf[1:], ip4)
		} else {
			buf = make([]byte, 1+16+2)
			buf[0] = 0x04 // IPv6
			copy(buf[1:], ip)
		}
	} else {
		if len(targetHost) > 255 {
			return nil, fmt.Errorf("domain too long")
		}
		buf = make([]byte, 1+1+len(targetHost)+2)
		buf[0] = 0x03 // Domain
		buf[1] = byte(len(targetHost))
		copy(buf[2:], targetHost)
	}
	
	// Append Port
	l := len(buf)
	buf[l-2] = byte(targetPort >> 8)
	buf[l-1] = byte(targetPort)
	
	return buf, nil
}
