package protocol

import (
	"bytes"
	"encoding/binary"
	"errors"
	"net"
	"strconv"
)

// BuildShadowsocksPayload 构造 Shadowsocks 握手包
// [修复] 手动实现 SOCKS5 地址格式构造，确保与 C 版本 proxy.c 行为完全一致。
// 格式: [ATYP] [ADDR] [PORT]
func BuildShadowsocksPayload(targetHost string, targetPort int) ([]byte, error) {
	var buf bytes.Buffer

	// 尝试解析为 IP
	ip := net.ParseIP(targetHost)
	if ip != nil {
		if ip4 := ip.To4(); ip4 != nil {
			// IPv4: 0x01 + 4 bytes
			buf.WriteByte(0x01)
			buf.Write(ip4)
		} else {
			// IPv6: 0x04 + 16 bytes
			buf.WriteByte(0x04)
			buf.Write(ip.To16())
		}
	} else {
		// Domain: 0x03 + Len + String
		if len(targetHost) > 255 {
			return nil, errors.New("domain too long")
		}
		buf.WriteByte(0x03)
		buf.WriteByte(byte(len(targetHost)))
		buf.WriteString(targetHost)
	}

	// Port: 2 bytes Big Endian
	if targetPort < 0 || targetPort > 65535 {
		return nil, errors.New("invalid port: " + strconv.Itoa(targetPort))
	}
	portBuf := make([]byte, 2)
	binary.BigEndian.PutUint16(portBuf, uint16(targetPort))
	buf.Write(portBuf)

	return buf.Bytes(), nil
}
