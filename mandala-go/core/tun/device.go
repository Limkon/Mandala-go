package tun

import (
	"os"
	"syscall"

	"gvisor.dev/gvisor/pkg/buffer" // [修复] 使用正确的 buffer 包路径
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
)

// Device 实现 stack.LinkEndpoint 接口
type Device struct {
	fd         int
	mtu        uint32
	dispatcher stack.NetworkDispatcher
}

// NewDevice 创建 TUN 设备适配器
func NewDevice(fd int, mtu uint32) (*Device, error) {
	return &Device{
		fd:  fd,
		mtu: mtu,
	}, nil
}

// ---------------------------------------------------------
// 实现 stack.LinkEndpoint 接口方法
// ---------------------------------------------------------

func (d *Device) MTU() uint32 {
	return d.mtu
}

func (d *Device) Capabilities() stack.LinkEndpointCapabilities {
	return stack.CapabilityNone
}

func (d *Device) MaxHeaderLength() uint16 {
	return 0
}

func (d *Device) LinkAddress() tcpip.LinkAddress {
	return ""
}

// WritePackets 将数据包写入 TUN 设备
func (d *Device) WritePackets(pkts stack.PacketBufferList) (int, tcpip.Error) {
	count := 0
	for _, pkt := range pkts.AsSlice() {
		if err := d.writePacket(pkt); err != nil {
			break
		}
		count++
	}
	return count, nil
}

func (d *Device) writePacket(pkt *stack.PacketBuffer) tcpip.Error {
	// [修复] 适配 2023 gVisor API: 使用 ToView().AsSlice() 获取数据
	view := pkt.ToView()
	data := view.AsSlice()

	// 写入文件描述符
	if _, err := syscall.Write(d.fd, data); err != nil {
		// [修复] 使用 ErrClosedForSend 替代未定义的 ErrInvalidOption
		return &tcpip.ErrClosedForSend{}
	}
	return nil
}

func (d *Device) Attach(dispatcher stack.NetworkDispatcher) {
	d.dispatcher = dispatcher
	// 启动读取循环
	go d.readLoop()
}

func (d *Device) IsAttached() bool {
	return d.dispatcher != nil
}

func (d *Device) Wait() {}

func (d *Device) ARPHardwareType() header.ARPHardwareType {
	return header.ARPHardwareNone
}

func (d *Device) AddHeader(pkt *stack.PacketBuffer) {}
func (d *Device) ParseHeader(pkt *stack.PacketBuffer) bool { return true }

// ---------------------------------------------------------
// 内部逻辑
// ---------------------------------------------------------

// readLoop 从 TUN 读取数据并注入协议栈
func (d *Device) readLoop() {
	buf := make([]byte, d.mtu)
	for {
		n, err := syscall.Read(d.fd, buf)
		if err != nil {
			return
		}
		if n <= 0 {
			continue
		}

		// 复制数据，因为 buf 会被重用
		data := make([]byte, n)
		copy(data, buf[:n])

		// [修复] 直接使用 buffer.MakeWithData(data) 创建 Payload
		// 这样避免了调用 undefined 的 NewViewFromBytes
		pkt := stack.NewPacketBuffer(stack.PacketBufferOptions{
			Payload: buffer.MakeWithData(data),
		})

		// 简单判断 IP 版本 (IPv4 vs IPv6)
		var proto tcpip.NetworkProtocolNumber
		if n > 0 {
			switch data[0] >> 4 {
			case 4:
				proto = header.IPv4ProtocolNumber
			case 6:
				proto = header.IPv6ProtocolNumber
			default:
				continue
			}
		}

		if d.dispatcher != nil {
			d.dispatcher.DeliverNetworkPacket(proto, pkt)
		}
		pkt.DecRef()
	}
}

func (d *Device) Close() error {
	return os.NewFile(uintptr(d.fd), "tun").Close()
}
