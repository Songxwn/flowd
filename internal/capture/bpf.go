package capture

import (
	"errors"

	"golang.org/x/net/bpf"
)

// ErrTimeout 表示这次读取在轮询时限内没有新数据包。
var ErrTimeout = errors.New("capture timeout")

// PacketSource 是一块网卡上的数据包来源。
type PacketSource interface {
	Read() ([]byte, error)
	Drops() uint64
	Close() error
}

func captureFilter() ([]bpf.RawInstruction, error) {
	return bpf.Assemble([]bpf.Instruction{
		bpf.LoadAbsolute{Off: 12, Size: 2},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 0x0800, SkipTrue: 0, SkipFalse: 4},
		bpf.LoadAbsolute{Off: 23, Size: 1},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 6, SkipTrue: 7, SkipFalse: 0},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 17, SkipTrue: 6, SkipFalse: 0},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 1, SkipTrue: 5, SkipFalse: 6},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 0x86dd, SkipTrue: 0, SkipFalse: 5},
		bpf.LoadAbsolute{Off: 20, Size: 1},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 6, SkipTrue: 2, SkipFalse: 0},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 17, SkipTrue: 1, SkipFalse: 0},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 58, SkipTrue: 0, SkipFalse: 1},
		bpf.RetConstant{Val: 65535},
		bpf.RetConstant{Val: 0},
	})
}
