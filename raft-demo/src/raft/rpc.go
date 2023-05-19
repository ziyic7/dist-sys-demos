package raft

import "math"

type RequestVoteArgs struct {
	// Your data here (2A, 2B).
	Term         int // candidate's term
	CandidateId  int // candidate requesting vote
	LastLogIndex int // index of candidate's last log entry
	LastLogTerm  int // term of candidate's last log entry
}

type RequestVoteReply struct {
	// Your data here (2A).
	Term        int  // current term, for candidate to update itself
	VoteGranted bool // true means candidate received vote
}

type AppendEntriesArgs struct {
	Term         int        // leader's term
	LeaderId     int        // so follower can redirect clients
	PrevLogIndex int        // index of log entry immediately preceding new ones
	PrevLogTerm  int        // term of prevLogIndex entry
	Entries      []LogEntry // log entries to store (empty for heartbeat; may send more than one for efficiency)
	LeaderCommit int        // leader's commitIndex
}

type AppendEntriesReply struct {
	FollowerId   int
	Term         int // current term, for leader to update itself
	LastLogIndex int
	IsHeartbeat  bool
	Success      bool // true if follower contained entry matching prevLogIndex and prevLogTerm
}

func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	if rf.state == FOLLOWER { // notifying the goroutine that runs the clock
		rf.rpcCh <- 1
	}

	voteGranted := false

	if args.Term >= rf.currentTerm {
		if args.Term > rf.currentTerm {
			rf.currentTerm = args.Term

			if rf.state != FOLLOWER { // split vote or leader failure
				rf.state = FOLLOWER
				go rf.toFollower()
			}
		}

		if (rf.votedFor == -1 || rf.votedFor == args.CandidateId) && rf.isCandUpToDate(args) {
			voteGranted = true
			rf.votedFor = args.CandidateId
		}
	}

	reply.Term = rf.currentTerm
	reply.VoteGranted = voteGranted
}

func (rf *Raft) sendRequestVote(server int, args *RequestVoteArgs, reply *RequestVoteReply) bool { // TODO: handle sender get response with larger term
	ok := rf.peers[server].Call("Raft.RequestVote", args, reply)
	return ok
}

func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	if rf.state == FOLLOWER {
		rf.rpcCh <- 2
	}

	if len(args.Entries) == 0 {
		reply.IsHeartbeat = true
	} else {
		reply.IsHeartbeat = false
	}

	reply.Term = rf.currentTerm
	reply.FollowerId = rf.me

	if args.Term < rf.currentTerm {
		reply.Success = false
		return
	}

	if args.Term > rf.currentTerm || rf.state == CANDIDATE {
		rf.currentTerm = args.Term

		if rf.state != FOLLOWER {
			rf.state = FOLLOWER
			go rf.toFollower()
		}
	}

	reply.Term = rf.currentTerm

	if args.PrevLogTerm != -1 { // leader has record that this si has no log entries
		// DPrintf("%d %d %d", args.PrevLogIndex, rf.log[args.PrevLogIndex].Term, args.PrevLogTerm)
		if len(rf.log)-1 < args.PrevLogIndex || rf.log[args.PrevLogIndex].Term != args.PrevLogTerm {
			reply.Success = false
			return
		}
	}

	reply.Success = true

	var inLog map[int]bool = make(map[int]bool)

	if len(rf.log) > 0 {
		for i := 0; i < len(args.Entries); i++ {
			index := args.Entries[i].Index

			if index < len(rf.log) {
				if rf.log[index].Term != args.Entries[i].Term {
					rf.log = rf.log[:index]
					break
				} else {
					inLog[i] = true
				}
			}
		}
	}

	needAppend := false

	for i := 0; i < len(args.Entries); i++ {
		if _, ok := inLog[i]; !ok {
			// DPrintf("not ok")
			needAppend = true
		}
		// DPrintf("ok")

		if needAppend {
			// DPrintf("term %d raft %d append", rf.currentTerm, rf.me)
			rf.log = append(rf.log, args.Entries[i])
		}
	}

	// DPrintf("entries len: %d, leadercommit %d, commitindex %d", len(args.Entries), args.LeaderCommit, rf.commitIndex)
	if args.LeaderCommit > rf.commitIndex {

		oldCommitIndex := rf.commitIndex
		rf.commitIndex = int(math.Min(float64(args.LeaderCommit), float64(rf.log[len(rf.log)-1].Index)))

		for oldCommitIndex < rf.commitIndex {
			oldCommitIndex++

			applyMsg := ApplyMsg{}
			applyMsg.Command = rf.log[oldCommitIndex].Command
			applyMsg.CommandIndex = rf.log[oldCommitIndex].Index
			applyMsg.CommandValid = true
			rf.applyCh <- applyMsg
		}
		DPrintf("term %d raft %d state %d update commitindex to %d", rf.currentTerm, rf.me, rf.state, rf.commitIndex)
	}

	reply.LastLogIndex = len(rf.log) - 1
}

func (rf *Raft) sendAppendEntries(server int, args *AppendEntriesArgs, reply *AppendEntriesReply) bool {
	ok := rf.peers[server].Call("Raft.AppendEntries", args, reply)
	return ok
}
