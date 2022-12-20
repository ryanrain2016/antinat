package multiplexer

import (
	"net"

	"github.com/pkg/errors"
)

type channel struct {
	sessionId   uint32
	multiplexer Multiplexer
	conn        net.Conn

	writeChannel chan []byte
	loop         bool
}

func (c *channel) Write(b []byte) (n int, err error) {
	return c.multiplexer.Write(c.sessionId, b)
}

func (c *channel) WriteToLocal(b []byte) (n int, err error) {
	defer func() {
		e := recover()
		if e != nil {
			err = e.(error)
		} else {
			n = len(b)
		}
	}()
	c.writeChannel <- b
	return
}

func (c *channel) Close() (err error) {
	if c.loop {
		c.loop = false
		close(c.writeChannel)
		c.multiplexer.SendClose(c.sessionId)
		c.multiplexer = nil
		return c.conn.Close()
	}
	return nil
}

func (c *channel) Connect(addr string) error {
	return c.multiplexer.SendConnect(c.sessionId, addr)
}

func (c *channel) Poll() error {
	go func() {
		for c.loop {
			b, ok := <-c.writeChannel
			if !ok {
				// fmt.Println("channel close")
				return
			}
			_, err := c.conn.Write(b)
			if err != nil {
				return
			}
		}
	}()
	for c.loop {
		b := make([]byte, 0xffff)
		n, err := c.conn.Read(b)
		if err != nil {
			return errors.WithStack(err)
		}
		b = b[:n]
		_, err = c.Write(b)
		if err != nil {
			return errors.WithStack(err)
		}
	}
	return nil
}
