// 文件路径: mandala-go/core/protocol/socks5.go

package protocol

import (
	"errors"
	"fmt"
	"io"
	"net"
)

// HandshakeSocks5 实现了支持认证的 SOCKS5 客户端握手协议
func HandshakeSocks5(conn net.Conn, user, pass, host string, port int) error {
	// 1. 发送版本和支持的认证方法
	// 同时宣告支持无需认证(0x00)和用户名密码认证(0x02)
	if user != "" && pass != "" {
		if _, err := conn.Write([]byte{0x05, 0x02, 0x00, 0x02}); err != nil {
			return err
		}
	} else {
		if _, err := conn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
			return err
		}
	}

	// 2. 读取服务端选择的方法
	// 严格读取 2 个字节 [VER, METHOD]
	methodBuf := make([]byte, 2)
	if _, err := io.ReadFull(conn, methodBuf); err != nil {
		return fmt.Errorf("read method selection failed: %v", err)
	}

	if methodBuf[0] != 0x05 {
		return fmt.Errorf("invalid socks version: %d", methodBuf[0])
	}

	// 3. 处理认证
	if methodBuf[1] == 0x02 {
		// 服务端要求用户名/密码认证
		if user == "" || pass == "" {
			return errors.New("server requires auth but no credentials provided")
		}

		// 构造认证包: [VER(0x01), ULEN, USER, PLEN, PASS]
		authBuf := make([]byte, 0, 3+len(user)+len(pass))
		authBuf = append(authBuf, 0x01)
		authBuf = append(authBuf, byte(len(user)))
		authBuf = append(authBuf, []byte(user)...)
		authBuf = append(authBuf, byte(len(pass)))
		authBuf = append(authBuf, []byte(pass)...)

		if _, err := conn.Write(authBuf); err != nil {
			return fmt.Errorf("write auth failed: %v", err)
		}

		// 读取认证响应: [VER, STATUS]
		resBuf := make([]byte, 2)
		if _, err := io.ReadFull(conn, resBuf); err != nil {
			return fmt.Errorf("read auth response failed: %v", err)
		}

		if resBuf[1] != 0x00 { // 状态码 0x00 表示成功
			return errors.New("socks5 authentication failed")
		}

	} else if methodBuf[1] != 0x00 {
		return fmt.Errorf("unsupported auth method: %d", methodBuf[1])
	}

	// 4. 发送连接请求 (CONNECT)
	// 格式: [VER, CMD, RSV, ATYP, DST.ADDR, DST.PORT]
	req := []byte{0x05, 0x01, 0x00}
	addrBytes, err := ToSocksAddr(host, port)
	if err != nil {
		return err
	}
	req = append(req, addrBytes...)

	if _, err := conn.Write(req); err != nil {
		return fmt.Errorf("write connect request failed: %v", err)
	}

	// 5. 读取连接响应 (BND.ADDR/PORT)
	// 精确读取，防止吃掉后续业务数据
	
	// 先读前 4 个字节: [VER, REP, RSV, ATYP]
	head := make([]byte, 4)
	if _, err := io.ReadFull(conn, head); err != nil {
		return fmt.Errorf("read connect response head failed: %v", err)
	}

	if head[1] != 0x00 {
		return fmt.Errorf("socks5 connect failed with status: 0x%02x", head[1])
	}

	// 根据 ATYP (地址类型) 决定还需要读多少字节
	var addrLen int
	switch head[3] {
	case 0x01: // IPv4
		addrLen = 4
	case 0x03: // Domain
		lenByte := make([]byte, 1)
		if _, err := io.ReadFull(conn, lenByte); err != nil {
			return err
		}
		addrLen = int(lenByte[0])
	case 0x04: // IPv6
		addrLen = 16
	default:
		return fmt.Errorf("unknown address type: %d", head[3])
	}

	// 读取剩余的地址内容 + 2字节端口
	restSize := addrLen + 2
	rest := make([]byte, restSize)
	if _, err := io.ReadFull(conn, rest); err != nil {
		return fmt.Errorf("read connect response body failed: %v", err)
	}

	// 握手完成
	return nil
}
