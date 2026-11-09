//go:build linux

package capture

import (
	"errors"
	"fmt"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gopacket/gopacket/afpacket"
)

func Open(iface string) (PacketSource, error) {
	h, err := afpacket.NewTPacket(
		afpacket.OptInterface(iface),
		afpacket.OptTPacketVersion(afpacket.TPacketVersion3),
		afpacket.OptFrameSize(4096),
		afpacket.OptBlockSize(4096*128),
		afpacket.OptNumBlocks(16),
		afpacket.OptBlockTimeout(100*time.Millisecond),
		afpacket.OptPollTimeout(500*time.Millisecond),
	)
	if err != nil {
		if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) {
			return nil, fmt.Errorf("没有抓包权限（需要 root 或 CAP_NET_RAW）: %w", err)
		}
		return nil, err
	}
	raw, err := tcpUDPFilter()
	if err != nil {
		h.Close()
		return nil, err
	}
	if err := h.SetBPF(raw); err != nil {
		h.Close()
		return nil, fmt.Errorf("设置 BPF 失败: %w", err)
	}
	return &afSource{h: h}, nil
}

type afSource struct {
	h        *afpacket.TPacket
	drops    atomic.Uint64
	lastStat time.Time
}

func (a *afSource) Read() ([]byte, error) {
	data, _, err := a.h.ZeroCopyReadPacketData()
	if err != nil {
		if errors.Is(err, afpacket.ErrTimeout) {
			a.pollStats()
			return nil, ErrTimeout
		}
		return nil, err
	}
	buf := make([]byte, len(data))
	copy(buf, data)
	a.pollStats()
	return buf, nil
}

func (a *afSource) pollStats() {
	if !a.lastStat.IsZero() && time.Since(a.lastStat) < time.Second {
		return
	}
	a.lastStat = time.Now()
	ss, ss3, err := a.h.SocketStats()
	if err != nil {
		return
	}
	drops := uint64(ss3.Drops())
	if drops == 0 {
		drops = uint64(ss.Drops())
	}
	a.drops.Store(drops)
}

func (a *afSource) Drops() uint64 { return a.drops.Load() }

func (a *afSource) Close() error {
	a.h.Close()
	return nil
}
