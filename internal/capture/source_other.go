//go:build !linux

package capture

import "errors"

func Open(iface string) (PacketSource, error) {
	return nil, errors.New("数据包采集仅支持 Linux")
}
