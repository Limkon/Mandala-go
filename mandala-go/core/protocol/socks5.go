package protocol

import (
	"fmt"
	"io"
	"net"
)

// HandshakeSocks5 执行 SOCKS5 客户端握手
func HandshakeSocks5(conn io.ReadWriter, username, password, targetHost string, targetPort int) error {
	// 1. 发送版本和支持的认证方法
	var methods []byte
	if username != "" {
		methods = []byte{0x02} // 仅支持密码认证
	} else {
		methods = []byte{0x00} // 无认证
	}
	
	initBuf := make([]byte, 2+len(methods))
	initBuf[0] = 0x05 
	initBuf[1] = byte(len(methods))
	copy(initBuf[2:], methods)
	
	if _, err := conn.Write(initBuf); err != nil {
		return fmt.Errorf("socks5 init write failed: %v", err)
	}

	// 2. 读取服务端选定的方法
	resp := make([]byte, 2)
	if _, err := io.ReadFull(conn, resp); err != nil {
		return fmt.Errorf("socks5 init read failed: %v", err)
	}
	if resp[0] != 0x05 {
		return fmt.Errorf("socks5 invalid version: %d", resp[0])
	}

	authMethod := resp[1]

	// 3. 认证逻辑
	if authMethod == 0x02 {
		uLen := len(username)
		pLen := len(password)
		if uLen > 255 || pLen > 255 {
			return fmt.Errorf("socks5 username/password too long")
		}
		
		authBuf := make([]byte, 3+uLen+pLen)
		authBuf[0] = 0x01 
		authBuf[1] = byte(uLen)
		copy(authBuf[2:], username)
		authBuf[2+uLen] = byte(pLen)
		copy(authBuf[3+uLen:], password)
		
		if _, err := conn.Write(authBuf); err != nil {
			return fmt.Errorf("socks5 auth write failed: %v", err)
		}
		
		authResp := make([]byte, 2)
		if _, err := io.ReadFull(conn, authResp); err != nil {
			return fmt.Errorf("socks5 auth resp read failed: %v", err)
		}
		
		if authResp[1] != 0x00 {
			return fmt.Errorf("socks5 authentication failed (status: 0x%02x)", authResp[1])
		}
	} else if authMethod != 0x00 && authMethod != 0xFF {
		return fmt.Errorf("socks5 unsupported auth method: 0x%02x", authMethod)
	}

	// 4. 发送连接请求 (内置地址生成逻辑，确保格式正确)
	// 格式: 05 01 00 [ATYP] [ADDR] [PORT]
	head := []byte{0x05, 0x01, 0x00}
	
	addrPayload, err := buildSocksAddr(targetHost, targetPort)
	if err != nil {
		return err
	}
	
	if _, err := conn.Write(append(head, addrPayload...)); err != nil {
		return fmt.Errorf("socks5 connect write failed: %v", err)
	}

	// 5. 读取连接响应 (必须完整读取，防止污染后续数据流)
	connRespHead := make([]byte, 4)
	if _, err := io.ReadFull(conn, connRespHead); err != nil {
		return fmt.Errorf("socks5 connect resp header read failed: %v", err)
	}

	if connRespHead[1] != 0x00 {
		return fmt.Errorf("socks5 connect failed with error: 0x%02x", connRespHead[1])
	}

	// [修复] 根据 ATYP 读取并丢弃 BND 地址
	// 这一步至关重要：如果不读完，这些字节会被当成网页数据发给浏览器，导致网页打不开
	var left int
	switch connRespHead[3] {
	case 0x01: left = 4 + 2 // IPv4 + Port
	case 0x04: left = 16 + 2 // IPv6 + Port
	case 0x03: // Domain
		lenByte := make([]byte, 1)
		if _, err := io.ReadFull(conn, lenByte); err != nil {
			return fmt.Errorf("socks5 read domain len failed: %v", err)
		}
		left = int(lenByte[0]) + 2 // Domain bytes + Port
	default:
		return fmt.Errorf("socks5 invalid address type: 0x%02x", connRespHead[3])
	}

	if left > 0 {
		discard := make([]byte, left)
		if _, err := io.ReadFull(conn, discard); err != nil {
			return fmt.Errorf("socks5 connect resp body read failed: %v", err)
		}
	}

	return nil
}

// buildSocksAddr 生成标准的 [ATYP][ADDR][PORT]
func buildSocksAddr(host string, port int) ([]byte, error) {
	ip := net.ParseIP(host)
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
		if len(host) > 255 {
			return nil, fmt.Errorf("domain too long")
		}
		buf = make([]byte, 1+1+len(host)+2)
		buf[0] = 0x03 // Domain
		buf[1] = byte(len(host))
		copy(buf[2:], host)
	}
	
	// Append Port
	l := len(buf)
	buf[l-2] = byte(port >> 8)
	buf[l-1] = byte(port)
	
	return buf, nil
}
