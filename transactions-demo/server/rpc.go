package main

import (
	"fmt"
	"net/rpc"
	"shared"
	"strconv"
	"strings"
)

/* Client-Server Communication */

func (srv *Server) TwoPhaseCommit(tid string, reply *shared.SendCoordReply) {
	shared.DPrintf("In two-phase commit")

	branches, ok := srv.relatedBranches[tid]

	if !ok {
		err := fmt.Errorf("2PC: no related branches in tid " + tid)
		shared.CheckError(err)
	}

	replyCh := make(chan bool)

	for branchName, _ := range branches { // send PREPARE to participants
		go func(branchName string, ch chan bool) {
			cli := srv.rpcClients[branchName]
			prepArgs := &PrepareArgs{Tid: tid}
			prepReply := &PrepareReply{}

			cli.Call("Server.Prepare", prepArgs, prepReply)
			ch <- prepReply.CanCommit

		}(branchName, replyCh)
	}

	shared.DPrintf("2PC: after sending PREPARE, collecting canCommit")
	shouldWait := true

	for shouldWait && srv.canCommit[tid] != len(srv.relatedBranches[tid]) {
		select {
		case canCommit := <-replyCh:
			if canCommit {
				srv.canCommit[tid] += 1
			} else {
				shouldWait = false
			}
		}
	}

	var commitArgs *CommitArgs

	if srv.canCommit[tid] == len(srv.relatedBranches[tid]) {
		shared.DPrintf("2PC: the coordinator is about to commit")
		commitArgs = &CommitArgs{DoCommit: true, Tid: tid}
		reply.ReplyType = shared.CM_OK
	} else {
		shared.DPrintf("2PC: the coordinator is about to abort")
		srv.DoAbort(tid, reply)
		return
	}

	for branchName, _ := range branches { // send doCommit to participants
		go func(branchName string, args *CommitArgs, ch chan bool) {
			cli := srv.rpcClients[branchName]
			commitReply := &CommitReply{}
			cli.Call("Server.Commit", commitArgs, commitReply)
			ch <- commitReply.HaveCommitted

		}(branchName, commitArgs, replyCh)
	}

	shared.DPrintf("2PC: after sending COMMIT, collecting haveCommitted")
	go func(ch chan bool) {
		for srv.haveCommitted[tid] != len(srv.relatedBranches[tid]) {
			select {
			case haveCommitted := <-ch:
				if haveCommitted {
					srv.haveCommitted[tid] += 1
				}
			}
		}

		delete(srv.canCommit, tid)
		delete(srv.haveCommitted, tid)
		delete(srv.relatedBranches, tid)
	}(replyCh)
}

func (srv *Server) DoAbort(tid string, reply *shared.SendCoordReply) {
	branches := srv.relatedBranches[tid]

	commitArgs := &CommitArgs{DoCommit: false, Tid: tid}
	commitReply := &CommitReply{}

	reply.ReplyType = shared.ABRT_OK

	for branchName, _ := range branches {
		go func(branchName string, args *CommitArgs, reply *CommitReply) {
			cli := srv.rpcClients[branchName]
			cli.Call("Server.Commit", commitArgs, commitReply)

		}(branchName, commitArgs, commitReply)
	}
	shared.DPrintf("Abort: after sending doAbort to participants, coordinator needs to remove the data for tid " + tid)

	go func() {
		delete(srv.canCommit, tid)
		delete(srv.haveCommitted, tid)
		delete(srv.relatedBranches, tid)
	}()
}

