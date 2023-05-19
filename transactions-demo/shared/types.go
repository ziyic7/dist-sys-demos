package shared

import "log"

const (
	ABRT    = "ABORT"
	ABRT_OK = "ABORTED"
	BGN     = "BEGIN"
	CM      = "COMMIT"
	CM_OK   = "COMMIT OK"
	NF_ABRT = "NOT FOUND, ABORTED"
	OK      = "OK"
)

type Command struct {
	Tid     string
	Branch  string
	Account string
	Method  string
	Amount  int // only valid for DEPOSIT and WITHDRAW
}

/* RPC args and reply type */
type SendCoordArgs struct {
	Tid string // the unique transaction id for the client
	Cmd string // one line of the transaction
}

type SendCoordReply struct {
	//Ok bool // true if participant accepts this cmd
	ReplyType string //possible replies: OK,COMMIT OK, ABORTED, (NOT FOUND, ABORTED)
}

func CheckError(e error) {
	if e != nil {
		log.Fatal(e)
	}
}

const Debug = 0

func DPrintf(format string, a ...interface{}) (n int, err error) {
	if Debug > 0 {
		log.Printf(format, a...)
	}
	return
}
