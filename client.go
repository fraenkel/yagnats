package yagnats

import (
	"bufio"
	"errors"
	"fmt"
	"net"
)

type Callback func(*Message)

type Client struct {
	writer chan Packet

	pongs chan *PongPacket
	oks   chan *OKPacket
	errs  chan *ERRPacket

	subscriptions map[int]*Subscription
}

type Message struct {
	Subject string
	Payload string
	ReplyTo string
}

type Subscription struct {
	Subject  string
	Callback Callback
	ID       int
}

func Dial(addr string) (client *Client, err error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}

	client = &Client{
		writer:        make(chan Packet),
		pongs:         make(chan *PongPacket),
		oks:           make(chan *OKPacket),
		errs:          make(chan *ERRPacket),
		subscriptions: make(map[int]*Subscription),
	}

	go client.writePackets(conn)
	go client.handlePackets(bufio.NewReader(conn))

	return
}

func (c *Client) Ping() *PongPacket {
	c.sendPacket(&PingPacket{})
	return <-c.pongs
}

func (c *Client) Connect(user, pass string) error {
	c.sendPacket(&ConnectPacket{User: user, Pass: pass})

	select {
	case <-c.oks:
		return nil
	case err := <-c.errs:
		return errors.New(err.Message)
	}
}

func (c *Client) Publish(subject, payload string) {
	c.sendPacket(
		&PubPacket{
			Subject: subject,
			Payload: payload,
		},
	)
}

func (c *Client) Subscribe(subject string, callback Callback) int {
	id := len(c.subscriptions) + 1

	c.subscriptions[id] = &Subscription{
		Subject:  subject,
		ID:       id,
		Callback: callback,
	}

	c.sendPacket(
		&SubPacket{
			Subject: subject,
			ID:      id,
		},
	)

	return id
}

func (c *Client) UnsubscribeAll(subject string) {
	for id, sub := range c.subscriptions {
		if sub.Subject == subject {
			c.Unsubscribe(id)
		}
	}
}

func (c *Client) Unsubscribe(sid int) {
	c.sendPacket(&UnsubPacket{ID: sid})
	delete(c.subscriptions, sid)
}

func (c *Client) sendPacket(packet Packet) {
	c.writer <- packet
}

func (c *Client) writePackets(conn net.Conn) {
	for {
		packet := <-c.writer

		// TODO: check if written == packet length?
		_, err := conn.Write(packet.Encode())

		if err != nil {
			// TODO
			fmt.Printf("Connection lost!")
			return
		}
	}
}

func (c *Client) handlePackets(io *bufio.Reader) {
	for {
		packet, err := Parse(io)
		if err != nil {
			// TODO
			fmt.Printf("ERROR! %s\n", err)
			break
		}

		switch packet.(type) {
		// TODO: inelegant
		case *PongPacket:
			select {
			case c.pongs <- packet.(*PongPacket):
			default:
			}
		// TODO: inelegant
		case *OKPacket:
			select {
			case c.oks <- packet.(*OKPacket):
			default:
			}
		// TODO: inelegant
		case *ERRPacket:
			select {
			case c.errs <- packet.(*ERRPacket):
			default:
			}
		case *InfoPacket:
		case *MsgPacket:
			msg := packet.(*MsgPacket)
			sub := c.subscriptions[msg.SubID]
			if sub == nil {
				fmt.Printf("Warning: Message for unknown subscription (%s, %d): %#v\n", msg.Subject, msg.SubID, msg)
				break
			}

			sub.Callback(
				&Message{
					Subject: msg.Subject,
					Payload: msg.Payload,
					ReplyTo: msg.ReplyTo,
				},
			)
		default:
			// TODO
			fmt.Printf("Unhandled packet: %#v\n", packet)
		}
	}
}