func (srv *Server) SendCoord(args *shared.SendCoordArgs, reply *shared.SendCoordReply) error { // In go impl, RPC call is a goroutine
	tid := args.Tid
	cmdString := args.Cmd
	shared.DPrintf(cmdString)

	if cmdString == shared.BGN { // beginning of a tx(tid)
		shared.DPrintf("BEGIN Tx")
		srv.canCommit[tid] = 0
		srv.haveCommitted[tid] = 0
		srv.relatedBranches[tid] = make(map[string]bool, 0)

		reply.ReplyType = shared.OK
		return nil
	}

	if cmdString == shared.CM { // 2-phase commit
		srv.TwoPhaseCommit(tid, reply)
		return nil
	}

	if cmdString == shared.ABRT { // client abort
		srv.DoAbort(tid, reply)
		return nil
	}

	/* DEPOSIT or WITHDRAW or BALANCE */
	splited := strings.Split(strings.TrimSuffix(cmdString, "\n"), " ")
	var cmd shared.Command

	if len(splited) == 2 { // BALANCE
		s := strings.Split(splited[1], ".")

		cmd = shared.Command{
			Tid:     tid,
			Branch:  s[0],
			Account: s[1],
			Method:  splited[0],
			Amount:  -1,
		}
	} else {
		s := strings.Split(splited[1], ".")
		bal, err := strconv.Atoi(splited[2])

		if err != nil {
			shared.CheckError(err)
		}

		cmd = shared.Command{
			Tid:     tid,
			Branch:  s[0],
			Account: s[1],
			Method:  splited[0],
			Amount:  bal,
		}
	}

	// setup a conn with the participant server
	if _, ok := srv.rpcClients[cmd.Branch]; !ok {
		shared.DPrintf("Creating a rpc client of branch %s", cmd.Branch)

		branchAddr := srv.branchInfo[cmd.Branch]
		// fmt.Printf("%+v\n", srv.branchInfo)
		cli, err := rpc.Dial("tcp", branchAddr.addr+":"+branchAddr.port)
		shared.CheckError(err)
		srv.rpcClients[cmd.Branch] = cli
	}

	if _, ok := srv.relatedBranches[tid][cmd.Branch]; !ok {
		srv.relatedBranches[tid][cmd.Branch] = true
	}

	rpcClient := srv.rpcClients[cmd.Branch]

	fwdArgs := &ForwardCmdArgs{}
	fwdArgs.Cmd = cmd
	fwdArgs.Tid = tid

	fwdReply := &ForwardCmdReply{}

	rpcClient.Call("Server.ForwardCmd", fwdArgs, fwdReply)

	reply.ReplyType = fwdReply.ReplyType

	go func() {
		tempReply := &shared.SendCoordReply{}

		if fwdReply.ReplyType == shared.NF_ABRT {
			srv.DoAbort(tid, tempReply)
		}
	}()

	return nil
}

/* coordinator - participant communication */

type ForwardCmdArgs struct {
	Tid string
	Cmd shared.Command
}

type ForwardCmdReply struct {
	ReplyType string //possible replies: OK,COMMIT OK, ABORTED, (NOT FOUND, ABORTED)
}

func (srv *Server) ForwardCmd(args *ForwardCmdArgs, reply *ForwardCmdReply) error {

	curCmd := args.Cmd

	if srv.app.cache[curCmd.Tid] == nil {
		srv.app.cache[curCmd.Tid] = make(Accounts)
	}

	if srv.app.txLockInfo[curCmd.Tid] == nil {
		srv.app.txLockInfo[curCmd.Tid] = make(map[string]bool)
	}

	if curCmd.Method == "DEPOSIT" {
		res := srv.app.Deposit(curCmd.Tid, curCmd.Account, curCmd.Amount)
		reply.ReplyType = res
	} else if curCmd.Method == "WITHDRAW" {
		res := srv.app.Withdraw(curCmd.Tid, curCmd.Account, curCmd.Amount)
		reply.ReplyType = res
	} else if curCmd.Method == "BALANCE" {
		res := srv.app.Balance(curCmd.Tid, curCmd.Account)
		if res == shared.NF_ABRT {
			reply.ReplyType = res
		} else {
			reply.ReplyType = srv.branch + "." + res
		}
	} else {
		shared.DPrintf("Method " + curCmd.Method + " is not supported")
	}

	return nil
}

func (srv *Server) Prepare(args *PrepareArgs, reply *PrepareReply) error {
	curTid := args.Tid
	res := true

	shared.DPrintf(curTid + "sending PREPARE")

	srv.app.mu.Lock()
	for acctName, _ := range srv.app.txLockInfo[curTid] {
		if srv.app.committedData[acctName]+srv.app.cache[curTid][acctName] < 0 {
			res = false
		}
	}
	srv.app.mu.Unlock()

	reply.CanCommit = res

	return nil
}

func (srv *Server) Commit(args *CommitArgs, reply *CommitReply) error {
	tid := args.Tid

	if args.DoCommit {
		srv.app.mu.Lock()
		for acctName, change := range srv.app.cache[tid] {
			srv.app.committedData[acctName] += change
		}
		fmt.Print("Non-zero balance: ")
		for acctName, bal := range srv.app.committedData {
			fmt.Print("Account name: " + acctName + " Balance: " + strconv.Itoa(bal) + " ")
		}
		srv.app.mu.Unlock()
	}
	delete(srv.app.cache, tid)
	srv.app.releaseLocks(tid)

	return nil
}

/* 2PC - coordinator */

type PrepareArgs struct {
	Tid string
}

type PrepareReply struct {
	CanCommit bool
}

type CommitArgs struct {
	DoCommit bool
	Tid      string
}

type CommitReply struct {
	HaveCommitted bool
}
