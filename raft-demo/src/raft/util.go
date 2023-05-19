package raft

import (
	"log"
	"math/rand"
	"time"
)

type RaftState int

const (
	FOLLOWER  RaftState = 0
	CANDIDATE RaftState = 1
	LEADER    RaftState = 2
)

const minTimeOut, maxTimeOut = 300, 700 // TODO: might need to adjust the time

type LogEntry struct {
	Command interface{}
	Term    int
	Index   int
}

type ApplyMsg struct {
	CommandValid bool
	Command      interface{}
	CommandIndex int
}

func (rf *Raft) GetState() (int, bool) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	term := rf.currentTerm
	isleader := rf.state == LEADER

	return term, isleader
}

func (rf *Raft) isState(rfState RaftState) bool {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	return rf.state == rfState
}

func (rf *Raft) isCandUpToDate(args *RequestVoteArgs) bool {
	logLength := len(rf.log)

	if logLength == 0 {
		return true
	}

	if args.LastLogTerm > rf.log[logLength-1].Term || (args.LastLogTerm == rf.log[logLength-1].Term && args.LastLogIndex >= logLength-1) {
		return true
	}

	return false
}

func (rf *Raft) resetTimeout() time.Duration {
	rand.Seed(int64(rf.me+1) * time.Now().UnixMilli())
	return time.Duration(rand.Intn(maxTimeOut-minTimeOut)+minTimeOut) * time.Millisecond
}

func (rf *Raft) isElected() bool {
	return rf.votesGot > (rf.numPeers / 2)
}

// Debugging
const Debug = 0

func DPrintf(format string, a ...interface{}) (n int, err error) {
	if Debug > 0 {
		log.Printf(format, a...)
	}
	return
}
