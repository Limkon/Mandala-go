// 文件路径: mandala-go/core/protocol/socks5.go

package protocol

import (
	"errors"
	"fmt"
	"io"
	"net"
)

// HandshakeSocks5 实现了标准 SOCKS5 客户端握手协议
func HandshakeSocks5(conn net.Conn, user, pass, host string, port int) error {
	// 1. 发送版本和支持的认证方法
	// [优化] 同时提供“无需认证”(0x00) 和“用户名密码”(0x02)，让服务端决定
	if user != "" && pass != "" {
		// [版本5, 支持2种方法, 0x00, 0x02]
		if _, err := conn.Write([]byte{0x05, 0x02, 0x00, 0x02}); err != nil {
			return err
		}
	} else {
		// [版本5, 支持1种方法, 0x00]
		if _, err := conn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
			return err
		}
	}

	// 2. 读取服务端选择的方法
	// 严格读取 2 个字节 [VER, METHOD]
	methodBuf := make([]byte, 2)
	if _, err := io.ReadFull(conn, methodBuf); err != nil {
		return fmt.Errorf("读取方法选择失败: %v", err)
	}

	if methodBuf[0] != 0x05 {
		return fmt.Errorf("无效的 SOCKS 版本: %d", methodBuf[0])
	}

	// 3. 根据服务端选择的方法执行认证
	if methodBuf[1] == 0x02 {
		// 执行用户名密码认证
		if user == "" || pass == "" {
			return errors.New("服务器要求认证但未提供凭据")
		}

		// 认证包格式: [1, 用户名长度, 用户名, 密码长度, 密码]
		authBuf := make([]byte, 0, 3+len(user)+len(pass))
		authBuf = append(authBuf, 0x01)
		authBuf = append(authBuf, byte(len(user)))
		authBuf = append(authBuf, []byte(user)...)
		authBuf = append(authBuf, byte(len(pass)))
		authBuf = append(authBuf, []byte(pass)...)

		if _, err := conn.Write(authBuf); err != nil {
			return fmt.Errorf("写入认证信息失败: %v", err)
		}

		// 读取认证响应: [版本, 状态]
		resBuf := make([]byte, 2)
		if _, err := io.ReadFull(conn, resBuf); err != nil {
			return fmt.Errorf("读取认证响应失败: %v", err)
		}

		if resBuf[1] != 0x00 {
			return errors.New("SOCKS5 认证失败")
		}

	} else if methodBuf[1] != 0x00 {
		// 如果服务端返回 0xFF 或其他不支持的方法
		return fmt.Errorf("不支持的认证方法: %d", methodBuf[1])
	}

	// 4. 发送连接请求 (CONNECT)
	// 格式: [VER(5), CMD(1), RSV(0), ATYP(1), ADDR, PORT]
	req := []byte{0x05, 0x01, 0x00}
	addrBytes, err := ToSocksAddr(host, port)
	if err != nil {
		return err
	}
	req = append(req, addrBytes...)

	if _, err := conn.Write(req); err != nil {
		return fmt.Errorf("写入连接请求失败: %v", err)
	}

	// 5. 读取连接响应 (BND.ADDR/PORT)
	// 必须分段精确读取，防止读入后续的业务数据
	
	// 先读前 4 个字节: [VER, REP, RSV, ATYP]
	head := make([]byte, 4)
	if _, err := io.ReadFull(conn, head); err != nil {
		return fmt.Errorf("读取连接响应头失败: %v", err)
	}

	if head[1] != 0x00 {
		return fmt.Errorf("SOCKS5 连接失败，状态码: 0x%02x", head[1])
	}

	// 根据地址类型 (ATYP) 确定后续长度
	var addrLen int
	switch head[3] {
	case 0x01: // IPv4 (4字节)
		addrLen = 4
	case 0x03: // Domain (首字节是长度)
		domainLenBuf := make([]byte, 1)
		if _, err := io.ReadFull(conn, domainLenBuf); err != nil {
			return err
		}
		addrLen = int(domainLenBuf[0])
	case 0x04: // IPv6 (16字节)
		addrLen = 16
	default:
		return fmt.Errorf("未知的地址类型: %d", head[3])
	}

	// 读取剩余的地址内容 + 2字节端口
	restSize := addrLen + 2
	rest := make([]byte, restSize)
	if _, err := io.ReadFull(conn, rest); err != nil {
		return fmt.Errorf("读取连接响应体失败: %v", err)
	}

	// 握手彻底完成
	return nil
}
