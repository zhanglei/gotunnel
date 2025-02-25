//
//   date  : 2014-06-05
//   author: xjdrew
//

package main

import (
	"bytes"
	"encoding/binary"
	"sync"
)

const (
	LINK_DATA uint8 = iota
	LINK_CREATE
	LINK_DESTROY
)

type CmdPayload struct {
	Cmd    uint8
	Linkid uint16
}

type Door interface {
	ctrl(cmd *CmdPayload) bool
}

type Coor struct {
	LinkSet
	tunnel *Tunnel
	door   Door
	wg     sync.WaitGroup
}

func (self *Coor) SendLinkCreate(linkid uint16) {
	self.Send(LINK_CREATE, linkid, nil)
}

func (self *Coor) SendLinkDestory(linkid uint16) {
	self.Send(LINK_DESTROY, linkid, nil)
}

func (self *Coor) SendLinkData(linkid uint16, data []byte) {
	self.Send(LINK_DATA, linkid, data)
}

func (self *Coor) Send(cmd uint8, linkid uint16, data []byte) {
	var payload TunnelPayload
	switch cmd {
	case LINK_DATA:
		Debug("link(%d) send data:%d", linkid, len(data))

		payload.Linkid = linkid
		payload.Data = data
	case LINK_CREATE, LINK_DESTROY:
		Debug("link(%d) send cmd:%d", linkid, cmd)

		buf := new(bytes.Buffer)
		var body CmdPayload
		body.Cmd = cmd
		body.Linkid = linkid
		binary.Write(buf, binary.LittleEndian, &body)

		payload.Linkid = 0
		payload.Data = buf.Bytes()
	default:
		Error("unknown cmd:%d, linkid:%d", cmd, linkid)
	}
	self.tunnel.Put(&payload)
}

func (self *Coor) ctrl(cmd *CmdPayload) {
	linkid := cmd.Linkid
	Debug("link(%d) recv cmd:%d", linkid, cmd.Cmd)

	if self.door != nil && self.door.ctrl(cmd) {
		return
	}

	switch cmd.Cmd {
	case LINK_DESTROY:
		ch, err := self.Reset(linkid)
		if err != nil || ch == nil {
			Error("link(%d) close failed, error:%s", linkid, err)
			return
		}
		// close ch, don't write to ch again
		close(ch)
		Info("link(%d) closed", linkid)
	default:
		Error("receive unknown cmd:%v", cmd)
	}
}

func (self *Coor) data(payload *TunnelPayload) {
	linkid := payload.Linkid
	Debug("link(%d) recv data:%d", linkid, len(payload.Data))

	ch, err := self.Get(linkid)
	if err != nil {
		Error("illegal link, linkid:%d", linkid)
		return
	}

	if ch != nil {
		ch <- payload.Data
	} else {
		Info("drop data because no link, linkid:%d", linkid)
	}
}

func (self *Coor) dispatch() {
	defer self.wg.Done()
	for {
		payload := self.tunnel.Pop()
		if payload == nil {
			Error("pop message failed, break dispatch")
			break
		}

		if payload.Linkid == 0 {
			var cmd CmdPayload
			buf := bytes.NewBuffer(payload.Data)
			err := binary.Read(buf, binary.LittleEndian, &cmd)
			if err != nil {
				Error("parse message failed:%s, break dispatch", err.Error())
				break
			}
			self.ctrl(&cmd)
		} else {
			self.data(payload)
		}
	}
}

func (self *Coor) pumpOut() {
	self.wg.Done()
	self.tunnel.PumpOut()
}

func (self *Coor) pumpUp() {
	self.wg.Done()
	self.tunnel.PumpUp()
}

func (self *Coor) Start() error {
	self.wg.Add(1)
	go self.pumpOut()
	self.wg.Add(1)
	go self.pumpUp()
	self.wg.Add(1)
	go self.dispatch()
	return nil
}

func (self *Coor) Wait() {
	self.wg.Wait()
	Error("coor quit")
	// tunnel disconnect, so reset all link
	Info("reset all link")
	var i uint16 = 1
	for ; i < options.capacity; i++ {
		ch, _ := self.Reset(i)
		if ch != nil {
			close(ch)
			Info("link(%d) closed", i)
		}
	}
}

func NewCoor(tunnel *Tunnel, door Door) *Coor {
	var wg sync.WaitGroup
	return &Coor{NewLinkSet(options.capacity), tunnel, door, wg}
}
