package main

import (
	"shared"
	"strconv"
	"sync"
)

type Accounts map[string]int

type Lock struct {
	rwMu *sync.RWMutex
	tids map[string]bool // tid -> isRead
}

type Application struct {
	mu *sync.Mutex

	locks      map[string]Lock            // acctName -> Lock
	txLockInfo map[string]map[string]bool // tid -> {acctName -> isRead}
	//waitfor    map[string]map[string]bool  		//waitFor[tid] => acctName: exist?
	committedData Accounts
	cache         map[string]Accounts
}

/*
func (app *Application) deadLockCheck() {
for k,v := range(waitfor){
	if same acctName in waitFor[tid]
		ABORT
}
}
*/

func (app *Application) AcquireRWLock(tid string, acctName string, IsRead bool) {
	if _, ok := app.locks[acctName]; !ok { // no mutex object for this account name
		shared.DPrintf("Init a mutex object for account name " + acctName)
		app.locks[acctName] = Lock{rwMu: new(sync.RWMutex), tids: make(map[string]bool)}
	}
	// go deadLockCheck
	l := app.locks[acctName]
	//app.waitfor[tid][acctName] = true //deadlock detection
	if IsRead {
		if isRead, ok := l.tids[tid]; ok && !isRead { // has write lock, wants to read
			return
		}
		shared.DPrintf(tid + " Acquiring the read lock for account " + acctName)
		l.rwMu.RLock()
		// after getting the read lock
		l.tids[tid] = true
		app.txLockInfo[tid][acctName] = true
	} else {
		shared.DPrintf(tid + " Acquiring the write lock for account " + acctName)
		if isRead, ok := l.tids[tid]; ok { // this tid has acquired one lock
			shared.DPrintf(tid + " has a lock for account " + acctName)
			if isRead {
				shared.DPrintf(tid + " is upgrading the lock for account " + acctName)
				l.rwMu.RUnlock() // for lock upgrading
				delete(l.tids, tid)
				delete(app.txLockInfo[tid], acctName)
			} else { // now has a write lock
				shared.DPrintf(tid + " is using the same write lock for account " + acctName)
				return
			}
		}
		l.rwMu.Lock()
		l.tids[tid] = false
		app.txLockInfo[tid][acctName] = false
	}
}

func (app *Application) Balance(tid string, acctName string) string {

	app.AcquireRWLock(tid, acctName, true)
	shared.DPrintf("got the read lock for BALANCE")

	app.mu.Lock()
	balance, inCommit := app.committedData[acctName]
	app.mu.Unlock()

	changes, inCache := app.cache[tid][acctName]

	if !inCommit && !inCache {
		shared.DPrintf("BALANCE: account name " + acctName + " not found")
		return shared.NF_ABRT
	}

	if !inCache {
		shared.DPrintf("BALANCE: account name " + acctName + " first balance")
		changes = 0
	}

	tempBalance := balance + changes

	return acctName + " = " + strconv.Itoa(tempBalance)
}

func (app *Application) Deposit(tid string, acctName string, amount int) string {

	app.AcquireRWLock(tid, acctName, false)
	shared.DPrintf("got the write lock for DEPOSIT")

	change, ok := app.cache[tid][acctName]

	if !ok {
		app.cache[tid][acctName] = amount
	} else {
		app.cache[tid][acctName] = change + amount
	}

	return shared.OK
}

func (app *Application) Withdraw(tid string, acctName string, amount int) string {

	app.AcquireRWLock(tid, acctName, false)
	shared.DPrintf("got the write lock for WITHDRAW")

	app.mu.Lock()
	_, inCommit := app.committedData[acctName]
	app.mu.Unlock()

	change, inCache := app.cache[tid][acctName]

	if !inCommit && !inCache {
		shared.DPrintf("WITHDRAW: account name " + acctName + " not found")
		return shared.NF_ABRT
	}

	if !inCache {
		shared.DPrintf("WITHDRAW: account name " + acctName + " first withdraw")
		change = 0
	}

	app.cache[tid][acctName] = change - amount

	return shared.OK
}

func (app *Application) releaseLocks(tid string) {

	lockInfo, ok := app.txLockInfo[tid]

	if !ok {
		shared.DPrintf("Fatal: no lock info for tid: " + tid + " before releasing the locks")
	}

	for name, isRead := range lockInfo {
		if isRead {
			shared.DPrintf("release read lock for account " + name)
			app.locks[name].rwMu.RUnlock()
		} else {
			shared.DPrintf("release write lock for account " + name)
			app.locks[name].rwMu.Unlock()
		}
		delete(app.locks[name].tids, tid)
	}
	delete(app.txLockInfo, tid)
}
