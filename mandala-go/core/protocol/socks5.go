package protocol

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
)

// HandshakeSocks5 执行 SOCKS5 客户端握手
func HandshakeSocks5(conn io.ReadWriter, username, password, targetHost string, targetPort int) error {
	log.Printf("[Socks5] Handshaking for %s:%d", targetHost, targetPort)

	// 1. 发送 Method
	methods := []byte{0x00} // 默认无认证
	if username != "" && password != "" {
		methods = []byte{0x02, 0x00} // 支持用户名/密码认证
	}
	initBuf := make([]byte, 2+len(methods))
	initBuf[0] = 0x05
	initBuf[1] = byte(len(methods))
	copy(initBuf[2:], methods)

	if _, err := conn.Write(initBuf); err != nil {
		return fmt.Errorf("write method selection failed: %v", err)
	}

	// 2. 读取 Method Response
	resp := make([]byte, 2)
	if _, err := io.ReadFull(conn, resp); err != nil {
		return fmt.Errorf("read method response failed: %v", err)
	}
	if resp[0] != 0x05 {
		return fmt.Errorf("invalid SOCKS version: %d", resp[0])
	}

	authMethod := resp[1]
	if authMethod == 0x02 && username != "" && password != "" {
		// 3. 用户名/密码认证
		uLen := len(username)
		pLen := len(password)
		authBuf := make([]byte, 3+uLen+pLen)
		authBuf[0] = 0x01
		authBuf[1] = byte(uLen)
		copy(authBuf[2:], username)
		authBuf[2+uLen] = byte(pLen)
		copy(authBuf[3+uLen:], password)

		if _, err := conn.Write(authBuf); err != nil {
			return fmt.Errorf("write auth failed: %v", err)
		}

		authResp := make([]byte, 2)
		if _, err := io.ReadFull(conn, authResp); err != nil {
			return fmt.Errorf("read auth response failed: %v", err)
		}
		if authResp[1] != 0x00 {
			return fmt.Errorf("authentication failed, status: 0x%02x", authResp[1])
		}
	} else if authMethod != 0x00 {
		return fmt.Errorf("unsupported authentication method: 0x%02x", authMethod)
	}

	// 4. 发送 CONNECT 请求
	var buf bytes.Buffer
	buf.Write([]byte{0x05, 0x01, 0x00}) // VER, CMD=CONNECT, RSV

	ip := net.ParseIP(targetHost)
	if ip != nil {
		if ip4 := ip.To4(); ip4 != nil {
			buf.WriteByte(0x01) // IPv4
			buf.Write(ip4)
		} else {
			buf.WriteByte(0x04) // IPv6
			buf.Write(ip.To16())
		}
	} else {
		if len(targetHost) > 255 {
			return errors.New("domain name too long")
		}
		buf.WriteByte(0x03) // Domain
		buf.WriteByte(byte(len(targetHost)))
		buf.WriteString(targetHost)
	}

	portBuf := make([]byte, 2)
	binary.BigEndian.PutUint16(portBuf, uint16(targetPort))
	buf.Write(portBuf)

	if _, err := conn.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("write connect request failed: %v", err)
	}

	// 5. 读取响应头部 (4 字节)
	connRespHead := make([]byte, 4)
	if _, err := io.ReadFull(conn, connRespHead); err != nil {
		return fmt.Errorf("read connect response header failed: %v", err)
	}
	if connRespHead[1] != 0x00 {
		return fmt.Errorf("connect failed, status: 0x%02x", connRespHead[1])
	}

	// 6. 完整读取剩余的 BND.ADDR + PORT（必须消耗，否则流会残留）
	var left int
	switch connRespHead[3] {
	case 0x01: // IPv4
		left = 4 + 2
	case 0x04: // IPv6
		left = 16 + 2
	case 0x03: // Domain
		lenByte := make([]byte, 1)
		if _, err := io.ReadFull(conn, lenByte); err != nil {
			return fmt.Errorf("read domain length failed: %v", err)
		}
		left = int(lenByte[0]) + 2
	default:
		return fmt.Errorf("invalid address type: 0x%02x", connRespHead[3])
	}

	discard := make([]byte, left)
	if _, err := io.ReadFull(conn, discard); err != nil {
		return fmt.Errorf("read connect response body failed: %v", err)
	}

	log.Printf("[Socks5] Handshake successful for %s:%d", targetHost, targetPort)
	return nil
}
