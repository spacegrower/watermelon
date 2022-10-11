package register

import (
	"net"
)

func ListenAddress(address string) (*net.TCPAddr, error) {
	l, err := net.Listen("tcp", address)
	if err != nil {
		return nil, err
	}

	addr, err := net.ResolveTCPAddr(l.Addr().Network(), l.Addr().String())
	if err != nil {
		return nil, err
	}
	return addr, nil
}
