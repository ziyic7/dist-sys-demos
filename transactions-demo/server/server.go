package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/rpc"
	"os"
	"shared"
	"strings"
	"sync"
)

type branchAddr struct {
	addr string
	port string
}

type Server struct {
	branch string
	port   string

	canCommit       map[string]int // tid : canCommit count
	haveCommitted   map[string]int
	branchInfo      map[string]branchAddr // the addresses of all the branches
	rpcClients      map[string]*rpc.Client
	relatedBranches map[string]map[string]bool // tid : map{ branch : isExist?}
	app             *Application
}

func (srv *Server) readFile(fp string) {
	f, err := os.Open(fp)
	shared.CheckError(err)
	defer f.Close()

	reader := bufio.NewReader(f)

	for {
		line, err := reader.ReadString('\n')

		if err != nil && err != io.EOF {
			shared.CheckError(err)
		}

		parsed := strings.Split(strings.TrimSuffix(line, "\n"), " ")
		curBranch, curAddr, curPort := parsed[0], parsed[1], parsed[2]

		if srv.branch == curBranch {
			srv.port = curPort
		}

		srv.branchInfo[curBranch] = branchAddr{
			addr: curAddr,
			port: curPort,
		}

		if err == io.EOF {
			break
		}
	}
}

func (srv *Server) Start(branch string, fp string) {

	srv.canCommit = make(map[string]int, 0)
	srv.haveCommitted = make(map[string]int, 0)
	srv.rpcClients = make(map[string]*rpc.Client, 0)
	srv.relatedBranches = make(map[string]map[string]bool, 0)
	srv.branchInfo = make(map[string]branchAddr, 0)
	

	srv.app = new(Application)
	srv.app.mu = new(sync.Mutex)
	srv.app.committedData = make(Accounts)
	srv.app.locks = make(map[string]Lock)
	srv.app.cache = make(map[string]Accounts)
	srv.app.txLockInfo = make(map[string]map[string]bool)
	//srv.app.waitfor   = make(map[string]map[string]bool)

	srv.branch = branch
	srv.readFile(fp)

	rpc.Register(srv)
	listener, err := net.Listen("tcp", ":"+srv.port)
	shared.CheckError(err)

	for {
		conn, err := listener.Accept()
		go func() {
			shared.CheckError(err)
			rpc.ServeConn(conn)
		}()
	}
}

func main() {
	args := os.Args[1:]

	if len(args) != 2 {
		err := fmt.Errorf("usage: ./server [branch] [configuration file]")
		shared.CheckError(err)
	}
	//start up server with branch id
	srv := new(Server)
	srv.Start(args[0], args[1])
}
