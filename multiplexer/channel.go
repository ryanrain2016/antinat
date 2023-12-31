package multiplexer

import (
	"net"
	"sync"
	"time"

	"github.com/pkg/errors"
)

type channel struct {
	sessionId   uint32
	multiplexer Multiplexer
	conn        net.Conn

	writeChannel chan []byte
	loop         bool

	pollMutex sync.Locker
	polling   bool
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
	poll := func() bool {
		c.pollMutex.Lock()
		defer c.pollMutex.Unlock()
		defer func() {
			c.polling = true
		}()
		p := c.polling
		return p
	}()
	if poll {
		return errors.WithStack(errors.Errorf("channle has already been polling"))
	}
	defer c.Close()
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
	i := 0
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
		i = (i + 1) % 1000
		if i == 0 {
			time.Sleep(0)
		}
	}
	return nil
}
