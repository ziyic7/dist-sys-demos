package main

import (
	"bufio"
	"fmt"
	"io"
	"math/rand"
	"net/rpc"
	"os"
	"shared"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

type branchInfo struct {
	branch string
	addr   string
	port   string
}

type Client struct {
	id       string
	branches []branchInfo
}

func (cli *Client) readConfig(fp string) {
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

		cli.branches = append(cli.branches, branchInfo{
			branch: curBranch,
			addr:   curAddr,
			port:   curPort,
		})

		if err == io.EOF {
			break
		}
	}
}

func (cli *Client) processTransaction() {
	reader := bufio.NewReader(os.Stdin)
	var coordClient *rpc.Client
	for {
		line, err := reader.ReadString('\n')
		line = strings.Trim(line, " \r\n")

		if err == io.EOF {
			break
		} else {
			shared.CheckError(err)
		}

		shared.DPrintf(cli.id, line)

		if line == "BEGIN" {
			// randomly pick one server as the coordinator
			r := rand.New(rand.NewSource(time.Now().UnixNano()))

			randIdx := r.Intn(5)
			shared.DPrintf("Pick server " + strconv.Itoa(randIdx))
			coordinator := cli.branches[randIdx]

			coordClient, err = rpc.Dial("tcp", coordinator.addr+":"+coordinator.port)
			shared.CheckError(err)
		}

		args := &shared.SendCoordArgs{
			Tid: cli.id,
			Cmd: line,
		}
		//possible replies: OK,COMMIT OK, ABORTED, (NOT FOUND, ABORTED)
		reply := &shared.SendCoordReply{}

		coordClient.Call("Server.SendCoord", args, reply)

		//Move into rpc just send strong
		if reply.ReplyType == shared.ABRT_OK {
			fmt.Println(shared.ABRT_OK)
			break
		} else if reply.ReplyType == shared.NF_ABRT {
			fmt.Println(shared.NF_ABRT)
			break
		} else if reply.ReplyType == shared.CM_OK {
			fmt.Println(shared.CM_OK)
			break
		} else { //should only print OK
			fmt.Println(reply.ReplyType)
		}
	}
}

func main() {
	args := os.Args[1:]

	if len(args) != 2 {
		err := fmt.Errorf("usage: ./client [client id] [configuration file]")
		shared.CheckError(err)
	}

	_, fp := args[0], args[1]

	client := Client{}
	client.branches = make([]branchInfo, 0)
	client.id = uuid.NewString()

	client.readConfig(fp)
	client.processTransaction()
}
