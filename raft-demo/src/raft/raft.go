package raft

import (
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"mp2-raft/src/labrpc"
)

type Raft struct {
	mu    sync.Mutex          // Lock to protect shared access to this peer's state
	peers []*labrpc.ClientEnd // RPC end points of all peers
	me    int                 // this peer's index into peers[] | also used as candidateId
	dead  int32               // set by Kill()

	currentTerm int        // latest term server has seen (initialized to 0 on first boot, increases monotonically)
	votedFor    int        // candidateId(rf.me) that received vote in current term (or -1 if none)
	log         []LogEntry // log entries; each entry contains command for state machine, and term when entry was received by leader(first index is 1)
	commitIndex int        // index of highest log entry known to be committed (initialized to 0, increases monotonically)
	lastApplied int        // index of highest log entry applied to state machine (initialized to 0, increases monotonically)
	nextIndex   []int      // for each server, index of the next log entry to send to that server (initialized to leader last log index + 1)
	matchIndex  []int      // for each server, index of highest log entry known to be replicated on server (initialized to 0, increases monotonically)

	/* implementation */
	state    RaftState     // one of (follower, candidate, leader), (initialized to FOLLOWER)
	votesGot int           // for CANDIDATE, the number of votes it receives during leader election in current term(initialized to 0)
	numPeers int           // number of Raft peers (initialized to len(peers))
	rpcCh    chan int      // for FOLLOWER, to get notified about the incoming RPCs
	entryCh  chan LogEntry // for LEADER
	applyCh  chan ApplyMsg
}

func (rf *Raft) Start(command interface{}) (int, int, bool) {
	index := -1
	term := -1
	isLeader := rf.isState(LEADER)

	if !isLeader {
		return index, term, isLeader
	}

	DPrintf("LEADER term %d raft %d state %d command %v", rf.currentTerm, rf.me, rf.state, command)

	rf.mu.Lock()
	newEntry := LogEntry{}
	newEntry.Index = len(rf.log)
	newEntry.Term = rf.currentTerm
	newEntry.Command = command
	term = rf.currentTerm
	index = len(rf.log)

	rf.log = append(rf.log, newEntry)

	go func() {
		rf.entryCh <- newEntry
	}()

	rf.mu.Unlock()

	return index, term, isLeader
}

func (rf *Raft) Kill() {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	atomic.StoreInt32(&rf.dead, 1)
	// Your code here, if desired.
	rf.state = -1
}

func (rf *Raft) killed() bool {
	z := atomic.LoadInt32(&rf.dead)
	return z == 1
}

