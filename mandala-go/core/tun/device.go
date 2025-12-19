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
	log.Printf("GoLog: Device Init - FD: %d, MTU: %d", fd, mtu)

	// [核心] 强制将 FD 设置为非阻塞模式
	if err := syscall.SetNonblock(fd, true); err != nil {
		log.Printf("GoLog: CRITICAL ERROR - Failed to set non-blocking: %v", err)
		return nil, fmt.Errorf("set nonblock: %v", err)
	}
	log.Println("GoLog: Device - SetNonblock(true) success")

	f := os.NewFile(uintptr(fd), "tun")
	
	return &Device{
		fd:   fd,
		file: f,
		mtu:  mtu,
	}, nil
}

func (d *Device) LinkEndpoint() stack.LinkEndpoint {
	// 创建基于文件描述符的 Endpoint
	// [修复 Rx=0 问题]
	// Android TUN 设备要求写入的数据包必须有正确的校验和。
	// gVisor 默认开启 ChecksumOffload (假定网卡硬件会计算)，导致写入 TUN 的包校验和为 0，
	// 从而被 Android 内核丢弃。这里必须显式关闭 Offload。
	ep, err := fdbased.New(&fdbased.Options{
		FDs: []int{d.fd},
		MTU: d.mtu,
		// 明确声明这是 IP 层设备 (TUN)，不含以太网头
		EthernetHeader: false, 
		// 关键修复：关闭接收校验和卸载
		RXChecksumOffload: false, 
		// 关键修复：关闭发送校验和卸载，强制 gVisor 计算校验和
		TXChecksumOffload: false, 
	})

	if err != nil {
		log.Printf("GoLog: Failed to create fdbased endpoint: %v", err)
		return nil
	}

	log.Println("GoLog: Device - LinkEndpoint created successfully with ChecksumOffload DISABLED")
	return ep
}

func (d *Device) Close() {
	log.Println("GoLog: Device Closing...")
	if d.file != nil {
		d.file.Close()
	}
}
