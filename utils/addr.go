package utils

import (
	"net"

	"github.com/pkg/errors"
)

func IP2Bytes(ip net.IP) ([]byte, error) {
	ipv4 := ip.To4()
	if ipv4 != nil {
		return append([]byte{0x04}, []byte(ipv4)...), nil
	}
	ipv6 := ip.To16()
	if ipv6 != nil {
		return append([]byte{0x04}, []byte(ipv6)...), nil
	}
	return nil, errors.WithStack(errors.New("invalid ip"))
}

func Port2Bytes(port int) []byte {
	return []byte{byte((port & 0xff00) >> 8), byte(port & 0xff)}
}

func ParseIP(buf []byte) (ip net.IP, remain []byte, err error) {
	switch buf[0] {
	case 0x04:
		if len(buf) < 5 {
			return nil, buf, errors.WithStack(errors.New("not enough length for ipv4"))
		}
		return net.IP(buf[1:5]), buf[5:], nil
	case 0x06:
		if len(buf) < 17 {
			return nil, buf, errors.WithStack(errors.New("not enough length for ipv6"))
		}
		return net.IP(buf[1:17]), buf[17:], nil
	default:
		return nil, buf, errors.WithStack(errors.New("invalid ip type"))
	}
}

func ParsePort(buf []byte) (port int, remain []byte, err error) {
	if len(buf) < 2 {
		return 0, buf, errors.WithStack(errors.New("not enough length to parse port"))
	}
	return (int(buf[0]) << 8) | int(buf[1]), buf[2:], nil
}

func AddrString2bytes(addrString string) (buf []byte, err error) {
	addr, err := net.ResolveUDPAddr("udp", addrString)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	ipBytes, err := IP2Bytes(addr.IP)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	portBytes := Port2Bytes(addr.Port)
	return append(ipBytes, portBytes...), nil
}

func ParseString(buf []byte) (str string, remain []byte, err error) {
	length := int(buf[0])
	if len(buf) < length+1 {
		return "", buf, errors.WithStack(errors.New("not enough length to parse string"))
	}
	return string(buf[1 : 1+length]), buf[1+length:], nil
}

func String2Bytes(str string) ([]byte, error) {
	length := len(str)
	if length > 255 {
		return nil, errors.WithStack(errors.New("str is to long"))
	}
	return append([]byte{byte(length)}, []byte(str)...), nil
}
