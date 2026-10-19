package script

import "context"

type runtimeLease struct{}
type lease struct {
	vm     *VM
	parent *lease
}

func leaseFrom(ctx context.Context) *lease {
	l, _ := ctx.Value(runtimeLease{}).(*lease)
	return l
}

// acquire reports whether this call took the runtime. A call chain that already
// owns it re-enters without replacing that owner's cancellation.
func (v *VM) acquire(ctx context.Context) (release func(), owned bool, err error) {
	if err = ctx.Err(); err != nil {
		return nil, false, err
	}
	for l := leaseFrom(ctx); l != nil; l = l.parent {
		if l.vm == v {
			return func() {}, false, nil
		}
	}
	select {
	case v.gate <- struct{}{}:
		return func() { <-v.gate }, true, nil
	case <-ctx.Done():
		return nil, false, ctx.Err()
	}
}

func (v *VM) changedLocked() {
	close(v.note)
	v.note = make(chan struct{})
}

// Park lets other retained calls run on this goroutine while it is blocked in
// listen or pause. Sobek cannot execute on two goroutines at once.
func (v *VM) Park(step func(wake <-chan struct{}) (bool, error)) error {
	wake := make(chan struct{}, 1)
	v.sched.Lock()
	v.parked = true
	v.wake = wake
	v.changedLocked()
	v.sched.Unlock()
	defer func() {
		v.sched.Lock()
		v.parked = false
		v.wake = nil
		v.changedLocked()
		v.sched.Unlock()
	}()
	for {
		v.runJobs()
		again, err := step(wake)
		if !again {
			v.runJobs()
			return err
		}
	}
}

func (v *VM) runJobs() {
	for {
		v.sched.Lock()
		if len(v.jobs) == 0 {
			v.sched.Unlock()
			return
		}
		job := v.jobs[0]
		v.jobs[0] = nil
		v.jobs = v.jobs[1:]
		v.sched.Unlock()
		job()
	}
}

// handoff runs job on the goroutine parked inside this runtime.
// Otherwise it returns the lease so the caller can run the job itself.
func (v *VM) handoff(ctx context.Context, job func()) (func(), bool, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		v.sched.Lock()
		note := v.note
		if v.parked && v.wake != nil {
			done := make(chan struct{})
			v.jobs = append(v.jobs, func() {
				defer close(done)
				job()
			})
			wake := v.wake
			v.sched.Unlock()
			select {
			case wake <- struct{}{}:
			default:
			}
			<-done
			return nil, true, nil
		}
		v.sched.Unlock()
		select {
		case v.gate <- struct{}{}:
			return func() { <-v.gate }, false, nil
		case <-note:
		case <-ctx.Done():
			return nil, false, ctx.Err()
		}
	}
}
