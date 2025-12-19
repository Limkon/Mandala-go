// 文件路径: mandala-go/core/tun/device.go

package tun

import (
	"fmt"
	"log"
	"os"
	"syscall"

	"gvisor.dev/gvisor/pkg/tcpip/link/fdbased"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
)

type Device struct {
	fd   int
	file *os.File
	mtu  uint32
}

func NewDevice(fd int, mtu uint32) (*Device, error) {
	log.Printf("GoLog: [Device] Init - FD: %d, MTU: %d", fd, mtu)

	// 1. 强制设置为非阻塞模式 (这对 gVisor 是必须的)
	if err := syscall.SetNonblock(fd, true); err != nil {
		log.Printf("GoLog: [Device] CRITICAL - Failed to set non-blocking: %v", err)
		return nil, fmt.Errorf("set nonblock: %v", err)
	}

	f := os.NewFile(uintptr(fd), "tun")
	
	return &Device{
		fd:   fd,
		file: f,
		mtu:  mtu,
	}, nil
}

func (d *Device) LinkEndpoint() stack.LinkEndpoint {
	// 2. 创建 Endpoint 配置
	// Android VPN Service 创建的是 L3 TUN 设备 (纯 IP 包)
	// 必须关闭 EthernetHeader，并强制由软件计算校验和
	ep, err := fdbased.New(&fdbased.Options{
		FDs: []int{d.fd},
		MTU: d.mtu,
		
		// 关键配置组：
		EthernetHeader:    false, // 确保是 TUN 模式而非 TAP
		// PacketInfo:     false, // [已删除] 旧版 gVisor 不支持此字段，删除以修复编译错误
		RXChecksumOffload: false, // 关闭接收卸载
		TXChecksumOffload: false, // 关闭发送卸载 (强制 gVisor 计算 Checksum)
	})

	if err != nil {
		log.Printf("GoLog: [Device] Failed to create endpoint: %v", err)
		return nil
	}

	log.Println("GoLog: [Device] Endpoint created. RX/TX Checksum Offload: DISABLED")
	return ep
}

func (d *Device) Close() {
	log.Println("GoLog: [Device] Closing...")
	if d.file != nil {
		d.file.Close()
	}
}
