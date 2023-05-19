package types

import (
	"container/heap"
	"encoding/gob"

	//"fmt"

	//"io"
	"math"
	"strconv"
	"sync"
)

type MulticastProtocol interface {
	Cast()
	Deliver()
}

type BMulticast struct {
	encMu *sync.Mutex
}

func (b BMulticast) Cast(enc *gob.Encoder, message Message) error { // send info to other nodes
	b.encMu.Lock()
	defer b.encMu.Unlock()
	err := enc.Encode(message)
	if err != nil {
		//Error.Println(err)
	}
	return err
}

func (b BMulticast) Deliver(dec *gob.Decoder) (Message, error) { //get info from other nodes

	var message Message
	err := dec.Decode(&message) //check failure through here

	return message, err
}

type Multicast struct {
	A             int
	P             int
	curNodeId     string
	aliveNodes    int
	nfailCheck    map[string]bool
	messageCheck  map[string]bool
	messageNCheck map[string]int
	bcast         *BMulticast
	pq            *MessagePQ
	proposed      map[string]int
	repliers      map[string]int
	Mu            *sync.Mutex
	Mu_alive      *sync.Mutex
	Mu_fail       *sync.Mutex
	Mu_dec        *sync.Mutex
}

func (m *Multicast) Cast(encs map[string]*gob.Encoder, message Message) {
	for k, enc := range encs {
		m.Mu_fail.Lock()
		f := m.nfailCheck[k]

		m.Mu_fail.Unlock()
		if !f {

			m.bcast.Cast(enc, message)
		}

	}
}

func (m *Multicast) Deliver(dec *gob.Decoder, locNodeid string, msgChan chan Message, ferr chan string) {
	for { //check for duplicate
		message, err := m.bcast.Deliver(dec)

		if err != nil {
			m.Mu_fail.Lock()
			m.nfailCheck[locNodeid] = true

			m.Mu_fail.Unlock()

			break
		}
		m.Mu.Lock()
		_, ok := m.messageCheck[message.Id]

		m.Mu.Unlock()
		if ok && message.Status == "i" {
			continue
		}
		message.RNodeId = locNodeid
		msgChan <- message
	}
}

func (m *Multicast) ISIS(encs map[string]*gob.Encoder, msgChan chan Message, listenChan chan Message, ferr chan string) {
	for {

		message := <-msgChan

		m.Mu_fail.Lock()
		for nFail, f := range m.nfailCheck {
			if f && m.messageNCheck[nFail] == 0 {
				m.messageNCheck[nFail] = m.messageNCheck[nFail] + 1
				for _, item := range *(m.pq) {
					if item != nil && item.Status == "p" {
						if item.NodeId == nFail {
							m.pq.Remove(item)
							//fmt.Println(item)
							continue
						}
						item.Status = "pf"
						m.bcast.Cast(encs[item.NodeId], (*item))
					}
				}
			}
		}
		m.Mu_fail.Unlock()

		if message.Status == "i" { // receiver, first multicasting message
			m.Mu.Lock()
			_, ok := m.messageCheck[message.Id]

			if !ok {
				m.messageCheck[message.Id] = true
				m.Mu.Unlock()
				for k, enc := range encs {
					if k != m.curNodeId {
						m.bcast.Cast(enc, message)
					}
				}
			} else {
				m.Mu.Unlock()
				continue
			}
			message.Status = "p"
			m.Mu_dec.Lock()
			m.P = int(math.Max(float64(m.A), float64(m.P))) + 1 // proposed priority
			message.Priority = m.P
			m.Mu_dec.Unlock()
			message.IsDeliverable = false
			heap.Push(m.pq, &message) // pass a message copy to the function

			nodeId := "node" + strconv.Itoa(message.Sender)
			enc := encs[nodeId]

			m.bcast.Cast(enc, message)
			//if 1st time recived message put in recieved map then multicast

		} else if message.Status == "pf" {
			//fmt.Println(message)
			_, k := m.messageNCheck[message.Id]
			if k {
				continue
			}
			m.messageNCheck[message.Id] = m.messageNCheck[message.Id] + 1
			numOfReply := m.repliers[message.Id]
			cnt := 0
			m.Mu_fail.Lock()
			for _, f := range m.nfailCheck {
				if !f {
					//fmt.Println(n.mcast.aliveNodes)
					cnt += 1
				}
			}
			m.Mu_fail.Unlock()

			m.Mu_alive.Lock()
			m.aliveNodes = cnt
			if numOfReply+1 == m.aliveNodes {
				m.Mu_alive.Unlock()
				maxPri := m.proposed[message.Id]
				message.Status = "a"
				message.Priority = maxPri
				m.Cast(encs, message)
			} else {
				m.Mu_alive.Unlock()
			}

		} else if message.Status == "p" { // sender, receiving proposed priorities
			m.Mu_fail.Lock()
			chk := m.nfailCheck[message.NodeId]
			m.Mu_fail.Unlock()
			if chk {
				m_r := m.pq.FindById(message.Id)
				if m_r != nil {
					m.pq.Remove(m_r)
				}

			}

			priority, ok := m.proposed[message.Id]
			if !ok {
				m.proposed[message.Id] = message.Priority
			}

			if ok && message.Priority > priority {
				m.proposed[message.Id] = message.Priority
			}

			numOfReply, ok := m.repliers[message.Id]

			if !ok {
				m.repliers[message.Id] = 1
			} else {
				m.repliers[message.Id] = numOfReply + 1

				cnt := 0
				m.Mu_fail.Lock()
				for _, f := range m.nfailCheck {
					if !f {
						cnt += 1
					}
				}
				m.Mu_fail.Unlock()

				m.Mu_alive.Lock()
				m.aliveNodes = cnt
				if numOfReply+1 == m.aliveNodes {
					m.Mu_alive.Unlock()
					maxPri := m.proposed[message.Id]
					message.Status = "a"
					message.Priority = maxPri
					m.Cast(encs, message)
				} else {
					m.Mu_alive.Unlock()
				}

			}
		} else { // receiver, receiving agreed priority

			oldMessage := m.pq.FindById(message.Id) // Index might be changed
			oldMessage.IsDeliverable = true
			agreed := message.Priority
			m.pq.Update(oldMessage, agreed)

			for m.pq.Len() > 0 && (*m.pq)[0].IsDeliverable {
				message := heap.Pop(m.pq)
				listenChan <- *(message.(*Message))
			}
		}

	}
}
