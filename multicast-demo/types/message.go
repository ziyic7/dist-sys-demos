package types

type Message struct {
	Id            string // uuid: https://pkg.go.dev/github.com/google/UUID
	Sender        int    // int for pq reordering, e.g. sender is node3, then this field should be 3
	Priority      int    // priority
	Index         int    // for update in pq
	NodeId        string
	RNodeId       string
	Status        string // "i": initial multicasting, "p"; proposed priority, "a": agreed priority
	IsDeliverable bool
	IsDeposit     bool
	Names         []string
	Amount        int
}
