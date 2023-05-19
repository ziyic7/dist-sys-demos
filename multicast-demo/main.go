package main

import (
	"fmt"
	"mp1_node/types"
	"mp1_node/utils"
	"os"
)

func main() {

	args := os.Args[1:] // [identifier, fp]

	if len(args) != 2 {
		err := fmt.Errorf("usage: ./main [identifier] [configuration file]")
		utils.ExitFromErr(err)
	}

	nodeId, fp := args[0], args[1]

	node := new(types.Node)

	node.Run(nodeId, fp)
}
