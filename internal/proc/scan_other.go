//go:build !linux

package proc

import "errors"

func Scan(root string) (*Table, error) {
	return nil, errors.New("进程关联仅支持 Linux")
}
