package types

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"
)

type Account struct {
	balance int
}

type Application struct { //ledger
	mu       sync.Mutex
	Accts    map[string]Account // {acctName : balances}
	writer   *bufio.Writer
	writer_g *bufio.Writer
	writer_p *bufio.Writer
}

func (app *Application) Init(nid string, nNodes int) {
	app.Accts = make(map[string]Account)
	f, _ := os.Create("test/data/" + strconv.Itoa(nNodes) + "nodes/" + nid + ".txt")
	f_g, _ := os.Create("test/data/graph/gen/" + nid + "_gen" + ".txt")
	f_p, _ := os.Create("test/data/graph/pro/" + nid + "_pro" + ".txt")
	//defer f.Close()
	app.writer = bufio.NewWriter(f)
	app.writer_g = bufio.NewWriter(f_g)
	app.writer_p = bufio.NewWriter(f_p)
}

func (app *Application) print() {
	fmt.Print("BALANCES ")
	app.writer.WriteString("BALANCES ")
	accts := [26]string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m", "n", "o", "p", "q", "r", "s", "t", "u", "v", "w", "x", "y", "z"}
	for i := 0; i < len(accts); i++ {
		curA := app.Accts[accts[i]]
		if curA.balance > 0 { // omit zero bal
			fmt.Print(accts[i] + ":" + strconv.Itoa(curA.balance) + " ") // name1:balance1 name2:balance2 ...
			data := accts[i] + ":" + strconv.Itoa(curA.balance) + " "
			app.writer.WriteString(data)
		}
	}
	// for k, v := range app.Accts {
	// 	if v.balance > 0 { // omit zero bal
	// 		fmt.Print(k + ":" + strconv.Itoa(v.balance) + " ") // name1:balance1 name2:balance2 ...
	// 	}
	// }
	fmt.Print("\n")
	app.writer.WriteString("\n")
	app.writer.Flush()
}

func (app *Application) Deposit(name string, amount int) {
	app.mu.Lock()
	defer app.mu.Unlock()
	defer app.print()

	acct, ok := app.Accts[name]

	if !ok {
		newAcct := Account{balance: amount}
		app.Accts[name] = newAcct
	} else {
		acct.balance += amount
		app.Accts[name] = acct
	}
}

func (app *Application) Transfer(src string, dest string, amount int) {
	app.mu.Lock()
	defer app.mu.Unlock()
	defer app.print()

	srcAcct, ok := app.Accts[src] // passing value

	/* cases to reject the transaction */

	// no src account
	if !ok {
		//err := fmt.Errorf("source account %s doesn't exist", src)
		//Error.Println(err)
		return
	}

	// no enough money in src account
	if srcAcct.balance < amount {
		//err := fmt.Errorf("source account %s doesn't have enough money to transfer %d to account %s", src, amount, dest)
		//Error.Println(err)
		return
	}

	/* withdraw money from src account */
	srcAcct.balance -= amount
	app.Accts[src] = srcAcct

	destAcct, ok := app.Accts[dest]

	/* Transfer money to dest account */

	// dest account not exist
	if !ok {
		newAcct := Account{balance: amount}
		app.Accts[dest] = newAcct
		return
	}

	destAcct.balance += amount
	app.Accts[dest] = destAcct
}

func (app *Application) ListenFrom(listenChan chan Message) { // review channel

	for {
		msg := <-listenChan
		var data string
		//processed time
		t_p := (float64(time.Now().UnixNano()) / 1e9)
		app.writer_p.WriteString(fmt.Sprintf("%f", t_p) + "\n")
		app.writer_p.Flush()
		if msg.IsDeposit {
			fmt.Println("DEPOSIT " + msg.Names[0] + " " + strconv.Itoa(msg.Amount))
			data = "Application calls: DEPOSIT " + msg.Names[0] + " " + strconv.Itoa(msg.Amount) + "\n"
			app.writer.WriteString(data)
			app.writer.Flush()
			app.Deposit(msg.Names[0], msg.Amount)

		} else {
			fmt.Println("TRANSFER " + msg.Names[0] + " -> " + msg.Names[1] + " " + strconv.Itoa(msg.Amount))
			data = "Application calls: TRANSFER " + msg.Names[0] + " " + msg.Names[1] + " " + strconv.Itoa(msg.Amount) + "\n"
			app.writer.WriteString(data)
			app.writer.Flush()
			app.Transfer(msg.Names[0], msg.Names[1], msg.Amount)

		}
	}
}
