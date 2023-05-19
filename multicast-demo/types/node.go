package types

import (
	"bufio"
	"encoding/gob"

	//"fmt"

	//"fmt"
	"fmt"
	"io"
	"log"
	"mp1_node/utils"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

var Error = log.New(os.Stdout, "\u001b[31mERROR: \u001b[0m", log.LstdFlags|log.Lshortfile)

type SockAddr struct {
	nodeId string
	host   string
	ip     string
	port   string
}

type Node struct {
	id    string
	port  string
	pool  []SockAddr
	app   *Application
	mcast *Multicast
}

func (n *Node) readConf(fp string) int {
	f, err := os.Open(fp)
	utils.ExitFromErr(err)
	defer f.Close()

	reader := bufio.NewReader(f)

	lineNum, err := reader.ReadString('\n')
	utils.ExitFromErr(err)

	num, err := strconv.Atoi(strings.TrimSuffix(lineNum, "\n"))
	utils.ExitFromErr(err)

	for i := 0; i < num; i++ {
		line, err := reader.ReadString('\n')
		if err != io.EOF {
			utils.ExitFromErr(err)
		}

		parsedLine := strings.Split(strings.TrimSuffix(line, "\n"), " ")

		if parsedLine[0] == n.id {
			n.port = parsedLine[2]
		}

		ipv4, err := net.ResolveIPAddr("ip4", parsedLine[1])
		utils.ExitFromErr(err)

		newAddr := SockAddr{
			nodeId: parsedLine[0],
			host:   parsedLine[1],
			ip:     ipv4.String(),
			port:   parsedLine[2],
		}
		n.pool = append(n.pool, newAddr)
	}

	return num
}

func (n *Node) Run(id string, fp string) {
	n.id = id
	numOfNodes := n.readConf(fp)
	nidx := 0
	for i, _ := range n.pool {
		if n.pool[i].nodeId == id {
			nidx = i + 1
		}
		n.pool[i].nodeId = "node" + strconv.Itoa(i+1)
	}
	n.id = "node" + strconv.Itoa(nidx)
	n.mcast = new(Multicast)
	n.app = new(Application)
	n.app.Init(n.id, numOfNodes)
	n.mcast.nfailCheck = make(map[string]bool)
	n.mcast.curNodeId = n.id
	n.mcast.messageCheck = make(map[string]bool)
	n.mcast.messageNCheck = make(map[string]int)
	n.mcast.proposed = make(map[string]int)
	n.mcast.repliers = make(map[string]int)

	n.mcast.bcast = new(BMulticast)
	n.mcast.pq = new(MessagePQ)
	n.mcast.bcast.encMu = new(sync.Mutex)
	n.mcast.Mu_fail = new(sync.Mutex)
	n.mcast.Mu_alive = new(sync.Mutex)
	n.mcast.Mu_dec = new(sync.Mutex)
	// n.mcast.bcast.decMu = new(sync.Mutex
	n.mcast.Mu = new(sync.Mutex)

	n.mcast.aliveNodes = numOfNodes
	//n.mcast.aliveNodes = numOfNodes
	n.mcast.A = 0
	n.mcast.P = 0

	listenChan := make(chan Message)
	msgChan := make(chan Message)
	ferr := make(chan string)
	go n.app.ListenFrom(listenChan) // c is the channel for multiple goroutines to r/w data

	/* accept connection from other nodes */

	listener, err := net.Listen("tcp", ":"+n.port)
	utils.ExitFromErr(err)
	defer listener.Close()

	go func() {
		for {
			conn, err := listener.Accept()

			if err != nil {
				//Error.Println(err)
				continue
			}
			var d_nid string
			trim := conn.RemoteAddr().String()
			ip_addr := strings.Split(trim,":")
			for i, _ := range n.pool {
				if n.pool[i].ip == ip_addr[0]{
					d_nid = n.pool[i].nodeId
				}
			}
			
			newDec := gob.NewDecoder(conn)
			go n.mcast.Deliver(newDec,d_nid, msgChan,ferr)
		}
	}()

	/* connect to other nodes */

	conns := sync.Map{}

	var connWg sync.WaitGroup

	for _, sockaddr := range n.pool {
		connWg.Add(1)
		go func(sockaddr SockAddr, wg *sync.WaitGroup) {
			defer wg.Done()
			for {
				conn, err := net.Dial("tcp", sockaddr.host+":"+sockaddr.port)

				if err != nil {
					//Error.Println(err)
				} else {
					conns.Store(sockaddr.nodeId, conn)
					break
				}
			}
		}(sockaddr, &connWg)
	}

	connWg.Wait()

	defer func() {
		conns.Range(func(key, value any) bool {
			conn := value.(net.Conn)
			conn.Close()
			return true
		})
	}()

	encs := make(map[string]*gob.Encoder)

	conns.Range(func(key, value any) bool {
		conn := value.(net.Conn)
		id := key.(string)
		n.mcast.nfailCheck[id] = false //add failcheck init
		n.mcast.messageNCheck[id] = 0
		//fmt.Println(id, n.mcast.nfailCheck[id])
		encs[id] = gob.NewEncoder(conn)
		return true
	})
	//call node check l

	go n.mcast.ISIS(encs, msgChan, listenChan, ferr)

	/* read from console and send to every nodes (including itself) */

	reader := bufio.NewReader(os.Stdin)

	selfOutConn, ok := conns.Load(n.id)

	if !ok {
		Error.Println("sync.Map cannot load self out-conn")
	}

	decoder := gob.NewDecoder(selfOutConn.(net.Conn))
	go n.mcast.Deliver(decoder,n.id,msgChan,ferr)

	for {
		/* string processing */
		tx, err := reader.ReadString('\n')
		utils.ExitFromErr(err)

		txList := strings.Split(strings.TrimSuffix(tx, "\n"), " ")

		//transaction generated    node1_gen.txt   node1_pro.txt
		t_g := (float64(time.Now().UnixNano()) / 1e9)
		n.app.writer_g.WriteString(fmt.Sprintf("%f", t_g) + "\n")
		n.app.writer_g.Flush()
		/* parsing tx string and assemble message */
		isDeposit := true
		var names []string
		var amount int

		if txList[0] != "DEPOSIT" {
			isDeposit = false
			names = append(names, txList[1], txList[3])
			amount, err = strconv.Atoi(txList[4])
			utils.ExitFromErr(err)

		} else {
			names = append(names, txList[1])
			amount, err = strconv.Atoi(txList[2])
			utils.ExitFromErr(err)
		}

		nodeId, err := strconv.Atoi(n.id[4:]) // "nodei" -> "i"
		utils.ExitFromErr(err)

		message := Message{
			Id:            uuid.NewString(),
			Sender:        nodeId,
			Priority:      0,
			Index:         -1,
			NodeId:        n.id,
			RNodeId:       n.id,
			Status:        "i",
			IsDeliverable: false,
			IsDeposit:     isDeposit,
			Names:         names,
			Amount:        amount,
		}
		go n.mcast.Cast(encs, message)
	}

}
