package registry

import (
	"path"
	"time"

	"github.com/coreos/go-etcd/etcd"

	"github.com/coreos/coreinit/machine"
)

const (
	machinePrefix  = "/machines/"
)

// Describe all active Machines
func (r *Registry) GetActiveMachines() []machine.Machine {
	key := path.Join(keyPrefix, machinePrefix)
	resp, err := r.etcd.Get(key, false, true)

	var machines []machine.Machine

	// Assume the error was KeyNotFound and return an empty data structure
	if err != nil {
		return machines
	}

	for _, kv := range resp.Node.Nodes {
		_, bootId := path.Split(kv.Key)
		mach := r.GetMachineState(bootId)
		if mach != nil {
			machines = append(machines, *mach)
		}
	}

	return machines
}

// Get Machine object from etcd
func (r *Registry) GetMachineState(bootid string) *machine.Machine {
	key := path.Join(keyPrefix, machinePrefix, bootid, "object")
	resp, err := r.etcd.Get(key, false, true)

	// Assume the error was KeyNotFound and return an empty data structure
	if err != nil {
		return nil
	}

	var mach machine.Machine
	if err := unmarshal(resp.Node.Value, &mach); err != nil {
		return nil
	}

	return &mach
}

// Push Machine object to etcd
func (r *Registry) SetMachineState(machine *machine.Machine, ttl time.Duration) {
	//TODO: Handle the error generated by marshal
	json, _ := marshal(machine)
	key := path.Join(keyPrefix, machinePrefix, machine.BootId, "object")
	r.etcd.Set(key, json, uint64(ttl.Seconds()))
}

func (self *EventStream) filterEventMachineUpdated(resp *etcd.Response) *Event {
	if base := path.Base(resp.Node.Key); base != "object" {
		return nil
	}

	if resp.Action != "set" {
		return nil
	}

	var m machine.Machine
	unmarshal(resp.Node.Value, &m)
	return &Event{"EventMachineUpdated", m, nil}
}

func (self *EventStream) filterEventMachineRemoved(resp *etcd.Response) *Event {
	if base := path.Base(resp.Node.Key); base != "object" {
		return nil
	}

	if resp.Action != "expire" && resp.Action != "delete" {
		return nil
	}

	machName := path.Base(path.Dir(resp.Node.Key))
	return &Event{"EventMachineRemoved", machName, nil}
}
