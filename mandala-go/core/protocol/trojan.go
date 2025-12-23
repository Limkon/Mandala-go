package protocol

import (
	"bytes"
	"log"
)

// BuildTrojanPayload 构造标准 Trojan 握手包
// 结构: Hash(pass) + CRLF + CMD(1) + SOCKS5_ADDR + CRLF
func BuildTrojanPayload(password, targetHost string, targetPort int) ([]byte, error) {
	log.Printf("[Trojan] 正在构造握手包 -> %s:%d", targetHost, targetPort)
	var buf bytes.Buffer

	// 1. 密码哈希
	passHash := TrojanPasswordHash(password)
	buf.WriteString(passHash)
	buf.Write([]byte{0x0D, 0x0A}) 
	log.Printf("[Trojan] 密码哈希已写入")

	// 2. 指令 (0x01 Connect)
	buf.WriteByte(0x01)

	// 3. 目标地址
	addr, err := ToSocksAddr(targetHost, targetPort)
	if err != nil {
		log.Printf("[Trojan] 地址解析失败: %v", err)
		return nil, err
	}
	buf.Write(addr)

	// 4. CRLF 结尾
	buf.Write([]byte{0x0D, 0x0A})

	log.Printf("[Trojan] 握手包构造成功")
	return buf.Bytes(), nil
}
