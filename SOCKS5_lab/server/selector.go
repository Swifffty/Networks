package server

import (
	"container/heap"
	"time"

	"golang.org/x/sys/unix"
)

const (
	evIn  = unix.EPOLLIN
	evOut = unix.EPOLLOUT
	evErr = unix.EPOLLERR
	evHup = unix.EPOLLHUP
)

type Handler interface {
	HandleEvent(fd int, events uint32)
}

type Selector struct {
	epfd     int
	handlers map[int]Handler
	timers   timerHeap
	events   []unix.EpollEvent
}

func NewSelector() (*Selector, error) {
	epfd, err := unix.EpollCreate1(unix.EPOLL_CLOEXEC)
	if err != nil {
		return nil, err
	}
	return &Selector{
		epfd:     epfd,
		handlers: make(map[int]Handler),
		events:   make([]unix.EpollEvent, 256),
	}, nil
}

func (s *Selector) Add(fd int, events uint32, h Handler) error {
	ev := unix.EpollEvent{Events: events, Fd: int32(fd)}
	if err := unix.EpollCtl(s.epfd, unix.EPOLL_CTL_ADD, fd, &ev); err != nil {
		return err
	}
	s.handlers[fd] = h
	return nil
}

func (s *Selector) Modify(fd int, events uint32) error {
	ev := unix.EpollEvent{Events: events, Fd: int32(fd)}
	return unix.EpollCtl(s.epfd, unix.EPOLL_CTL_MOD, fd, &ev)
}

func (s *Selector) Remove(fd int) {
	_ = unix.EpollCtl(s.epfd, unix.EPOLL_CTL_DEL, fd, nil)
	delete(s.handlers, fd)
}

func (s *Selector) After(d time.Duration, fn func()) *Timer {
	t := &Timer{when: time.Now().Add(d), fn: fn}
	heap.Push(&s.timers, t)
	return t
}

func (s *Selector) Cancel(t *Timer) {
	if t != nil && t.idx >= 0 {
		heap.Remove(&s.timers, t.idx)
	}
}

func (s *Selector) Run() error {
	for {
		n, err := unix.EpollWait(s.epfd, s.events, s.timeoutMs())
		if err != nil {
			if err == unix.EINTR {
				continue
			}
			return err
		}
		for i := 0; i < n; i++ {
			fd := int(s.events[i].Fd)
			if h, ok := s.handlers[fd]; ok {
				h.HandleEvent(fd, s.events[i].Events)
			}
		}
		s.runTimers()
	}
}

func (s *Selector) timeoutMs() int {
	if len(s.timers) == 0 {
		return -1
	}
	d := time.Until(s.timers[0].when)
	if d <= 0 {
		return 0
	}
	return int((d + time.Millisecond - 1) / time.Millisecond)
}

func (s *Selector) runTimers() {
	now := time.Now()
	for len(s.timers) > 0 && !s.timers[0].when.After(now) {
		t := heap.Pop(&s.timers).(*Timer)
		t.fn()
	}
}

type Timer struct {
	when time.Time
	fn   func()
	idx  int
}

type timerHeap []*Timer

func (h timerHeap) Len() int {
	return len(h)
}

func (h timerHeap) Less(i, j int) bool {
	return h[i].when.Before(h[j].when)
}

func (h timerHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].idx = i
	h[j].idx = j
}

func (h *timerHeap) Push(x any) {
	t := x.(*Timer)
	t.idx = len(*h)
	*h = append(*h, t)
}

func (h *timerHeap) Pop() any {
	old := *h
	n := len(old)
	t := old[n-1]
	old[n-1] = nil
	*h = old[:n-1]
	t.idx = -1
	return t
}
