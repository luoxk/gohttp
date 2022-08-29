package gohttp

import (
	"errors"
	"log"
	"sync"
)

type Task struct {
	Invoke    func(in interface{}, out chan interface{}) error
	ThreadNum int
}

type (
	SuperPipe struct {
		mtx       sync.Mutex
		building  bool
		tasks     []*Task
		worksChan []chan bool
		prevPipe  *pipeThread
		taskCache chan interface{}
		Owner     *BasicHttpClient
	}

	pipeThread struct {
		signals   []chan bool
		stopChan  chan bool
		statusMgr SyncFlag
		interrupt func()
		lastErr   error
	}
)

func newPipeThread(l int) *pipeThread {
	p := &pipeThread{
		signals:  make([]chan bool, l),
		stopChan: make(chan bool),
	}
	p.interrupt, p.statusMgr = NewSyncFlag()

	for i := range p.signals {
		p.signals[i] = make(chan bool)
	}
	return p
}

func NewSuperPipe(owner *BasicHttpClient) *SuperPipe {
	return &SuperPipe{
		building:  true,
		tasks:     make([]*Task, 0),
		taskCache: make(chan interface{}, 1024),
		Owner:     owner,
	}
}

func (sp *SuperPipe) PushTask(task *Task) (*SuperPipe, error) {
	if !sp.building {
		log.Println("only building pipeline can push task")
		return nil, errors.New("pipe is running")
	}
	sp.tasks = append(sp.tasks, task)
	return sp, nil
}

func (sp *SuperPipe) Build() (*SuperPipe, error) {
	if !sp.building {
		log.Println("only building pipeline can execute")
		return nil, errors.New("pipe is running")
	}
	if len(sp.tasks) <= 0 {
		return nil, errors.New("at least 1 method can be executed")
	}
	workChan := make([]chan bool, len(sp.tasks))
	for i := range workChan {
		workChan[i] = make(chan bool, sp.tasks[i].ThreadNum)
	}
	sp.worksChan = workChan
	sp.prevPipe = newPipeThread(len(sp.tasks))
	for _, value := range sp.prevPipe.signals {
		close(value)
	}
	close(sp.prevPipe.stopChan)
	sp.building = false
	go sp.loop()
	return sp, nil
}

func (sp *SuperPipe) Push(in interface{}) {
	sp.taskCache <- in
}

func (sp *SuperPipe) loop() {
	for {
		in := <-sp.taskCache
		sp.mtx.Lock()
		if sp.prevPipe.statusMgr.Done() {
			sp.mtx.Unlock()
			return
		}
		prev := sp.prevPipe
		this := newPipeThread(len(sp.worksChan))
		sp.prevPipe = this
		sp.mtx.Unlock()

		lock := func(idx int) bool {
			select {
			case <-prev.statusMgr.Chan():
				return false
			case <-prev.signals[idx]: //wait for signal
			}
			select {
			case <-prev.statusMgr.Chan():
				return false
			case sp.worksChan[idx] <- true: //get lock
			}
			return true
		}

		if !lock(0) {
			this.interrupt()
			<-this.stopChan
			this.lastErr = prev.lastErr
			close(this.stopChan)
			return
		}

		go func() {
			select {
			case <-prev.statusMgr.Chan():
				this.interrupt()
			case <-this.stopChan:
			}
		}()

		go func() {
			var err error
			for i, work := range sp.tasks {
				var o = make(chan interface{}, 1000)
				close(this.signals[i]) //signal next thread
				if work != nil {
					err = sp.tasks[i].Invoke(in, o)
				}
				if err != nil || (i+1 < len(sp.tasks) && !lock(i+1)) {
					this.interrupt()
					break
				}
				<-sp.worksChan[i] //release lock
			}

			<-prev.stopChan
			if prev.statusMgr.Done() {
				this.interrupt()
			}
			if prev.lastErr != nil {
				this.lastErr = prev.lastErr
			} else {
				this.lastErr = err
			}
			close(this.stopChan)
		}()
	}
}

func (sp *SuperPipe) Wait() error {
	sp.mtx.Lock()
	lastThd := sp.prevPipe
	sp.mtx.Unlock()
	<-lastThd.stopChan
	return lastThd.lastErr
}
