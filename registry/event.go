package registry

import (
	"fmt"
	"path"
	"reflect"
	"strings"
	"time"

	"github.com/coreos/go-etcd/etcd"
	log "github.com/golang/glog"

	"github.com/coreos/coreinit/machine"
)

type Event struct {
	Type    string
	Payload interface{}
	Context *machine.Machine
}

type EventStream struct {
	etcd      *etcd.Client
	reg       *Registry
	listeners []EventListener
	chClose   chan bool
}

type EventListener struct {
	Handler interface{}
	Context *machine.Machine
}

func (self *EventListener) String() string {
	if self.Context != nil {
		return self.Context.BootId
	} else {
		return "N/A"
	}
}

func NewEventStream(reg *Registry) *EventStream {
	client := etcd.NewClient(nil)
	client.SetConsistency(etcd.WEAK_CONSISTENCY)
	listeners := make([]EventListener, 0)

	return &EventStream{client, reg, listeners, make(chan bool)}
}

func (self *EventStream) RegisterListener(l interface{}, m *machine.Machine) {
	self.listeners = append(self.listeners, EventListener{l, m})
}

// Distribute an event to all listeners registered to event.Type
func (self *EventStream) send(event Event) {
	handlerFuncName := fmt.Sprintf("Handle%s", event.Type)
	for _, listener := range self.listeners {
		if event.Context == nil || event.Context.BootId == listener.Context.BootId {
			handlerFunc := reflect.ValueOf(listener.Handler).MethodByName(handlerFuncName)
			if handlerFunc.IsValid() {
				log.V(2).Infof("Calling event handler for %s on listener %s", event.Type, listener.String())
				go handlerFunc.Call([]reflect.Value{reflect.ValueOf(event)})
			}
		}
	}
}

func (self *EventStream) eventLoop(event chan Event, stop chan bool) {
	for {
		select {
		case <-stop:
			return
		case e := <-event:
			log.V(2).Infof("Sending %s to listeners", e.Type)
			self.send(e)
		}
	}
}

func (self *EventStream) Open() {
	eventchan := make(chan Event)

	watchMap := map[string][]func(*etcd.Response) *Event{
		path.Join(keyPrefix, statePrefix):   []func(*etcd.Response) *Event{filterEventJobStatePublished, filterEventJobStateExpired},
		path.Join(keyPrefix, jobPrefix):     []func(*etcd.Response) *Event{filterEventJobCreated, filterEventJobScheduled, filterEventJobCancelled},
		path.Join(keyPrefix, machinePrefix): []func(*etcd.Response) *Event{self.filterEventMachineUpdated, self.filterEventMachineRemoved},
		path.Join(keyPrefix, requestPrefix): []func(*etcd.Response) *Event{filterEventRequestCreated},
		path.Join(keyPrefix, offerPrefix):   []func(*etcd.Response) *Event{filterEventJobOffered, filterEventJobBidSubmitted},
	}

	for key, funcs := range watchMap {
		for _, f := range funcs {
			etcdchan := make(chan *etcd.Response)
			go self.watch(etcdchan, key)
			go pipe(etcdchan, f, eventchan)
		}
	}

	go self.eventLoop(eventchan, self.chClose)
}

func (self *EventStream) Close() {
	self.chClose <- true
}

func pipe(etcdchan chan *etcd.Response, translate func(resp *etcd.Response) *Event, eventchan chan Event) {
	for true {
		resp := <-etcdchan
		log.V(2).Infof("Received response from etcd watcher: Action=%s ModifiedIndex=%d Key=%s", resp.Action, resp.Node.ModifiedIndex, resp.Node.Key)
		event := translate(resp)
		if event != nil {
			log.V(2).Infof("Translated response(ModifiedIndex=%d) to event(Type=%s)", resp.Node.ModifiedIndex, event.Type)
			eventchan <- *event
		} else {
			log.V(2).Infof("Discarding response(ModifiedIndex=%d) from etcd watcher", resp.Node.ModifiedIndex)
		}
	}
}

func (self *EventStream) watch(etcdchan chan *etcd.Response, key string) {
	for true {
		log.V(2).Infof("Creating etcd watcher: key=%s, machines=%s", key, strings.Join(self.etcd.GetCluster(), ","))
		_, err := self.etcd.Watch(key, 0, true, etcdchan, nil)

		var errString string
		if err == nil {
			errString = "N/A"
		} else {
			errString = err.Error()
		}

		log.V(2).Infof("etcd watch exited: key=%s, err=\"%s\"", key, errString)

		time.Sleep(time.Second)
	}
}
