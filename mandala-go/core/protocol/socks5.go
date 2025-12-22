// 文件路径: mandala-go/core/protocol/socks5.go

package protocol

import (
	"errors"
	"fmt"
	"io"
	"net"
)

// HandshakeSocks5 实现与 Cloudflare Worker (SOCKS5 认证模式) 的精準對接
func HandshakeSocks5(conn net.Conn, user, pass, host string, port int) error {
	// 1. 发送版本和支持的方法
	// 宣告支持 0x00 (无需认证) 和 0x02 (用户密码认证)
	if _, err := conn.Write([]byte{0x05, 0x02, 0x00, 0x02}); err != nil {
		return err
	}

	// 2. 读取服务端选择的方法
	methodBuf := make([]byte, 2)
	if _, err := io.ReadFull(conn, methodBuf); err != nil {
		return fmt.Errorf("read method selection failed: %v", err)
	}

	if methodBuf[0] != 0x05 {
		return fmt.Errorf("invalid socks version: %d", methodBuf[0])
	}

	// 3. 处理认证
	// 如果服务端返回 0x02，则执行 RFC 1929 认证
	if methodBuf[1] == 0x02 {
		if user == "" || pass == "" {
			return errors.New("server chose auth but no credentials provided")
		}

		// 认证包: [1, ULEN, USER, PLEN, PASS]
		authBuf := make([]byte, 0, 3+len(user)+len(pass))
		authBuf = append(authBuf, 0x01)
		authBuf = append(authBuf, byte(len(user)))
		authBuf = append(authBuf, []byte(user)...)
		authBuf = append(authBuf, byte(len(pass)))
		authBuf = append(authBuf, []byte(pass)...)

		if _, err := conn.Write(authBuf); err != nil {
			return err
		}

		// 读取认证响应: [1, STATUS]
		resBuf := make([]byte, 2)
		if _, err := io.ReadFull(conn, resBuf); err != nil {
			return fmt.Errorf("read auth status failed: %v", err)
		}

		if resBuf[1] != 0x00 {
			return errors.New("socks5 authentication failed at remote server")
		}
	} else if methodBuf[1] != 0x00 {
		return fmt.Errorf("server rejected auth methods: %d", methodBuf[1])
	}

	// 4. 发送 CONNECT 请求
	req := []byte{0x05, 0x01, 0x00}
	addrBytes, err := ToSocksAddr(host, port)
	if err != nil {
		return err
	}
	req = append(req, addrBytes...)

	if _, err := conn.Write(req); err != nil {
		return err
	}

	// 5. 读取 SOCKS5 响应 (严格对齐服务端的 10 位元組回應)
	// 服务端固定返回: [5, 0, 0, 1, 0, 0, 0, 0, 0, 0]
	head := make([]byte, 4)
	if _, err := io.ReadFull(conn, head); err != nil {
		return fmt.Errorf("read connect response head failed: %v", err)
	}

	if head[1] != 0x00 {
		return fmt.Errorf("socks5 connect failed with status: 0x%02x", head[1])
	}

	var addrLen int
	switch head[3] {
	case 0x01: // IPv4
		addrLen = 4
	case 0x03: // Domain
		lenByte := make([]byte, 1)
		io.ReadFull(conn, lenByte)
		addrLen = int(lenByte[0])
	case 0x04: // IPv6
		addrLen = 16
	default:
		return fmt.Errorf("unknown ATYP: %d", head[3])
	}

	// 读取地址内容和 2 字节端口
	rest := make([]byte, addrLen+2)
	if _, err := io.ReadFull(conn, rest); err != nil {
		return err
	}

	return nil
}
