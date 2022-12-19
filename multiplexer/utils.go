package multiplexer

import "github.com/pkg/errors"

func sessionId2Bytes(sessionId uint32) []byte {
	buf := make([]byte, 4)
	for i := 3; i >= 0; i-- {
		buf[i] = byte(sessionId & 0xff)
		sessionId = sessionId >> 8
	}
	return buf
}

func bytes2SessionId(buf []byte) uint32 {
	var sessionId uint32 = 0
	for _, v := range buf {
		sessionId = (sessionId << 8) + uint32(v)
	}
	return sessionId
}

func bytes2Length(buf []byte) int {
	return int(bytes2SessionId(buf))
}

func length2Bytes(l int) []byte {
	buf := make([]byte, 2)
	for i := 1; i >= 0; i-- {
		buf[i] = byte(l & 0xff)
		l = l >> 8
	}
	return buf
}

func parseLength(buf []byte) (int, []byte, error) {
	if len(buf) < 2 {
		return 0, nil, errors.WithStack(errors.New("not enough bytes to parse length"))
	}
	return bytes2Length(buf[:2]), buf[2:], nil
}

func parseString(buf []byte) (string, []byte, error) {
	length, buf, err := parseLength(buf)
	if err != nil {
		return "", nil, errors.WithStack(err)
	}
	if len(buf) < length {
		return "", nil, errors.WithStack(errors.New("not enough bytes to parse str"))
	}
	return string(buf[:length]), buf[length:], nil
}