func (rf *Raft) toFollower() {

	rf.mu.Lock()
	rf.votedFor = -1
	DPrintf("FOLLOWER: term %d raft %d state %d", rf.currentTerm, rf.me, rf.state)
	rf.mu.Unlock()

	timeCh := time.After(rf.resetTimeout())

	for !rf.killed() && rf.isState(FOLLOWER) {

		select {
		case <-rf.rpcCh:
			// if n == 2 {
			// 	DPrintf("FOLLOWER: term %d, raft peer %d receive appendEntries RPC, reset timeout", rf.currentTerm, rf.me)
			// }
			timeCh = time.After(rf.resetTimeout())
		case <-timeCh:
			rf.mu.Lock()

			DPrintf("FOLLOWER: term %d, raft %d state %d election timeout, convert to CANDIDATE", rf.currentTerm, rf.me, rf.state)

			rf.state = CANDIDATE
			rf.mu.Unlock()
			go rf.toCandidate()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (rf *Raft) startElection(wg *sync.WaitGroup) {
	if rf.killed() {
		return
	}

	DPrintf("CANDIDATE: term %d raft %d state %d", rf.currentTerm, rf.me, rf.state)

	rf.mu.Lock()
	rf.votedFor = rf.me
	rf.votesGot = 1
	rvreplyCh := make(chan RequestVoteReply, rf.numPeers-1)

	args := RequestVoteArgs{}
	args.Term = rf.currentTerm
	args.CandidateId = rf.me
	args.LastLogTerm = rf.log[len(rf.log)-1].Term
	args.LastLogIndex = rf.log[len(rf.log)-1].Index

	for si := 0; si < rf.numPeers; si++ {
		if si != rf.me {
			go func(si int, args RequestVoteArgs, ch chan RequestVoteReply) {
				reply := RequestVoteReply{}
				ok := rf.sendRequestVote(si, &args, &reply)

				if ok {
					ch <- reply
				}
			}(si, args, rvreplyCh)
		}
	}

	timeout := rf.resetTimeout()
	timeCh := time.After(timeout)
	rf.mu.Unlock()

	for !rf.killed() {
		select {
		case <-timeCh: // timeout, re-election
			// DPrintf("CANDIDATE: term %d raft %d state %d election timeout, start re-election", rf.currentTerm, rf.me, rf.state)
			wg.Done()
			return
		case reply := <-rvreplyCh:
			rf.mu.Lock()
			if reply.Term > rf.currentTerm { // convert to follower
				DPrintf("CANDIDATE: term %d raft %d state %d got request with larger term number, convert to FOLLOWER", rf.currentTerm, rf.me, rf.state)
				rf.currentTerm = reply.Term
				rf.state = FOLLOWER
				go rf.toFollower()
				rf.mu.Unlock()
				wg.Done()
				return
			}

			if reply.VoteGranted {
				rf.votesGot += 1

				if rf.isElected() { // convert to leader
					DPrintf("CANDIDATE: term %d raft %d state %d got %d votes, convert to LEADER", rf.currentTerm, rf.me, rf.state, rf.votesGot)
					rf.state = LEADER
					go rf.toLeader()
					rf.mu.Unlock()
					wg.Done()
					return
				}
			}
			rf.mu.Unlock()
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (rf *Raft) toCandidate() {
	wg := sync.WaitGroup{}

	for !rf.killed() && rf.isState(CANDIDATE) {
		rf.mu.Lock()
		rf.state = CANDIDATE
		rf.currentTerm += 1
		rf.mu.Unlock()

		wg.Add(1)
		go rf.startElection(&wg)
		wg.Wait()
	}
}

func (rf *Raft) replicate(newEntry LogEntry, aereplyCh chan AppendEntriesReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	lastLogIndex := len(rf.log) - 1

	for si := 0; rf.state == LEADER && si < rf.numPeers; si++ {

		if si == rf.me {
			continue
		}

		args := AppendEntriesArgs{}
		args.Term = rf.currentTerm
		args.LeaderId = rf.me
		args.Entries = make([]LogEntry, 0)
		args.LeaderCommit = rf.commitIndex
		args.PrevLogIndex = len(rf.log) - 1
		args.PrevLogTerm = rf.log[args.PrevLogIndex].Term

		if lastLogIndex >= rf.nextIndex[si] { // not a heartbeat
			DPrintf("LEADER term %d raft %d start to replicate log to %d from index %d to %d", rf.currentTerm, rf.me, si, rf.nextIndex[si], lastLogIndex)
			args.PrevLogIndex = rf.nextIndex[si] - 1
			args.PrevLogTerm = rf.log[args.PrevLogIndex].Term
			args.Entries = rf.log[rf.nextIndex[si]:]
		}

		go func(si int, args AppendEntriesArgs, ch chan AppendEntriesReply) {
			reply := AppendEntriesReply{}
			ok := rf.sendAppendEntries(si, &args, &reply)

			if ok {
				ch <- reply
			}
		}(si, args, aereplyCh)
	}
}

func (rf *Raft) commitAt(index int) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	if index <= rf.commitIndex {
		return
	}

	DPrintf("LEADER term %d raft %d need commit index %d(%v) commitIndex %d", rf.currentTerm, rf.me, index, rf.log[index].Command, rf.commitIndex)

	count := 1

	for si := 0; si < rf.numPeers; si++ {
		if si != rf.me {
			if rf.matchIndex[si] >= index {
				count++
			}
		}
	}

	if count > rf.numPeers/2 && rf.log[index].Term == rf.currentTerm {
		rf.commitIndex = index
		DPrintf("LEADER term %d raft %d commit log with cmd %v at index %d", rf.currentTerm, rf.me, rf.log[index].Command, rf.commitIndex)
	}

	for rf.lastApplied < rf.commitIndex {
		rf.lastApplied++
		logEntry := rf.log[rf.lastApplied]

		applyMsg := ApplyMsg{}
		applyMsg.Command = logEntry.Command
		applyMsg.CommandIndex = logEntry.Index
		applyMsg.CommandValid = true
		rf.applyCh <- applyMsg
	}
}

func (rf *Raft) handleReply(aereplyCh chan AppendEntriesReply) {
	for !rf.killed() {
		reply := <-aereplyCh

		rf.mu.Lock()
		if reply.Term > rf.currentTerm {
			rf.currentTerm = reply.Term
			rf.state = FOLLOWER
			DPrintf("LEADER term %d raft %d state %d got reply with larger term, convert to FOLLOWER", rf.currentTerm, rf.me, rf.state)
			go rf.toFollower()
			rf.mu.Unlock()
			return
		}
		rf.mu.Unlock()

		if reply.Success {
			if reply.IsHeartbeat {
				continue
			}

			rf.mu.Lock()
			if rf.matchIndex[reply.FollowerId] < reply.LastLogIndex {
				rf.matchIndex[reply.FollowerId] = reply.LastLogIndex
			}
			rf.nextIndex[reply.FollowerId] = reply.LastLogIndex + 1
			rf.mu.Unlock()

			go rf.commitAt(reply.LastLogIndex)
		} else {
			DPrintf("LEADER term %d raft %d Replication FAILED from raft %d: log inconsistency!", rf.currentTerm, rf.me, reply.FollowerId)
			rf.mu.Lock()
			rf.nextIndex[reply.FollowerId] -= 1
			rf.mu.Unlock()
		}
	}
}

func (rf *Raft) toLeader() {

	rf.nextIndex = make([]int, rf.numPeers)
	rf.matchIndex = make([]int, rf.numPeers)

	aereplyCh := make(chan AppendEntriesReply, rf.numPeers-1)

	for si := 0; si < rf.numPeers; si++ {
		if si != rf.me {
			rf.nextIndex[si] = len(rf.log)
			rf.matchIndex[si] = 0
		}
	}

	go rf.handleReply(aereplyCh)

	timeCh := time.After(0 * time.Millisecond) // upon election, immediately send heartbeat

	for !rf.killed() && rf.isState(LEADER) {

		select {
		case newEntry := <-rf.entryCh:
			go rf.replicate(newEntry, aereplyCh)

		case <-timeCh:
			// DPrintf("LEADER term %d raft %d state %d heartbeat", rf.currentTerm, rf.me, rf.state)
			go rf.replicate(LogEntry{Index: -1}, aereplyCh) // heartbeat
		}

		timeCh = time.After(time.Duration(rand.Intn(150)) * time.Millisecond)
		time.Sleep(10 * time.Millisecond)
	}
}

func Make(peers []*labrpc.ClientEnd, me int, applyCh chan ApplyMsg) *Raft {
	rf := &Raft{}
	rf.peers = peers
	rf.me = me
	rf.mu = sync.Mutex{}

	rf.log = make([]LogEntry, 0)
	rf.log = append(rf.log, LogEntry{Command: 0, Term: 0, Index: 0})

	rf.currentTerm = 0
	rf.commitIndex = 0
	rf.lastApplied = 0

	rf.numPeers = len(peers)
	rf.rpcCh = make(chan int, rf.numPeers)
	rf.entryCh = make(chan LogEntry, rf.numPeers)
	rf.applyCh = applyCh
	rf.state = FOLLOWER

	go rf.toFollower()

	return rf
}
